"""Audit events CLI commands."""

from avrea_cli.api_client import ApiClient
from avrea_cli.config import CliConfig
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import validate_cursor
from avrea_cli.json_output import emit_json
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import split_fields
from avrea_cli.output import format_timestamp
from avrea_cli.output import output_list
import click
import httpx
import json

_AUDIT_FIELDS = make_schema(
    "event_id",
    "created_at",
    "actor_user_id",
    "actor_type",
    "acting_api_key_id",
    "client_ip",
    "resource_type",
    "resource_id",
    "action",
    "event_data",
)


@click.group("audit-events")
@click.pass_context
def audit_events(ctx):
    """View audit events for organization writes."""
    ensure_ctx(ctx)


@audit_events.command("list")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option("--resource-type", "resource_type", default=None, help="Filter by resource type (e.g. api_key, user).")
@click.option("--action", "action", default=None, help="Filter by action (CREATE, UPDATE, DELETE, ...).")
@click.option("--actor-user-id", "actor_user_id", default=None, help="Filter by acting user id.")
@click.option(
    "--from",
    "--created-after",
    "created_after",
    default=None,
    help="ISO-8601 lower bound (inclusive) on created_at.",
)
@click.option(
    "--to",
    "--created-before",
    "created_before",
    default=None,
    help="ISO-8601 upper bound (exclusive) on created_at.",
)
@click.option(
    "-L",
    "--limit",
    type=click.IntRange(1, 1000),
    default=100,
    show_default=True,
    help="Max events to return.",
)
@click.option("--cursor", default=None, help="Opaque cursor from a previous response's next_cursor.")
@click.option(
    "--json",
    "json_fields",
    default=None,
    help='Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.',
)
@click.option("-q", "--jq", "jq_expr", default=None, help="Filter --json output through a jq expression.")
@click.pass_context
def audit_events_list(
    ctx,
    org_id,
    resource_type,
    action,
    actor_user_id,
    created_after,
    created_before,
    limit,
    cursor,
    json_fields,
    jq_expr,
):
    """List audit events for the organization.

    \b
    JSON FIELDS
        acting_api_key_id, action, actor_type, actor_user_id, client_ip,
        created_at, event_data, event_id, resource_id, resource_type
    """
    if handle_json_meta(json_fields, jq_expr, _AUDIT_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)
    cursor = validate_cursor(cursor)

    params: dict = {"limit": limit}
    if resource_type:
        params["resource_type"] = resource_type
    if action:
        params["action"] = action
    if actor_user_id:
        params["actor_user_id"] = actor_user_id
    if created_after:
        params["created_after"] = created_after
    if created_before:
        params["created_before"] = created_before
    if cursor:
        params["cursor"] = cursor

    try:
        response = client.public_get(f"/orgs/{org_id}/audit-events", params=params)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list audit events")

    rows = response.get("data", [])

    if json_fields is not None:
        emit_json(rows, split_fields(json_fields, _AUDIT_FIELDS), _AUDIT_FIELDS, jq_expr)
        return

    for row in rows:
        row["created_at_display"] = format_timestamp(row.get("created_at"))
        row["target"] = row.get("resource_id") or "-"
        row["actor"] = row.get("actor_user_id") or row.get("actor_type") or "-"
        row["event_data_display"] = json.dumps(row.get("event_data") or {}, separators=(",", ":"))

    output_list(
        rows,
        columns=[
            "created_at_display",
            "actor",
            "actor_type",
            "resource_type",
            "target",
            "action",
            "event_data_display",
        ],
        column_labels=["Time", "Actor", "Type", "Resource", "Target", "Action", "Data"],
    )

    next_cursor = (response.get("pagination") or {}).get("next_cursor")
    if next_cursor:
        click.echo(f"\nMore results available. Re-run with --cursor {next_cursor}", err=True)
