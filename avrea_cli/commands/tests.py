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
from avrea_cli.run_refs import InvalidRunReference
from avrea_cli.run_refs import filter_run_jobs
from avrea_cli.run_refs import parse_run_reference
from avrea_cli.run_refs import resolve_run_org
from avrea_cli.run_refs import resolve_run_reference
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
    "filters",
)
_FIELDS = make_schema("organization_id", "repository_id", *_SNAPSHOT_FIELDS, "data", "pagination")
_MAX_PAGES = 20
_JOB_ID = re.compile(r"(job|awj)-[0-9a-f]{32}")
_INVALID_NEXT_CURSOR = "The API returned an invalid next cursor."


class JobTestsGroup(GhGroup):
    """Accept run/job shorthand without swallowing unknown verbs."""

    def resolve_command(
        self, ctx: click.Context, args: list[str]
    ) -> tuple[str | None, click.Command | None, list[str]]:
        if args and (
            args[0].startswith(("job-", "awj-", "run-", "awr-", "https://"))
            or args[0].isascii()
            and args[0].isdecimal()
        ):
            args = ["list", *args]
        return super().resolve_command(ctx, args)


def _resolve_job(
    client: ApiClient,
    config: CliConfig,
    target: str,
    org_id: str | None,
    job_name: str | None,
) -> tuple[str, str]:
    if target.startswith(("job-", "awj-")):
        if not _JOB_ID.fullmatch(target):
            raise click.BadParameter(
                "use job- or awj- followed by 32 lowercase hex characters", param_hint="RUN_OR_JOB"
            )
        if job_name is not None:
            raise click.UsageError("--job selects a job within a run. Omit it when passing a job ID.")
        return get_org_id(config, org_id, client=client), target
    if job_name is not None and not job_name.strip():
        raise click.BadParameter("use a nonempty job name", param_hint="--job")
    try:
        reference = parse_run_reference(target, api_url=config.public_api_url)
    except (InvalidRunReference, click.BadParameter) as exc:
        hint = (
            "Use an Avrea run or job ID, a positive GitHub run ID, "
            "a GitHub Actions run URL or an Avrea console run URL. "
            "For GitLab jobs, use their Avrea job ID (job- or awj-)."
        )
        raise click.BadParameter(hint, param_hint="RUN_OR_JOB") from exc
    organization = resolve_run_org(client, config, reference, org_id)
    try:
        run = resolve_run_reference(client, organization, reference, include=["jobs"])
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "resolve the run's jobs")
    jobs = run.get("jobs")
    if not isinstance(jobs, list) or not all(isinstance(job, dict) for job in jobs):
        raise click.ClickException("The API returned invalid run jobs.")
    if not jobs:
        raise click.ClickException("This run has no available jobs. Pass an Avrea job ID to inspect a known job.")
    matched = filter_run_jobs(jobs, job_name)
    if len(matched) != 1:
        choices = "\n".join(f"  {_text(job.get('job_name'))}: {_text(job.get('job_id'))}" for job in jobs)
        raise click.ClickException(
            f"{'No jobs' if not matched else 'Several jobs'} match. Use --job NAME to select one job "
            f"or pass its Avrea job ID.\nAvailable jobs:\n{choices}"
        )
    job_id = matched[0].get("job_id")
    if not isinstance(job_id, str) or not _JOB_ID.fullmatch(job_id):
        raise click.ClickException("The selected job has no available Avrea job ID.")
    return organization, job_id


def _get(
    client: ApiClient,
    path: str,
    action: str,
    not_found_hint: str,
    params: dict[str, Any] | None = None,
    *,
    invalid_cursor_hint: str | None = None,
) -> dict[str, Any]:
    try:
        response = client.public_get(path, params=params)
    except httpx.HTTPStatusError as exc:
        hints = {404: not_found_hint}
        if invalid_cursor_hint is not None:
            hints[400] = invalid_cursor_hint
        handle_http_error(exc, action, hints=hints)
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


def _results(
    client: ApiClient, path: str, job: str, limit: int, cursor: str | None, filters: dict[str, str]
) -> dict[str, Any]:
    """Follow short pages within a row and request budget, retaining the resume cursor."""
    rows: list[dict[str, Any]] = []
    snapshot: dict[str, Any] | None = None
    seen = {cursor} if cursor else set()
    for _ in range(_MAX_PAGES):
        page_limit = limit - len(rows)
        params: dict[str, Any] = {"limit": page_limit, **{key: f"eq.{value}" for key, value in filters.items()}}
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
            invalid_cursor_hint="Use the same filters the cursor was issued with, or restart without --cursor.",
        )
        applied = page.get("filters", {})
        if not isinstance(applied, dict):
            raise click.ClickException("The API did not confirm the requested test filters. It may need an update.")
        applied = {key: value for key, value in applied.items() if value is not None}
        if applied != filters:
            raise click.ClickException("The API did not confirm the requested test filters. It may need an update.")
        # Old deployments omit the echo; new ones include nullable fields.
        # Normalize both before checking that later pages use the same query.
        page["filters"] = applied
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
    if response.get("execution_id") is not None:
        click.echo(f"Execution: {_text(response['execution_id'])}")
    click.echo(f"Commit: {_text(response.get('commit_sha'))}")
    click.echo(f"Reporting: {_text(response.get('reporting_state'))}  Results: {_text(response.get('status'))}")
    if response.get("reason"):
        click.echo(f"Reason: {_text(response['reason'])}")
    filtered = any(value is not None for value in (response.get("filters") or {}).values())
    label = "matching shown" if filtered else "shown"
    click.echo(
        f"Retained occurrences (all tests): {_text(response.get('total_count'))}; {label}: {len(response['data'])}"
    )
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
    elif response.get("status") == "available" and response.get("total_count") == 0:
        click.echo("The retained report contains zero test occurrences. This does not establish that no tests ran.")
    elif response.get("status") == "available":
        if filtered:
            click.echo(
                "No matches in the searched portion."
                if response["pagination"]["next_cursor"]
                else "No matching tests in the remaining retained results."
            )
        else:
            click.echo("No more results.")
    else:
        click.echo("No test results returned. Missing or expired reports do not mean that zero tests ran.")
    if response.get("reporting_state") == "complete":
        click.echo("Complete describes the collected reports, not whether every expected test ran.")


@click.group(cls=JobTestsGroup)
@click.pass_context
def tests(ctx: click.Context) -> None:
    """Read a job's tests, timings and recorded failure messages.

    Use avr tests RUN_OR_JOB (or avr tests list RUN_OR_JOB). Accepts Avrea
    run/job IDs, GitHub run IDs and run URLs, and Avrea console run URLs.
    A single-job run is selected automatically. Use --job NAME to select
    one job from a run with several jobs.
    """
    ensure_ctx(ctx)


@tests.command("list")
@click.argument("target", metavar="RUN_OR_JOB")
@click.option("--job", "job_name", help="Select one run job by case-insensitive name substring.")
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
@click.option("--cursor", help="Continue from a previous response's next_cursor with the same filters.")
@click.option("--test", "test_name", help="Exact test name (case-sensitive; no wildcards).")
@click.option("--suite", help="Exact suite name; combined with other filters using AND.")
@click.option("--class", "class_name", help="Exact nonempty class name.")
@click.option(
    "--status", "outcome", type=click.Choice(["passed", "failed", "skipped", "errored"]), help="Recorded test outcome."
)
@json_options
@click.pass_context
def tests_list(
    ctx: click.Context,
    target: str,
    job_name: str | None,
    org_id: str | None,
    repo_id: str | None,
    limit: int,
    cursor: str | None,
    test_name: str | None,
    suite: str | None,
    class_name: str | None,
    outcome: str | None,
    json_fields: str | None,
    jq_expr: str | None,
) -> None:
    """Read one job's latest confirmed execution, including repeated tests.

    RUN_OR_JOB accepts the same run references as avr run view, or an Avrea
    job ID (job- or awj-). A run selects one job, never combines all jobs.
    --job NAME must match exactly one job; without it a run must have one job.

    Names, outcomes, recorded durations and available failure messages are
    shown together. A failed test is data, not a command error. Missing,
    pending, partial and expired evidence are reported explicitly; an older
    execution is never substituted. Complete describes collected reports.

    Use --json '*' for the complete response or --json FIELDS to select
    fields. Continue with --cursor when pagination.next_cursor is present.
    Filters match literally and are combined with AND. Every matching
    occurrence is retained, including duplicate names. A continuation means
    more of the report remains to be searched, not necessarily more matches.
    API refusal or a changed report fails the command without partial output.
    """
    if handle_json_meta(json_fields, jq_expr, _FIELDS):
        return
    filters = {
        key: value
        for key, value in {"name": test_name, "suite": suite, "class_name": class_name, "outcome": outcome}.items()
        if value is not None
    }
    for option, value, maximum in (("--test", test_name, 4096), ("--suite", suite, 256), ("--class", class_name, 512)):
        if value is not None and not 1 <= len(value) <= maximum:
            raise click.BadParameter(f"use between 1 and {maximum} characters", param_hint=option)
    cursor = validate_cursor(cursor, max_length=1024)
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    organization, job = _resolve_job(client, config, target, org_id, job_name)
    repository = (
        resolve_repo(client, config, organization, repo_id) if repo_id else _job_repository(client, organization, job)
    )
    response = _results(
        client, f"/orgs/{organization}/repos/{repository}/jobs/{job}/tests", job, limit, cursor, filters
    )
    response.update(organization_id=organization, repository_id=repository)
    if json_fields is not None:
        emit_json_record(response, split_fields(json_fields, _FIELDS), _FIELDS, jq_expr)
    else:
        _show(response)
    next_cursor = response["pagination"]["next_cursor"]
    if next_cursor:
        message = "Search incomplete; more of the report remains." if filters else "More results available."
        click.echo(f"{message} Continue with the same filters and --cursor {shlex.quote(_text(next_cursor))}", err=True)
