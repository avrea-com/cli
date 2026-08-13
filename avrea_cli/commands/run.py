"""Workflow run CLI commands."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.config import CliConfig
from avrea_cli.display import DIM_FG
from avrea_cli.display import format_conclusion_colored
from avrea_cli.display import format_duration
from avrea_cli.display import get_console_url
from avrea_cli.display import hint as _hint
from avrea_cli.display import hyperlink
from avrea_cli.display import is_piped
from avrea_cli.display import job_url
from avrea_cli.display import notify
from avrea_cli.display import open_or_print_url
from avrea_cli.display import page_output
from avrea_cli.display import print_piped_header
from avrea_cli.display import print_piped_row
from avrea_cli.display import repo_url
from avrea_cli.display import run_url
from avrea_cli.display import status_indicator
from avrea_cli.display import terminal_title
from avrea_cli.display import truncate as _truncate
from avrea_cli.display import workflow_url
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import ensure_prompts_allowed
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import get_org_slug
from avrea_cli.helpers import get_verified_org_slug
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import parse_since
from avrea_cli.helpers import validate_cursor
from avrea_cli.json_output import emit_json
from avrea_cli.json_output import emit_json_record
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import reject_web_with_json
from avrea_cli.json_output import split_fields
from avrea_cli.log_display import fetch_all_logs
from avrea_cli.log_display import follow_logs
from avrea_cli.log_display import print_failed_step_logs
from avrea_cli.log_display import print_logs_grouped
from avrea_cli.output import format_key_value
from avrea_cli.output import format_relative_timestamp
from avrea_cli.repo_context import resolve_repos_or_detect
from avrea_cli.run_diagnostics_display import render_run_diagnostics
from avrea_cli.run_refs import RunReference
from avrea_cli.run_refs import parse_run_reference
from avrea_cli.run_refs import resolve_run_reference
from datetime import UTC
from datetime import datetime
from typing import Any
import click
import httpx
import json
import sys
import time
import webbrowser

# Values accepted by --status on run/job commands. Names are agnostic of
# entity (a job and a run share the same state/conclusion vocabulary on the
# upstream API), so `avr job list --status failure` resolves through the
# same partition function as `avr run list --status failure`.
_PLATFORM_STATES = frozenset({"queued", "in_progress", "completed"})
_PLATFORM_CONCLUSIONS = frozenset(
    {
        "success",
        "failure",
        "neutral",
        "cancelled",
        "skipped",
        "timed_out",
        "action_required",
        "stale",
        "startup_failure",
    }
)


_RUN_LIST_FIELDS = make_schema(
    "run_id",
    "platform_run_id",
    "display_title",
    "status",
    "conclusion",
    "head_branch",
    "head_sha",
    "event",
    "run_number",
    "run_attempt",
    "duration_seconds",
    "created_at",
    "updated_at",
    "workflow_id",
    "workflow",
    "repository",
    "triggering_actor",
)

# View is a strict superset — adding to list propagates here. Split back to
# a hand-maintained mapping if you ever need a list-only entry.
_RUN_VIEW_FIELDS = {**_RUN_LIST_FIELDS, **make_schema("jobs")}


_STATUS_CHOICES = sorted(_PLATFORM_STATES | _PLATFORM_CONCLUSIONS)
"""Unified --status validator used by run/job list. Use as
``type=click.Choice(_STATUS_CHOICES, case_sensitive=False)`` on the
option — Click handles validation + tab completion + the auto-generated
error message, leaving ``_split_status_values`` to just partition the
known-good values."""


def _split_status_values(values: tuple[str, ...]) -> tuple[list[str], list[str]]:
    """Partition unified --status values into (platform_statuses, platform_conclusions).

    Caller must validate via ``click.Choice(_STATUS_CHOICES)`` — this
    function trusts every value is already a known state or conclusion.
    """
    states: list[str] = []
    conclusions: list[str] = []
    for v in values:
        vl = v.lower()
        if vl in _PLATFORM_STATES:
            states.append(vl)
        else:
            conclusions.append(vl)
    return states, conclusions


@click.group(cls=GhGroup)
@click.pass_context
def run(ctx):
    """View and manage GitHub workflow runs."""
    ensure_ctx(ctx)


@run.command("list")
@click.option(
    "--org", "org_id", help="Organization ID or slug. Uses default org if not specified (see: avr config set org)."
)
@click.option(
    "--repo",
    "repository_ids",
    multiple=True,
    help="Filter by repository (org/repo or rep-xxx, repeatable). Auto-detected from git remote if omitted.",
)
@click.option(
    "--status",
    "status_values",
    type=click.Choice(_STATUS_CHOICES, case_sensitive=False),
    multiple=True,
    help="Filter by state (queued, in_progress, completed) or conclusion (success, failure, ...). Repeatable.",
)
@click.option("--branch", "head_branches", multiple=True, help="Filter by head branch (repeatable).")
@click.option("-w", "--workflow", "workflow_ids", multiple=True, help="Filter by workflow ID (wfl-xxx, repeatable).")
@click.option("--since", default=None, help="Relative time window: '7d', '24h', etc. Sugar for --created-after.")
@click.option(
    "--from",
    "--created-after",
    "created_after",
    default=None,
    help="Only runs created after this ISO timestamp.",
)
@click.option(
    "--to",
    "--created-before",
    "created_before",
    default=None,
    help="Only runs created before this ISO timestamp.",
)
@click.option("-L", "--limit", type=click.IntRange(1, 1000), default=20, show_default=True, help="Max runs to return.")
@click.option("--cursor", default=None, help="Pagination cursor from a previous response.")
@click.option(
    "--order",
    type=click.Choice(["created_at.desc", "created_at.asc"]),
    default="created_at.desc",
    show_default=True,
    help="Sort order.",
)
@click.option(
    "--json",
    "json_fields",
    default=None,
    help='Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.',
)
@click.option("-q", "--jq", "jq_expr", default=None, help="Filter --json output through a jq expression.")
@click.option("--web", is_flag=True, help="Open in browser.")
@click.pass_context
def run_list(
    ctx,
    org_id,
    repository_ids,
    status_values,
    head_branches,
    workflow_ids,
    since,
    created_after,
    created_before,
    limit,
    cursor,
    order,
    json_fields,
    jq_expr,
    web: bool,
):
    """List workflow runs for an organization.

    \b
    Examples:
        avr run list
        avr run list --status failure --limit 5
        avr run list --status in_progress --json status,conclusion,head_branch
        avr run list --branch main --status completed
        avr run list --since 24h
        avr run list --json '?'           # list available fields
        avr run list --json '*'           # all fields
        avr run list --json status,conclusion -q '[.[] | select(.status == "completed")]'

    \b
    JSON FIELDS
        conclusion, created_at, display_title, duration_seconds, event,
        head_branch, head_sha, platform_run_id, repository, run_attempt, run_id,
        run_number, status, triggering_actor, updated_at, workflow, workflow_id
    """
    reject_web_with_json(json_fields, web)
    if handle_json_meta(json_fields, jq_expr, _RUN_LIST_FIELDS):
        return
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)

    if web:
        slug = get_org_slug(client, org_id)
        console_url = get_console_url(config.public_api_url)
        url = f"{console_url}/org/{slug}?view=workflows"
        open_or_print_url(url)
        return

    repository_ids = tuple(resolve_repos_or_detect(client, config, org_id, repository_ids, soft_detect=True))

    # --since is sugar for --created-after; reject double-spec rather than
    # silently picking one — accidental combinations are usually mistakes.
    if since and (created_after or created_before):
        raise click.UsageError("--since cannot be combined with --created-after or --created-before")
    if since:
        created_after = parse_since(since).isoformat()

    if created_after:
        try:
            created_after = datetime.fromisoformat(created_after).isoformat()
        except ValueError as err:
            click.echo("Error: --created-after must be a valid ISO-8601 timestamp", err=True)
            raise click.Abort() from err
    if created_before:
        try:
            created_before = datetime.fromisoformat(created_before).isoformat()
        except ValueError as err:
            click.echo("Error: --created-before must be a valid ISO-8601 timestamp", err=True)
            raise click.Abort() from err

    platform_statuses, platform_conclusions = _split_status_values(status_values)
    cursor = validate_cursor(cursor)

    params: dict[str, Any] = {"limit": limit, "order": order, "include": ["workflow"]}
    if repository_ids:
        params["repository_ids"] = list(repository_ids)
    if platform_statuses:
        params["statuses"] = platform_statuses
    if platform_conclusions:
        params["conclusions"] = platform_conclusions
    if head_branches:
        params["head_branches"] = list(head_branches)
    if workflow_ids:
        params["workflow_ids"] = list(workflow_ids)
    if created_after:
        params["created_after"] = created_after
    if created_before:
        params["created_before"] = created_before
    if cursor:
        params["cursor"] = cursor

    try:
        response = client.public_get(f"/orgs/{org_id}/workflow-runs", params=params)
        runs = response.get("data") or []
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list workflow runs")

    if json_fields is not None:
        emit_json(list(runs), split_fields(json_fields, _RUN_LIST_FIELDS), _RUN_LIST_FIELDS, jq_expr)
        return

    if not runs:
        click.echo("No workflow runs found.")
        if since or repository_ids or workflow_ids:
            click.echo(
                click.style(
                    "  Try: --since 90d, drop --repo/--workflow, or check `avr run list --json '*' --jq '. | length'`.",
                    dim=True,
                ),
                err=True,
            )
        return

    if is_piped():
        # Tab-separated output for cut/awk/grep, with a header row so scripts
        # can index by column name rather than memorizing positional order.
        print_piped_header(
            ["status", "title", "workflow", "branch", "event", "run_id", "duration_seconds", "created_at"]
        )
        for r in runs:
            status = r.get("conclusion") or r.get("status") or "unknown"
            print_piped_row(
                [
                    status,
                    r.get("display_title", ""),
                    (r.get("workflow") or {}).get("name", ""),
                    r.get("head_branch") or "",
                    r.get("event", ""),
                    r.get("run_id", ""),
                    r.get("duration_seconds"),
                    r.get("created_at", ""),
                ]
            )
        return

    W = {"title": 50, "wf": 30, "branch": 30, "event": 18, "elapsed": 12, "age": 12, "id": 36}
    s = " "

    def _hdr(label: str, width: int) -> str:
        return click.style(f"{label:{width}s}", fg=DIM_FG, underline=True)

    click.echo(
        f"  {_hdr('TITLE', W['title'])}{s}{_hdr('WORKFLOW', W['wf'])}{s}{_hdr('BRANCH', W['branch'])}{s}"
        f"{_hdr('EVENT', W['event'])}{s}{_hdr('ID', W['id'])}{s}"
        f"{_hdr('ELAPSED', W['elapsed'])}{s}{_hdr('AGE', W['age'])}"
    )
    # OSC 8 link context — slug lookup is best-effort, one extra API call.
    links_enabled = ctx.obj.get("links_enabled", False)
    console_url = get_console_url(config.public_api_url) if links_enabled else ""
    slug = get_org_slug(client, org_id) if links_enabled else ""
    for r in runs:
        ind = status_indicator(r.get("status", "unknown"), r.get("conclusion"))
        title = f"{_truncate(r.get('display_title', ''), W['title'] - 2):{W['title']}s}"
        wf = f"{_truncate((r.get('workflow') or {}).get('name', ''), W['wf'] - 2):{W['wf']}s}"
        head_branch = r.get("head_branch") or ""
        br = f"{_truncate(head_branch, W['branch'] - 2):{W['branch']}s}"
        event = f"{r.get('event', ''):{W['event']}s}"
        elapsed = f"{format_duration(r.get('duration_seconds')):>{W['elapsed']}s}"
        age = f"{format_relative_timestamp(r.get('created_at')):>{W['age']}s}"
        r_id = r.get("run_id", "")
        repo_full = (r.get("repository") or {}).get("full_name", "")
        # Pad-then-style — f-string width counts ANSI escape bytes,
        # breaking column alignment if applied first.
        title_cell = title
        wf_cell = click.style(wf, fg="magenta")
        br_cell = click.style(br, bold=True)
        r_id_cell = click.style(f"{r_id:{W['id']}s}", fg="cyan")
        if links_enabled and r_id:
            url = run_url(console_url, slug, r_id)
            title_cell = hyperlink(title, url)
            wf_cell = hyperlink(wf_cell, url)
            r_id_cell = hyperlink(r_id_cell, url)
        if links_enabled and head_branch and repo_full and "/" in repo_full:
            br_cell = hyperlink(br_cell, f"https://github.com/{repo_full}/tree/{head_branch}")
        click.echo(
            f"{ind} {title_cell}{s}"
            f"{wf_cell}{s}"
            f"{br_cell}{s}"
            f"{event}{s}{r_id_cell}{s}"
            f"{elapsed}{s}{click.style(age, dim=True)}"
        )

    next_cursor = response.get("pagination", {}).get("next_cursor")
    if next_cursor:
        click.echo(f"\nMore results available. Next page: --cursor {next_cursor}", err=True)


def _print_run_jobs(
    jobs_list: list[dict[str, Any]],
    *,
    verbose: bool = False,
    console_url: str = "",
    slug: str = "",
) -> None:
    """Print job list with status indicators. If verbose, include steps.

    When ``console_url`` and ``slug`` are non-empty, each job_name is wrapped
    in an OSC 8 hyperlink to the console job page. Caller is responsible for
    only passing them when ``ctx.obj['links_enabled']`` is true.

    Returns early on empty input so callers can call unconditionally without
    the awkward 'JOBS' header above an empty section."""
    if not jobs_list:
        return
    click.echo()
    click.echo(click.style("JOBS", bold=True, fg="bright_white"))
    click.echo()
    for j in jobs_list:
        j_state = j.get("state", "unknown")
        j_conclusion = j.get("conclusion")
        j_indicator = status_indicator(j_state, j_conclusion)
        j_dur = format_duration(j.get("duration_seconds"))
        if j_state == "in_progress":
            started = j.get("started_at")
            if started:
                try:
                    started_dt = datetime.fromisoformat(started.replace("Z", "+00:00"))
                    elapsed = (datetime.now(UTC) - started_dt).total_seconds()
                    j_dur = f"running {format_duration(elapsed)}"
                except ValueError, TypeError:
                    j_dur = "running"
            else:
                j_dur = "running"
        j_name = j.get("job_name", "?")
        j_id = j.get("job_id") or ""
        j_id_cell = click.style(j_id, fg="cyan") if j_id else ""
        if console_url and slug and j_id:
            url = job_url(console_url, slug, j_id)
            j_name = hyperlink(j_name, url)
            j_id_cell = hyperlink(j_id_cell, url)
        suffix = f"  {j_id_cell}" if j_id_cell else ""
        click.echo(f"  {j_indicator}  {j_name} ({j_dur}){suffix}")

        if verbose:
            for step in j.get("steps") or []:
                s_status = step.get("status", "pending")
                s_conclusion = step.get("conclusion")
                s_indicator = status_indicator(s_status, s_conclusion)
                s_dur = ""
                if step.get("started_at") and step.get("completed_at"):
                    try:
                        s_started = datetime.fromisoformat(step["started_at"].replace("Z", "+00:00"))
                        s_completed = datetime.fromisoformat(step["completed_at"].replace("Z", "+00:00"))
                        s_dur = f" ({format_duration((s_completed - s_started).total_seconds())})"
                    except ValueError, TypeError:
                        pass
                click.echo(f"       {s_indicator}  {step.get('name', '?')}{s_dur}")
            click.echo("")


def _run_reference_and_org(
    client: ApiClient,
    config: CliConfig,
    value: str,
    org_id: str | None,
) -> tuple[RunReference, str]:
    """Parse RUN and resolve/verify the organization embedded in a URL."""
    reference = parse_run_reference(value, api_url=config.public_api_url)
    if reference.organization_slug is not None and org_id is None:
        org_id = reference.organization_slug
    resolved_org_id = get_org_id(config, org_id, client=client)

    if reference.organization_slug is not None:
        active_org_slug = get_verified_org_slug(client, resolved_org_id)
        if active_org_slug.casefold() != reference.organization_slug.casefold():
            raise click.ClickException(
                f"Avrea run URL organization {reference.organization_slug!r} does not match "
                f"the selected organization {active_org_slug!r}."
            )
    return reference, resolved_org_id


@run.command("view")
@click.argument("run", required=False)
@click.option("--org", "org_id", help="Organization ID or slug.")
@click.option(
    "--steps",
    "show_steps",
    is_flag=True,
    help="Expand each job to show its individual steps.",
)
@click.option("--log", "show_log", is_flag=True, help="Print full logs for all jobs.")
@click.option("--log-failed", is_flag=True, help="Print logs only for failed steps.")
@click.option("--job", "job_name_filter", help="Restrict view and logs to jobs whose name contains this string.")
@click.option(
    "--json",
    "json_fields",
    default=None,
    help='Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.',
)
@click.option("-q", "--jq", "jq_expr", default=None, help="Filter --json output through a jq expression.")
@click.option("--web", is_flag=True, help="Open in browser.")
@click.option(
    "--no-pager",
    is_flag=True,
    help="Print logs directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.",
)
@click.pass_context
def run_view(
    ctx,
    run: str | None,
    org_id,
    show_steps: bool,
    show_log: bool,
    log_failed: bool,
    job_name_filter: str | None,
    json_fields: str | None,
    jq_expr: str | None,
    web: bool,
    no_pager: bool,
):
    """View a workflow run with its jobs.

    \b
    RUN accepts an Avrea run ID, a positive GitHub run ID, a GitHub Actions
    run URL, or an Avrea console run URL. Without RUN, shows 10 most recent
    runs.

    \b
    Examples:
        avr run view
        avr run view run-abc123
        avr run view 123456789
        avr run view https://github.com/acme/widgets/actions/runs/123456789
        avr run view run-abc123 --steps
        avr run view run-abc123 --log-failed
        avr run view run-abc123 --job Build
        avr run view run-abc123 --json conclusion,jobs
        avr run view run-abc123 --json '*' --jq '.jobs[].job_name'

    \b
    JSON FIELDS
        conclusion, created_at, display_title, duration_seconds, event,
        head_branch, head_sha, jobs, platform_run_id, repository, run_attempt,
        run_id, run_number, status, triggering_actor, updated_at, workflow,
        workflow_id
    """
    reject_web_with_json(json_fields, web)
    if handle_json_meta(json_fields, jq_expr, _RUN_VIEW_FIELDS):
        return
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    if run:
        reference, org_id = _run_reference_and_org(client, config, run, org_id)
    else:
        reference = None
        org_id = get_org_id(config, org_id, client=client)

    if not run:
        try:
            response = client.public_get(
                f"/orgs/{org_id}/workflow-runs",
                params={"limit": 10, "order": "created_at.desc", "include": ["jobs"]},
            )
            runs = response.get("data") or []
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "list workflow runs")

        if not runs:
            click.echo("No workflow runs found.")
            return

        for r in runs:
            r_status = r.get("status", "unknown")
            r_conclusion = r.get("conclusion")
            r_indicator = status_indicator(r_status, r_conclusion)
            r_dur = format_duration(r.get("duration_seconds"))
            repo = (r.get("repository") or {}).get("full_name", "")
            title = r.get("display_title", "")
            branch = r.get("head_branch") or ""
            r_id = r.get("run_id", "")
            created = format_relative_timestamp(r.get("created_at"))
            click.echo(f"  {r_indicator}  {repo} {title} -- {branch} -- {r_dur} -- {created}")
            click.echo(click.style(f"     {r_id}", fg=DIM_FG))

        click.echo("\nTo view a run, try: avr run view <run-id>", err=True)
        return

    include = ["jobs", "workflow"]
    try:
        if reference is None:
            raise click.ClickException("A run reference is required.")
        run_data = resolve_run_reference(client, org_id, reference, include=include)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "get workflow run")

    resolved_run_id = run_data.get("run_id")
    if not isinstance(resolved_run_id, str):
        raise click.ClickException("Avrea returned a workflow run without a run_id.")
    run_id = resolved_run_id

    if json_fields is not None:
        emit_json_record(run_data, split_fields(json_fields, _RUN_VIEW_FIELDS), _RUN_VIEW_FIELDS, jq_expr)
        return

    if web:
        repo = (run_data.get("repository") or {}).get("full_name", "")
        slug = get_org_slug(client, org_id)
        console_url = get_console_url(config.public_api_url)
        avrea_url = f"{console_url}/org/{slug}/runs/{run_id}"
        click.echo(f"Avrea:  {avrea_url}")
        platform_run_id = run_data.get("platform_run_id")
        run_attempt = run_data.get("run_attempt")
        if repo and platform_run_id:
            gh_url = f"https://github.com/{repo}/actions/runs/{platform_run_id}"
            if run_attempt and run_attempt > 1:
                gh_url += f"/attempts/{run_attempt}"
            click.echo(f"GitHub: {gh_url}")
        if sys.stdout.isatty():
            webbrowser.open(avrea_url)
        return

    status = run_data.get("status", "unknown")
    conclusion = run_data.get("conclusion")
    indicator = status_indicator(status, conclusion)
    conclusion_display = f"{indicator} {conclusion}" if conclusion else status

    repo_data = run_data.get("repository") or {}
    repo = repo_data.get("full_name", "-")
    repo_id = repo_data.get("id")
    head_sha_full = run_data.get("head_sha") or ""
    head_sha = head_sha_full[:8]
    branch = run_data.get("head_branch") or "-"
    actor_data = run_data.get("triggering_actor") or {}
    actor = actor_data.get("login") or actor_data.get("platform_login") or "-"
    wf_data = run_data.get("workflow") or {}
    wf_name = wf_data.get("name", "")
    wf_path = (wf_data.get("path") or "").rsplit("/", 1)[-1]  # e.g. ".github/workflows/build.yml" → "build.yml"
    workflow = f"{wf_name} ({wf_path})" if wf_name and wf_path else wf_name
    run_number = run_data.get("run_number", "?")
    run_attempt = run_data.get("run_attempt", 1)

    # OSC 8 link context. Github repos always have an `org/name` shape;
    # gate platform-specific links on `/` so a future non-github repo
    # skips them gracefully.
    links_enabled = ctx.obj.get("links_enabled", False)
    link_console_url = get_console_url(config.public_api_url) if links_enabled else ""
    link_slug = get_org_slug(client, org_id) if links_enabled else ""
    is_github = repo and "/" in repo

    title_text = run_data.get("display_title", "-")
    branch_cell = branch
    commit_cell = head_sha or "-"
    workflow_cell = workflow or "-"
    repo_cell = repo
    title_cell = title_text
    if links_enabled and link_console_url and link_slug:
        title_cell = hyperlink(title_text, run_url(link_console_url, link_slug, run_id))
        if wf_data.get("workflow_id") and workflow:
            workflow_cell = hyperlink(workflow_cell, workflow_url(link_console_url, link_slug, wf_data["workflow_id"]))
        if repo_id and repo != "-":
            repo_cell = hyperlink(repo_cell, repo_url(link_console_url, link_slug, repo_id))
    if links_enabled and is_github:
        if branch != "-":
            branch_cell = hyperlink(branch_cell, f"https://github.com/{repo}/tree/{branch}")
        if head_sha_full:
            commit_cell = hyperlink(commit_cell, f"https://github.com/{repo}/commit/{head_sha_full}")

    click.echo(
        format_key_value(
            {
                "Title": title_cell,
                "Conclusion": conclusion_display,
                "Branch": branch_cell,
                "Commit": commit_cell,
                "Event": run_data.get("event", "-"),
                "Actor": actor,
                "Duration": format_duration(run_data.get("duration_seconds")),
                "Run": f"#{run_number} (attempt {run_attempt})",
                "Workflow": workflow_cell,
                "Repository": repo_cell,
            }
        )
    )

    run_jobs = run_data.get("jobs") or []
    if job_name_filter:
        needle = job_name_filter.lower()
        matched = [j for j in run_jobs if needle in (j.get("job_name") or "").lower()]
        if not matched:
            click.echo(f"No job matching '{job_name_filter}' in this run.", err=True)
            return
        run_jobs = matched

    if run_jobs:
        links_enabled = ctx.obj.get("links_enabled", False)
        link_console_url = get_console_url(config.public_api_url) if links_enabled else ""
        link_slug = get_org_slug(client, org_id) if links_enabled else ""
        _print_run_jobs(run_jobs, verbose=show_steps, console_url=link_console_url, slug=link_slug)

    if show_log or log_failed:
        log_links_enabled = ctx.obj.get("links_enabled", False)
        log_console_url = get_console_url(config.public_api_url) if log_links_enabled else ""
        log_slug = get_org_slug(client, org_id) if log_links_enabled else ""
        # OSC 133 marks omitted here: output goes through `less`, which
        # doesn't strip OSC sequences, so they'd leak as visible bytes.
        log_buf: list[str] = []
        log_emit = log_buf.append
        for j in run_jobs:
            j_id = j.get("job_id")
            j_steps = j.get("steps") or []
            if not j_id:
                continue
            if log_failed and j.get("conclusion") == "success":
                continue
            j_url = job_url(log_console_url, log_slug, j_id) if log_console_url and log_slug else None
            header = click.style(f"=== {j.get('job_name', '?')} ({j_id}) ===", bold=True)
            log_emit(f"\n{'=' * 60}")
            log_emit(hyperlink(header, j_url) if j_url else header)
            if log_failed:
                print_failed_step_logs(client, j_id, j_steps, emit=log_emit, link_url=j_url)
            else:
                entries = fetch_all_logs(client, j_id)
                print_logs_grouped(entries, emit=log_emit, link_url=j_url)
        page_output("\n".join(log_buf), bypass=no_pager)
    elif run_jobs and len(run_jobs) == 1:
        j_id = run_jobs[0].get("job_id", "")
        click.echo(f"\nTo view the job, try: avr job view {j_id}", err=True)
    elif run_jobs:
        click.echo("\nTo view a job, try: avr job view <job-id>", err=True)

    _print_other_attempts(client, org_id, run_id, run_data)

    slug = get_org_slug(client, org_id)
    console_url = get_console_url(config.public_api_url)
    _hint(f"View this run on Avrea: {console_url}/org/{slug}/runs/{run_id}")


@run.command("diagnose")
@click.argument("run")
@click.option("--org", "org_id", help="Organization ID or slug.")
@click.option("--json", "json_output", is_flag=True, help="Output the diagnostic report as JSON.")
@click.pass_context
def run_diagnose(ctx, run: str, org_id: str | None, json_output: bool) -> None:
    """Explain a failed or unexpectedly slow workflow run.

    RUN accepts the same Avrea IDs, GitHub run IDs, and run URLs as
    `avr run view`. The report combines jobs and failed steps, bounded
    failed-job log tails, queue/execution timings, runner metrics, and a
    prior-success workflow baseline.

    \b
    Examples:
        avr run diagnose run-abc123
        avr run diagnose 123456789 --json
        avr run diagnose https://github.com/acme/widgets/actions/runs/123456789
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    reference, org_id = _run_reference_and_org(client, config, run, org_id)
    if reference.run_id is not None:
        run_id = reference.run_id
    else:
        try:
            resolved = resolve_run_reference(client, org_id, reference, include=[])
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "resolve workflow run")
        run_id = resolved.get("run_id")
        if not isinstance(run_id, str):
            raise click.ClickException("Avrea returned a workflow run without a run_id.")

    try:
        response = client.public_get(f"/orgs/{org_id}/workflow-runs/{run_id}/diagnostics")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "diagnose workflow run")

    report = response.get("data", response)
    if not isinstance(report, dict):
        raise click.ClickException("Avrea returned an invalid diagnostics response.")
    if json_output:
        click.echo(json.dumps(report, indent=2, default=str))
        return
    click.echo(render_run_diagnostics(report))


def _print_other_attempts(client: ApiClient, org_id: str, run_id: str, run_data: dict[str, Any]) -> None:
    """List sibling attempts of the workflow run currently being viewed.

    Each Avrea ``run_id`` represents one (workflow_run, attempt) pair, so a
    re-run produces a separate row sharing the same ``platform_run_id``. List
    the others so users can navigate to them without leaving the CLI.
    """
    platform_run_id = run_data.get("platform_run_id")
    if not platform_run_id:
        return
    try:
        response = client.public_get(
            f"/orgs/{org_id}/workflow-runs",
            params={"platform_run_id": platform_run_id, "limit": 10, "order": "created_at.desc"},
        )
    except httpx.HTTPStatusError as exc:
        # Don't fail the whole `run view` — siblings are auxiliary. Surface
        # the cause to stderr so users know the section is missing data.
        _hint(f"(could not load other attempts: HTTP {exc.response.status_code})")
        return
    except (httpx.ConnectError, httpx.TimeoutException) as exc:
        _hint(f"(could not load other attempts: {type(exc).__name__})")
        return
    siblings = [r for r in response.get("data", []) if r.get("run_id") != run_id]
    if not siblings:
        return
    click.echo()
    click.echo(click.style("OTHER ATTEMPTS", bold=True, fg="bright_white"))
    click.echo()
    for sib in siblings:
        ind = status_indicator(sib.get("status", "unknown"), sib.get("conclusion"))
        attempt_n = sib.get("run_attempt", "?")
        duration = format_duration(sib.get("duration_seconds"))
        sib_id = sib.get("run_id", "")
        click.echo(f"  {ind}  attempt {attempt_n:<3}  {duration:>8s}   {click.style(sib_id, fg='cyan')}")


def _emit_ndjson_event(event: dict[str, Any]) -> None:
    """Write a single NDJSON event to stdout. Swallows BrokenPipeError so
    piping into ``head``/``jq`` exits cleanly instead of dumping a stack
    trace."""
    try:
        sys.stdout.write(json.dumps(event, default=str) + "\n")
        sys.stdout.flush()
    except BrokenPipeError:
        # Detach stdout so any later writes/flushes (e.g. atexit) don't try
        # again and re-trigger the same error.
        try:
            sys.stdout.close()
        except BrokenPipeError, OSError:
            pass
        raise SystemExit(0) from None


def _job_event_payload(event: str, run_id: str, job: dict[str, Any], timestamp: str) -> dict[str, Any]:
    return {
        "event": event,
        "timestamp": timestamp,
        "run_id": run_id,
        "avrea_job_id": job.get("job_id"),
        "platform_job_id": job.get("platform_job_id"),
        "job_name": job.get("job_name"),
        "state": job.get("state"),
        "conclusion": job.get("conclusion"),
    }


def watch_run_loop_ndjson(client: ApiClient, org_id: str, run_id: str, *, interval: int, exit_status: bool) -> None:
    """Poll a workflow run and emit NDJSON events on state transitions.

    One JSON object per line, designed for piping into automation. Three
    event kinds, with intentionally different payloads:

    - ``job_started`` / ``job_completed`` — emitted via ``_job_event_payload``
      and share the keys ``event``, ``timestamp``, ``run_id``,
      ``avrea_job_id``, ``platform_job_id``, ``job_name``, ``state``,
      ``conclusion``. ``conclusion`` is ``null`` until ``job_completed``.

    - ``run_completed`` — top-level run summary; carries ``event``,
      ``timestamp``, ``run_id``, ``status``, ``conclusion``,
      ``duration_seconds``. It deliberately omits the per-job fields —
      consumers correlate via ``run_id`` to the preceding ``job_*`` events.

    The shape is asymmetric on purpose: padding the run-level event with
    null per-job fields would suggest those fields are sometimes populated."""
    last_state: dict[str, str] = {}
    consecutive_failures = 0
    max_consecutive_failures = 5

    def record_failure(reason: str, msg: str) -> None:
        """Emit a transient_error event and abort with watch_aborted after
        ``max_consecutive_failures`` strikes — bounded retry so a stuck
        consumer doesn't loop forever on a dead network."""
        nonlocal consecutive_failures
        consecutive_failures += 1
        ts = datetime.now(UTC).isoformat().replace("+00:00", "Z")
        _emit_ndjson_event(
            {
                "event": "transient_error",
                "timestamp": ts,
                "run_id": run_id,
                "reason": reason,
                "consecutive_failures": consecutive_failures,
            }
        )
        click.echo(msg, err=True)
        if consecutive_failures >= max_consecutive_failures:
            _emit_ndjson_event({"event": "watch_aborted", "timestamp": ts, "run_id": run_id, "reason": reason})
            sys.exit(1)

    while True:
        try:
            response = client.public_get(
                f"/orgs/{org_id}/workflow-runs/{run_id}",
                params={"include": ["jobs"]},
            )
            run_data = response.get("data", response)
            consecutive_failures = 0
        except httpx.HTTPStatusError as exc:
            # 4xx is terminal (auth lost, run deleted, etc.) — same as the
            # non-JSON watch loop. Looping forever on a permanent failure
            # would leave the consumer hanging without a useful event stream.
            if exc.response.status_code < 500 and exc.response.status_code != 429:
                handle_http_error(exc, "get workflow run")
            record_failure(
                f"http_{exc.response.status_code}",
                f"Error fetching run (HTTP {exc.response.status_code}); retrying.",
            )
            time.sleep(interval)
            continue
        except (httpx.ConnectError, httpx.TimeoutException) as exc:
            record_failure(type(exc).__name__, "Couldn't reach Avrea; retrying.")
            time.sleep(interval)
            continue

        now = datetime.now(UTC).isoformat().replace("+00:00", "Z")
        for j in run_data.get("jobs") or []:
            j_id = j.get("job_id")
            if not j_id:
                continue
            j_state = j.get("state", "unknown")
            prev = last_state.get(j_id)
            if prev != j_state:
                last_state[j_id] = j_state
                if j_state == "in_progress":
                    _emit_ndjson_event(_job_event_payload("job_started", run_id, j, now))
                elif j_state == "completed":
                    _emit_ndjson_event(_job_event_payload("job_completed", run_id, j, now))

        status = run_data.get("status", "unknown")
        if status == "completed":
            _emit_ndjson_event(
                {
                    "event": "run_completed",
                    "timestamp": now,
                    "run_id": run_id,
                    "status": status,
                    "conclusion": run_data.get("conclusion"),
                    "duration_seconds": run_data.get("duration_seconds"),
                }
            )
            if exit_status and run_data.get("conclusion") != "success":
                sys.exit(1)
            return

        time.sleep(interval)


def _active_job_name(jobs: list[dict[str, Any]]) -> str | None:
    """Pick the most informative job name for the terminal title.

    Prefers in-progress jobs over queued (the user wants to know what's
    *running*); ties broken by ``started_at`` so the oldest active job
    surfaces first. Returns None when nothing's active or queued — caller
    falls back to a progress fraction or the run state."""
    in_progress = [j for j in jobs if j.get("state") == "in_progress"]
    if in_progress:
        in_progress.sort(key=lambda j: j.get("started_at") or "")
        return in_progress[0].get("job_name")
    queued = [j for j in jobs if j.get("state") == "queued"]
    if queued:
        return queued[0].get("job_name")
    return None


def _watch_title(run_data: dict[str, Any]) -> str:
    """Render a tab-friendly terminal title for the watched run.

    Format: ``avr ▸ <workflow>: <stage>`` where ``<stage>`` is the active
    job name when something's running, the conclusion on completion, or
    a progress fraction / run state as fallback. ``avr ▸`` prefix marks
    the tab as belonging to this CLI; the workflow name then disambiguates
    multiple concurrent watch tabs."""
    wf_name = ((run_data.get("workflow") or {}).get("name") or "run").strip()
    jobs = run_data.get("jobs") or []
    status = run_data.get("status", "unknown")
    conclusion = run_data.get("conclusion")
    if status == "completed":
        stage = conclusion or "done"
    elif (active := _active_job_name(jobs)) is not None:
        stage = active
    elif jobs:
        completed = sum(1 for j in jobs if j.get("state") == "completed")
        stage = f"{completed}/{len(jobs)} jobs done"
    else:
        stage = status
    return f"avr ▸ {wf_name}: {stage}"


def watch_run_loop(
    client: ApiClient,
    org_id: str,
    run_id: str,
    *,
    interval: int,
    exit_status: bool,
    console_url: str = "",
    slug: str = "",
) -> None:
    """Poll a workflow run until it completes, clearing+redrawing each tick.

    Shared by `avr run watch` and `avr workflow run --watch`. When
    ``console_url`` and ``slug`` are non-empty, JOBS-table job names are
    OSC 8 hyperlinked — clicking opens the job in the console without
    interrupting the redraw loop. The terminal title (OSC 2) is updated
    each tick with progress so users with many tabs can spot the run
    they're watching.
    """
    with terminal_title("avr ▸ run: starting") as term_title:
        try:
            while True:
                try:
                    response = client.public_get(
                        f"/orgs/{org_id}/workflow-runs/{run_id}",
                        # ``include=workflow`` so the title can show the
                        # workflow name (``avr ▸ tools: sleep``) rather
                        # than the opaque short run id.
                        params={"include": ["jobs", "workflow"]},
                    )
                    run_data = response.get("data", response)
                except httpx.HTTPStatusError as exc:
                    if exc.response.status_code < 500 and exc.response.status_code != 429:
                        handle_http_error(exc, "get workflow run")
                    # Don't clear — keep the last frame so the user can still
                    # see job progress while we retry. Status footer on stderr
                    # tells them what's going on.
                    click.echo(
                        click.style(f"  [retry — HTTP {exc.response.status_code}]", fg="yellow"),
                        err=True,
                    )
                    term_title.set("avr ▸ run: error")
                    time.sleep(interval)
                    continue
                except (httpx.ConnectError, httpx.TimeoutException) as exc:
                    click.echo(
                        click.style(f"  [retry — couldn't reach Avrea: {type(exc).__name__}]", fg="yellow"),
                        err=True,
                    )
                    term_title.set("avr ▸ run: network error")
                    time.sleep(interval)
                    continue

                click.clear()
                now_str = datetime.now(UTC).strftime("%H:%M:%S UTC")
                title = run_data.get("display_title", "")
                run_number = run_data.get("run_number", "?")
                status = run_data.get("status", "unknown")
                conclusion = run_data.get("conclusion")

                header = f"Watching run #{run_number}: {title} -- {now_str}"
                if console_url and slug:
                    header = hyperlink(header, run_url(console_url, slug, run_id))
                click.echo(header + "\n")

                run_jobs = run_data.get("jobs") or []
                if run_jobs:
                    _print_run_jobs(run_jobs, console_url=console_url, slug=slug)
                    # Hints for drilling into a job. Static lines (job ids
                    # vary across rows), but the verbs are the demo flow.
                    click.echo()
                    click.echo(
                        click.style(
                            "  → avr job logs --follow <job-id>    # tail logs of a running job",
                            fg=DIM_FG,
                        )
                    )
                    click.echo(
                        click.style(
                            "  → avr job metrics --watch <job-id>  # live CPU/mem/IO gauges",
                            fg=DIM_FG,
                        )
                    )
                    click.echo(
                        click.style(
                            "  → avr job ssh <job-id>              # ssh into the runner VM",
                            fg=DIM_FG,
                        )
                    )
                else:
                    click.echo("  No jobs yet.")

                # Update the terminal title each tick. ``<workflow>: <active
                # job>`` while running, ``<workflow>: <conclusion>`` on done.
                term_title.set(_watch_title(run_data))

                if status == "completed":
                    dur = format_duration(run_data.get("duration_seconds"))
                    conclusion_str = format_conclusion_colored(status, conclusion)
                    click.echo(f"\nRun #{run_number} completed: {conclusion_str} ({dur})")
                    # Reset title to bare ``avr ▸ <workflow>`` before notify
                    # so the conclusion isn't echoed twice (once via Ghostty's
                    # source line, once in the notification body).
                    wf_name = ((run_data.get("workflow") or {}).get("name") or "run").strip()
                    term_title.set(f"avr ▸ {wf_name}")
                    notify(f"Run #{run_number} {title}: {conclusion or 'completed'}")
                    if exit_status and conclusion != "success":
                        sys.exit(1)
                    return

                time.sleep(interval)
        except KeyboardInterrupt:
            click.echo("\nStopped watching.")


@run.command("watch")
@click.argument("run_id", required=False)
@click.option("--org", "org_id", help="Organization ID or slug.")
@click.option(
    "--repo",
    "repo_options",
    multiple=True,
    help="Scope the auto-select to a repo (org/name or rep-xxx, repeatable). Auto-detected from git remote if omitted.",
)
@click.option("--exit-status", is_flag=True, help="Exit non-zero if run failed.")
@click.option("--interval", type=int, default=3, show_default=True, help="Refresh interval in seconds.")
@click.option(
    "--ndjson",
    "ndjson_output",
    is_flag=True,
    help="Force NDJSON event stream (default when stdout isn't a TTY).",
)
@click.pass_context
def run_watch(
    ctx,
    run_id: str | None,
    org_id,
    repo_options: tuple[str, ...],
    exit_status: bool,
    interval: int,
    ndjson_output: bool,
):
    """Watch a workflow run until it completes.

    \b
    Without RUN_ID, auto-selects the latest in-progress run. Pass --repo
    (repeatable) to scope the auto-select to specific repositories.

    \b
    Examples:
        avr run watch
        avr run watch --repo acme/web
        avr run watch --repo a/x --repo b/y
        avr run watch run-abc123 --exit-status
        avr run watch run-abc123 --ndjson | jq -c .
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    if interval < 1:
        click.echo("Error: --interval must be at least 1 second.", err=True)
        raise click.Abort()

    org_id = get_org_id(config, org_id, client=client)

    if not run_id:
        click.echo(click.style("Looking for an in-progress run…", dim=True), err=True)
        params: dict[str, Any] = {
            "limit": 1,
            "order": "created_at.desc",
            "statuses": ["in_progress", "queued"],
        }
        # Auto-select must scope to --repo / AVR_REPO / git auto-detect if
        # any are present, so we don't silently watch an unrelated repo's run.
        # Only the "no git dir" auto-detect failure is swallowed — explicit
        # user choices propagate the error.
        if repo_options or config.repo_override:
            repo_ids = resolve_repos_or_detect(client, config, org_id, repo_options)
        else:
            try:
                repo_ids = resolve_repos_or_detect(client, config, org_id, ())
            except click.ClickException:
                repo_ids = []
        if repo_ids:
            params["repository_ids"] = list(repo_ids)
        try:
            response = client.public_get(f"/orgs/{org_id}/workflow-runs", params=params)
            runs = response.get("data") or []
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "list workflow runs")

        if not runs:
            scope = " for these repos" if repo_ids else ""
            click.echo(f"No in-progress workflow runs found{scope}.")
            return
        run_id = runs[0]["run_id"]
        title = runs[0].get("display_title", "")
        click.echo(f"Auto-selected: {run_id} ({title})", err=True)

    # Default to NDJSON off-TTY so `avr run watch | jq` works without an
    # explicit flag; the redrawing TUI would otherwise produce unparseable
    # output to the pipe consumer. Named --ndjson (not --json) because watch
    # streams events, not records — the asymmetry with --json is intentional.
    if ndjson_output or not sys.stdout.isatty():
        watch_run_loop_ndjson(client, org_id, run_id, interval=interval, exit_status=exit_status)
    else:
        # TUI pre-check: an already-finished run would flash and exit, leaving
        # the user wondering whether they actually watched anything. NDJSON
        # mode is fine emitting a single terminal event so it skips this.
        try:
            pre = client.public_get(f"/orgs/{org_id}/workflow-runs/{run_id}")
            pre_data = pre.get("data", pre)
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "look up workflow run before watch")
        if pre_data.get("status") == "completed":
            conclusion = pre_data.get("conclusion") or "completed"
            click.echo(
                click.style(
                    f"Run {run_id} already finished ({conclusion}). Nothing to watch — see `avr run view {run_id}`.",
                    fg="yellow",
                ),
                err=True,
            )
            if exit_status and conclusion != "success":
                sys.exit(1)
            return
        # Resolve link context once — the watch loop redraws every interval,
        # but slug/console_url stay constant for the lifetime of the watch.
        links_enabled = ctx.obj.get("links_enabled", False)
        link_console_url = get_console_url(config.public_api_url) if links_enabled else ""
        link_slug = get_org_slug(client, org_id) if links_enabled else ""
        watch_run_loop(
            client,
            org_id,
            run_id,
            interval=interval,
            exit_status=exit_status,
            console_url=link_console_url,
            slug=link_slug,
        )


def _poll_for_new_attempt(
    client: ApiClient,
    org_id: str,
    platform_run_id: int,
    current_attempt: int,
    *,
    timeout: float = 30.0,
) -> dict[str, Any] | None:
    """Poll the workflow-runs list for a row with run_attempt > current_attempt.

    A re-run produces a brand-new Avrea ``run_id`` (each attempt is its own
    row), but only after GitHub fires the webhook and Avrea ingests it. This
    closes the gap between "rerun requested" and "the new attempt is queryable"
    so users get the new run_id printed inline.

    Transient errors (5xx, network) keep retrying until the deadline; 4xx are
    terminal — a stale token or revoked access mid-poll otherwise looks like
    'GitHub is slow' all the way to the timeout, hiding a real failure."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            response = client.public_get(
                f"/orgs/{org_id}/workflow-runs",
                params={"platform_run_id": platform_run_id, "limit": 10, "order": "created_at.desc"},
            )
            for r in response.get("data", []):
                if (r.get("run_attempt") or 0) > current_attempt:
                    return r
        except httpx.HTTPStatusError as exc:
            if exc.response.status_code < 500 and exc.response.status_code != 429:
                handle_http_error(exc, "poll for rerun attempt")
            # 5xx falls through to the sleep+retry below.
        except httpx.ConnectError, httpx.TimeoutException:
            # Transient network failures during poll — keep retrying until the
            # deadline rather than aborting the rerun command.
            pass
        time.sleep(1.0)
    return None


@run.command("rerun")
@click.argument("run_id")
@click.option("--org", "org_id", help="Organization ID or slug.")
@click.option("--failed", is_flag=True, help="Re-run only the failed jobs.")
@click.option("-y", "--yes", is_flag=True, help="Skip the confirmation prompt.")
@click.pass_context
def run_rerun(ctx, run_id: str, org_id, failed: bool, yes: bool):
    """Re-run a completed workflow run.

    \b
    Examples:
        avr run rerun run-abc123
        avr run rerun run-abc123 --failed
        avr run rerun run-abc123 --yes
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    if not yes:
        scope = "failed jobs only" if failed else "all jobs"
        ensure_prompts_allowed("run rerun needs confirmation")
        click.confirm(f"Re-run {run_id} ({scope})?", abort=True)

    # Fetch the run first so we know which platform_run_id and attempt to
    # diff against — the new attempt's run_id is what users want to navigate to.
    try:
        existing = client.public_get(f"/orgs/{org_id}/workflow-runs/{run_id}")
        existing_data = existing.get("data", existing)
        platform_run_id = existing_data.get("platform_run_id")
        current_attempt = existing_data.get("run_attempt", 1)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "look up workflow run before rerun")

    try:
        client.public_post(
            f"/orgs/{org_id}/workflow-runs/{run_id}/rerun",
            json={"failed_only": failed},
        )
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "rerun workflow run")

    scope = "failed jobs" if failed else "all jobs"
    click.echo(f"{click.style('✓', fg='green')} Re-run requested for {click.style(run_id, fg='cyan')} ({scope}).")

    if platform_run_id is not None:
        click.echo(click.style("Waiting for the new attempt to appear...", dim=True), err=True)
        new_run = _poll_for_new_attempt(client, org_id, int(platform_run_id), int(current_attempt))
        if new_run:
            new_id = new_run.get("run_id", "")
            new_attempt = new_run.get("run_attempt", "?")
            click.echo(f"  Run: {click.style(new_id, fg='cyan')}  (attempt {new_attempt})")
            run_id = new_id  # use the new run_id for the console-URL hint below
        else:
            click.echo(
                click.style(
                    "New attempt not yet visible after 30s. Use `avr run list` to find it.",
                    dim=True,
                ),
                err=True,
            )

    slug = get_org_slug(client, org_id)
    console_url = get_console_url(config.public_api_url)
    click.echo(f"  {console_url}/org/{slug}/runs/{run_id}")
    click.echo(click.style(f"  → avr run watch {run_id}", dim=True), err=True)


# Subset of job-level conclusion values that count as "failed" for
# `avr run logs --failed`. `stale` and `startup_failure` are run-level
# only and never appear on a job.
_FAILED_CONCLUSIONS = frozenset({"failure", "timed_out", "cancelled", "action_required"})


@run.command("logs")
@click.argument("run_id")
@click.option("--org", "org_id", help="Organization ID or slug.")
@click.option("--job", "job_name_filter", help="Restrict to GitHub jobs whose name contains this string.")
@click.option("-f", "--follow", is_flag=True, help="Tail logs as they appear (running jobs only).")
@click.option("--failed", "failed_only", is_flag=True, help="Show only logs from failed jobs.")
@click.option(
    "--all-levels",
    "show_all_levels",
    is_flag=True,
    help="Include diagnostic-level lines (off by default).",
)
@click.option(
    "--no-pager",
    is_flag=True,
    help="Print directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.",
)
@click.pass_context
def run_logs(
    ctx,
    run_id: str,
    org_id,
    job_name_filter: str | None,
    follow: bool,
    failed_only: bool,
    show_all_levels: bool,
    no_pager: bool,
):
    """Fetch logs for a workflow run's GitHub jobs.

    Long-form alternative to `avr run view --log[-failed]`. Use --follow to
    tail logs in real time for an in-progress job; pass --job to scope to a
    specific job when a run has many.

    \b
    Examples:
        avr run logs run-abc123
        avr run logs run-abc123 --failed
        avr run logs run-abc123 --job test
        avr run logs run-abc123 --follow
    """
    if follow and failed_only:
        raise click.UsageError("--follow and --failed cannot be combined")

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get(
            f"/orgs/{org_id}/workflow-runs/{run_id}",
            params={"include": ["jobs"]},
        )
        run_data = response.get("data", response)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "get workflow run")

    jobs_list: list[dict[str, Any]] = run_data.get("jobs") or []
    if job_name_filter:
        needle = job_name_filter.lower()
        jobs_list = [j for j in jobs_list if needle in (j.get("job_name") or "").lower()]
    if not jobs_list:
        click.echo("No matching jobs found.")
        return

    # Per-job console URL — used for header and per-line timestamps.
    links_enabled = ctx.obj.get("links_enabled", False)
    link_console_url = get_console_url(config.public_api_url) if links_enabled else ""
    link_slug = get_org_slug(client, org_id) if links_enabled else ""

    def _job_link(job_id_: str) -> str | None:
        if link_console_url and link_slug and job_id_:
            return job_url(link_console_url, link_slug, job_id_)
        return None

    if follow:
        # Prefer a running job; fall back to the first. Note: jobs use
        # ``state``, steps use ``status``.
        running = [j for j in jobs_list if j.get("state") in ("in_progress", "queued")]
        target = running[0] if running else jobs_list[0]
        job_id = target.get("job_id")
        if not job_id:
            raise click.ClickException("Cannot follow: target job has no ID yet.")
        url = _job_link(job_id)
        header = f"--- {target.get('job_name', '?')} ({job_id}) ---"
        click.echo(hyperlink(click.style(header, bold=True), url) if url else click.style(header, bold=True))
        # Single-job follow → step-level marks (matches `avr job logs --follow`).
        follow_logs(
            client,
            org_id,
            job_id,
            show_all_levels=show_all_levels,
            link_url=url,
            mark_steps=True,
            job_name=target.get("job_name"),
        )
        return

    if failed_only:
        jobs_list = [j for j in jobs_list if (j.get("conclusion") or "") in _FAILED_CONCLUSIONS]
        if not jobs_list:
            click.echo("No failed jobs in this run.")
            return

    # Buffer all log output, then page it in one go. Logs can be tens of MB
    # for long jobs; a pager makes that navigable. piped consumers and
    # AVR_PAGER='' / PAGER='' callers fall through to direct echo.
    #
    # OSC 133 marks intentionally omitted here: `less` doesn't strip OSC
    # sequences, so they'd leak as visible bytes when paged. Step-level
    # marks live in `avr job logs --follow` instead, which streams direct
    # to the terminal.
    buffer: list[str] = []
    emit = buffer.append
    for job in jobs_list:
        job_id = job.get("job_id")
        if not job_id:
            continue
        steps = job.get("steps") or []
        url = _job_link(job_id)
        emit("")
        header = f"--- {job.get('job_name', '?')} ({job_id}) ---"
        styled = click.style(header, bold=True)
        emit(hyperlink(styled, url) if url else styled)
        if failed_only and steps:
            print_failed_step_logs(client, job_id, steps, show_all_levels=show_all_levels, emit=emit, link_url=url)
        else:
            entries = fetch_all_logs(client, job_id, show_all_levels=show_all_levels)
            print_logs_grouped(entries, emit=emit, link_url=url)
    page_output("\n".join(buffer), bypass=no_pager)


@run.command("cancel")
@click.argument("run_id")
@click.option("--org", "org_id", help="Organization ID or slug.")
@click.option("-y", "--yes", is_flag=True, help="Skip the confirmation prompt.")
@click.pass_context
def run_cancel(ctx, run_id: str, org_id, yes: bool):
    """Cancel an in-progress or queued workflow run.

    \b
    Examples:
        avr run cancel run-abc123
        avr run cancel run-abc123 --yes
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id, client=client)

    if not yes:
        ensure_prompts_allowed("run cancel needs confirmation")
        click.confirm(f"Cancel {run_id}?", abort=True)

    try:
        result = client.public_post(f"/orgs/{org_id}/workflow-runs/{run_id}/cancel")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "cancel workflow run")

    status = result.get("data", {}).get("status", "cancel_requested")
    styled_run_id = click.style(run_id, fg="cyan")
    if status == "already_terminal":
        click.echo(f"{click.style('•', fg='yellow')} Run {styled_run_id} had already finished; nothing to cancel.")
    else:
        click.echo(f"{click.style('✓', fg='green')} Cancel requested for {styled_run_id}.")

    slug = get_org_slug(client, org_id)
    console_url = get_console_url(config.public_api_url)
    click.echo(f"  {console_url}/org/{slug}/runs/{run_id}")
    if status != "already_terminal":
        click.echo(
            click.style(f"  → avr run watch {run_id}  (verify cancellation)", dim=True),
            err=True,
        )
