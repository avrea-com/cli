"""Shared helper functions for CLI command implementations."""

from avrea_cli.api_client import ApiClient
from avrea_cli.config import CliConfig
from datetime import UTC
from datetime import datetime
from datetime import timedelta
from typing import NoReturn
import click
import httpx
import os
import sys

# Exit codes:
#   0  ok
#   1  general error (HTTP failure, validation, etc.)
#   2  usage error (handled by Click for bad flags)
#   4  auth required — script can detect "log in" cleanly
EXIT_AUTH_REQUIRED = 4

_AUTH_HINT = (
    "To get started with Avrea CLI, please run:  avr auth login\n"
    "Alternatively, set the AVR_TOKEN environment variable to an Avrea API token."
)


def exit_with_auth_hint() -> NoReturn:
    """Print the auth suggestion and exit with code 4. Used for both no-token
    (boundary check) and 401 (server rejected our token).

    Exit 4 lets scripts distinguish auth from generic failure: `if [ $? -eq 4 ];
    then avr auth login; fi`."""
    click.echo(_AUTH_HINT, err=True)
    sys.exit(EXIT_AUTH_REQUIRED)


def ensure_prompts_allowed(action: str = "this command needs confirmation") -> None:
    """Refuse interactive prompts when AVR_PROMPT_DISABLED is set.

    Lets scripts fail fast instead of hanging on stdin. Pass --yes (or the
    equivalent confirmation-bypass flag) when automating destructive
    operations."""
    if os.environ.get("AVR_PROMPT_DISABLED"):
        click.echo(
            f"Error: {action}, but AVR_PROMPT_DISABLED is set. Pass the bypass flag (e.g. --yes) instead.",
            err=True,
        )
        raise click.Abort()


def ensure_ctx(ctx):
    """Ensure click context has an object dict initialized."""
    ctx.ensure_object(dict)


def ensure_authenticated(config: CliConfig) -> None:
    """Abort if the user is not authenticated."""
    if not config.auth_token:
        exit_with_auth_hint()


def get_org_id(config: CliConfig, org_option: str | None, *, client: ApiClient | None = None) -> str:
    """Resolve the organization to operate on.

    Accepts either an Avrea org ID (``org-...``) or a slug; a slug is translated
    to its ID via the user's membership list (needs ``client``). Falls back to
    the stored default, then auto-selects when the user belongs to exactly one
    org."""
    org_id = org_option or config.default_org
    if org_id:
        # ``org-`` is the opaque-ID prefix (mirrors ``rep-`` for repos); treat
        # anything else as a slug to resolve. Skip the round-trip for IDs and
        # when there's no client to resolve a slug against.
        if client is not None and not org_id.startswith("org-"):
            return _resolve_org_slug(client, org_id)
        return org_id

    # Try auto-selecting if user belongs to exactly one org
    if client is not None:
        orgs = _fetch_user_orgs(client)
        if len(orgs) == 1:
            auto_id = orgs[0]["organization_id"]
            click.echo(f"(using org: {orgs[0].get('slug', auto_id)})", err=True)
            return auto_id
        if orgs:
            click.echo("Error: No organization specified. Available orgs:", err=True)
            _print_available_orgs(orgs)
            click.echo("\nUse --org <slug|id> or set a default with: avr config set org <slug|id>", err=True)
            raise click.Abort()
        # Zero orgs
        click.echo("Error: No organizations found for your account.", err=True)
        raise click.Abort()

    click.echo("Error: No organization specified.", err=True)
    click.echo("Use --org <slug|id> or set a default with: avr config set org <slug|id>", err=True)
    raise click.Abort()


def match_org(orgs: list[dict], value: str) -> dict | None:
    """Find the org in ``orgs`` whose ID or slug matches ``value``.

    Tries exact org-ID, then exact slug, then case-folded slug (slugs are
    lowercase by convention, but accept ``Acme`` for ``acme``). Returns the
    matching org dict, or ``None`` when nothing matches."""
    for org in orgs:
        if org.get("organization_id") == value:
            return org
    for org in orgs:
        if org.get("slug") == value:
            return org
    folded = value.casefold()
    for org in orgs:
        slug = org.get("slug")
        if slug and slug.casefold() == folded:
            return org
    return None


def _resolve_org_slug(client: ApiClient, value: str) -> str:
    """Translate an org slug to its ``org-...`` ID via the user's membership
    list. Aborts with the available orgs if nothing matches."""
    orgs = _fetch_user_orgs(client)
    org = match_org(orgs, value)
    if org is not None:
        return org["organization_id"]
    click.echo(f"Error: No organization matching '{value}'.", err=True)
    if orgs:
        _print_available_orgs(orgs)
    raise click.Abort()


def _fetch_user_orgs(client: ApiClient) -> list[dict]:
    """Fetch the orgs the user belongs to. Aborts on HTTP failure."""
    try:
        response = client.public_get("/users/me/organizations")
    except httpx.HTTPStatusError as exc:
        click.echo(f"Error: Failed to resolve organization: {exc.response.status_code}", err=True)
        raise click.Abort() from exc
    return response.get("data", [])


def _print_available_orgs(orgs: list[dict]) -> None:
    """Render the orgs the user can pick from, one per line, to stderr."""
    for org in orgs:
        click.echo(f"  - {org.get('slug', org['organization_id'])} ({org['organization_id']})", err=True)


def get_org_slug(client: ApiClient, org_id: str) -> str:
    """Resolve org ID to slug for console URLs. Falls back to org_id.

    Best-effort: any failure (transport, malformed response, missing fields)
    returns the raw org_id. Console URLs still resolve with a UUID — slug is
    purely cosmetic, so we don't propagate errors up into the calling
    command."""
    try:
        response = client.public_get("/users/me/organizations")
    except httpx.HTTPStatusError, httpx.TransportError:
        return org_id
    data = response.get("data") if isinstance(response, dict) else None
    if not isinstance(data, list):
        return org_id
    for org in data:
        if not isinstance(org, dict):
            continue
        if org.get("organization_id") == org_id:
            return org.get("slug") or org_id
    return org_id


# Opaque base64 cursors are short — Avrea's cursors are well under 200 chars.
# 512 leaves headroom for protocol changes while still rejecting paste-mistakes
# (e.g. a whole file pasted into --cursor) before they hit the server.
_MAX_CURSOR_LEN = 512


def validate_cursor(cursor: str | None) -> str | None:
    """Bound a user-supplied pagination cursor at the CLI boundary.

    Cursors are opaque base64 from a previous response, so the only useful
    local validation is "obviously not a cursor" — empty, absurdly long, or
    surrounded by whitespace (a paste-mistake; base64 doesn't contain
    whitespace). On failure raises ``click.BadParameter`` so the user gets a
    clear local error instead of a server 400. ``None`` (no cursor) passes
    through."""
    if cursor is None:
        return None
    if not cursor or cursor.strip() != cursor or len(cursor) > _MAX_CURSOR_LEN:
        raise click.BadParameter(
            f"cursor must be 1..{_MAX_CURSOR_LEN} chars and not surrounded by whitespace (got {len(cursor)} chars)",
            param_hint="--cursor",
        )
    return cursor


def parse_since(since: str) -> datetime:
    """Parse a relative time string like '30d', '7d', '24h', '30m' into an
    absolute cutoff. Used by `--since` flags as sugar for `--created-after`.

    Only non-negative integers are accepted: ``-7d`` or ``1.5d`` raise. A
    negative window would silently produce a future cutoff and zero results,
    which looks like a successful empty query."""
    suffix_map = {"d": "days", "h": "hours", "m": "minutes"}
    if not since or since[-1] not in suffix_map or not since[:-1].isdigit():
        raise click.ClickException(f"Invalid --since value: {since} (use e.g. 30d, 7d, 24h, 30m)")
    return datetime.now(UTC) - timedelta(**{suffix_map[since[-1]]: int(since[:-1])})


def format_size(size_bytes: int) -> str:
    """Format byte count as human-readable string."""
    value = float(size_bytes)
    for unit in ("B", "KB", "MB", "GB", "TB"):
        if abs(value) < 1024:
            return f"{value:.1f} {unit}"
        value /= 1024
    return f"{value:.1f} PB"


def handle_http_error(exc: httpx.HTTPStatusError, action: str, *, hint: str | None = None) -> NoReturn:
    """Format and display an HTTP error, then exit non-zero.

    Status-specific framing:
        401 → auth-hint, exit 4 (matches ensure_authenticated)
        403 → access-denied + suggest `avr org list`
        404 → caller's hint takes over (e.g. "Run `avr run list`…")
        409 → surface the API's `detail` prominently — usually meaningful
              ("already cancelled", "already running")
        429 → rate-limit hint
        5xx → "Avrea is having trouble" + suggest `avr health`
    """
    status = exc.response.status_code
    if status == 401:
        exit_with_auth_hint()

    detail = _extract_detail(exc.response)
    detail_suffix = f": {detail}" if detail else ""

    if status == 403:
        click.echo(f"Error: You don't have access to {action} (HTTP 403){detail_suffix}", err=True)
        click.echo("  Hint: run `avr org list` to see available orgs, or pass --org.", err=True)
    elif status == 404:
        click.echo(f"Error: Not found while trying to {action} (HTTP 404){detail_suffix}", err=True)
        if hint:
            click.echo(f"  Hint: {hint}", err=True)
    elif status == 409:
        # 409 detail is usually load-bearing ("run is already cancelled").
        click.echo(f"Error: Conflict while trying to {action} (HTTP 409){detail_suffix}", err=True)
    elif status == 429:
        click.echo("Error: Avrea is rate-limiting requests (HTTP 429). Try again in a few seconds.", err=True)
    elif 500 <= status < 600:
        click.echo(
            f"Error: Avrea is having trouble (HTTP {status}). Try again shortly — `avr health` shows status.",
            err=True,
        )
        if detail:
            click.echo(f"  Detail: {detail}", err=True)
    else:
        click.echo(f"Error: Failed to {action} (HTTP {status}){detail_suffix}", err=True)
    sys.exit(1)


def _extract_detail(response: httpx.Response) -> str:
    """Pull the FastAPI-style ``detail`` field out of a JSON error body."""
    try:
        body = response.json()
    except ValueError:
        return ""
    if isinstance(body, dict):
        detail = body.get("detail")
        if isinstance(detail, str):
            return detail
    return ""
