"""Read the retained test results of a job."""

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
from avrea_cli.repo_context import resolve_repo
from typing import Any
import click
import httpx
import re
import shlex

_SNAPSHOT_FIELDS = (
    "job_id",
    "execution_id",
    "status",
    "reporting_state",
    "reason",
    "commit_sha",
    "observed_at",
    "total_count",
)
_FIELDS = make_schema("organization_id", "repository_id", *_SNAPSHOT_FIELDS, "data", "pagination")
_MAX_PAGES = 20


class JobTestsGroup(GhGroup):
    """Accept the documented job shorthand without swallowing unknown verbs."""

    def resolve_command(
        self, ctx: click.Context, args: list[str]
    ) -> tuple[str | None, click.Command | None, list[str]]:
        if args and args[0].startswith("job-"):
            args = ["list", *args]
        return super().resolve_command(ctx, args)


def _job_id(_ctx: click.Context, param: click.Parameter, value: str) -> str:
    if not re.fullmatch(r"job-[0-9a-f]{32}", value):
        raise click.BadParameter(
            "use the Avrea job ID from 'avr job list' (job- followed by 32 lowercase hex characters)", param=param
        )
    return value


def _get(client: ApiClient, path: str, params: dict[str, Any] | None = None) -> dict[str, Any]:
    try:
        response = client.public_get(path, params=params)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "read job test results")
    return response


def _job_repository(client: ApiClient, organization: str, job: str) -> str:
    response = _get(client, f"/orgs/{organization}/jobs/{job}")
    data = response.get("data", response)
    repository = data.get("repository_id") if isinstance(data, dict) else None
    if not isinstance(repository, str) or not re.fullmatch(r"rep-[0-9a-f]{32}", repository):
        raise click.ClickException("The job has no available repository. Check the job and selected organization.")
    if data.get("job_id") != job:
        raise click.ClickException("The API returned a different job. Retry with the exact Avrea job ID.")
    return repository


def _results(client: ApiClient, path: str, job: str, limit: int, cursor: str | None) -> dict[str, Any]:
    """Follow short pages within a row and request budget, retaining the resume cursor."""
    rows: list[dict[str, Any]] = []
    snapshot: dict[str, Any] | None = None
    seen = {cursor} if cursor else set()
    for _ in range(_MAX_PAGES):
        page_limit = min(100, limit - len(rows))
        params: dict[str, Any] = {"limit": page_limit}
        if cursor is not None:
            params["cursor"] = cursor
        page = _get(client, path, params)
        data = page.get("data")
        pagination = page.get("pagination")
        if page.get("job_id") != job or not isinstance(data, list) or not isinstance(pagination, dict):
            raise click.ClickException("The API returned invalid job test results.")
        if len(data) > page_limit or not all(isinstance(row, dict) for row in data):
            raise click.ClickException("The API returned an invalid test result page.")
        if snapshot is None:
            snapshot = page
        elif any(page.get(key) != snapshot.get(key) for key in _SNAPSHOT_FIELDS):
            raise click.ClickException("Job results changed while reading pages. Retry without --cursor.")
        cursor = pagination.get("next_cursor")
        if cursor is not None:
            if not isinstance(cursor, str):
                raise click.ClickException("The API returned an invalid next cursor.")
            validate_cursor(cursor, max_length=1024)
            if cursor in seen:
                raise click.ClickException("The API repeated a test-results cursor; stopping pagination.")
            seen.add(cursor)
        rows.extend(data)
        if cursor is None or len(rows) == limit:
            break
    assert snapshot is not None
    return {**snapshot, "data": rows, "pagination": {"next_cursor": cursor}}


def _text(value: Any) -> str:
    return "unknown" if value is None else escape_control_characters(str(value))


def _show(response: dict[str, Any]) -> None:
    click.echo(f"Job: {_text(response['job_id'])}")
    click.echo(f"Organization: {_text(response['organization_id'])}  Repository: {_text(response['repository_id'])}")
    click.echo(f"Execution: {_text(response.get('execution_id'))}  Commit: {_text(response.get('commit_sha'))}")
    click.echo(f"Reporting: {_text(response.get('reporting_state'))}  Results: {_text(response.get('status'))}")
    if response.get("reason"):
        click.echo(f"Reason: {_text(response['reason'])}")
    click.echo(f"Retained occurrences: {_text(response.get('total_count'))}; shown: {len(response['data'])}")
    rows = response["data"]
    if rows:
        columns = ("suite", "class_name", "name", "outcome", "duration_ms")
        output_list(
            [{key: _text(row.get(key)) for key in columns} for row in rows],
            columns=columns,
            column_labels=["Suite", "Class", "Test", "Outcome", "Time (ms)"],
        )
        for row in rows:
            if row.get("failure_summary"):
                click.echo(f"\n{_text(row.get('name'))}: {_text(row['failure_summary'])}")
    elif response.get("status") == "available" and response.get("total_count") == 0:
        click.echo("The retained report contains zero test occurrences. This does not establish that no tests ran.")
    else:
        click.echo("No test results returned. Missing or expired reports do not mean that zero tests ran.")
    if response.get("reporting_state") == "complete":
        click.echo("Complete describes the collected reports, not whether every expected test ran.")


@click.group(cls=JobTestsGroup)
@click.pass_context
def tests(ctx: click.Context) -> None:
    """Read a job's tests, timings and recorded failure messages.

    Use avr tests JOB (or avr tests list JOB). JOB is an Avrea job ID from
    avr job list. No report capture ID or configuration digest is required.
    """
    ensure_ctx(ctx)


@tests.command("list")
@click.argument("job", metavar="JOB", callback=_job_id)
@click.option("--org", "org_id", help="Organization ID or slug. Defaults to the configured organization.")
@click.option("--repo", "repo_id", help="Repository (owner/name or rep-ID). Defaults to the job's repository.")
@click.option(
    "-L",
    "--limit",
    type=click.IntRange(1, 1000),
    default=100,
    show_default=True,
    help=f"Maximum occurrences to return; short pages are followed, up to {_MAX_PAGES} result pages.",
)
@click.option("--cursor", help="Continue from a previous response's next_cursor.")
@click.option(
    "--output",
    type=click.Choice(["text", "json"]),
    help="Output format. JSON includes status, context and the next cursor.",
)
@json_options
@click.pass_context
def tests_list(
    ctx: click.Context,
    job: str,
    org_id: str | None,
    repo_id: str | None,
    limit: int,
    cursor: str | None,
    output: str | None,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """Read the latest confirmed execution of JOB, including repeated tests.

    Names, outcomes, recorded durations and available failure messages are
    shown together. A failed test is data, not a command error. Missing,
    pending, partial and expired evidence are reported explicitly; an older
    execution is never substituted. Complete describes collected reports.

    Use --output json for the complete response or --json FIELDS to select
    fields. Continue with --cursor when pagination.next_cursor is present.
    API refusal or a changed report fails the command without partial output.
    """
    if output == "text" and json_fields is not None:
        raise click.UsageError("--output text cannot be combined with --json")
    if output == "json" and json_fields is None:
        json_fields = "*"
    if handle_json_meta(json_fields, jq_expr, _FIELDS):
        return
    cursor = validate_cursor(cursor, max_length=1024)
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    organization = get_org_id(config, org_id, client=client)
    repository = (
        resolve_repo(client, config, organization, repo_id) if repo_id else _job_repository(client, organization, job)
    )
    response = _results(client, f"/orgs/{organization}/repos/{repository}/jobs/{job}/tests", job, limit, cursor)
    response.update(organization_id=organization, repository_id=repository)
    if json_fields is not None:
        emit_json_record(response, split_fields(json_fields, _FIELDS), _FIELDS, jq_expr)
    else:
        _show(response)
    next_cursor = response["pagination"]["next_cursor"]
    if next_cursor:
        click.echo(f"More results available. Continue with --cursor {shlex.quote(_text(next_cursor))}", err=True)
