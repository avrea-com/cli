"""Authentication CLI commands (avr auth login/logout/status)."""

from avrea_cli import auth
from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.config import CliConfig
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import handle_http_error
from avrea_cli.json_output import emit_json_record
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import json_options
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import split_fields
from avrea_cli.output import format_key_value
import click
import httpx
import sys

# `host`/`default_org`/`token` come from local config, not the API response.
# The handler injects them under `_local:*` keys before projection so the
# schema stays a flat wire-name → path map (matching every other command).
_AUTH_STATUS_FIELDS = make_schema(
    "email",
    "name",
    "created_at",
    user_id="id",
    host="_local:host",
    default_org="_local:default_org",
    token="_local:token",
)


def _fetch_email(api_url: str, api_key: str) -> str | None:
    """Best-effort: read the just-issued key's owner so we can show
    'Logged in as <email>'. Failures are silent — the key is already stored."""
    try:
        with httpx.Client(timeout=5.0) as c:
            r = c.get(f"{api_url}/users/me", headers={"Authorization": f"Bearer {api_key}"})
            r.raise_for_status()
            return r.json().get("email")
    except httpx.HTTPError, ValueError, KeyError:
        return None


def _maybe_auto_pin_default_org(api_url: str, api_key: str) -> tuple[str, str] | None:
    """If the user belongs to exactly one organization, store it as the
    default for this host so subsequent commands don't have to re-resolve.

    Returns (slug, org_id) when an org was pinned, None otherwise (zero
    orgs, multiple orgs, transport failure). Multi-org users still pick
    explicitly via ``avr config set org`` — auto-picking would silently
    bias every command toward an arbitrary org."""
    try:
        with httpx.Client(timeout=5.0) as c:
            r = c.get(f"{api_url}/users/me/organizations", headers={"Authorization": f"Bearer {api_key}"})
            r.raise_for_status()
            data = r.json().get("data") or []
    except httpx.HTTPError, ValueError, KeyError:
        return None
    if len(data) != 1:
        return None
    org_id = data[0].get("organization_id")
    slug = data[0].get("slug") or org_id
    if not org_id:
        return None
    return slug, org_id


def _do_login(ctx, provider):
    config: CliConfig = ctx.obj["config"]
    try:
        api_key = auth.login(config.public_api_url, provider=provider)
    except click.ClickException as exc:
        # Click's exception carries a useful `.message`; surface it under a
        # consistent "Login failed:" prefix and exit 1.
        click.echo(f"Login failed: {exc.format_message()}", err=True)
        sys.exit(1)
    # click.Abort is intentionally not caught here — auth.login already
    # printed a context-specific error to stderr before raising it. Letting
    # it propagate hands control back to Click's runner, which exits cleanly
    # without our generic "Login failed:" prefix duplicating the message.

    auth.store_token(api_key, host=config.public_api_url)

    check = click.style("✓", fg="green")
    email = _fetch_email(config.public_api_url, api_key)
    if email:
        click.echo(f"{check} Logged in as {click.style(email, bold=True)}")
    else:
        click.echo(f"{check} Authentication complete.")
    click.echo(f"  Credentials saved to {auth.HOSTS_FILE}")

    # Auto-pin only fires for single-org users — see _maybe_auto_pin_default_org.
    if not auth.load_default_org(host=config.public_api_url):
        pinned = _maybe_auto_pin_default_org(config.public_api_url, api_key)
        if pinned:
            slug, org_id = pinned
            auth.store_default_org(org_id, host=config.public_api_url)
            click.echo(f"  Default organization: {click.style(slug, bold=True)} ({org_id})")

    click.echo()
    click.echo("Try: avr status")


def _do_logout(ctx):
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]

    if config.auth_token:
        try:
            client.public_delete("/users/me/api-keys/current")
        except httpx.HTTPStatusError:
            click.echo("Warning: Could not revoke API key on server.", err=True)
        except httpx.ConnectError, httpx.TimeoutException:
            click.echo("Warning: Could not reach server to revoke API key.", err=True)

    cleared = auth.clear(host=config.public_api_url)
    check = click.style("✓", fg="green")
    if cleared:
        click.echo(f"{check} Logged out.")
    else:
        click.echo("No stored credentials found.")


@click.group(cls=GhGroup)
@click.pass_context
def auth_group(ctx):
    """Authenticate and manage credentials."""
    ensure_ctx(ctx)


@auth_group.command("login")
@click.option(
    "--provider",
    type=click.Choice(["google", "github"], case_sensitive=False),
    default="github",
    show_default=True,
    help="OAuth provider to use for CLI login.",
)
@click.pass_context
def auth_login(ctx, provider: str):
    """Authenticate via browser and store credentials."""
    _do_login(ctx, provider)


@auth_group.command("logout")
@click.pass_context
def auth_logout(ctx):
    """Revoke the current API key and remove stored credentials."""
    _do_logout(ctx)


@auth_group.command("switch")
@click.argument("host", required=False)
@click.pass_context
def auth_switch(ctx, host: str | None):
    """Switch the default host used when AVR_HOST isn't set.

    \b
    Examples:
        avr auth switch                     # show current default + all hosts
        avr auth switch https://api.avrea.com
    """
    ensure_ctx(ctx)
    hosts = auth.list_hosts()
    if not hosts:
        click.echo("No stored credentials. Run `avr auth login` first.", err=True)
        sys.exit(1)

    current = auth.load_default_host()
    if host is None:
        # Bare `auth switch` lists what's available so the user can pick.
        # Stdout gets bare hostnames so `avr auth switch | fzf` works; the
        # header, default marker, and pick hint go to stderr.
        if sys.stdout.isatty():
            click.echo("Stored hosts:", err=True)
        for h in hosts:
            click.echo(h)
            if h == current and sys.stdout.isatty():
                click.echo(click.style(f"  (default: {h})", fg="green"), err=True)
        click.echo(f"\nPass one of these to switch, e.g. `avr auth switch {hosts[0]}`.", err=True)
        return

    target = host.rstrip("/")
    if target not in hosts:
        click.echo(f"Error: no stored credentials for host {target!r}.", err=True)
        click.echo("Available hosts:", err=True)
        for h in hosts:
            click.echo(f"  {h}", err=True)
        sys.exit(1)

    auth.set_default_host(target)
    check = click.style("✓", fg="green")
    click.echo(f"{check} Default host set to {click.style(target, bold=True)}")


@auth_group.command("status")
@click.option("--show-token", is_flag=True, help="Display the auth token in plain text.")
@json_options
@click.pass_context
def auth_status(ctx, show_token: bool, json_fields, jq_expr):
    """Display the authenticated user and connection state."""
    if handle_json_meta(json_fields, jq_expr, _AUTH_STATUS_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]

    ensure_authenticated(config)

    try:
        result = client.public_get("/users/me")
    except httpx.HTTPStatusError as exc:
        # handle_http_error short-circuits 401 to the same auth hint, and
        # surfaces other statuses with detail-aware framing.
        handle_http_error(exc, "fetch user info")

    # `.get()` for every field — a partial /users/me response (schema drift,
    # degraded backend) should yield a missing-field message, not a KeyError
    # traceback dumped at the user.

    if json_fields is not None:
        # Build a synthetic record so the schema can mix server fields with
        # local config (host, default_org, token). The token field is removed
        # from the schema entirely unless --show-token is set — `--json '*'`
        # then returns only the fields that exist, and `--json token` errors
        # with the available-fields hint, prompting the user to add the flag.
        record: dict[str, object] = dict(result)
        record["_local:host"] = config.public_api_url
        record["_local:default_org"] = config.default_org
        if show_token:
            record["_local:token"] = config.auth_token
            schema = _AUTH_STATUS_FIELDS
        else:
            schema = {k: v for k, v in _AUTH_STATUS_FIELDS.items() if k != "token"}
        emit_json_record(record, split_fields(json_fields, schema), schema, jq_expr)
        return

    click.echo(
        format_key_value(
            {
                "User ID": result.get("id") or "-",
                "Email": result.get("email") or "-",
                "Name": result.get("name", "N/A"),
                "Host": config.public_api_url,
                "Default org": config.default_org or "(not set)",
                "Created": result.get("created_at") or "-",
                "Token": _format_token(config.auth_token, show_token),
            }
        )
    )
    click.echo()
    click.echo(click.style("  Switch host:        avr auth switch <host>", dim=True))
    click.echo(click.style("  Switch default org: avr config set org <slug>", dim=True))


def _format_token(token: str | None, show: bool) -> str:
    """Render the API token for `auth status`. Masks by default; reveals the
    full token only when --show-token is passed."""
    if not token:
        return "(none)"
    if show:
        return token
    # Show enough of the prefix to identify the key family without leaking it.
    # Tokens ≤ 8 chars are out-of-spec (real Avrea API keys are much longer);
    # collapsing all such inputs to a featureless 32-asterisk string is
    # intentional, not a bug — the test suite pins this for safety.
    visible = token[:4] if len(token) > 8 else ""
    return f"{visible}{'*' * 32}" if visible else "*" * 32


# ---------------------------------------------------------------------------
# Top-level aliases (hidden, for backward compat)
# ---------------------------------------------------------------------------


@click.command("login", hidden=True)
@click.option(
    "--provider",
    type=click.Choice(["google", "github"], case_sensitive=False),
    default="github",
    show_default=True,
    help="OAuth provider to use for CLI login.",
)
@click.pass_context
def login_alias(ctx, provider: str):
    """Authenticate via browser and store credentials."""
    _do_login(ctx, provider)


@click.command("logout", hidden=True)
@click.pass_context
def logout_alias(ctx):
    """Revoke the current API key and remove stored credentials."""
    _do_logout(ctx)
