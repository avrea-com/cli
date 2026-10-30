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
from avrea_cli.repo_context import resolve_repo_or_detect
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
_JOB_ID = re.compile(r"(job|awj)-[0-9a-f]{32}")
_INVALID_NEXT_CURSOR = "The API returned an invalid next cursor."


class JobTestsGroup(GhGroup):
    """Accept the documented job shorthand without swallowing unknown verbs."""

    def resolve_command(
        self, ctx: click.Context, args: list[str]
    ) -> tuple[str | None, click.Command | None, list[str]]:
        if args and args[0].startswith(("job-", "awj-")):
            args = ["list", *args]
        return super().resolve_command(ctx, args)


def _job_id(_ctx: click.Context, param: click.Parameter, value: str) -> str:
    if not _JOB_ID.fullmatch(value):
        raise click.BadParameter(
            "use the Avrea job ID from 'avr job list' (job- or awj- followed by 32 lowercase hex characters)",
            param=param,
        )
    return value


def _get(
    client: ApiClient, path: str, action: str, not_found_hint: str, params: dict[str, Any] | None = None
) -> dict[str, Any]:
    try:
        response = client.public_get(path, params=params)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, action, hints={404: not_found_hint})
    return response


def _job_repository(client: ApiClient, organization: str, job: str) -> str:
    response = _get(
        client,
        f"/orgs/{organization}/jobs/{job}",
        "read the job",
        "Check the job ID, or pass --org with the job's organization.",
    )
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
        page_limit = limit - len(rows)
        params: dict[str, Any] = {"limit": page_limit}
        if cursor is not None:
            params["cursor"] = cursor
        page = _get(
            client,
            path,
            "read job test results",
            # A new runtime invalidates old cursors, and the route also answers 404
            # when Repository Tests are off or the job is outside the repository.
            "If you passed --cursor, restart without it. Otherwise check that Repository"
            " Tests are enabled and that the job belongs to the repository.",
            params,
        )
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
                raise click.ClickException(_INVALID_NEXT_CURSOR)
            try:
                validate_cursor(cursor, max_length=1024)
            except click.BadParameter:
                # The server's cursor is not user input, so it gets no usage error.
                raise click.ClickException(_INVALID_NEXT_CURSOR) from None
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
            # The class keeps same-named tests in different classes apart.
            label = ".".join(_text(row.get(key)) for key in ("class_name", "name") if row.get(key)) or "unknown"
            if row.get("failure_summary"):
                click.echo(f"\n{label}: {_text(row['failure_summary'])}")
            if row.get("result_id") and row.get("outcome") in ("failed", "errored"):
                click.echo(
                    f"View {label}: avr tests view {shlex.quote(_text(row['result_id']))} "
                    f"--org {shlex.quote(_text(response['organization_id']))} "
                    f"--repo {shlex.quote(_text(response['repository_id']))}"
                )
    elif response.get("status") == "available" and response.get("total_count") == 0:
        click.echo("The retained report contains zero test occurrences. This does not establish that no tests ran.")
    elif response.get("status") == "available":
        click.echo("No more results.")
    else:
        click.echo("No test results returned. Missing or expired reports do not mean that zero tests ran.")
    if response.get("reporting_state") == "complete":
        click.echo("Complete describes the collected reports, not whether every expected test ran.")


@click.group(cls=JobTestsGroup)
@click.pass_context
def tests(ctx: click.Context) -> None:
    """Read a job's tests, timings and recorded failure messages.

    Use avr tests JOB (or avr tests list JOB). JOB is an Avrea job ID
    (job- or awj-) from avr job list.
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
@json_options
@click.pass_context
def tests_list(
    ctx: click.Context,
    job: str,
    org_id: str | None,
    repo_id: str | None,
    limit: int,
    cursor: str | None,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """Read the latest confirmed execution of JOB, including repeated tests.

    Names, outcomes, recorded durations and available failure messages are
    shown together. A failed test is data, not a command error. Missing,
    pending, partial and expired evidence are reported explicitly; an older
    execution is never substituted. Complete describes collected reports.

    Use --json '*' for the complete response or --json FIELDS to select
    fields. Continue with --cursor when pagination.next_cursor is present.
    API refusal or a changed report fails the command without partial output.
    """
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


_RESULT_FIELDS = make_schema(
    "organization_id",
    "repository_id",
    "result_id",
    "status",
    "job_id",
    "execution_id",
    "commit_sha",
    "report_finished_at",
    "data",
)


@tests.command("view")
@click.argument("result_id", metavar="RESULT")
@click.option("--org", "org_id", help="Organization ID or slug. Defaults to the configured organization.")
@click.option(
    "--repo", "repo_id", help="Repository (owner/name or rep-ID). Defaults to AVR_REPO or the current checkout."
)
@json_options
@click.pass_context
def tests_view(
    ctx: click.Context,
    result_id: str,
    org_id: str | None,
    repo_id: str | None,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """Read one exact test result using its result_id from avr tests JOB.

    Reruns never substitute a newer result. Expired reports return an explicit
    status; deleted or inaccessible results fail the command. Use --repo when
    opening a result outside its repository checkout.
    """
    if handle_json_meta(json_fields, jq_expr, _RESULT_FIELDS):
        return
    if not re.fullmatch(r"[A-Za-z0-9_-]{1,1024}", result_id):
        raise click.BadParameter("use the result_id returned by avr tests JOB", param_hint="RESULT")
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    organization = get_org_id(config, org_id, client=client)
    repository = resolve_repo_or_detect(client, config, organization, repo_id, required=True)
    response = _get(
        client,
        f"/orgs/{organization}/repos/{repository}/tests/results/{result_id}",
        "read the test result",
        "Check the result ID, selected organization and repository, and that Repository Tests are enabled.",
    )
    row = response.get("data")
    state = response.get("status")
    if response.get("result_id") != result_id or state not in ("available", "expired", "not_recorded"):
        raise click.ClickException("The API returned invalid test result details.")
    if state == "available":
        if not isinstance(row, dict) or row.get("result_id") != result_id:
            raise click.ClickException("The API returned a different or missing test result.")
    elif row is not None:
        raise click.ClickException("The API returned content for unavailable test evidence.")
    response.update(organization_id=organization, repository_id=repository)
    if json_fields is not None:
        emit_json_record(response, split_fields(json_fields, _RESULT_FIELDS), _RESULT_FIELDS, jq_expr)
        return
    click.echo(f"Result: {_text(result_id)}  Status: {_text(state)}")
    click.echo(f"Organization: {_text(organization)}  Repository: {_text(repository)}")
    if state == "expired":
        click.echo("This report has expired. Test names and failure output are no longer retained.")
        if response.get("commit_sha"):
            click.echo(f"Recorded job commit: {_text(response['commit_sha'])}")
    elif state == "not_recorded":
        click.echo("No retained report is available for this result.")
    else:
        assert isinstance(row, dict)
        click.echo(f"Original job: {_text(response.get('job_id'))}  Execution: {_text(response.get('execution_id'))}")
        click.echo(f"Recorded job commit: {_text(response.get('commit_sha'))}")
        click.echo(f"Report finished: {_text(response.get('report_finished_at'))}")
        click.echo(
            f"Test: {_text(row.get('name'))}  Suite: {_text(row.get('suite'))}  Class: {_text(row.get('class_name'))}"
        )
        click.echo(f"Outcome: {_text(row.get('outcome'))}  Duration (ms): {_text(row.get('duration_ms'))}")
        click.echo(_text(row["failure_summary"]) if row.get("failure_summary") else "No failure summary was recorded.")
