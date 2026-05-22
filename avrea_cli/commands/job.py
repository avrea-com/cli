"""Avrea job CLI commands — inspect VMs that run CI work."""

from avrea_cli.api_client import ApiClient
from avrea_cli.click_ext import GhGroup
from avrea_cli.commands.run import _STATUS_CHOICES
from avrea_cli.commands.run import _split_status_values
from avrea_cli.config import CliConfig
from avrea_cli.display import DIM_FG
from avrea_cli.display import format_conclusion_colored
from avrea_cli.display import format_duration
from avrea_cli.display import get_console_url
from avrea_cli.display import hint as _hint
from avrea_cli.display import hyperlink
from avrea_cli.display import is_piped
from avrea_cli.display import job_url
from avrea_cli.display import page_output
from avrea_cli.display import parse_runner_specs
from avrea_cli.display import print_piped_header
from avrea_cli.display import print_piped_row
from avrea_cli.display import repo_url
from avrea_cli.display import run_url
from avrea_cli.display import status_indicator
from avrea_cli.display import terminal_title
from avrea_cli.display import truncate as _truncate
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import get_org_slug
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
from avrea_cli.log_display import fetch_logs_after
from avrea_cli.log_display import follow_logs
from avrea_cli.log_display import format_log_line
from avrea_cli.log_display import print_failed_step_logs
from avrea_cli.log_display import print_logs_grouped
from avrea_cli.metrics_display import ALL_SOURCES
from avrea_cli.metrics_display import render_gauge_line
from avrea_cli.output import format_key_value
from avrea_cli.output import format_relative_timestamp
from avrea_cli.output import format_timestamp
from avrea_cli.output import short_id
from avrea_cli.repo_context import resolve_repos_or_detect
from collections import deque
from datetime import UTC
from datetime import datetime
from typing import Any
import click
import httpx
import json
import shlex
import shutil
import subprocess
import sys
import time
import webbrowser

_JOB_LIST_FIELDS = make_schema(
    "job_id",
    "platform_job_id",
    "job_name",
    "state",
    "conclusion",
    "duration_seconds",
    "created_at",
    "started_at",
    "completed_at",
    "running_on_avrea",
    "repository_id",
    "platform_run_id",
    labels="job_labels",
    repository="repository_full_name",
)

_JOB_VIEW_FIELDS = {**_JOB_LIST_FIELDS, **make_schema("steps", "workflow_run")}


# Column widths sized so common values fit untruncated:
# names ≤38, repos ≤28, conclusion fits "startup_failure" (15).
_JOBS_TABLE_W = {"name": 38, "repo": 28, "status": 15, "on": 7, "age": 10, "id": 36}

# Recent log lines shown beneath the gauges in `avr job metrics --live`.
# Sized so a typical terminal still fits the metrics + footer above the fold.
_LIVE_LOG_TAIL_LINES = 12

# Decoupled refresh cadences for `avr job metrics --live`. Logs feel live at
# 1s; metrics scrape at the OTel collector's interval (every ~5s), so polling
# them faster wouldn't surface new data and would burn through the 60/min IP
# rate limit on /metrics quickly.
_LIVE_LOG_REFRESH_S = 1.0
_LIVE_METRICS_REFRESH_S = 5.0


def _hdr_cell(label: str, width: int) -> str:
    return click.style(f"{label:{width}s}", fg=DIM_FG, underline=True)


def _print_jobs_table(jobs_data: list[dict[str, Any]], *, console_url: str = "", slug: str = "") -> None:
    """Sectioned-table renderer shared by `avr job list` and `avr job watch`.

    When ``console_url`` and ``slug`` are non-empty, each job_id is wrapped
    in an OSC 8 hyperlink to the console job page. Caller passes them only
    when ``ctx.obj['links_enabled']`` is true.

    Switches to tab-separated output (header row + data rows, no color, no
    truncation, ISO timestamps) when stdout isn't a TTY — the standard
    scriptability convention."""
    if is_piped():
        print_piped_header(["status", "job_name", "repository", "on_avrea", "job_id", "duration_seconds", "created_at"])
        for j in jobs_data:
            state = j.get("state", "unknown")
            conclusion = j.get("conclusion")
            status = conclusion if state == "completed" and conclusion else state
            print_piped_row(
                [
                    status,
                    j.get("job_name", ""),
                    j.get("repository_full_name", ""),
                    "yes" if j.get("running_on_avrea") else "shadow",
                    j.get("job_id", ""),
                    j.get("duration_seconds"),
                    j.get("created_at", ""),
                ]
            )
        return

    w = _JOBS_TABLE_W
    s = " "
    click.echo(
        f"  {_hdr_cell('NAME', w['name'])}{s}{_hdr_cell('REPOSITORY', w['repo'])}{s}"
        f"{_hdr_cell('STATUS', w['status'])}{s}{_hdr_cell('ON', w['on'])}{s}"
        f"{_hdr_cell('AGE', w['age'])}{s}{_hdr_cell('ID', w['id'])}"
    )
    for j in jobs_data:
        state = j.get("state", "unknown")
        conclusion = j.get("conclusion")
        indicator = status_indicator(state, conclusion)
        name = f"{_truncate(j.get('job_name', ''), w['name'] - 2):{w['name']}s}"
        repo = f"{_truncate(j.get('repository_full_name', ''), w['repo'] - 2):{w['repo']}s}"
        status_text = (conclusion or state) if state == "completed" else state
        status_padded = f"{_truncate(status_text, w['status'] - 1):{w['status']}s}"
        status_cell = format_conclusion_colored(state, conclusion, text=status_padded)
        on_text = "yes" if j.get("running_on_avrea") else "shadow"
        on_padded = f"{on_text:{w['on']}s}"
        on_cell = on_padded if on_text == "yes" else click.style(on_padded, dim=True)
        age = f"{format_relative_timestamp(j.get('created_at')):{w['age']}s}"
        job_id = j.get("job_id", "")
        # Pad before styling — f-string padding counts the SGR/OSC escape
        # bytes if applied after styling, defeating column alignment for
        # the bold name. Job_id is rightmost so post-pad styling there
        # is fine.
        name_cell = click.style(name, bold=True)
        job_id_cell = click.style(job_id, fg="cyan")
        if console_url and slug and job_id:
            url = job_url(console_url, slug, job_id)
            name_cell = hyperlink(name_cell, url)
            job_id_cell = hyperlink(job_id_cell, url)
        click.echo(
            f"{indicator} {name_cell}{s}"
            f"{click.style(repo, dim=True)}{s}"
            f"{status_cell}{s}{on_cell}{s}"
            f"{click.style(age, dim=True)}{s}"
            f"{job_id_cell}"
        )


@click.group(cls=GhGroup)
@click.pass_context
def job(ctx):
    """Inspect Avrea job VMs (SSH, metrics, logs)."""
    ensure_ctx(ctx)


@job.command("list")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option(
    "--repo",
    "repository_ids",
    multiple=True,
    help="Filter by repository (org/repo or rep-xxx, repeatable). Auto-detected from git remote if omitted.",
)
@click.option("--name", "job_names", multiple=True, help="Filter by job name (repeatable).")
@click.option(
    "--status",
    "status_values",
    type=click.Choice(_STATUS_CHOICES, case_sensitive=False),
    multiple=True,
    help="Filter by state (queued, in_progress, completed) or conclusion (success, failure, ...). Repeatable.",
)
@click.option("--on-avrea/--shadowing", "running_on_avrea", default=None, help="Filter by Avrea-run vs shadowing jobs.")
@click.option("-w", "--workflow", "workflow_ids", multiple=True, help="Filter by workflow ID (wfl-xxx, repeatable).")
@click.option("--since", default=None, help="Relative time window: '7d', '24h', etc.")
@click.option("-L", "--limit", type=click.IntRange(1, 1000), default=20, show_default=True, help="Max jobs to return.")
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
@click.pass_context
def job_list(
    ctx,
    org_id,
    repository_ids,
    job_names,
    status_values,
    running_on_avrea,
    workflow_ids,
    since,
    limit,
    cursor,
    order,
    json_fields,
    jq_expr,
):
    """List jobs for an organization.

    \b
    Examples:
        avr job list
        avr job list --status failure --limit 5
        avr job list --status in_progress --json job_name,state,conclusion
        avr job list --since 24h
        avr job list --json '?'           # list available fields
        avr job list --json '*'           # all fields

    \b
    JSON FIELDS
        completed_at, conclusion, created_at, duration_seconds, platform_job_id,
        platform_run_id, job_id, job_name, labels, repository, repository_id,
        running_on_avrea, started_at, state
    """
    if handle_json_meta(json_fields, jq_expr, _JOB_LIST_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)
    repository_ids = tuple(resolve_repos_or_detect(client, config, org_id, repository_ids))

    platform_states, platform_conclusions = _split_status_values(status_values)
    cursor = validate_cursor(cursor)

    params: dict[str, Any] = {"limit": limit, "order": order}
    if repository_ids:
        params["repository_ids"] = list(repository_ids)
    if job_names:
        params["job_names"] = list(job_names)
    if platform_states:
        params["states"] = platform_states
    if platform_conclusions:
        params["conclusions"] = platform_conclusions
    if running_on_avrea is not None:
        params["running_on_avrea"] = running_on_avrea
    if workflow_ids:
        params["workflow_ids"] = list(workflow_ids)
    if since:
        params["created_after"] = parse_since(since).isoformat()
    if cursor:
        params["cursor"] = cursor

    try:
        response = client.public_get(f"/orgs/{org_id}/jobs", params=params)
        jobs_data = response.get("data") or []
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list jobs")

    if json_fields is not None:
        emit_json(list(jobs_data), split_fields(json_fields, _JOB_LIST_FIELDS), _JOB_LIST_FIELDS, jq_expr)
        return

    if not jobs_data:
        click.echo("No jobs found.")
        return

    # OSC 8 hyperlinks on each job_id when stdout is a TTY. Slug lookup is
    # best-effort and shared across rows.
    links_enabled = ctx.obj.get("links_enabled", False)
    link_console_url = get_console_url(config.public_api_url) if links_enabled else ""
    link_slug = get_org_slug(client, org_id) if links_enabled else ""
    _print_jobs_table(jobs_data, console_url=link_console_url, slug=link_slug)

    next_cursor = response.get("pagination", {}).get("next_cursor")
    if next_cursor:
        click.echo(f"\nMore results available. Next page: --cursor {next_cursor}", err=True)


@job.command("watch")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified.")
@click.option(
    "--repo",
    "repository_ids",
    multiple=True,
    help="Filter by repository (org/repo or rep-xxx, repeatable). Auto-detected from git remote if omitted.",
)
@click.option("--name", "job_names", multiple=True, help="Filter by job name (repeatable).")
@click.option("--interval", type=int, default=5, show_default=True, help="Refresh interval in seconds.")
@click.option(
    "--ndjson",
    "ndjson_output",
    is_flag=True,
    help="Emit one JSON object per refresh (default when stdout isn't a TTY).",
)
@click.pass_context
def job_watch(ctx, org_id, repository_ids, job_names, interval, ndjson_output):
    """Watch active jobs with auto-refresh (Ctrl+C to stop)."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    if interval < 1:
        click.echo("Error: --interval must be at least 1 second.", err=True)
        raise click.Abort()
    if not sys.stdout.isatty():
        ndjson_output = True
    org_id = get_org_id(config, org_id, client=client)
    repository_ids = tuple(resolve_repos_or_detect(client, config, org_id, repository_ids))

    # Resolve link context once — slug + console_url stay constant across
    # the watch loop's redraws, so we don't pay the slug lookup per tick.
    links_enabled = ctx.obj.get("links_enabled", False)
    link_console_url = get_console_url(config.public_api_url) if links_enabled else ""
    link_slug = get_org_slug(client, org_id) if links_enabled else ""

    params: dict[str, Any] = {
        "limit": 50,
        "order": "created_at.desc",
        "states": ["queued", "in_progress"],
    }
    if repository_ids:
        params["repository_ids"] = list(repository_ids)
    if job_names:
        params["job_names"] = list(job_names)

    # ``enabled=not ndjson_output``: JSON-streaming mode is intended for a
    # downstream consumer even when stdout happens to be a TTY, so the
    # OSC 2 set-bytes shouldn't leak into the first JSON line.
    try:
        with terminal_title("avr ▸ jobs: watching", enabled=not ndjson_output) as term_title:
            while True:
                try:
                    response = client.public_get(f"/orgs/{org_id}/jobs", params=params)
                    jobs_data = response.get("data") or []
                except httpx.HTTPStatusError as exc:
                    code = exc.response.status_code
                    # 429 (rate limit) is retryable — riding it out is exactly
                    # what a watch loop is for. Other 4xx are terminal so the
                    # auth-hint contract (exit 4 on 401) keeps working.
                    if code < 500 and code != 429:
                        handle_http_error(exc, "list jobs")
                    click.clear()
                    click.echo(f"Error fetching jobs (HTTP {code}); retrying.", err=True)
                    term_title.set("avr ▸ jobs: error")
                    time.sleep(interval)
                    continue
                except (httpx.ConnectError, httpx.TimeoutException) as exc:
                    click.clear()
                    click.echo(f"Error fetching jobs: {exc}", err=True)
                    click.echo(f"Retrying in {interval}s...", err=True)
                    term_title.set("avr ▸ jobs: network error")
                    time.sleep(interval)
                    continue

                if ndjson_output:
                    click.echo(json.dumps({"data": jobs_data}, default=str))
                    time.sleep(interval)
                    continue

                click.clear()
                now_str = datetime.now(UTC).strftime("%H:%M:%S UTC")
                click.echo(f"Active jobs (refreshing every {interval}s) -- {now_str}\n")

                count = len(jobs_data) if jobs_data else 0
                term_title.set(f"avr ▸ jobs: {count} active")

                if not jobs_data:
                    click.echo("No active jobs.")
                else:
                    _print_jobs_table(jobs_data, console_url=link_console_url, slug=link_slug)

                time.sleep(interval)
    except KeyboardInterrupt:
        click.echo("\nStopped watching.")


@job.command("ssh")
@click.argument("job_id")
@click.option("--print-command", is_flag=True, help="Print the SSH command instead of connecting.")
@click.option("--show-password", is_flag=True, help="Display the SSH password (use with caution).")
@click.pass_context
def job_ssh(ctx, job_id: str, print_command: bool, show_password: bool):
    """SSH into a running job's VM."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    try:
        result = client.public_post("/vms/ssh-forwarding", json={"job_id": job_id}, timeout=60.0)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "connect to VM")

    host = result["host"]
    port = result["port"]
    username = result["username"]
    password = result.get("password")
    ssh_keys_installed = result.get("ssh_keys_installed", False)

    ssh_opts = ["-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null"]
    ssh_exec = ["ssh", *ssh_opts, "-p", str(port), f"{username}@{host}"]
    ssh_cmd = shlex.join(ssh_exec)

    if print_command:
        if password and not ssh_keys_installed and show_password and shutil.which("sshpass"):
            click.echo(f"sshpass -p {shlex.quote(password)} {ssh_cmd}")
        else:
            click.echo(ssh_cmd)
        if password and not ssh_keys_installed:
            if show_password:
                click.echo(f"Password: {password}", err=True)
            else:
                click.echo("Password: ******** (hidden; re-run with --show-password to reveal)", err=True)
        return

    if password and not ssh_keys_installed:
        # Always stderr — even with --show-password. Stdout would let `tee`
        # / `>` capture the password into a log; stderr keeps it on the
        # terminal where the user can copy it but pipelines can't trap it.
        if show_password:
            click.echo(f"Password: {password}", err=True)
        else:
            click.echo("Password: ******** (hidden; re-run with --show-password to reveal)", err=True)

    sys.exit(subprocess.run(ssh_exec).returncode)


@job.command("view")
@click.argument("job_id")
@click.option("--org", "org_id", help="Organization ID.")
@click.option("--log", "show_log", is_flag=True, help="Print full logs for the job.")
@click.option("--log-failed", is_flag=True, help="Print logs only for failed steps.")
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
def job_view(
    ctx,
    job_id: str,
    org_id,
    show_log: bool,
    log_failed: bool,
    json_fields: str | None,
    jq_expr: str | None,
    web: bool,
    no_pager: bool,
):
    """View a single job with its steps.

    \b
    Examples:
        avr job view job-abc123
        avr job view job-abc123 --log-failed
        avr job view job-abc123 --json conclusion,steps
        avr job view job-abc123 --json '*' --jq '.steps[] | select(.conclusion=="failure")'

    \b
    JSON FIELDS
        completed_at, conclusion, created_at, duration_seconds, platform_job_id,
        platform_run_id, job_id, job_name, labels, repository, repository_id,
        running_on_avrea, started_at, state, steps, workflow_run
    """
    reject_web_with_json(json_fields, web)
    if handle_json_meta(json_fields, jq_expr, _JOB_VIEW_FIELDS):
        return
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get(
            f"/orgs/{org_id}/jobs/{job_id}",
            params={"include": ["steps", "workflow_run"]},
        )
        job_data = response.get("data", response)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "get job")

    if json_fields is not None:
        emit_json_record(job_data, split_fields(json_fields, _JOB_VIEW_FIELDS), _JOB_VIEW_FIELDS, jq_expr)
        return

    if web:
        repo_name = job_data.get("repository_full_name", "")
        slug = get_org_slug(client, org_id)
        console_url = get_console_url(config.public_api_url)
        avrea_url = f"{console_url}/org/{slug}/jobs/{job_id}"
        click.echo(f"Avrea:  {avrea_url}")
        platform_run_id = job_data.get("platform_run_id")
        platform_job_id = job_data.get("platform_job_id")
        if repo_name and platform_run_id and platform_job_id:
            click.echo(f"GitHub: https://github.com/{repo_name}/actions/runs/{platform_run_id}/job/{platform_job_id}")
        if sys.stdout.isatty():
            webbrowser.open(avrea_url)
        return

    state = job_data.get("state", "unknown")
    conclusion = job_data.get("conclusion")
    indicator = status_indicator(state, conclusion)
    conclusion_display = f"{indicator} {conclusion}" if conclusion else state

    wf_run = job_data.get("workflow_run")
    run_display = ""
    run_id_full = ""
    if wf_run:
        run_id_full = wf_run.get("run_id", "")
        run_display = f"#{wf_run.get('run_number', '?')} ({short_id(run_id_full)})"

    repo_full = job_data.get("repository_full_name", "-")
    repo_id = job_data.get("repository_id")

    # OSC 8 link context.
    links_enabled = ctx.obj.get("links_enabled", False)
    link_console_url = get_console_url(config.public_api_url) if links_enabled else ""
    link_slug = get_org_slug(client, org_id) if links_enabled else ""

    job_cell = job_id
    repo_cell = repo_full
    run_cell = run_display or "-"
    if links_enabled and link_console_url and link_slug:
        job_cell = hyperlink(job_id, job_url(link_console_url, link_slug, job_id))
        if repo_id and repo_full != "-":
            repo_cell = hyperlink(repo_cell, repo_url(link_console_url, link_slug, repo_id))
        if run_id_full:
            run_cell = hyperlink(run_cell, run_url(link_console_url, link_slug, run_id_full))

    click.echo(
        format_key_value(
            {
                "Job": job_cell,
                "Name": job_data.get("job_name", "-"),
                "Repository": repo_cell,
                "Run": run_cell,
                "Conclusion": conclusion_display,
                "Duration": format_duration(job_data.get("duration_seconds")),
                "Labels": ", ".join(job_data.get("job_labels", [])) or "-",
                "On Avrea": "yes" if job_data.get("running_on_avrea") else "shadow",
                "Created": format_timestamp(job_data.get("created_at")),
                "Started": format_timestamp(job_data.get("started_at")),
                "Completed": format_timestamp(job_data.get("completed_at")),
            }
        )
    )

    steps = job_data.get("steps") or []
    if steps:
        click.echo()
        click.echo(click.style("STEPS", bold=True, fg="bright_white"))
        click.echo()
        for step in steps:
            s_status = step.get("status", "pending")
            s_conclusion = step.get("conclusion")
            s_indicator = status_indicator(s_status, s_conclusion)
            s_dur = ""
            if step.get("started_at") and step.get("completed_at"):
                try:
                    started = datetime.fromisoformat(step["started_at"].replace("Z", "+00:00"))
                    completed = datetime.fromisoformat(step["completed_at"].replace("Z", "+00:00"))
                    s_dur = f" ({format_duration((completed - started).total_seconds())})"
                except ValueError, TypeError:
                    pass
            click.echo(f"  {s_indicator}  {step.get('name', '?')}{s_dur}")

    if show_log or log_failed:
        # OSC 8 link target for per-line timestamps.
        log_links_enabled = ctx.obj.get("links_enabled", False)
        log_url: str | None = None
        if log_links_enabled:
            log_url = job_url(get_console_url(config.public_api_url), get_org_slug(client, org_id), job_id)
        # mark_steps left off — OSC 133 marks survive direct-to-TTY but
        # `less` doesn't strip OSC sequences, so they'd leak as visible
        # bytes when paged.
        log_buf: list[str] = []
        if log_failed:
            print_failed_step_logs(client, job_id, steps, emit=log_buf.append, link_url=log_url)
        else:
            entries = fetch_all_logs(client, job_id)
            print_logs_grouped(entries, emit=log_buf.append, link_url=log_url, steps=steps)
        page_output("\n".join(log_buf), bypass=no_pager)
    else:
        click.echo(f"\nTo see the full job log, try: avr job logs {job_id}", err=True)

    slug = get_org_slug(client, org_id)
    console_url = get_console_url(config.public_api_url)
    _hint(f"View this job on Avrea: {console_url}/org/{slug}/jobs/{job_id}")


@job.command("logs")
@click.argument("job_id")
@click.option("--org", "org_id", help="Organization ID.")
@click.option("--failed", is_flag=True, help="Only show logs from failed steps.")
@click.option("--step", "step_name", help="Filter to a specific step by name.")
@click.option(
    "--level",
    type=click.Choice(["debug", "info", "notice", "warning", "error"], case_sensitive=False),
    help="Filter by log level.",
)
@click.option("--follow", "-f", is_flag=True, help="Follow logs for in-progress jobs.")
@click.option("--all-levels", is_flag=True, help="Include diagnostic-level lines (hidden by default).")
@click.option(
    "--no-pager",
    is_flag=True,
    help="Print directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.",
)
@click.pass_context
def job_logs(
    ctx,
    job_id: str,
    org_id,
    failed: bool,
    step_name: str | None,
    level: str | None,
    follow: bool,
    all_levels: bool,
    no_pager: bool,
):
    """View logs for a job, grouped by step.

    \b
    Examples:
        avr job logs job-abc123
        avr job logs job-abc123 --failed
        avr job logs job-abc123 --step "Build" --level error
        avr job logs job-abc123 --follow
    """
    if follow and failed:
        raise click.UsageError("--follow and --failed cannot be combined")

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)

    try:
        response = client.public_get(
            f"/orgs/{org_id}/jobs/{job_id}",
            params={"include": ["steps"]},
        )
        job_data = response.get("data", response)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "get job")

    steps = job_data.get("steps") or []

    started_at = job_data.get("started_at")
    completed_at = job_data.get("completed_at")
    if started_at:
        click.echo(click.style(f"Started:   {started_at}", fg=DIM_FG))
    if completed_at:
        click.echo(click.style(f"Completed: {completed_at}", fg=DIM_FG))

    log_links_enabled = ctx.obj.get("links_enabled", False)
    log_url: str | None = None
    if log_links_enabled:
        log_url = job_url(get_console_url(config.public_api_url), get_org_slug(client, org_id), job_id)

    if follow:
        follow_logs(
            client,
            org_id,
            job_id,
            show_all_levels=all_levels,
            link_url=log_url,
            mark_steps=True,
            job_name=job_data.get("job_name"),
        )
        return

    # mark_steps left off — OSC 133 marks survive direct-to-TTY but `less`
    # doesn't strip OSC sequences, so they'd leak as visible bytes when paged.
    log_buf: list[str] = []
    if failed:
        print_failed_step_logs(
            client,
            job_id,
            steps,
            show_all_levels=all_levels,
            emit=log_buf.append,
            link_url=log_url,
        )
        page_output("\n".join(log_buf), bypass=no_pager)
        return

    entries = fetch_all_logs(
        client,
        job_id,
        step_name=step_name,
        level=level,
        show_all_levels=all_levels,
    )
    if not entries:
        state = job_data.get("state", "unknown")
        if state in ("queued", "in_progress"):
            click.echo(f"No log entries found (job is {state}). Use --follow to wait for logs.")
        else:
            click.echo("No log entries found.")
        return
    print_logs_grouped(entries, emit=log_buf.append, link_url=log_url, steps=steps)
    page_output("\n".join(log_buf), bypass=no_pager)


def _format_job_status(job_data: dict[str, Any]) -> str:
    """Render a single-line status for the metrics header.

    Examples:
      ``● running (1m 22s)``
      ``✓ success (3m 45s, finished 5m ago)``
      ``✗ failure (3m 47s, finished 1h ago)``
    """
    state = job_data.get("state", "")
    conclusion = job_data.get("conclusion")
    indicator = status_indicator(state, conclusion)
    label = conclusion or state or "unknown"
    duration = format_duration(job_data.get("duration_seconds"))
    completed_at = job_data.get("completed_at")
    if state == "completed" and completed_at:
        when = format_relative_timestamp(completed_at)
        return f"{indicator} {label} ({duration}, finished {when})"
    return f"{indicator} {label} ({duration})"


def _fetch_metrics_sources(
    client: ApiClient,
    org_id: str,
    job_id: str,
    sources: tuple[str, ...],
    *,
    start: int | None,
    end: int | None,
) -> dict[str, dict[str, Any]]:
    """Fetch metrics for each source.

    404 is treated as "no execution yet" (queued job) so the user still sees
    a partial pane. Other failures propagate — the caller decides whether to
    abort (static path) or render the error in the footer (live path)."""
    params: dict[str, Any] = {}
    if start is not None:
        params["start"] = start
    if end is not None:
        params["end"] = end

    out: dict[str, dict[str, Any]] = {}
    for src in sources:
        try:
            response = client.public_get(f"/orgs/{org_id}/jobs/{job_id}/metrics/{src}", params=params or None)
        except httpx.HTTPStatusError as exc:
            if exc.response.status_code == 404:
                # Job has no execution yet (queued) — keep going so partial sources still render.
                out[src] = {"series": [], "_error": "no execution"}
                continue
            raise
        out[src] = response
    return out


def _resolve_vm_specs(job_data: dict[str, Any]) -> tuple[int | None, int | None, str | None]:
    """Best-effort runner spec extraction from job labels.

    Returns (cpus, ram_bytes, matched_label). The matched_label is the first
    runner label whose vCPU pattern hit — surfaced in the metrics header so
    the user can see exactly which runner spec the gauges are scaled to.
    """
    labels = job_data.get("job_labels") or []
    specs = parse_runner_specs(labels)
    cpus = specs.get("cpus")
    ram_gb = specs.get("memory_gb")
    matched = next((label for label in labels if "vcpu" in label.lower()), None)
    return cpus, (ram_gb * 1024**3 if ram_gb else None), matched


@job.command("metrics")
@click.argument("job_id")
@click.option("--org", "org_id", help="Organization ID.")
@click.option(
    "--source",
    "sources",
    multiple=True,
    type=click.Choice(list(ALL_SOURCES)),
    help="Metric source (repeatable). Defaults to cpu and memory.",
)
@click.option("--start", type=int, help="Start time (Unix seconds). Defaults to execution start.")
@click.option("--end", type=int, help="End time (Unix seconds). Defaults to execution end or now.")
@click.option("-w", "--watch", "live", is_flag=True, help="Refresh every 5 seconds (Ctrl-C to exit).")
@click.option("--json", "json_output", is_flag=True, help="Output raw metrics responses as JSON.")
@click.pass_context
def job_metrics(ctx, job_id, org_id, sources, start, end, live, json_output):
    """Show CPU/memory/IO gauges for a job's VM.

    \b
    Examples:
        avr job metrics job-abc123
        avr job metrics job-abc123 --source cpu --source network
        avr job metrics job-abc123 --watch
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)
    selected = sources or ("cpu", "memory")

    try:
        job_resp = client.public_get(f"/orgs/{org_id}/jobs/{job_id}")
        job_data = job_resp.get("data", job_resp)
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "get job")

    vm_cpus, vm_ram_bytes, vm_label = _resolve_vm_specs(job_data)
    job_finished = job_data.get("state") == "completed" or bool(job_data.get("conclusion"))

    # --live on a finished job has nothing to watch; fall through to the
    # static post-mortem path instead of looping over unchanging data.
    if live and job_finished:
        click.echo(
            click.style("Job already finished — showing static metrics.", fg=DIM_FG),
            err=True,
        )
        live = False

    def _build_frame(fetched: dict[str, dict[str, Any]], log_tail: list[dict[str, Any]] | None = None) -> str:
        if json_output:
            return json.dumps(fetched, indent=2, default=str)
        header: dict[str, str] = {"Job": job_id, "Name": job_data.get("job_name", "-")}
        status_line = _format_job_status(job_data)
        if status_line:
            header["Status"] = status_line
        if vm_cpus is not None and vm_ram_bytes is not None:
            header["VM"] = f"{vm_cpus} vCPU, {vm_ram_bytes // 1024**3} GB RAM"
        if vm_label:
            header["Runner"] = vm_label
        lines = [format_key_value(header), ""]
        for src in selected:
            lines.append(render_gauge_line(src, fetched[src], vm_ram_bytes, live=live))
        if log_tail is not None:
            lines.append("")
            lines.append(click.style("LOGS", bold=True, fg="bright_white"))
            if log_tail:
                for entry in log_tail:
                    formatted = format_log_line(entry)
                    if formatted is not None:
                        lines.append(formatted)
            else:
                lines.append(click.style("(no log lines yet)", fg=DIM_FG))
        return "\n".join(lines)

    if not live:
        try:
            fetched = _fetch_metrics_sources(client, org_id, job_id, selected, start=start, end=end)
        except httpx.HTTPStatusError as exc:
            handle_http_error(exc, "get metrics")
        click.echo(_build_frame(fetched))
        if not json_output:
            _hint(f"View logs:  avr job logs {job_id}")
        return

    # Build the next frame in memory before clearing the screen — the
    # ~1-2s fetch would otherwise leave the user staring at a blank
    # screen until the response lands.
    log_tail: deque[dict[str, Any]] = deque(maxlen=_LIVE_LOG_TAIL_LINES)
    last_log_line: int | None = None
    fetched: dict[str, dict[str, Any]] | None = None
    last_metrics_at = 0.0
    footer = click.style(
        f"(logs every {_LIVE_LOG_REFRESH_S:g}s, metrics every {_LIVE_METRICS_REFRESH_S:g}s — Ctrl-C to exit)",
        fg=DIM_FG,
    )
    log_error: str | None = None
    metrics_error: str | None = None
    try:
        while True:
            now = time.monotonic()
            if fetched is None or (now - last_metrics_at) >= _LIVE_METRICS_REFRESH_S:
                # Soft-retry on 5xx/429/transport errors so a transient hiccup
                # doesn't kill the watcher; previous frame stays visible. Other
                # 4xx (auth lost, job deleted) is terminal — surface it rather
                # than render a stale frame indefinitely.
                try:
                    fetched = _fetch_metrics_sources(client, org_id, job_id, selected, start=start, end=end)
                except httpx.HTTPStatusError as exc:
                    code = exc.response.status_code
                    if 400 <= code < 500 and code != 429:
                        handle_http_error(exc, "fetch metrics")
                    metrics_error = f"metrics fetch failed: HTTP {code}"
                except (httpx.ConnectError, httpx.TimeoutException) as exc:
                    metrics_error = f"metrics fetch failed: {type(exc).__name__}"
                else:
                    metrics_error = None
                last_metrics_at = time.monotonic()
            try:
                new_entries, new_cursor = fetch_logs_after(client, job_id, after_line=last_log_line)
            except httpx.HTTPStatusError as exc:
                code = exc.response.status_code
                if 400 <= code < 500 and code != 429:
                    handle_http_error(exc, "fetch logs")
                new_entries, new_cursor = [], last_log_line
                log_error = f"log fetch failed: HTTP {code}"
            except (httpx.ConnectError, httpx.TimeoutException) as exc:
                # Live mode polls on a tight loop — a transient API blip
                # shouldn't kill the watcher. Surface it in the LOGS pane so
                # the user knows we're stalled, then retry next tick.
                new_entries, new_cursor = [], last_log_line
                log_error = f"log fetch failed: {type(exc).__name__}"
            else:
                log_error = None
            if new_entries:
                log_tail.extend(new_entries)
                last_log_line = new_cursor
            frame_footer = footer
            if metrics_error or log_error:
                error_lines = [click.style(e, fg="red") for e in (metrics_error, log_error) if e]
                frame_footer = "\n".join(error_lines) + "\n" + footer
            # `fetched` may still be None on the very first tick if the initial
            # metrics fetch raised. Render an empty placeholder so the frame
            # still draws — the error footer tells the user why values are absent.
            frame = _build_frame(fetched or {src: {"series": []} for src in selected}, list(log_tail))
            frame = frame + "\n\n" + frame_footer
            click.clear()
            click.echo(frame)
            time.sleep(_LIVE_LOG_REFRESH_S)
    except KeyboardInterrupt:
        pass
