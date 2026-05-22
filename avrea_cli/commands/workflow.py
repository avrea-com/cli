"""Workflow CLI commands — list workflows with aggregate stats."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.commands.run import watch_run_loop
from avrea_cli.config import CliConfig
from avrea_cli.display import DIM_FG
from avrea_cli.display import format_duration
from avrea_cli.display import get_console_url
from avrea_cli.display import hyperlink
from avrea_cli.display import open_or_print_url
from avrea_cli.display import run_url
from avrea_cli.display import status_indicator
from avrea_cli.display import truncate as _truncate
from avrea_cli.display import workflow_url
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import get_org_slug
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import parse_since
from avrea_cli.json_output import emit_json
from avrea_cli.json_output import emit_json_record
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import json_options
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import reject_web_with_json
from avrea_cli.json_output import split_fields
from avrea_cli.output import format_relative_timestamp
from avrea_cli.repo_context import resolve_repo_or_detect
from avrea_cli.repo_context import resolve_repos_or_detect
from datetime import UTC
from datetime import datetime
from datetime import timedelta
from typing import Any
from typing import NamedTuple
import click
import httpx
import json
import sys
import time


def _pct(num: int, denom: int) -> str:
    if denom == 0:
        return "-"
    return f"{100 * num / denom:.1f}%"


@click.group(cls=GhGroup)
@click.pass_context
def workflow(ctx):
    """List and view workflow definitions."""
    ensure_ctx(ctx)


_WORKFLOW_LIST_FIELDS = make_schema(
    "failure_count",
    "flaked_count",
    "median_duration_seconds",
    workflow_id="workflow.workflow_id",
    platform_workflow_id="workflow.platform_id",
    name="workflow.name",
    path="workflow.path",
    repository="workflow.repository_full_name",
    runs="count",
    completed_runs="completed_count",
)

# View is the list aggregate plus p95 + per-job breakdown.
_WORKFLOW_VIEW_FIELDS = {**_WORKFLOW_LIST_FIELDS, **make_schema("p95_duration_seconds", "jobs")}


@workflow.command("list")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option(
    "--repo",
    "repo_ids",
    multiple=True,
    help="Filter by repository (org/repo or rep-xxx ID, repeatable).",
)
@click.option("--since", default="30d", show_default=True, help="Time window: '30d', '7d', '24h', or 'all'.")
@click.option(
    "-L", "--limit", type=click.IntRange(1, 1000), default=20, show_default=True, help="Max workflows to show."
)
@json_options
@click.pass_context
def workflow_list(ctx, org_id, repo_ids, since, limit, json_fields, jq_expr):
    """List workflows with aggregate run stats.

    \b
    Examples:
        avr workflow list
        avr workflow list --repo acme/web
        avr workflow list --since 7d
        avr workflow list --limit 5
        avr workflow list --since all
        avr workflow list --json name,runs,median_duration_seconds

    \b
    JSON FIELDS
        completed_runs, failure_count, flaked_count, median_duration_seconds, name,
        path, platform_workflow_id, repository, runs, workflow_id
    """
    if handle_json_meta(json_fields, jq_expr, _WORKFLOW_LIST_FIELDS):
        return
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)

    params: dict[str, Any] = {"time_bucket": "total"}
    resolved_repo_ids = resolve_repos_or_detect(client, config, org_id, repo_ids)
    if resolved_repo_ids:
        params["repository_ids"] = resolved_repo_ids

    if since != "all":
        cutoff = parse_since(since)
        params["created_after"] = cutoff.isoformat()

    try:
        response = client.public_get(f"/orgs/{org_id}/workflow-runs/aggregate", params=params)
        buckets = response.get("data") or []
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch workflow aggregate")

    if json_fields is not None:
        # Sort before projecting so --limit applies to the most-active workflows.
        buckets.sort(key=lambda b: b.get("count", 0), reverse=True)
        emit_json(
            buckets[:limit],
            split_fields(json_fields, _WORKFLOW_LIST_FIELDS),
            _WORKFLOW_LIST_FIELDS,
            jq_expr,
        )
        return

    if not buckets:
        click.echo("No workflow runs found in the selected window.")
        return

    # Sort by run count descending (console's default), then truncate.
    # The /aggregate endpoint returns every workflow that ran in the window;
    # --limit is applied client-side after sorting so the most active workflows
    # are kept regardless of the input order.
    buckets.sort(key=lambda b: b.get("count", 0), reverse=True)
    total = len(buckets)
    truncated = total > limit
    buckets = buckets[:limit]

    # Widths
    W = {"name": 40, "repo": 30, "runs": 6, "median": 12, "failure": 8, "flake": 8, "id": 36}
    s = " "

    def _hdr(label: str, width: int, right: bool = False) -> str:
        aligned = f"{label:>{width}s}" if right else f"{label:{width}s}"
        return click.style(aligned, fg=DIM_FG, underline=True)

    click.echo(
        f"  {_hdr('NAME', W['name'])}{s}{_hdr('REPOSITORY', W['repo'])}{s}"
        f"{_hdr('RUNS', W['runs'], right=True)}{s}{_hdr('MEDIAN', W['median'], right=True)}{s}"
        f"{_hdr('FAILURE', W['failure'], right=True)}{s}{_hdr('FLAKE', W['flake'], right=True)}{s}"
        f"{_hdr('ID', W['id'])}"
    )

    for b in buckets:
        wf = b.get("workflow") or {}
        name = f"{_truncate(wf.get('name', '(unknown)'), W['name'] - 2):{W['name']}s}"
        repo = f"{_truncate(wf.get('repository_full_name', ''), W['repo'] - 2):{W['repo']}s}"
        runs = f"{b.get('count', 0):>{W['runs']}d}"
        median = f"{format_duration(b.get('median_duration_seconds')):>{W['median']}s}"
        completed = b.get("completed_count", 0)
        failure_rate = f"{_pct(b.get('failure_count', 0), completed):>{W['failure']}s}"
        flake_rate = f"{_pct(b.get('flaked_count', 0), completed):>{W['flake']}s}"
        wf_id = wf.get("workflow_id", "")

        # Color failure rate based on value
        fail_pct = 100 * b.get("failure_count", 0) / completed if completed else 0
        if fail_pct >= 20:
            failure_rate = click.style(failure_rate, fg="red")
        elif fail_pct >= 5:
            failure_rate = click.style(failure_rate, fg="yellow")

        click.echo(
            f"  {click.style(name, bold=True)}{s}"
            f"{click.style(repo, dim=True)}{s}"
            f"{runs}{s}{median}{s}{failure_rate}{s}{flake_rate}{s}"
            f"{click.style(wf_id, fg='cyan')}"
        )

    click.echo()
    if truncated:
        msg = f"Showing top {len(buckets)} of {total} workflows from the last {since}."
    else:
        msg = f"Showing stats for the last {since} ({len(buckets)} workflows)."
    click.echo(click.style(msg, dim=True))
    click.echo()
    click.echo("To view a workflow, try: avr workflow view <wfl-id>", err=True)
    if truncated:
        click.echo("To see more, try: avr workflow list --limit 100", err=True)
    else:
        click.echo("To adjust the time window, try: avr workflow list --since 7d", err=True)


@workflow.command("view")
@click.argument("workflow_identifier", metavar="WORKFLOW")
@click.option("--org", "org_id", help="Organization ID.")
@click.option(
    "--repo",
    "repo_flag",
    help="Repository (org/repo or rep-xxx). Auto-detected from git remote when WORKFLOW is a filename or display name.",
)
@click.option("--since", default="30d", show_default=True, help="Time window: '30d', '7d', '24h', or 'all'.")
@click.option(
    "--json",
    "json_fields",
    default=None,
    help='Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.',
)
@click.option("-q", "--jq", "jq_expr", default=None, help="Filter --json output through a jq expression.")
@click.option("--web", is_flag=True, help="Open in browser.")
@click.pass_context
def workflow_view(ctx, workflow_identifier: str, org_id, repo_flag, since, json_fields, jq_expr, web: bool):
    """View a workflow with aggregate stats and per-job breakdown.

    WORKFLOW can be an Avrea workflow ID (wfl-...), a GitHub numeric
    workflow ID (the integer in the GH URL), a workflow filename
    (build.yml), or the workflow's display name. All forms except wfl-...
    need a repository — pass --repo or run from inside the repo's git
    checkout.

    \b
    Examples:
        avr workflow view wfl-abc123
        avr workflow view 200589168
        avr workflow view ci.yml
        avr workflow view "Build and Deploy" --since 7d
        avr workflow view ci --json runs,median_duration_seconds
        avr workflow view wfl-abc123 --json '*' --jq '.jobs[].job.name'

    \b
    JSON FIELDS
        completed_runs, failure_count, flaked_count, jobs, median_duration_seconds,
        name, p95_duration_seconds, path, platform_workflow_id, repository, runs,
        workflow_id
    """
    reject_web_with_json(json_fields, web)
    if handle_json_meta(json_fields, jq_expr, _WORKFLOW_VIEW_FIELDS):
        return
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)

    if workflow_identifier.startswith("wfl-"):
        workflow_id = workflow_identifier
    else:
        repo_id = resolve_repo_or_detect(client, config, org_id, repo_flag, required=True)
        workflow_id = _lookup_workflow(client, org_id, repo_id, workflow_identifier)["workflow_id"]

    if web:
        slug = get_org_slug(client, org_id)
        console_url = get_console_url(config.public_api_url)
        url = f"{console_url}/org/{slug}/workflows/{workflow_id}"
        open_or_print_url(url)
        return

    params: dict[str, Any] = {
        "time_bucket": "total",
        "workflow_ids": [workflow_id],
        "include": ["jobs"],
    }
    if since != "all":
        params["created_after"] = parse_since(since).isoformat()

    try:
        response = client.public_get(f"/orgs/{org_id}/workflow-runs/aggregate", params=params)
        buckets = response.get("data") or []
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch workflow stats")

    if json_fields is not None:
        bucket = buckets[0] if buckets else {}
        emit_json_record(bucket, split_fields(json_fields, _WORKFLOW_VIEW_FIELDS), _WORKFLOW_VIEW_FIELDS, jq_expr)
        return

    if not buckets:
        click.echo(f"No runs found for workflow {workflow_id} in the selected window.")
        return

    bucket = buckets[0]
    wf = bucket.get("workflow") or {}

    # OSC 8 link context — slug lookup is best-effort, one extra API call.
    links_enabled = ctx.obj.get("links_enabled", False)
    console_url = get_console_url(config.public_api_url) if links_enabled else ""
    slug = get_org_slug(client, org_id) if links_enabled else ""

    # Header
    name = wf.get("name") or "(unknown)"
    repo = wf.get("repository_full_name") or "-"
    path = wf.get("path") or "-"
    name_styled = click.style(name, bold=True, fg="bright_white")
    if links_enabled and wf.get("workflow_id"):
        name_styled = hyperlink(name_styled, workflow_url(console_url, slug, wf["workflow_id"]))
    click.echo(name_styled)
    click.echo(click.style(f"{repo} -- {path}", dim=True))
    click.echo()

    # Summary stats
    total = bucket.get("count", 0)
    completed = bucket.get("completed_count", 0)
    median = format_duration(bucket.get("median_duration_seconds"))
    p95 = format_duration(bucket.get("p95_duration_seconds"))
    failure = _pct(bucket.get("failure_count", 0), completed)
    flake = _pct(bucket.get("flaked_count", 0), completed)

    def _stat(label: str, value: str) -> str:
        return f"  {click.style(label, dim=True)}  {value}"

    click.echo(_stat("Runs        ", click.style(str(total), bold=True)))
    click.echo(_stat("Median      ", median))
    click.echo(_stat("P95         ", p95))
    click.echo(_stat("Failure rate", _colorize_rate(bucket.get("failure_count", 0), completed, failure)))
    click.echo(_stat("Flake rate  ", flake))

    # Per-job breakdown
    jobs = bucket.get("jobs") or []
    if jobs:
        jobs.sort(key=lambda j: j.get("median_duration_seconds") or 0, reverse=True)

        # Size job-name column to fit actual content (capped)
        max_name = max(len((j.get("job") or {}).get("name", "")) for j in jobs)
        name_width = min(max(max_name, 10), 50)

        click.echo()
        click.echo(click.style("JOBS", bold=True, fg="bright_white"))
        click.echo()

        W = {"name": name_width, "runs": 5, "median": 10, "p95": 10, "failure": 8, "flake": 7}
        s = "  "

        def _hdr(label: str, width: int, right: bool = False) -> str:
            aligned = f"{label:>{width}s}" if right else f"{label:{width}s}"
            return click.style(aligned, fg=DIM_FG, underline=True)

        click.echo(
            f"  {_hdr('NAME', W['name'])}{s}"
            f"{_hdr('RUNS', W['runs'], right=True)}{s}"
            f"{_hdr('MEDIAN', W['median'], right=True)}{s}"
            f"{_hdr('P95', W['p95'], right=True)}{s}"
            f"{_hdr('FAILURE', W['failure'], right=True)}{s}"
            f"{_hdr('FLAKE', W['flake'], right=True)}"
        )

        for j in jobs:
            j_name = (j.get("job") or {}).get("name", "(unknown)")
            j_completed = j.get("completed_count", 0)
            j_fail = j.get("failure_count", 0)
            j_flake = j.get("flaked_count", 0)
            name_col = f"{_truncate(j_name, W['name'] - 2):{W['name']}s}"
            runs_col = f"{j.get('count', 0):>{W['runs']}d}"
            median_col = f"{format_duration(j.get('median_duration_seconds')):>{W['median']}s}"
            p95_col = f"{format_duration(j.get('p95_duration_seconds')):>{W['p95']}s}"
            fail_col = f"{_pct(j_fail, j_completed):>{W['failure']}s}"
            flake_col = f"{_pct(j_flake, j_completed):>{W['flake']}s}"
            fail_col = _colorize_rate(j_fail, j_completed, fail_col)

            click.echo(
                f"  {click.style(name_col, bold=True)}{s}"
                f"{runs_col}{s}{median_col}{s}{p95_col}{s}{fail_col}{s}{flake_col}"
            )

    # Recent runs — auxiliary section. Don't fail the whole view if the
    # extra fetch errors; surface the cause via a hint so it's not silent.
    try:
        runs_resp = client.public_get(
            f"/orgs/{org_id}/workflow-runs",
            params={"workflow_ids": [workflow_id], "limit": 5, "order": "created_at.desc"},
        )
        recent = runs_resp.get("data", [])
    except httpx.HTTPStatusError as exc:
        click.echo(
            click.style(f"(could not load recent runs: HTTP {exc.response.status_code})", fg=DIM_FG),
            err=True,
        )
        recent = []

    if recent:
        click.echo()
        click.echo(click.style("RECENT RUNS", bold=True, fg="bright_white"))
        click.echo()

        # Size columns for recent runs
        max_title = max(len(r.get("display_title", "")) for r in recent)
        title_w = min(max(max_title, 10), 50)
        max_branch = max(len(r.get("head_branch") or "") for r in recent)
        branch_w = min(max(max_branch, 6), 32)

        Wr = {"title": title_w, "branch": branch_w, "event": 18, "elapsed": 10, "age": 12, "id": 36}
        s = "  "

        def _hdr_r(label: str, width: int, right: bool = False) -> str:
            aligned = f"{label:>{width}s}" if right else f"{label:{width}s}"
            return click.style(aligned, fg=DIM_FG, underline=True)

        click.echo(
            f"  {_hdr_r('TITLE', Wr['title'])}{s}{_hdr_r('BRANCH', Wr['branch'])}{s}"
            f"{_hdr_r('EVENT', Wr['event'])}{s}{_hdr_r('ID', Wr['id'])}{s}"
            f"{_hdr_r('ELAPSED', Wr['elapsed'], right=True)}{s}{_hdr_r('AGE', Wr['age'], right=True)}"
        )
        # ``platform`` defaults to "github" when missing — staging APIs that
        # predate the field exposure on AggregateWorkflow still return github
        # workflows by DB constraint (workflows.platform CHECK = 'github').
        gh_branch_base = (
            f"https://github.com/{repo}/tree/"
            if wf.get("platform", "github") == "github" and repo and "/" in repo
            else ""
        )

        for r in recent:
            ind = status_indicator(r.get("status", "unknown"), r.get("conclusion"))
            title = f"{_truncate(r.get('display_title', ''), Wr['title'] - 2):{Wr['title']}s}"
            head_branch = r.get("head_branch") or ""
            branch = f"{_truncate(head_branch, Wr['branch'] - 2):{Wr['branch']}s}"
            event = f"{r.get('event', ''):{Wr['event']}s}"
            elapsed = f"{format_duration(r.get('duration_seconds')):>{Wr['elapsed']}s}"
            age = f"{format_relative_timestamp(r.get('created_at')):>{Wr['age']}s}"
            r_id = r.get("run_id", "")
            title_cell = title
            branch_cell = click.style(branch, bold=True)
            r_id_cell = click.style(f"{r_id:{Wr['id']}s}", fg="cyan")
            if links_enabled and r_id:
                run_link = run_url(console_url, slug, r_id)
                title_cell = hyperlink(title, run_link)
                r_id_cell = hyperlink(r_id_cell, run_link)
            if links_enabled and gh_branch_base and head_branch:
                branch_cell = hyperlink(branch_cell, f"{gh_branch_base}{head_branch}")
            click.echo(
                f"{ind} {title_cell}{s}{branch_cell}{s}{event}{s}{r_id_cell}{s}{elapsed}{s}{click.style(age, dim=True)}"
            )

    click.echo()
    click.echo(click.style(f"Stats for the last {since}.", dim=True))
    click.echo()
    click.echo(f"To see more runs for this workflow, try: avr run list --repo {repo}", err=True)
    click.echo("To adjust the time window, try: avr workflow view <wfl-id> --since 7d", err=True)


def _colorize_rate(fail: int, completed: int, rendered: str) -> str:
    """Apply red/yellow coloring to a failure percentage string."""
    if completed == 0:
        return rendered
    pct = 100 * fail / completed
    if pct >= 20:
        return click.style(rendered, fg="red")
    if pct >= 5:
        return click.style(rendered, fg="yellow")
    return rendered


# --- avr workflow run ------------------------------------------------------

_WF_FILENAME_SUFFIXES = (".yml", ".yaml")


def _parse_raw_fields(fields: tuple[str, ...]) -> dict[str, str]:
    """Parse -f/--raw-field key=value pairs into a dict."""
    out: dict[str, str] = {}
    for pair in fields:
        if "=" not in pair:
            raise click.ClickException(f"Invalid -f value {pair!r}, expected key=value")
        k, v = pair.split("=", 1)
        k = k.strip()
        if not k:
            raise click.ClickException(f"Invalid -f value {pair!r}, key is empty")
        out[k] = v
    return out


class ResolvedWorkflow(NamedTuple):
    """Resolved workflow identifier — what to dispatch + how to find the run.

    ``display_name`` and ``platform_workflow_id`` are None when the input was
    a filename passthrough (no API lookup happened); the post-dispatch poll
    falls back to repo-scoped scanning in that case."""

    filename: str
    display_name: str | None
    platform_workflow_id: int | None


def _basename(path: str | None) -> str:
    return (path or "").rsplit("/", 1)[-1]


def _stem(path: str | None) -> str:
    """Strip ``.yml``/``.yaml`` so users don't have to type the suffix."""
    base = _basename(path).lower()
    for ext in _WF_FILENAME_SUFFIXES:
        if base.endswith(ext):
            return base[: -len(ext)]
    return base


def _lookup_workflow(client: ApiClient, org_id: str, repo_id: str, identifier: str) -> dict:
    """Match a wfl-xxx ID, filename, or display name against the repo's workflow list.

    Hits the repo-scoped ``GET /orgs/{org}/repos/{repo}/workflows`` endpoint.
    Returns the matched row; raises ClickException for not-found / ambiguous matches."""
    # Paginate through the envelope's next_cursor. The endpoint emits a fixed
    # null cursor today, but matching the contract keeps lookups correct if it
    # ever starts paginating.
    try:
        workflows: list[dict] = []
        cursor: str | None = None
        for _ in range(50):  # hard ceiling defends against runaway loops
            params = {"cursor": cursor} if cursor is not None else None
            response = client.public_get(f"/orgs/{org_id}/repos/{repo_id}/workflows", params=params)
            workflows.extend(response.get("data") or [])
            cursor = (response.get("pagination") or {}).get("next_cursor")
            if cursor is None:
                break
    except httpx.HTTPStatusError as exc:
        hint = (
            f"Repository {repo_id!r} is not in this org or you lack access. Check `avr repo list --org <org>`."
            if exc.response.status_code == 404
            else None
        )
        handle_http_error(exc, "list workflows for resolution", hint=hint)

    if identifier.startswith("wfl-"):
        for wf in workflows:
            if wf.get("workflow_id") == identifier:
                return wf
        raise click.ClickException(f"Workflow {identifier!r} not found in this repository")

    # All-digits → GitHub numeric workflow ID (the integer in the GH URL).
    if identifier.isdigit():
        platform_id = int(identifier)
        for wf in workflows:
            if wf.get("platform_id") == platform_id:
                return wf
        raise click.ClickException(f"Workflow with platform ID {identifier!r} not found in this repository")

    ident_lower = identifier.lower()
    matches = [wf for wf in workflows if (wf.get("name") or "").lower() == ident_lower]
    if not matches:
        # Filename match — full ("ci.yml") or stem ("ci").
        matches = [
            wf
            for wf in workflows
            if _basename(wf.get("path")).lower() == ident_lower or _stem(wf.get("path")) == ident_lower
        ]
    if not matches:
        raise click.ClickException(
            f"Workflow {identifier!r} not found. Run 'avr workflow list --repo <repo>' to see available workflows."
        )
    if len(matches) > 1:
        names = ", ".join(wf.get("name", "") for wf in matches)
        raise click.ClickException(f"Ambiguous workflow {identifier!r}. Matches: {names}. Pass --wfl-id instead.")
    return matches[0]


def _resolve_workflow_filename(client: ApiClient, org_id: str, repo_id: str, identifier: str) -> ResolvedWorkflow:
    """Resolve a user-provided workflow identifier to a filename GitHub will accept.

    Accepts: Avrea ``wfl-xxx`` ID, a filename (``build.yml``), or a display name."""
    # Filename passthrough — GitHub accepts these directly. We don't have a
    # display name or platform id to surface in this branch (no API call),
    # so the caller's success line will print just the filename.
    if identifier.endswith(_WF_FILENAME_SUFFIXES) and "/" not in identifier:
        return ResolvedWorkflow(identifier, None, None)

    wf = _lookup_workflow(client, org_id, repo_id, identifier)
    path = wf.get("path")
    # GitHub serves workflows that lack a ``.yml`` path while disabled or
    # mid-rename. Surface the cause instead of dispatching ``workflow=""``,
    # which the dispatch endpoint rejects with an opaque 422.
    if not path:
        raise click.ClickException(
            f"Workflow {identifier} has no file path on disk and can't be dispatched. "
            "Pass a filename like 'build.yml' explicitly."
        )
    return ResolvedWorkflow(_basename(path), wf.get("name"), wf.get("platform_id"))


def _resolve_default_branch(client: ApiClient, org_id: str, repo_id: str) -> str | None:
    """Return the repo's default branch, or None if it isn't on the row.

    Returns None when the platform's default_branch hasn't been synced yet
    (the full installation sync populates it; push-event webhooks do not).
    Callers must treat None as "default unknown" and fail fast (e.g. with
    a ClickException asking the user to pass --ref) rather than guessing
    a fallback branch — assuming "main" silently dispatches the wrong ref
    on master/trunk repos.

    HTTP and transport errors propagate so a server outage or auth failure
    surfaces as the real cause rather than being misread as "branch missing".
    """
    resp = client.public_get(f"/orgs/{org_id}/repos/{repo_id}")
    return (resp.get("data") or {}).get("default_branch")


def _resolve_repo_full_name(client: ApiClient, org_id: str, repo_id: str, repo_flag: str | None) -> str | None:
    """Resolve ``org/name`` for a repo. Uses ``repo_flag`` if it looks like
    ``org/name`` already; otherwise looks the repo up by ``rep-xxx`` id.
    Returns None on transport failure — callers must tolerate."""
    if repo_flag and "/" in repo_flag:
        return repo_flag
    try:
        resp = client.public_get(f"/orgs/{org_id}/repos/{repo_id}")
    except httpx.HTTPStatusError, httpx.ConnectError, httpx.TimeoutException:
        return None
    return (resp.get("data") or {}).get("full_name")


_POLL_TIMEOUT_S: float = 90.0
_POLL_LIMIT: int = 100


def _poll_for_run(
    client: ApiClient,
    org_id: str,
    *,
    repo_id: str,
    platform_workflow_id: int | None,
    dispatch_time: datetime,
    timeout: float = _POLL_TIMEOUT_S,
) -> dict | None:
    """Poll the repo's workflow-runs list and return the first run that
    matches the just-dispatched workflow.

    Match criteria (in order of strictness):
      1. ``created_at >= dispatch_time`` — must be created after we dispatched.
      2. ``platform_workflow_id`` matches (when known) — same workflow we
         just triggered.

    We deliberately don't match by ``platform_run_id`` because the list
    endpoint has been observed to return ``null`` for that field on freshly-
    ingested rows, even though the underlying column is NOT NULL. Matching
    by (created_at, platform_workflow_id) is robust to that.

    Transient errors (5xx, 429, network) keep retrying until the deadline;
    other 4xx are terminal. Emits a trailing dot every 5s on stderr so the
    user knows polling is alive.
    """
    deadline = time.monotonic() + timeout
    interval = 1.0
    next_tick = time.monotonic() + 5.0
    show_progress = sys.stderr.isatty()

    def _finish() -> None:
        if show_progress:
            sys.stderr.write("\n")
            sys.stderr.flush()

    while time.monotonic() < deadline:
        try:
            response = client.public_get(
                f"/orgs/{org_id}/workflow-runs",
                params={"repository_ids": [repo_id], "limit": _POLL_LIMIT, "order": "created_at.desc"},
            )
            for row in response.get("data", []) or []:
                if not _row_matches_dispatch(row, dispatch_time, platform_workflow_id):
                    continue
                _finish()
                return row
        except httpx.HTTPStatusError as exc:
            if exc.response.status_code < 500 and exc.response.status_code != 429:
                _finish()
                handle_http_error(exc, "poll for dispatched run")
            # 5xx / 429: fall through to sleep+retry.
        if show_progress and time.monotonic() >= next_tick:
            sys.stderr.write(".")
            sys.stderr.flush()
            next_tick = time.monotonic() + 5.0
        time.sleep(interval)
    _finish()
    return None


def _row_matches_dispatch(row: dict[str, Any], dispatch_time: datetime, platform_workflow_id: int | None) -> bool:
    """Decide whether ``row`` is the run we just dispatched."""
    created_raw = row.get("created_at")
    if not isinstance(created_raw, str):
        return False
    try:
        created = datetime.fromisoformat(created_raw.replace("Z", "+00:00"))
    except ValueError:
        return False
    # Allow a small skew (server clock vs ours) in case the dispatched row
    # is timestamped a hair before our local "now".
    if created < dispatch_time - timedelta(seconds=5):
        return False
    if platform_workflow_id is not None and row.get("platform_workflow_id") != platform_workflow_id:
        return False
    return True


@workflow.command("run")
@click.argument("workflow_identifier", metavar="WORKFLOW")
@click.option("--org", "org_id", help="Organization ID.")
@click.option("--repo", "repo_flag", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.option(
    "-r",
    "--ref",
    default=None,
    help="Branch or tag to run at. Defaults to the repository's default branch.",
)
@click.option("-f", "--raw-field", "raw_fields", multiple=True, help="Workflow input: key=value (repeatable).")
@click.option("--json", "json_inputs", is_flag=True, help="Read a JSON object of inputs from stdin.")
@click.option(
    "-w/-W",
    "--watch/--no-watch",
    default=True,
    show_default=True,
    help="Poll for the new run and watch it until completion. Pass --no-watch / -W to return immediately.",
)
@click.option("--exit-status", is_flag=True, help="With --watch, exit non-zero if the run failed.")
@click.option("--interval", type=int, default=3, show_default=True, help="With --watch, refresh interval in seconds.")
@click.pass_context
def workflow_run(
    ctx,
    workflow_identifier: str,
    org_id: str | None,
    repo_flag: str | None,
    ref: str | None,
    raw_fields: tuple[str, ...],
    json_inputs: bool,
    watch: bool,
    exit_status: bool,
    interval: int,
):
    """Trigger a workflow_dispatch event.

    WORKFLOW can be an Avrea workflow ID (wfl-...), a GitHub numeric
    workflow ID, a workflow filename (build.yml), or the workflow's
    display name.

    \b
    Examples:
        avr workflow run build.yml
        avr workflow run "Build and Deploy" --ref feat/x
        avr workflow run wfl-abc123 -f env=prod -f region=eu
        echo '{"env":"prod"}' | avr workflow run build.yml --json
        avr workflow run build.yml --watch --exit-status
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    if json_inputs and raw_fields:
        raise click.ClickException("Use either -f/--raw-field or --json, not both.")
    if interval < 1:
        raise click.ClickException("--interval must be at least 1 second.")

    # Validate inputs *before* any network call — fail fast on bad args
    # without burning a round-trip on resolve / default-branch lookup.
    inputs: dict[str, str] = {}
    if json_inputs:
        if sys.stdin.isatty():
            raise click.ClickException(
                "--json reads inputs from stdin. Pipe a JSON object in, "
                'e.g. echo \'{"env":"prod"}\' | avr workflow run ... --json'
            )
        try:
            raw = sys.stdin.read()
            parsed = json.loads(raw) if raw.strip() else {}
        except json.JSONDecodeError as exc:
            raise click.ClickException(f"Invalid JSON on stdin: {exc}") from exc
        if not isinstance(parsed, dict):
            raise click.ClickException("JSON inputs must be an object.")
        inputs = {str(k): str(v) for k, v in parsed.items()}
    elif raw_fields:
        inputs = _parse_raw_fields(raw_fields)

    org_id_resolved = get_org_id(config, org_id, client=client)
    repo_id = resolve_repo_or_detect(client, config, org_id_resolved, repo_flag, required=True)
    resolved = _resolve_workflow_filename(client, org_id_resolved, repo_id, workflow_identifier)
    workflow_file = resolved.filename

    if ref is None:
        # Don't silently substitute "main": for master/trunk repos that would
        # dispatch the wrong ref AND label it as the default. _resolve_default_branch
        # returns None when the repo's default_branch hasn't been synced yet —
        # surface that as an actionable error instead of guessing.
        ref = _resolve_default_branch(client, org_id_resolved, repo_id)
        if ref is None:
            raise click.ClickException(
                "Could not determine the repository's default branch (not synced yet). Pass --ref <branch> explicitly."
            )
        click.echo(click.style(f"(using default branch: {ref})", dim=True), err=True)

    body: dict[str, Any] = {"workflow": workflow_file, "ref": ref}
    if inputs:
        body["inputs"] = inputs

    # Capture before dispatch so the post-dispatch poll can match by
    # ``created_at >= dispatch_time``. Server-side platform_run_id is
    # currently unreliable in the list response (returns null on freshly-
    # ingested rows), so timestamp + workflow_id is what we match on.
    dispatch_time = datetime.now(UTC)

    try:
        response = client.public_post(f"/orgs/{org_id_resolved}/repos/{repo_id}/dispatch-workflow", json=body)
        data = response.get("data", {})
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "dispatch workflow")

    platform_run_id = data.get("platform_run_id")
    # Show the matched display name when we have one — keeps a fuzzy
    # case-insensitive match auditable from the success line. Falls back to
    # the filename alone for filename-passthrough invocations.
    if resolved.display_name and resolved.display_name != workflow_file:
        triggered_label = (
            f"{click.style(resolved.display_name, fg='magenta', bold=True)} "
            f"({click.style(workflow_file, fg='magenta')})"
        )
    else:
        triggered_label = click.style(workflow_file, fg="magenta", bold=True)
    click.echo(f"{click.style('✓', fg='green')} Triggered {triggered_label} at {click.style(ref, bold=True)}.")

    links_enabled = ctx.obj.get("links_enabled", False)

    if platform_run_id is not None:
        run_id_label = click.style(str(platform_run_id), fg="bright_cyan", bold=True)
        # Only resolve repo full_name when we actually need it for the
        # hyperlink — keeps the no-watch / non-TTY path from paying an
        # extra round trip.
        if links_enabled:
            repo_full_name = _resolve_repo_full_name(client, org_id_resolved, repo_id, repo_flag)
            if repo_full_name:
                run_id_label = hyperlink(
                    run_id_label,
                    f"https://github.com/{repo_full_name}/actions/runs/{platform_run_id}",
                )
        click.echo(f"  GitHub run id: {run_id_label}")

    if not watch:
        click.echo(
            click.style(
                f"  → avr run list --repo {repo_flag or repo_id}    # avrea sync lands shortly",
                dim=True,
            ),
            err=True,
        )
        return

    click.echo(click.style("Waiting for run to appear in Avrea...", dim=True), err=True)
    run = _poll_for_run(
        client,
        org_id_resolved,
        repo_id=repo_id,
        platform_workflow_id=resolved.platform_workflow_id,
        dispatch_time=dispatch_time,
    )
    if not run:
        click.echo(
            click.style(
                f"  Avrea hasn't ingested the run yet after {int(_POLL_TIMEOUT_S)}s. "
                f"Re-run with `avr run watch <run-id>` once `avr run list --repo {repo_flag or repo_id}` shows it.",
                dim=True,
            ),
            err=True,
        )
        return

    avrea_run_id = run.get("run_id", "")
    slug = get_org_slug(client, org_id_resolved)
    console_url = get_console_url(config.public_api_url)
    if avrea_run_id:
        click.echo(f"  Run: {click.style(avrea_run_id, fg='bright_cyan', bold=True)}")
        click.echo(f"  {console_url}/org/{slug}/runs/{avrea_run_id}")

    watch_run_loop(
        client,
        org_id_resolved,
        avrea_run_id,
        interval=interval,
        exit_status=exit_status,
        console_url=console_url if links_enabled else "",
        slug=slug if links_enabled else "",
    )
