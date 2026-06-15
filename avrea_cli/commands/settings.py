"""Settings management CLI commands."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.config import CliConfig
from avrea_cli.display import get_console_url
from avrea_cli.display import open_or_print_url
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import get_org_slug
from avrea_cli.helpers import handle_http_error
from avrea_cli.json_output import emit_json
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import reject_web_with_json
from avrea_cli.json_output import split_fields
from avrea_cli.output import output_list
from avrea_cli.repo_context import resolve_repo_or_detect
from urllib.parse import quote
import click
import httpx

_SETTINGS_LIST_FIELDS = make_schema("key", "value", "source")
_SETTINGS_SCHEMA_FIELDS = make_schema(
    "key", "value_type", "default", "scopes", "inherits", "description", "choices", "min_value", "max_value"
)


@click.group(cls=GhGroup)
@click.pass_context
def settings(ctx):
    """View and toggle cache and runner settings."""
    ensure_ctx(ctx)


@settings.command("list")
@click.option("--org", "org_id", help="Organization ID or slug. Uses default org if not specified.")
@click.option("--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.option("--prefix", help="Filter by key prefix (e.g. 'cache.').")
@click.option("--web", is_flag=True, help="Open in browser.")
@click.option(
    "--json",
    "json_fields",
    default=None,
    help='Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.',
)
@click.option("-q", "--jq", "jq_expr", default=None, help="Filter --json output through a jq expression.")
@click.pass_context
def settings_list(ctx, org_id, repo_id, prefix, web: bool, json_fields, jq_expr):
    """List settings with their current values and source.

    \b
    Examples:
        avr settings list --org org-abc123
        avr settings list --org org-abc123 --repo rep-xyz789
        avr settings list --repo acme/web
        avr settings list --prefix cache.
        avr settings list --json key,value,source

    \b
    JSON FIELDS
        key, source, value
    """
    reject_web_with_json(json_fields, web)
    if handle_json_meta(json_fields, jq_expr, _SETTINGS_LIST_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)
    repo_id = resolve_repo_or_detect(client, config, org_id, repo_id)

    if web:
        slug = get_org_slug(client, org_id)
        console_url = get_console_url(config.public_api_url)
        url = f"{console_url}/org/{slug}/repos/{repo_id}/settings" if repo_id else f"{console_url}/org/{slug}/settings"
        open_or_print_url(url)
        return

    params = {}
    if prefix:
        params["prefix"] = prefix

    try:
        if repo_id:
            response = client.public_get(f"/orgs/{org_id}/repos/{repo_id}/settings", params=params)
        else:
            response = client.public_get(f"/orgs/{org_id}/settings", params=params)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list settings")

    if json_fields is not None:
        emit_json(response, split_fields(json_fields, _SETTINGS_LIST_FIELDS), _SETTINGS_LIST_FIELDS, jq_expr)
        return

    for entry in response:
        entry["enabled"] = "yes" if entry["value"] is True else "no" if entry["value"] is False else str(entry["value"])

    output_list(
        response,
        columns=["key", "enabled", "source"],
        column_labels=["Key", "Value", "Source"],
    )


@settings.command("set")
@click.argument("key")
@click.argument("value")
@click.option("--org", "org_id", help="Organization ID or slug. Uses default org if not specified.")
@click.option("--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.pass_context
def settings_set(ctx, key, value, org_id, repo_id):
    """Set a setting value.

    VALUE is parsed as a boolean (true/false) or integer when possible,
    otherwise treated as a string.

    \b
    Examples:
        avr settings set cache.gha.enabled false --org org-abc123
        avr settings set cache.packages.enabled true --repo rep-xyz789
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)
    repo_id = resolve_repo_or_detect(client, config, org_id, repo_id)
    parsed = _parse_value(value)

    try:
        if repo_id:
            result = client.public_put(
                f"/orgs/{org_id}/repos/{repo_id}/settings",
                json={"key": key, "value": parsed},
            )
        else:
            result = client.public_put(
                f"/orgs/{org_id}/settings",
                json={"key": key, "value": parsed},
            )
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "update setting")

    scope = "repo" if repo_id else "org"
    click.echo(f"Set {result['key']} = {result['value']} ({scope})")


@settings.command("reset")
@click.argument("key")
@click.option("--org", "org_id", help="Organization ID or slug. Uses default org if not specified.")
@click.option("--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.pass_context
def settings_reset(ctx, key, org_id, repo_id):
    """Remove a setting override, reverting to the inherited or default value.

    \b
    Examples:
        avr settings reset cache.gha.enabled --repo rep-xyz789
        avr settings reset cache.packages.enabled --org org-abc123
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)
    repo_id = resolve_repo_or_detect(client, config, org_id, repo_id)
    encoded_key = quote(key, safe="")

    try:
        if repo_id:
            client.public_delete(f"/orgs/{org_id}/repos/{repo_id}/settings/{encoded_key}")
        else:
            client.public_delete(f"/orgs/{org_id}/settings/{encoded_key}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "reset setting")

    scope = "repo" if repo_id else "org"
    click.echo(f"Reset {key} ({scope}) to inherited default")


@settings.command("schema")
@click.option("--prefix", help="Filter by key prefix (e.g. 'cache.').")
@click.option("--scope", type=click.Choice(["repository", "organization"]), help="Filter by scope.")
@click.option(
    "--json",
    "json_fields",
    default=None,
    help='Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.',
)
@click.option("-q", "--jq", "jq_expr", default=None, help="Filter --json output through a jq expression.")
@click.pass_context
def settings_schema(ctx, prefix, scope, json_fields, jq_expr):
    """List available setting definitions.

    \b
    Examples:
        avr settings schema
        avr settings schema --prefix cache. --scope repository
        avr settings schema --json '*'

    \b
    JSON FIELDS
        choices, default, description, inherits, key, max_value, min_value,
        scopes, value_type
    """
    if handle_json_meta(json_fields, jq_expr, _SETTINGS_SCHEMA_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    params = {}
    if prefix:
        params["prefix"] = prefix
    if scope:
        params["scope"] = scope

    try:
        response = client.public_get("/settings/schema", params=params)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list settings schema")

    if json_fields is not None:
        emit_json(response, split_fields(json_fields, _SETTINGS_SCHEMA_FIELDS), _SETTINGS_SCHEMA_FIELDS, jq_expr)
        return

    for entry in response:
        entry["scopes_display"] = ", ".join(entry.get("scopes", []))
        entry["inherits_display"] = "yes" if entry.get("inherits") else "no"
        entry["constraints_display"] = _format_constraints(entry)

    output_list(
        response,
        columns=["key", "value_type", "default", "constraints_display", "scopes_display", "inherits_display"],
        column_labels=["Key", "Type", "Default", "Constraints", "Scopes", "Inherits"],
    )


def _format_constraints(entry: dict) -> str:
    """Format choices/min/max into a display string."""
    parts = []
    choices = entry.get("choices", [])
    if choices:
        parts.append("|".join(choices))
    min_val = entry.get("min_value")
    max_val = entry.get("max_value")
    if min_val is not None and max_val is not None:
        parts.append(f"{min_val}..{max_val}")
    elif min_val is not None:
        parts.append(f">={min_val}")
    elif max_val is not None:
        parts.append(f"<={max_val}")
    return ", ".join(parts) if parts else "-"


def _parse_value(raw: str) -> bool | int | str:
    """Parse a CLI string argument into a typed value."""
    lower = raw.lower()
    if lower in ("true", "yes", "on"):
        return True
    if lower in ("false", "no", "off"):
        return False
    try:
        return int(raw)
    except ValueError:
        return raw
