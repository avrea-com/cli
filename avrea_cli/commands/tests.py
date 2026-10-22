"""Read retained test evidence through the customer API."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.config import CliConfig
from avrea_cli.display import escape_control_characters
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import validate_cursor
from avrea_cli.json_output import emit_json_record
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import json_options
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import split_fields
from avrea_cli.output import output_list
from avrea_cli.repo_context import resolve_repo_or_detect
from typing import Any
import click
import httpx
import re

_FIELDS = make_schema(
    "organization_id",
    "repository_id",
    "test_id",
    "configuration_digest",
    "capture_id",
    "status",
    "reason",
    "pending",
    "as_of_revision",
    "current_revision",
    "data",
    "pagination",
    "truncated",
)
_CONTROL_FIELDS = make_schema("organization_id", "repository_id", "data")
_repo = click.option("--repo", "repo_id", help="Repository (owner/name or rep-ID). Defaults to the git remote.")
_org = click.option("--org", "org_id", help="Organization ID or slug. Defaults to the configured organization.")
_limit = click.option(
    "-L", "--limit", type=click.IntRange(1, 100), default=50, show_default=True, help="Rows per page (1-100)."
)
_cursor = click.option("--cursor", help="Opaque next_cursor from the previous page. One page is read per invocation.")


def _digest(_ctx: click.Context, param: click.Parameter, value: str) -> str:
    if not re.fullmatch(r"[0-9a-f]{64}", value):
        raise click.BadParameter("must be a 64-character lowercase hex identifier", param=param)
    return value


def _capture(_ctx: click.Context, param: click.Parameter, value: str) -> str:
    if not re.fullmatch(r"tec-[0-9a-f]{32}", value):
        raise click.BadParameter("must be a tec- identifier followed by 32 lowercase hex characters", param=param)
    return value


def _page_params(limit: int, cursor: str | None) -> dict[str, Any]:
    cursor = validate_cursor(cursor, max_length=8192)
    return {"limit": limit, **({"cursor": cursor} if cursor is not None else {})}


def _read(
    ctx: click.Context,
    org_id: str | None,
    repo_id: str | None,
    suffix: str,
    params: dict[str, Any] | None = None,
) -> dict[str, Any]:
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    organization = get_org_id(config, org_id, client=client)
    repository = resolve_repo_or_detect(client, config, organization, repo_id, required=True)
    try:
        response = client.public_get(f"/orgs/{organization}/repos/{repository}/tests/{suffix}", params=params)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "read test evidence")
    return {**response, "organization_id": organization, "repository_id": repository}


def _show(response: dict[str, Any], json_fields: str | None, jq_expr: str | None, *, detail: bool = False) -> None:
    if json_fields is not None:
        emit_json_record(response, split_fields(json_fields, _FIELDS), _FIELDS, jq_expr)
        return
    click.echo(f"Organization: {response['organization_id']}  Repository: {response['repository_id']}")
    for field in ("test_id", "configuration_digest", "capture_id", "status", "reason"):
        if response.get(field) is not None:
            click.echo(f"{field}: {escape_control_characters(str(response[field]))}")
    if response.get("pending"):
        click.echo("New results are still being indexed.")
    if response.get("truncated"):
        click.echo(
            "This result is partial. The detail read returns at most 500 occurrences; "
            "--limit controls the requested count."
        )
    rows = response.get("data") or []
    if detail:
        for row in rows:
            click.echo()
            for key in ("suite", "class_name", "name", "outcome", "duration_ms", "failure_summary"):
                if row.get(key) is not None:
                    click.echo(f"{key}: {escape_control_characters(str(row[key]))}")
    elif rows:
        if "test_id" in rows[0]:
            display_rows = [{**row, "name": (row.get("identity") or {}).get("name") or row["test_id"]} for row in rows]
            display_rows = [
                {
                    key: escape_control_characters(str(value)) if isinstance(value, str) else value
                    for key, value in row.items()
                }
                for row in display_rows
            ]
            output_list(
                display_rows,
                columns=["name", "test_id", "configuration_digest", "failed_count", "errored_count", "passed_count"],
                column_labels=["Test", "Test ID", "Configuration", "Failed", "Errors", "Passed"],
            )
        else:
            columns = [
                key
                for key in (
                    "capture_id",
                    "captured_at",
                    "armed_at",
                    "state",
                    "detail_status",
                    "commit_sha",
                    "reason",
                    "passed_count",
                    "failed_count",
                    "errored_count",
                    "duration_total_ms",
                )
                if key in rows[0]
            ]
            display_rows = [
                {key: "-" if value is None else escape_control_characters(str(value)) for key, value in row.items()}
                for row in rows
            ]
            output_list(display_rows, columns=columns, column_labels=columns)
    else:
        click.echo("No recorded results in this page. This does not establish that no tests ran.")
    next_cursor = (response.get("pagination") or {}).get("next_cursor")
    if next_cursor:
        click.echo(f"Next page: --cursor {escape_control_characters(str(next_cursor))}", err=True)


@click.group(cls=GhGroup)
@click.pass_context
def tests(ctx: click.Context) -> None:
    """Inspect recorded tests, failures, and reporting status."""
    ensure_ctx(ctx)


@tests.command("list")
@_repo
@_org
@_limit
@_cursor
@json_options
@click.pass_context
def tests_list(
    ctx: click.Context,
    repo_id: str | None,
    org_id: str | None,
    limit: int,
    cursor: str | None,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """List one page of test histories, grouped by test and configuration.

    Counts describe each returned history window, not repository totals.
    Names appear when retained private detail is available; otherwise IDs are shown.
    Use --json '*' to retain status, pagination and repository context for agents.
    """
    if handle_json_meta(json_fields, jq_expr, _FIELDS):
        return
    _show(_read(ctx, org_id, repo_id, "history", _page_params(limit, cursor)), json_fields, jq_expr)


@tests.command("history")
@click.argument("test_id", callback=_digest)
@click.option(
    "--configuration",
    "configuration_digest",
    required=True,
    callback=_digest,
    help="History grouping identifier returned by tests list; not a platform label.",
)
@_repo
@_org
@_limit
@_cursor
@json_options
@click.pass_context
def tests_history(
    ctx: click.Context,
    test_id: str,
    configuration_digest: str,
    repo_id: str | None,
    org_id: str | None,
    limit: int,
    cursor: str | None,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """Read recorded outcomes for one test and configuration, newest first."""
    if handle_json_meta(json_fields, jq_expr, _FIELDS):
        return
    params = {**_page_params(limit, cursor), "configuration": configuration_digest}
    response = _read(ctx, org_id, repo_id, f"history/{test_id}", params)
    response.update(test_id=test_id, configuration_digest=configuration_digest)
    _show(response, json_fields, jq_expr)


@tests.command("view")
@click.argument("test_id", callback=_digest)
@click.option("--capture", "capture_id", required=True, callback=_capture, help="Exact capture from tests history.")
@_repo
@_org
@click.option(
    "-L", "--limit", type=click.IntRange(1, 500), default=50, show_default=True, help="Occurrences to read (1-500)."
)
@json_options
@click.pass_context
def tests_view(
    ctx: click.Context,
    test_id: str,
    capture_id: str,
    repo_id: str | None,
    org_id: str | None,
    limit: int,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """Read the exact retained result, including available failure messages.

    An expired or unrecorded result is never replaced with a different capture.
    A recorded test failure is data, not a CLI error. No tests are rerun.
    """
    if handle_json_meta(json_fields, jq_expr, _FIELDS):
        return
    response = _read(ctx, org_id, repo_id, f"history/{test_id}/detail/{capture_id}", {"limit": limit})
    response.update(test_id=test_id, capture_id=capture_id)
    _show(response, json_fields, jq_expr, detail=True)


@tests.command("captures")
@_repo
@_org
@_limit
@_cursor
@json_options
@click.pass_context
def tests_captures(
    ctx: click.Context,
    repo_id: str | None,
    org_id: str | None,
    limit: int,
    cursor: str | None,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """List report deliveries, including missing, partial and failed captures."""
    if handle_json_meta(json_fields, jq_expr, _FIELDS):
        return
    _show(_read(ctx, org_id, repo_id, "captures", _page_params(limit, cursor)), json_fields, jq_expr)


@tests.command("status")
@_repo
@_org
@json_options
@click.pass_context
def tests_status(
    ctx: click.Context, repo_id: str | None, org_id: str | None, json_fields: str | None, jq_expr: str | None
) -> None:
    """Read collection settings. Use --json for retained-history lifecycle details."""
    if handle_json_meta(json_fields, jq_expr, _CONTROL_FIELDS):
        return
    result = _read(ctx, org_id, repo_id, "control")
    response = {
        "organization_id": result.pop("organization_id"),
        "repository_id": result.pop("repository_id"),
        "data": result,
    }
    if json_fields is not None:
        emit_json_record(response, split_fields(json_fields, _CONTROL_FIELDS), _CONTROL_FIELDS, jq_expr)
        return
    click.echo(f"Organization: {response['organization_id']}  Repository: {response['repository_id']}")
    click.echo(f"Collection: {'on' if result['capture_enabled'] else 'off'}")
    click.echo(f"Can manage collection: {'yes' if result['can_manage'] else 'no'}")
    click.echo("Use 'avr tests captures' to inspect delivery and missing-report evidence.")
