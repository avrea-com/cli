"""Status command -- snapshot of recent runs, cache health, and performance trends."""

from avrea_cli.api_client import ApiClient
from avrea_cli.config import CliConfig
from avrea_cli.display import DIM_FG
from avrea_cli.display import format_duration
from avrea_cli.display import get_console_url
from avrea_cli.display import hint as _hint
from avrea_cli.display import hyperlink
from avrea_cli.display import repo_url
from avrea_cli.display import run_url
from avrea_cli.display import status_indicator
from avrea_cli.display import truncate as _truncate
from avrea_cli.display import workflow_url
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import format_size
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import get_org_slug
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import parse_since
from avrea_cli.output import format_relative_timestamp
from avrea_cli.repo_context import resolve_repo_or_detect
from datetime import UTC
from datetime import datetime
from typing import Any
import click
import httpx
import json

MIN_SUCCESS_FOR_SLOW = 2  # Items with fewer successful runs are excluded from slowest panels
MIN_SUCCESS_FOR_TREND = 3  # Items with fewer successful runs are excluded from slowing-down panels
TOP_N = 5


def _success_median(b: dict) -> float:
    """Prefer success-only median; timed-out / failed runs would otherwise dominate."""
    return b.get("success_median_duration_seconds") or 0


def _fetch_aggregate(
    client: ApiClient,
    org_id: str,
    endpoint: str,
    repo_id: str | None,
    created_after: datetime,
    created_before: datetime | None = None,
) -> list[dict]:
    """Fetch one stats panel. Auxiliary to the main run-list, so we don't
    abort the whole status view on failure — but we *do* surface the cause
    to stderr. A silently empty panel reads identical to "no slow workflows"
    when in fact the data was never fetched (auth lost, backend outage)."""
    params: dict[str, Any] = {"time_bucket": "total", "created_after": created_after.isoformat()}
    if created_before is not None:
        params["created_before"] = created_before.isoformat()
    if repo_id:
        params["repository_ids"] = [repo_id]
    try:
        response = client.public_get(f"/orgs/{org_id}/{endpoint}", params=params)
        return response.get("data", [])
    except httpx.HTTPStatusError as exc:
        _hint(f"(could not load {endpoint}: HTTP {exc.response.status_code})")
        return []
    except (httpx.ConnectError, httpx.TimeoutException) as exc:
        _hint(f"(could not load {endpoint}: {type(exc).__name__})")
        return []


def _hdr(label: str, width: int, right: bool = False) -> str:
    aligned = f"{label:>{width}s}" if right else f"{label:{width}s}"
    return click.style(aligned, fg=DIM_FG, underline=True)


def _print_slowest_workflows(items: list[dict], *, console_url: str = "", slug: str = "") -> None:
    items = [b for b in items if _success_median(b) > 0 and b.get("success_count", 0) >= MIN_SUCCESS_FOR_SLOW]
    items.sort(key=_success_median, reverse=True)
    items = items[:TOP_N]
    if not items:
        return

    click.echo(click.style("SLOWEST WORKFLOWS", bold=True, fg="bright_white"))
    click.echo()
    W = {"rank": 3, "name": 40, "repo": 28, "median": 12, "runs": 5}
    s = "  "
    click.echo(
        f"  {_hdr('#', W['rank'], right=True)}{s}{_hdr('NAME', W['name'])}{s}"
        f"{_hdr('REPOSITORY', W['repo'])}{s}"
        f"{_hdr('MEDIAN', W['median'], right=True)}{s}{_hdr('RUNS', W['runs'], right=True)}"
    )
    for i, b in enumerate(items, start=1):
        wf = b.get("workflow") or {}
        name = f"{_truncate(wf.get('name', '(unknown)'), W['name'] - 2):{W['name']}s}"
        repo = f"{_truncate(wf.get('repository_full_name', ''), W['repo'] - 2):{W['repo']}s}"
        median = f"{format_duration(_success_median(b)):>{W['median']}s}"
        runs = f"{b.get('success_count', 0):>{W['runs']}d}"
        # Pad-then-style (ANSI bytes break column width); ids may be absent.
        name_cell = click.style(name, bold=True)
        repo_cell = click.style(repo, dim=True)
        if console_url and slug:
            wf_id = wf.get("workflow_id")
            if wf_id:
                name_cell = hyperlink(name_cell, workflow_url(console_url, slug, wf_id))
            r_id = wf.get("repository_id")
            if r_id:
                repo_cell = hyperlink(repo_cell, repo_url(console_url, slug, r_id))
        click.echo(f"  {i:>{W['rank']}d}{s}{name_cell}{s}{repo_cell}{s}{median}{s}{runs}")


def _print_slowest_jobs(items: list[dict]) -> None:
    items = [b for b in items if _success_median(b) > 0 and b.get("success_count", 0) >= MIN_SUCCESS_FOR_SLOW]
    items.sort(key=_success_median, reverse=True)
    items = items[:TOP_N]
    if not items:
        return

    click.echo(click.style("SLOWEST JOBS", bold=True, fg="bright_white"))
    click.echo()
    W = {"rank": 3, "name": 40, "median": 12, "runs": 5}
    s = "  "
    click.echo(
        f"  {_hdr('#', W['rank'], right=True)}{s}{_hdr('NAME', W['name'])}{s}"
        f"{_hdr('MEDIAN', W['median'], right=True)}{s}{_hdr('RUNS', W['runs'], right=True)}"
    )
    for i, b in enumerate(items, start=1):
        name_raw = (b.get("job") or {}).get("name", "(unknown)")
        name = f"{_truncate(name_raw, W['name'] - 2):{W['name']}s}"
        median = f"{format_duration(_success_median(b)):>{W['median']}s}"
        runs = f"{b.get('success_count', 0):>{W['runs']}d}"
        click.echo(f"  {i:>{W['rank']}d}{s}{click.style(name, bold=True)}{s}{median}{s}{runs}")


def _compute_deltas(
    current: list[dict],
    previous: list[dict],
    key_fn,
) -> list[dict]:
    """Compute median-duration deltas between two aggregate windows.

    Returns items that appear in both windows with enough runs, sorted by
    absolute percent increase descending.
    """
    prev_by_key = {key_fn(b): b for b in previous}
    results = []
    for cur in current:
        k = key_fn(cur)
        prev = prev_by_key.get(k)
        if prev is None:
            continue
        cur_median = _success_median(cur)
        prev_median = _success_median(prev)
        if cur_median <= 0 or prev_median <= 0:
            continue
        if cur.get("success_count", 0) < MIN_SUCCESS_FOR_TREND or prev.get("success_count", 0) < MIN_SUCCESS_FOR_TREND:
            continue
        delta_pct = 100 * (cur_median - prev_median) / prev_median
        if delta_pct <= 0:
            continue
        results.append(
            {
                **cur,
                "_cur_median": cur_median,
                "_prev_median": prev_median,
                "_delta_pct": delta_pct,
            }
        )
    results.sort(key=lambda r: r["_delta_pct"], reverse=True)
    return results[:TOP_N]


def _fmt_delta(pct: float, width: int) -> str:
    """Right-align a delta percentage to ``width`` and then color it.

    Width-padding the styled string would count ANSI escape bytes as visible
    columns, shifting the cell visibly left. Apply width formatting on the
    plain text first, then wrap with style."""
    plain = f"+{pct:.0f}%"
    return click.style(f"{plain:>{width}s}", fg="red", bold=True)


def _print_slowing_down_workflows(items: list[dict], *, console_url: str = "", slug: str = "") -> None:
    if not items:
        return
    click.echo(click.style("WORKFLOWS SLOWING DOWN", bold=True, fg="bright_white"))
    click.echo()
    W = {"rank": 3, "name": 40, "median": 12, "change": 10}
    s = "  "
    click.echo(
        f"  {_hdr('#', W['rank'], right=True)}{s}{_hdr('NAME', W['name'])}{s}"
        f"{_hdr('CURRENT', W['median'], right=True)}{s}{_hdr('CHANGE', W['change'], right=True)}"
    )
    for i, b in enumerate(items, start=1):
        wf = b.get("workflow") or {}
        name = f"{_truncate(wf.get('name', '(unknown)'), W['name'] - 2):{W['name']}s}"
        median = f"{format_duration(b['_cur_median']):>{W['median']}s}"
        change = _fmt_delta(b["_delta_pct"], W["change"])
        name_cell = click.style(name, bold=True)
        if console_url and slug and wf.get("workflow_id"):
            name_cell = hyperlink(name_cell, workflow_url(console_url, slug, wf["workflow_id"]))
        click.echo(f"  {i:>{W['rank']}d}{s}{name_cell}{s}{median}{s}{change}")


def _print_slowing_down_jobs(items: list[dict]) -> None:
    if not items:
        return
    click.echo(click.style("JOBS SLOWING DOWN", bold=True, fg="bright_white"))
    click.echo()
    W = {"rank": 3, "name": 40, "median": 12, "change": 10}
    s = "  "
    click.echo(
        f"  {_hdr('#', W['rank'], right=True)}{s}{_hdr('NAME', W['name'])}{s}"
        f"{_hdr('CURRENT', W['median'], right=True)}{s}{_hdr('CHANGE', W['change'], right=True)}"
    )
    for i, b in enumerate(items, start=1):
        name_raw = (b.get("job") or {}).get("name", "(unknown)")
        name = f"{_truncate(name_raw, W['name'] - 2):{W['name']}s}"
        median = f"{format_duration(b['_cur_median']):>{W['median']}s}"
        change = _fmt_delta(b["_delta_pct"], W["change"])
        click.echo(f"  {i:>{W['rank']}d}{s}{click.style(name, bold=True)}{s}{median}{s}{change}")


@click.command("status")
@click.option("--org", "org_id", help="Organization ID or slug.")
@click.option("--repo", "repo_id", help="Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")
@click.option("--since", default="7d", show_default=True, help="Time window for stats panels: '7d', '24h', etc.")
@click.option("--json", "json_output", is_flag=True, help="Output raw JSON.")
@click.pass_context
def status(ctx, org_id, repo_id, since, json_output):
    """Show recent runs, performance stats, and cache health."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)

    org_id = get_org_id(config, org_id, client=client)
    slug = get_org_slug(client, org_id)

    repo_id = resolve_repo_or_detect(client, config, org_id, repo_id)

    # parse_since returns the absolute cutoff (now - window); recover the
    # window itself for the "previous period" lookback so we can render
    # period-over-period deltas.
    current_after = parse_since(since)
    now = datetime.now(UTC)
    window = now - current_after
    previous_after = current_after - window
    previous_before = current_after

    run_params: dict = {"limit": 5, "order": "created_at.desc", "include": ["workflow"]}
    if repo_id:
        run_params["repository_ids"] = [repo_id]

    try:
        runs_response = client.public_get(f"/orgs/{org_id}/workflow-runs", params=run_params)
        runs = runs_response.get("data", [])
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list workflow runs")

    wf_current = _fetch_aggregate(client, org_id, "workflow-runs/aggregate", repo_id, current_after)
    wf_previous = _fetch_aggregate(client, org_id, "workflow-runs/aggregate", repo_id, previous_after, previous_before)
    job_current = _fetch_aggregate(client, org_id, "jobs/aggregate", repo_id, current_after)
    job_previous = _fetch_aggregate(client, org_id, "jobs/aggregate", repo_id, previous_after, previous_before)

    wf_slowing = _compute_deltas(wf_current, wf_previous, lambda b: b.get("workflow_id"))
    job_slowing = _compute_deltas(job_current, job_previous, lambda b: (b.get("job") or {}).get("name"))

    # Fetch cache usage if repo context. Auxiliary to the main view, so a
    # transient failure shouldn't abort — but the user needs to know they're
    # looking at incomplete data, not a fresh repo with no cache.
    cache_data = None
    if repo_id:
        try:
            cache_response = client.public_get(f"/orgs/{org_id}/repos/{repo_id}/cache")
            cache_data = cache_response.get("data")
        except httpx.HTTPStatusError as exc:
            _hint(f"(could not load cache: HTTP {exc.response.status_code})")
        except (httpx.ConnectError, httpx.TimeoutException) as exc:
            _hint(f"(could not load cache: {type(exc).__name__})")

    # Fetch repo metadata (full_name, platform) for the LINKS footer.
    repo_info: dict[str, Any] | None = None
    if repo_id:
        try:
            resp = client.public_get(f"/orgs/{org_id}/repos/{repo_id}")
            repo_info = resp.get("data")
        except httpx.HTTPStatusError as exc:
            _hint(f"(could not load repo metadata: HTTP {exc.response.status_code})")
        except (httpx.ConnectError, httpx.TimeoutException) as exc:
            _hint(f"(could not load repo metadata: {type(exc).__name__})")

    if json_output:
        click.echo(
            json.dumps(
                {
                    "org": slug,
                    "since": since,
                    "runs": runs,
                    "slowest_workflows": wf_current,
                    "slowest_jobs": job_current,
                    "workflows_slowing_down": wf_slowing,
                    "jobs_slowing_down": job_slowing,
                    "cache": cache_data,
                },
                indent=2,
                default=str,
            )
        )
        return

    # OSC 8 link context shared across all panels.
    links_enabled = ctx.obj.get("links_enabled", False)
    link_console_url = get_console_url(config.public_api_url) if links_enabled else ""
    link_slug = slug if links_enabled else ""

    click.echo(f"Organization: {click.style(slug, bold=True)}  ({since})")
    click.echo()

    if runs:
        click.echo(click.style("RECENT RUNS", bold=True, fg="bright_white"))
        click.echo()
        W = {"title": 50, "wf": 30, "branch": 30, "event": 18, "elapsed": 12, "age": 12, "id": 36}
        s = " "
        click.echo(
            f"  {_hdr('TITLE', W['title'])}{s}{_hdr('WORKFLOW', W['wf'])}{s}{_hdr('BRANCH', W['branch'])}{s}"
            f"{_hdr('EVENT', W['event'])}{s}{_hdr('ID', W['id'])}{s}"
            f"{_hdr('ELAPSED', W['elapsed'])}{s}{_hdr('AGE', W['age'])}"
        )
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
            title_cell = title
            wf_cell = click.style(wf, fg="magenta")
            br_cell = click.style(br, bold=True)
            r_id_cell = click.style(f"{r_id:{W['id']}s}", fg="cyan")
            if link_console_url and link_slug and r_id:
                url = run_url(link_console_url, link_slug, r_id)
                title_cell = hyperlink(title, url)
                wf_cell = hyperlink(wf_cell, url)
                r_id_cell = hyperlink(r_id_cell, url)
            if link_console_url and link_slug and head_branch and repo_full and "/" in repo_full:
                br_cell = hyperlink(br_cell, f"https://github.com/{repo_full}/tree/{head_branch}")
            click.echo(
                f"{ind} {title_cell}{s}"
                f"{wf_cell}{s}"
                f"{br_cell}{s}"
                f"{event}{s}{r_id_cell}{s}"
                f"{elapsed}{s}{click.style(age, dim=True)}"
            )
    else:
        click.echo("No recent workflow runs.")

    for panel_fn, items in (
        (_print_slowest_workflows, wf_current),
        (_print_slowing_down_workflows, wf_slowing),
    ):
        click.echo()
        panel_fn(items, console_url=link_console_url, slug=link_slug)
    for panel_fn, items in (
        (_print_slowest_jobs, job_current),
        (_print_slowing_down_jobs, job_slowing),
    ):
        click.echo()
        panel_fn(items)

    if cache_data:
        total = cache_data.get("total_size_bytes", 0)
        quota = cache_data.get("quota_bytes", 0)
        pct = (total / quota * 100) if quota > 0 else 0

        click.echo()
        click.echo(click.style("CACHE", bold=True, fg="bright_white"))
        click.echo()
        click.echo(f"  {format_size(total)} / {format_size(quota)} ({pct:.0f}%)")

        by_type = cache_data.get("by_type", [])
        if by_type:
            for bt in by_type:
                ct = bt.get("cache_type", "?")
                size = format_size(bt.get("size_bytes", 0))
                count = bt.get("entry_count", 0)
                click.echo(f"  {ct:12s} {size:>10s}  ({count} entries)")

    # Footer: deep links to the repo's pages on Avrea console + the source
    # platform (currently only GitHub). Skipped when there's no repo context
    # since the URLs would point at the org dashboard, which is already
    # surfaced via the org slug header above.
    if repo_info:
        full_name = repo_info.get("full_name", "")
        platform = (repo_info.get("platform") or "").lower()
        console_url = get_console_url(config.public_api_url)

        click.echo()
        click.echo(click.style("LINKS", bold=True, fg="bright_white"))
        click.echo()
        click.echo(f"  Avrea:   {console_url}/org/{slug}/activity?repositories={repo_id}")
        if platform == "github" and full_name:
            click.echo(f"  GitHub:  https://github.com/{full_name}/actions")

    click.echo()
    click.echo(click.style("  ✓ success  ✗ failure  ● running  ○ queued  — skipped/cancelled", fg=DIM_FG))
