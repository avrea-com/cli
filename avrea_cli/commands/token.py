"""Scoped access token CLI commands (avr token)."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.config import CliConfig
from avrea_cli.display import is_piped
from avrea_cli.display import print_piped_header
from avrea_cli.display import print_piped_row
from avrea_cli.helpers import echo_api_url
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import ensure_prompts_allowed
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import parse_duration_seconds
from avrea_cli.helpers import retry_after_seconds
from avrea_cli.json_output import emit_json
from avrea_cli.json_output import emit_json_record
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import json_options
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import split_fields
from avrea_cli.output import format_key_value
from avrea_cli.output import format_relative_timestamp
from avrea_cli.output import format_timestamp
from avrea_cli.output import output_list
from avrea_cli.repo_context import resolve_repo
from datetime import UTC
from datetime import datetime
from subprocess import TimeoutExpired
from typing import Any
from typing import NoReturn
import click
import httpx
import shlex
import sys

# The one access level the API mints per resource type. A level suffix on
# --repo / --vm is accepted only when it names this level.
_MINTED_LEVELS = {"repository": "read", "customer_vm": "admin"}
_TYPE_LABELS = {"repository": "repository", "customer_vm": "VM"}

# Bounds mirror the API; kept here only to fail obviously-bad input locally
# with a clear message. The server remains the source of truth.
_MIN_TTL_SECONDS = 60
_MAX_TTL_SECONDS = 7 * 24 * 3600
_MAX_GRANTS = 100
_PAGE_LIMIT = 200
_LIVE_TOKEN_LIMIT = 20

_GRANT_ORDER_HINT = (
    "grants[N] counts from 0 after resources are resolved and duplicates removed: "
    "the --repo values first, then the --vm values, in the order given."
)
_NO_TOKEN_HINT = "Run `avr token list` to see the tokens visible to you."

_TOKEN_FIELDS = make_schema(
    "id",
    "organization_id",
    "user_id",
    "name",
    "created_at",
    "expires_at",
    "last_used_at",
    "revoked_at",
    "revoked_reason",
    "allow_vm_create",
    "vm_create_limit",
    "vm_create_count",
    "grants",
)
# Only the create response carries the credential.
_TOKEN_CREATE_FIELDS = make_schema(*_TOKEN_FIELDS, "token")

_org_option = click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)


@click.group(cls=GhGroup)
@click.pass_context
def token(ctx):
    """Create and manage scoped access tokens.

    A scoped access token is a credential bound to one organization that
    expires within seven days. It reaches only the repositories and VMs it
    names, and it cannot create further tokens. To use one, set AVR_TOKEN to
    the credential and AVR_ORG to the organization ID.
    """
    ensure_ctx(ctx)


def _strip_level(spec: str, resource_type: str, flag: str) -> str:
    """Return a --repo / --vm value without its optional ``:level`` suffix.

    The suffix exists for forward compatibility with per-grant levels; today
    only the level the API mints for the type is accepted."""
    target, has_suffix, level = spec.rpartition(":")
    if not has_suffix:
        target = spec
    minted = _MINTED_LEVELS[resource_type]
    if not target or (has_suffix and level.lower() != minted):
        raise click.UsageError(
            f"Invalid {flag} value {spec!r}: {_TYPE_LABELS[resource_type]} grants are created at the "
            f"{minted} level. Use {flag} {target or 'ID'} or {flag} {target or 'ID'}:{minted}."
        )
    return target


def _exit_create_unavailable(exc: httpx.HTTPStatusError, *, names_vms: bool) -> NoReturn:
    """Explain a 404 from creating a token, then exit 1.

    The API's bare 404 does not distinguish disabled scoped tokens from
    disabled customer-VM support when VM grants or creation were requested."""
    click.echo("Error: Scoped token creation is not available for this organization (HTTP 404).", err=True)
    click.echo("  To request access, contact support@avrea.com.", err=True)
    if names_vms:
        click.echo(
            "  Check whether scoped tokens and customer VMs are enabled for this organization.",
            err=True,
        )
    echo_api_url(exc)
    sys.exit(1)


def _create_hints(response: httpx.Response) -> dict[int, str]:
    """What the create endpoint means by the statuses it gives a specific sense."""
    retry_after = retry_after_seconds(response)
    retry = f"Retry in {retry_after}s." if retry_after is not None else "Retry shortly."
    # A refused grant is reported by its position in the request body, which
    # the user only ever expressed as flags.
    grant_order = {403: _GRANT_ORDER_HINT, 422: _GRANT_ORDER_HINT} if "grants" in response.text else {}
    return {
        **grant_order,
        409: (
            f"A member can hold at most {_LIVE_TOKEN_LIMIT} live tokens per organization. "
            "Free one with `avr token list` and `avr token revoke <token-id>`."
        ),
        503: f"No token was created. {retry}",
    }


@token.command("create")
@click.option("--name", required=True, help="Token name (1-100 characters).")
@click.option(
    "--repo",
    "repos",
    multiple=True,
    help="Grant read access to a repository (org/repo or rep-xxx). Repeatable.",
)
@click.option("--vm", "vms", multiple=True, help="Grant admin access to a VM, by VM ID. Repeatable.")
@click.option("--allow-vm-create", is_flag=True, help="Let the token create VMs.")
@click.option(
    "--vm-create-limit",
    type=click.IntRange(1, 10),
    default=None,
    help="Max VMs the token may create; needs --allow-vm-create. The server default is 1.",
)
@click.option(
    "--ttl",
    default=None,
    help="Lifetime: e.g. 30m, 8h, 7d, or a number of seconds. 60 seconds to 7 days; the server default is 8 hours.",
)
@_org_option
@json_options
@click.pass_context
def token_create(ctx, name, repos, vms, allow_vm_create, vm_create_limit, ttl, org_id, json_fields, jq_expr):
    """Create a scoped token and print its credential.

    The credential is shown once and cannot be retrieved later. A token needs
    at least one of --repo, --vm or --allow-vm-create. Repository grants are
    read-only and VM grants are admin; an optional level suffix
    (--repo acme/api:read, --vm cvm-abc123:admin) must name that level.

    \b
    Examples:
        avr token create --name ci-read --repo acme/api --repo acme/web
        avr token create --name agent --vm cvm-abc123 --ttl 30m
        avr token create --name sandbox --allow-vm-create --vm-create-limit 3
        avr token create --name ci-read --repo acme/api --json token --jq .token

    \b
    JSON FIELDS
        allow_vm_create, created_at, expires_at, grants, id, last_used_at,
        name, organization_id, revoked_at, revoked_reason, token, user_id,
        vm_create_count, vm_create_limit
    """
    if handle_json_meta(json_fields, jq_expr, _TOKEN_CREATE_FIELDS):
        return
    # Checked before minting: the credential is returned once, so a field
    # list without it would leave a live token nobody can use.
    if json_fields is not None and "token" not in split_fields(json_fields, _TOKEN_CREATE_FIELDS):
        raise click.UsageError('--json must include the token field (or "*"): the credential is returned only once.')
    if not (repos or vms or allow_vm_create):
        raise click.UsageError(
            "Pass at least one of --repo, --vm or --allow-vm-create: a token with no grant can do nothing."
        )
    if vm_create_limit is not None and not allow_vm_create:
        raise click.UsageError("--vm-create-limit requires --allow-vm-create.")
    repo_names = [_strip_level(spec, "repository", "--repo") for spec in repos]
    vm_ids = [_strip_level(spec, "customer_vm", "--vm") for spec in vms]
    if len(repo_names) + len(vm_ids) > _MAX_GRANTS:
        raise click.UsageError(f"A token takes at most {_MAX_GRANTS} grants (got {len(repo_names) + len(vm_ids)}).")
    ttl_seconds = (
        parse_duration_seconds(ttl, minimum=_MIN_TTL_SECONDS, maximum=_MAX_TTL_SECONDS, param_hint="--ttl")
        if ttl is not None
        else None
    )

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    # The API refuses a resource named twice, which `acme/api` next to its
    # rep- ID would be, so duplicates are dropped once the names are resolved.
    targets = {
        "repository": dict.fromkeys(resolve_repo(client, config, org_id, repo) for repo in repo_names),
        "customer_vm": dict.fromkeys(vm_ids),
    }
    body: dict[str, Any] = {
        "name": name,
        "grants": [
            {"resource_type": resource_type, "resource_id": resource_id, "access_level": _MINTED_LEVELS[resource_type]}
            for resource_type, resource_ids in targets.items()
            for resource_id in resource_ids
        ],
        "allow_vm_create": allow_vm_create,
    }
    # Optional fields are left out so the server defaults apply.
    if vm_create_limit is not None:
        body["vm_create_limit"] = vm_create_limit
    if ttl_seconds is not None:
        body["ttl_seconds"] = ttl_seconds

    try:
        minted = client.public_post(f"/orgs/{org_id}/access-tokens", json=body)
    except httpx.HTTPStatusError as exc:
        if exc.response.status_code == 404:
            _exit_create_unavailable(exc, names_vms=bool(vm_ids) or allow_vm_create)
        handle_http_error(exc, "create the token", hints=_create_hints(exc.response))

    if json_fields is not None:
        fields = split_fields(json_fields, _TOKEN_CREATE_FIELDS)
        try:
            emit_json_record(minted, fields, _TOKEN_CREATE_FIELDS, jq_expr)
        except click.ClickException, TimeoutExpired:
            # Minting committed; jq's error text may also quote the credential.
            emit_json_record(minted, fields, _TOKEN_CREATE_FIELDS, None)
            raise click.ClickException(
                "jq output filtering failed. The token was created; stdout contains its unfiltered JSON. "
                "Save the credential rather than retrying creation."
            ) from None
        return

    _print_token(minted)
    click.echo()
    click.echo("The credential is shown only once. To use the token:")
    click.echo()
    # A scoped token cannot look its organization up by slug, so the consumer
    # needs the ID alongside the credential.
    click.echo(f"  export AVR_TOKEN={shlex.quote(minted['token'])}")
    click.echo(f"  export AVR_ORG={shlex.quote(minted.get('organization_id') or org_id)}")


@token.command("list")
@_org_option
@click.option(
    "-L", "--limit", type=click.IntRange(1, 1000), default=50, show_default=True, help="Max tokens to return."
)
@json_options
@click.pass_context
def token_list(ctx, org_id, limit, json_fields, jq_expr):
    """List live tokens, newest first.

    Shows your own tokens, or every member's when you are an organization
    admin. Revoked and expired tokens are not listed; `avr token view` still
    reads them by ID.

    \b
    Examples:
        avr token list
        avr token list --json id,name,expires_at

    \b
    JSON FIELDS
        allow_vm_create, created_at, expires_at, grants, id, last_used_at,
        name, organization_id, revoked_at, revoked_reason, user_id,
        vm_create_count, vm_create_limit
    """
    if handle_json_meta(json_fields, jq_expr, _TOKEN_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    tokens: list[dict[str, Any]] = []
    cursor = None
    while True:
        params: dict[str, Any] = {"limit": min(limit - len(tokens), _PAGE_LIMIT)}
        if cursor:
            params["cursor"] = cursor
        try:
            response = client.public_get(f"/orgs/{org_id}/access-tokens", params=params)
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "list tokens")
        page = response.get("data", [])
        tokens.extend(page)
        cursor = (response.get("pagination") or {}).get("next_cursor")
        # An empty page ends the walk even with a cursor, so a cursor that
        # never advances cannot loop forever.
        if not cursor or not page or len(tokens) >= limit:
            break
    more = bool(cursor) or len(tokens) > limit
    tokens = tokens[:limit]

    if json_fields is not None:
        emit_json(tokens, split_fields(json_fields, _TOKEN_FIELDS), _TOKEN_FIELDS, jq_expr)
    else:
        # Keep pipe columns stable; the human table needs owner only across members.
        piped = is_piped()
        show_owner = piped or len({t.get("user_id") for t in tokens}) > 1
        columns = [
            "id",
            "name",
            "expires_at" if piped else "expires_display",
            "last_used_at" if piped else "last_used_display",
            *(["user_id"] if show_owner else []),
            "grants_display",
        ]
        for t in tokens:
            t["grants_display"] = _grants_summary(t)
            if not piped:
                t["expires_display"] = format_timestamp(t.get("expires_at"))
                t["last_used_display"] = (
                    format_relative_timestamp(t["last_used_at"]) if t.get("last_used_at") else "never"
                )
        if piped:
            print_piped_header([column.removesuffix("_display") for column in columns])
            for t in tokens:
                print_piped_row([t.get(column) for column in columns])
        else:
            output_list(
                tokens,
                columns=columns,
                column_labels=[
                    "Token ID",
                    "Name",
                    "Expires",
                    "Last used",
                    *(["Owner"] if show_owner else []),
                    "Grants",
                ],
            )
    if more:
        click.echo(f"\nShowing the newest {len(tokens)} tokens. Raise --limit to see the rest.", err=True)


@token.command("view")
@click.argument("token_id")
@_org_option
@json_options
@click.pass_context
def token_view(ctx, token_id, org_id, json_fields, jq_expr):
    """Show a token's details and grants, including a revoked or expired one.

    The credential itself is never shown again after `avr token create`.

    \b
    Examples:
        avr token view key-abc123
        avr token view key-abc123 --json grants

    \b
    JSON FIELDS
        allow_vm_create, created_at, expires_at, grants, id, last_used_at,
        name, organization_id, revoked_at, revoked_reason, user_id,
        vm_create_count, vm_create_limit
    """
    if handle_json_meta(json_fields, jq_expr, _TOKEN_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        record = client.public_get(f"/orgs/{org_id}/access-tokens/{token_id}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch the token", hint=_NO_TOKEN_HINT)

    if json_fields is not None:
        emit_json_record(record, split_fields(json_fields, _TOKEN_FIELDS), _TOKEN_FIELDS, jq_expr)
        return
    _print_token(record)


@token.command("revoke")
@click.argument("token_id")
@_org_option
@click.option("--yes", "-y", is_flag=True, help="Skip the confirmation prompt.")
@click.pass_context
def token_revoke(ctx, token_id, org_id, yes):
    """Revoke a token.

    Requests made with the token are refused from then on. Revoking a token
    that is already revoked succeeds.

    \b
    Examples:
        avr token revoke key-abc123
        avr token revoke key-abc123 --yes
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    if not yes:
        ensure_prompts_allowed("revoking a token")
        click.confirm(f"Revoke token {token_id}? Anything using it stops working.", abort=True)

    try:
        client.public_delete(f"/orgs/{org_id}/access-tokens/{token_id}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "revoke the token", hint=_NO_TOKEN_HINT)

    click.echo(f"Token {token_id} revoked.")


def _print_token(record: dict[str, Any]) -> None:
    """Render a token's details and its grants. Never prints the credential."""
    last_used = record.get("last_used_at")
    click.echo(
        format_key_value(
            {
                "Token ID": record.get("id"),
                "Name": record.get("name"),
                "Owner": record.get("user_id"),
                "Status": _status(record),
                "Created": format_timestamp(record.get("created_at")),
                "Expires": format_timestamp(record.get("expires_at")),
                "Last used": format_timestamp(last_used) if last_used else "never",
                "VM create": _vm_create_display(record) or "not allowed",
            }
        )
    )
    grants = record.get("grants") or []
    if not grants:
        return
    click.echo()
    click.echo("Grants:")
    output_list(
        [
            {
                "type": _TYPE_LABELS.get(grant.get("resource_type"), grant.get("resource_type")),
                # A VM grant without an ID covers every VM its owner can reach.
                "resource": grant.get("resource_id") or "every VM the owner can reach",
                "level": grant.get("access_level"),
            }
            for grant in grants
        ],
        columns=["type", "resource", "level"],
        column_labels=["Type", "Resource", "Level"],
    )


def _status(record: dict[str, Any]) -> str:
    if record.get("revoked_at"):
        reason = record.get("revoked_reason")
        return f"revoked {format_timestamp(record['revoked_at'])}" + (f" ({reason})" if reason else "")
    try:
        expires = datetime.fromisoformat(record.get("expires_at") or "")
    except ValueError:
        return "active"
    if expires.tzinfo is None:
        expires = expires.replace(tzinfo=UTC)
    return "expired" if expires <= datetime.now(UTC) else "active"


def _vm_create_display(record: dict[str, Any]) -> str:
    """How many VMs the token may still create, or ``""`` when it may not."""
    if not record.get("allow_vm_create"):
        return ""
    limit = record.get("vm_create_limit")
    used = record.get("vm_create_count") or 0
    return f"allowed, {used} of {limit} used" if limit is not None else f"allowed, {used} used"


def _grants_summary(record: dict[str, Any]) -> str:
    """One-cell summary of a token's scope for the list table, e.g.
    ``2 repos, 1 VM, VM create 1/3``. `avr token view` lists each grant."""
    grants = record.get("grants") or []
    repos = sum(1 for g in grants if g.get("resource_type") == "repository")
    vms = [g for g in grants if g.get("resource_type") == "customer_vm"]
    parts: list[str] = []
    if repos:
        parts.append(f"{repos} repo{'' if repos == 1 else 's'}")
    if any(g.get("resource_id") is None for g in vms):
        parts.append("all VMs")
    elif vms:
        parts.append(f"{len(vms)} VM{'' if len(vms) == 1 else 's'}")
    if record.get("allow_vm_create"):
        limit = record.get("vm_create_limit")
        used = record.get("vm_create_count") or 0
        parts.append(f"VM create {used}/{limit}" if limit is not None else "VM create")
    return ", ".join(parts) or "-"
