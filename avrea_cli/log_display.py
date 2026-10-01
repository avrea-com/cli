"""Log fetching, formatting, and display for CLI commands."""

from avrea_cli.api_client import ApiClient
from avrea_cli.display import DIM_FG
from avrea_cli.display import conclusion_to_exit_code
from avrea_cli.display import hyperlink
from avrea_cli.display import notify
from avrea_cli.display import osc133_done
from avrea_cli.display import osc133_output
from avrea_cli.display import osc133_prompt
from avrea_cli.display import status_indicator
from avrea_cli.display import terminal_title
from avrea_cli.helpers import handle_http_error
from collections.abc import Callable
from typing import Any
import click
import httpx
import time

# Default sink — print one line. Callers wanting to buffer (e.g. for paging)
# pass a list's append method instead.
_Emit = Callable[[str], None]

HIDDEN_LEVELS = frozenset({"diagnostic"})

LEVEL_COLORS: dict[str, str] = {
    "error": "red",
    "warning": "yellow",
    "notice": "cyan",
    "debug": "bright_black",
}

_GROUP_PREFIX = "##[group]"
_ENDGROUP_PREFIX = "##[endgroup]"
_COMMAND_PREFIX = "##[command]"

# Runner log line that signals job teardown — used as an early-drain
# sentinel. If GitHub rewords it, the state=completed path below still
# drains correctly, just a few seconds later.
_JOB_FINISH_MARKER = "Cleaning up orphan processes"

# Pause after state=completed before the final log fetch. The runner →
# ingestion → DB → search-index pipeline needs a few seconds to flush;
# smaller values miss tail lines.
_FINAL_DRAIN_SECONDS = 8


def _format_timestamp(ts: str | None) -> str:
    """Extract HH:MM:SS from an ISO timestamp, or empty string."""
    if not ts:
        return ""
    # "2026-04-21T15:25:27.723Z" → "15:25:27.723"
    try:
        t_part = ts.split("T", 1)[1] if "T" in ts else ts
        return t_part[:12].rstrip("Z")
    except IndexError, TypeError:
        return ""


def format_log_line(entry: dict[str, Any], *, link_url: str | None = None) -> str | None:
    """Format a single log entry for display.

    Returns None for lines that should be hidden (endgroup markers).
    Group headers are bolded, command lines are cyan. Timestamps prefixed.

    When ``link_url`` is set, the timestamp is wrapped in an OSC 8
    hyperlink so a click jumps to the corresponding view in the console
    (today: the job page; later: anchored to the specific log line).
    """
    content = entry.get("content", "")
    ts = _format_timestamp(entry.get("timestamp"))
    if ts:
        ts_styled = click.style(ts, fg=DIM_FG)
        if link_url:
            ts_styled = hyperlink(ts_styled, link_url)
        prefix = ts_styled + " "
    else:
        prefix = ""

    if content.startswith(_ENDGROUP_PREFIX):
        return None
    if content.startswith(_GROUP_PREFIX):
        return prefix + click.style(content[len(_GROUP_PREFIX) :], bold=True)
    if content.startswith(_COMMAND_PREFIX):
        return prefix + click.style(content[len(_COMMAND_PREFIX) :], fg="cyan")

    level = entry.get("level", "info")
    color = LEVEL_COLORS.get(level)
    if color:
        return prefix + click.style(content, fg=color)
    return prefix + content


def fetch_logs_after(
    client: ApiClient,
    job_id: str,
    *,
    after_line: int | None = None,
    show_all_levels: bool = False,
) -> tuple[list[dict[str, Any]], int | None]:
    """Page through all log entries strictly after ``after_line`` and return
    them along with the new cursor. ``after_line=None`` returns every line so
    far — appropriate for the first tick of a tail loop, where the deque on
    the consumer trims to the visible tail.

    Returns ``(entries, new_after_line)``. ``new_after_line`` is None when no
    entries came back, so the caller can preserve its previous cursor.
    """
    out: list[dict[str, Any]] = []
    cursor = after_line
    while True:
        payload: dict[str, Any] = {"job_id": job_id, "limit": 1000, "order_by": "line_number"}
        if cursor is not None:
            payload["after_line"] = cursor
        # Errors propagate so callers can distinguish "no more logs" (empty
        # result) from "API/transport failure" (exception). The --live caller
        # catches httpx.HTTPError to retry on the next tick.
        response = client.public_post("/logs/search", json=payload)
        raw_entries: list[dict[str, Any]] = response.get("results", [])
        if not raw_entries:
            break
        # Advance the cursor from raw_entries (not the filtered set) so a page
        # that's entirely diagnostic-level can't pin us in place when the API
        # also fails to populate next_cursor — the alternative is an infinite
        # loop on the same after_line.
        prev_cursor = cursor
        cursor = response.get("next_cursor") or raw_entries[-1].get("line_number")
        entries = raw_entries
        if not show_all_levels:
            entries = [e for e in entries if e.get("level") not in HIDDEN_LEVELS]
        out.extend(entries)
        if not response.get("has_more", False):
            break
        # Cursor failed to advance but the server says there's more — refusing
        # to loop is the only safe answer. Keep the entries we already collected
        # and break so the caller surfaces a normal "no more logs" rather than
        # a hang.
        if cursor is None or cursor == prev_cursor:
            break
    return out, cursor


def fetch_all_logs(
    client: ApiClient,
    job_id: str,
    *,
    step_name: str | None = None,
    level: str | None = None,
    show_all_levels: bool = False,
) -> list[dict[str, Any]]:
    """Fetch all log entries for a job, paginating via after_line cursor."""
    results: list[dict[str, Any]] = []
    after_line: int | None = None

    while True:
        payload: dict[str, Any] = {
            "job_id": job_id,
            "limit": 1000,
            "order_by": "line_number",
        }
        if level:
            payload["level"] = level
        if after_line is not None:
            payload["after_line"] = after_line

        try:
            response = client.public_post("/logs/search", json=payload)
        except httpx.HTTPStatusError as exc:
            # A job can be recorded as completed without ever receiving an
            # execution or GitHub log source. The API represents that state as
            # 404, which is equivalent to an empty result for a static read.
            if exc.response.status_code == 404:
                break
            raise
        raw_entries: list[dict[str, Any]] = response.get("results", [])

        if not raw_entries:
            break

        entries = raw_entries
        if not show_all_levels and not level:
            entries = [e for e in entries if e.get("level") not in HIDDEN_LEVELS]
        if step_name:
            entries = [e for e in entries if e.get("step_name") == step_name]

        results.extend(entries)

        after_line = response.get("next_cursor") or raw_entries[-1].get("line_number")

        if not response.get("has_more", False):
            break

    return results


def _build_step_conclusions(steps: list[dict[str, Any]] | None) -> dict[str, str | None]:
    """Map step name → conclusion. Used to drive OSC 133;D exit codes so
    failed steps light up the gutter in supporting terminals."""
    if not steps:
        return {}
    return {s["name"]: s.get("conclusion") for s in steps if s.get("name")}


def print_logs_grouped(
    entries: list[dict[str, Any]],
    *,
    emit: _Emit = click.echo,
    link_url: str | None = None,
    steps: list[dict[str, Any]] | None = None,
    mark_steps: bool = False,
) -> None:
    """Print log entries grouped by step_name with section headers.

    Pass ``emit=buffer.append`` (a list method) to capture output for paging
    instead of streaming to stdout. ``link_url`` wraps both the per-step
    header (``--- Set up job ---``) and per-line timestamps in OSC 8
    hyperlinks — same target as the job header for now; switch to a
    step-anchor URL when the console supports it.

    ``mark_steps=True`` sandwiches each step header in OSC 133;A/C/D
    boundaries so kitty / iTerm2 / VS Code / Ghostty etc. can navigate
    between steps and color failed ones in the gutter. ``steps`` provides
    the conclusion lookup for accurate exit codes on the closing mark."""
    conclusions = _build_step_conclusions(steps)
    current_step: str | None = None
    prev_marked_step: str | None = None
    for entry in entries:
        step = entry.get("step_name") or "(unknown step)"
        if step != current_step:
            current_step = step
            emit("")
            header = click.style(f"--- {step} ---", bold=True)
            header_text = hyperlink(header, link_url) if link_url else header
            if mark_steps:
                close = ""
                if prev_marked_step:
                    close = osc133_done(conclusion_to_exit_code(conclusions.get(prev_marked_step)))
                emit(close + osc133_prompt() + header_text + osc133_output())
                prev_marked_step = step
            else:
                emit(header_text)
        line = format_log_line(entry, link_url=link_url)
        if line is not None:
            emit(line)
    if mark_steps and prev_marked_step is not None:
        emit(osc133_done(conclusion_to_exit_code(conclusions.get(prev_marked_step))))


def print_failed_step_logs(
    client: ApiClient,
    job_id: str,
    steps: list[dict[str, Any]],
    *,
    show_all_levels: bool = False,
    emit: _Emit = click.echo,
    link_url: str | None = None,
    mark_steps: bool = False,
) -> None:
    """Fetch logs and print only entries from failed steps. ``emit`` follows
    the same buffer-or-print contract as ``print_logs_grouped``.
    ``link_url`` makes per-line timestamps clickable. ``mark_steps`` adds
    OSC 133 boundaries so terminals can navigate between failed sections."""
    failed_steps = [s for s in steps if s.get("conclusion") and s["conclusion"] not in ("success", "skipped")]
    if not failed_steps:
        emit("No failed steps.")
        return

    all_logs = fetch_all_logs(client, job_id, show_all_levels=show_all_levels)
    failed_conclusion = {s["name"]: s.get("conclusion", "failure") for s in failed_steps}

    prev_conclusion: str | None = None
    for step in failed_steps:
        name = step["name"]
        step_logs = [e for e in all_logs if e.get("step_name") == name]
        if not step_logs:
            continue
        conclusion = failed_conclusion.get(name, "failure")
        indicator = status_indicator("completed", conclusion)
        emit("")
        header = click.style(f"--- {indicator} {name} ({conclusion}) ---", bold=True)
        header_text = hyperlink(header, link_url) if link_url else header
        if mark_steps:
            close = osc133_done(conclusion_to_exit_code(prev_conclusion)) if prev_conclusion is not None else ""
            emit(close + osc133_prompt() + header_text + osc133_output())
            prev_conclusion = conclusion
        else:
            emit(header_text)
        for entry in step_logs:
            line = format_log_line(entry, link_url=link_url)
            if line is not None:
                emit(line)
    if mark_steps and prev_conclusion is not None:
        emit(osc133_done(conclusion_to_exit_code(prev_conclusion)))


def _print_follow_entries(
    entries: list[dict[str, Any]],
    current_step: str | None,
    *,
    link_url: str | None = None,
    mark_steps: bool = False,
    step_conclusions: dict[str, str | None] | None = None,
) -> str | None:
    """Print log entries with step headers. Returns the current step name.

    When ``mark_steps`` is set, surround step transitions with OSC 133;A/C
    marks; the closing D for the previous step uses the latest known
    conclusion from ``step_conclusions`` (best-effort — in --follow mode
    the dict updates each time the job-state poll fires, so transitions
    rendered before the next poll fall back to exit code 0)."""
    conclusions = step_conclusions or {}
    for entry in entries:
        step = entry.get("step_name") or "(unknown step)"
        if step != current_step:
            click.echo("")
            header = click.style(f"--- {step} ---", bold=True)
            header_text = hyperlink(header, link_url) if link_url else header
            if mark_steps:
                close = ""
                if current_step is not None:
                    close = osc133_done(conclusion_to_exit_code(conclusions.get(current_step)))
                click.echo(close + osc133_prompt() + header_text + osc133_output())
            else:
                click.echo(header_text)
            current_step = step
        line = format_log_line(entry, link_url=link_url)
        if line is not None:
            click.echo(line)
    return current_step


def follow_logs(
    client: ApiClient,
    org_id: str,
    job_id: str,
    *,
    interval: int = 2,
    show_all_levels: bool = False,
    link_url: str | None = None,
    mark_steps: bool = False,
    job_name: str | None = None,
) -> None:
    """Poll for new log lines until the job completes. Ctrl+C to stop.

    ``link_url`` is forwarded to per-line formatting so timestamps become
    clickable in terminals that support OSC 8. ``mark_steps`` emits OSC
    133;A/C/D boundaries on step transitions; in-stream D codes are
    best-effort from the most recent job-state poll, and a final D for
    the last step is emitted using the job's overall conclusion.
    ``job_name`` is used as the OSC 9 desktop-notification title fired on
    completion — useful when the user has tabbed away."""
    last_raw_line: int | None = None
    current_step: str | None = None
    idle_polls = 0
    saw_job_finish = False
    step_conclusions: dict[str, str | None] = {}

    # ``avr ▸ <job>: <stage>`` — same pattern as `avr run watch`.
    job_label = job_name or "job"

    def _title(stage: str | None) -> str:
        return f"avr ▸ {job_label}: {stage}" if stage else f"avr ▸ {job_label}"

    with terminal_title(_title(None)) as term_title:
        try:
            while True:
                payload: dict[str, Any] = {
                    "job_id": job_id,
                    "limit": 1000,
                    "order_by": "line_number",
                }
                if last_raw_line is not None:
                    payload["after_line"] = last_raw_line

                try:
                    response = client.public_post("/logs/search", json=payload)
                except httpx.HTTPStatusError as exc:
                    code = exc.response.status_code
                    # Soft-retry on 404/429/5xx; other 4xx is terminal and
                    # routes through handle_http_error so 401 honors the
                    # auth-hint exit-4 contract.
                    if code == 404 or code == 429 or code >= 500:
                        idle_polls += 1
                        time.sleep(interval)
                        continue
                    handle_http_error(exc, "fetch logs")
                except httpx.ConnectError, httpx.TimeoutException:
                    # Network blip — `--follow` is interactive; we'd rather
                    # hold the session open and retry than crash the user.
                    idle_polls += 1
                    time.sleep(interval)
                    continue
                else:
                    raw_entries: list[dict[str, Any]] = response.get("results", [])

                    if raw_entries:
                        last_raw_line = response.get("next_cursor") or raw_entries[-1].get("line_number")

                    for e in raw_entries:
                        if e.get("content", "").startswith(_JOB_FINISH_MARKER):
                            saw_job_finish = True

                    entries = raw_entries
                    if not show_all_levels:
                        entries = [e for e in entries if e.get("level") not in HIDDEN_LEVELS]

                    if entries:
                        idle_polls = 0
                        current_step = _print_follow_entries(
                            entries,
                            current_step,
                            link_url=link_url,
                            mark_steps=mark_steps,
                            step_conclusions=step_conclusions,
                        )
                        term_title.set(_title(current_step))
                    elif not raw_entries:
                        idle_polls += 1

                if saw_job_finish:
                    if mark_steps and current_step is not None:
                        click.echo(osc133_done(conclusion_to_exit_code(step_conclusions.get(current_step))))
                    # Best-effort fetch of the conclusion for the notification.
                    # The orphan-cleanup sentinel fires before the API surfaces
                    # state=completed, so we look it up explicitly here.
                    final_conclusion: str | None = None
                    try:
                        job_resp = client.public_get(f"/orgs/{org_id}/jobs/{job_id}")
                        final_conclusion = (job_resp.get("data", job_resp) or {}).get("conclusion")
                    except httpx.HTTPStatusError, httpx.ConnectError, httpx.TimeoutException:
                        pass
                    # Reset title before notify — Ghostty echoes the title
                    # in the notification source line; no need to repeat
                    # the conclusion that's already in the body.
                    term_title.set(_title(None))
                    notify(f"{job_label}: {final_conclusion or 'completed'}")
                    click.echo("\nJob completed.")
                    return

                if idle_polls > 0 and idle_polls % 2 == 0:
                    try:
                        job_resp = client.public_get(
                            f"/orgs/{org_id}/jobs/{job_id}",
                            params={"include": ["steps"]},
                        )
                    except httpx.HTTPStatusError, httpx.ConnectError, httpx.TimeoutException:
                        # Job-completion check failed; we'll try again next idle window.
                        pass
                    else:
                        job_data = job_resp.get("data", job_resp)
                        for s in job_data.get("steps") or []:
                            name = s.get("name")
                            if name:
                                step_conclusions[name] = s.get("conclusion")
                        if job_data.get("state") == "completed":
                            time.sleep(_FINAL_DRAIN_SECONDS)
                            final_payload: dict[str, Any] = {
                                "job_id": job_id,
                                "limit": 1000,
                                "order_by": "line_number",
                            }
                            if last_raw_line is not None:
                                final_payload["after_line"] = last_raw_line
                            drain_failed = False
                            for _attempt in range(3):
                                try:
                                    resp = client.public_post("/logs/search", json=final_payload)
                                except httpx.HTTPStatusError, httpx.ConnectError, httpx.TimeoutException:
                                    # Final drain failed; surface the warning
                                    # but don't crash — the user still gets
                                    # the conclusion and a re-fetch hint.
                                    drain_failed = True
                                    break
                                raw = resp.get("results", [])
                                if raw:
                                    last_raw_line = resp.get("next_cursor") or raw[-1].get("line_number")
                                    final_payload["after_line"] = last_raw_line
                                    display_entries = raw
                                    if not show_all_levels:
                                        display_entries = [
                                            e for e in display_entries if e.get("level") not in HIDDEN_LEVELS
                                        ]
                                    if display_entries:
                                        current_step = _print_follow_entries(
                                            display_entries,
                                            current_step,
                                            link_url=link_url,
                                            mark_steps=mark_steps,
                                            step_conclusions=step_conclusions,
                                        )
                                        term_title.set(_title(current_step))
                                if not resp.get("has_more", False):
                                    break
                            conclusion = job_data.get("conclusion", "unknown")
                            if drain_failed:
                                click.echo(
                                    click.style(
                                        "(warning: failed to drain final log batch — "
                                        f"re-run `avr job logs {job_id}` to see the tail)",
                                        fg=DIM_FG,
                                    ),
                                    err=True,
                                )
                            if mark_steps and current_step is not None:
                                # Final D for the last step uses the job's overall
                                # conclusion as the most reliable signal — per-step
                                # conclusions may not have caught up yet.
                                final_code = conclusion_to_exit_code(step_conclusions.get(current_step) or conclusion)
                                click.echo(osc133_done(final_code))
                            term_title.set(_title(None))
                            notify(f"{job_label}: {conclusion}")
                            click.echo(f"\nJob completed: {conclusion}")
                            return

                time.sleep(interval)
        except KeyboardInterrupt:
            click.echo("\nStopped following.")
