"""Shared display utilities — status indicators, duration formatting, runner specs."""

from collections.abc import Callable
from typing import Final
from urllib.parse import urlparse
from urllib.parse import urlunparse
import click
import os
import re
import sys
import webbrowser


def is_piped() -> bool:
    """Return True when stdout is being piped or redirected to a file.

    List commands switch to a script-friendly format in this case: tab-separated
    columns with a header row, no truncation, ISO timestamps, no color — the
    standard scriptability convention.
    """
    try:
        return not sys.stdout.isatty()
    except AttributeError, ValueError:
        return True


def print_piped_header(columns: list[str]) -> None:
    """Emit the header row for piped output.

    Produces lower-snake-case column names (``run_id``, ``duration_seconds``)
    so consumers can ``awk -F'\\t' 'NR==1 {for(i=1;i<=NF;i++)c[$i]=i} NR>1 {...}'``
    without guessing positional order.
    """
    click.echo("\t".join(columns))


def print_piped_row(values: list[object]) -> None:
    """Emit a single tab-separated row for piped consumers (cut, awk, grep).

    None becomes an empty cell; everything else is str()'d. Numeric 0 stays
    "0" — callers shouldn't replace it with "" upstream."""
    click.echo("\t".join("" if v is None else str(v) for v in values))


def page_output(content: str, *, bypass: bool = False) -> None:
    """Print ``content``; pipe through a pager when stdout is a TTY and a pager
    is configured. AVR_PAGER takes priority over PAGER; setting either to the
    empty string disables paging. ``bypass=True`` (wired to ``--no-pager`` on
    log commands) skips paging unconditionally.

    Default ``LESS`` flags include ``K`` (``--quit-on-intr``) so Ctrl+C exits
    the pager the same way ``q`` does — matches what most users reach for
    first. We only set defaults when the user hasn't customized ``LESS``
    themselves; respect their env if so. ``F`` skips paging when content
    fits one screen, ``R`` preserves our ANSI colors, ``X`` keeps the
    output on screen after exit so users can scroll back in their
    terminal scrollback.

    A BrokenPipeError (user `q`'d out of less, or piped into ``head``) is
    swallowed: it's the consumer signaling "I'm done", not a CLI failure.
    KeyboardInterrupt is treated the same — pagers that don't honor ``K``
    still exit on SIGINT, and we shouldn't dump a traceback for a "bail
    out fast" gesture.
    """
    try:
        if bypass or is_piped():
            click.echo(content)
            return

        avr_pager = os.environ.get("AVR_PAGER")
        if avr_pager == "":
            click.echo(content)
            return

        pager_env = os.environ.get("PAGER")
        if avr_pager is None and pager_env == "":
            click.echo(content)
            return

        prev_less = os.environ.get("LESS")
        less_was_set = prev_less is not None
        if not less_was_set:
            os.environ["LESS"] = "FRXK"
        try:
            if avr_pager:
                # Custom AVR_PAGER overrides PAGER for click's resolution.
                prev = pager_env
                os.environ["PAGER"] = avr_pager
                try:
                    click.echo_via_pager(content)
                finally:
                    if prev is None:
                        os.environ.pop("PAGER", None)
                    else:
                        os.environ["PAGER"] = prev
            else:
                click.echo_via_pager(content)
        finally:
            if not less_was_set:
                os.environ.pop("LESS", None)
    except BrokenPipeError, KeyboardInterrupt:
        pass


# Foreground gray used everywhere we want "secondary" text — relative
# timestamps, hints, dim repo names, table headers. ANSI's `bright_black`
# (color 8) is too dark on most modern terminal themes; using the 256-color
# palette pulls a lighter mid-gray that stays visibly muted but readable.
DIM_FG: Final = 245


def _green(s: str) -> str:
    return click.style(s, fg="green")


def _red(s: str) -> str:
    return click.style(s, fg="red")


def _yellow(s: str) -> str:
    return click.style(s, fg="yellow")


def _gray(s: str) -> str:
    return click.style(s, fg=DIM_FG)


_CONCLUSION_MAP: dict[str | None, tuple[str, Callable[[str], str]]] = {
    "success": ("\u2713", _green),  # ✓
    "failure": ("\u2717", _red),  # ✗
    "cancelled": ("\u2014", _gray),  # —
    "skipped": ("\u2014", _gray),  # —
    "timed_out": ("\u2717", _red),  # ✗
    "action_required": ("\u25cf", _yellow),  # ●
    "neutral": ("\u2014", _gray),  # —
    "stale": ("\u2014", _gray),  # —
    "startup_failure": ("\u2717", _red),  # ✗
}

_STATE_MAP: dict[str, tuple[str, Callable[[str], str]]] = {
    "queued": ("\u25cb", _gray),  # ○
    "pending": ("\u25cb", _gray),  # ○
    "in_progress": ("\u25cf", _yellow),  # ●
}

_VCPU_RE = re.compile(r"-(\d+)-vcpu\b", re.IGNORECASE)

# vCPU → RAM (GB)
_VCPU_TO_RAM: dict[int, int] = {
    1: 2,
    2: 8,
    4: 16,
    8: 32,
    16: 64,
    32: 128,
}


def status_indicator(state: str, conclusion: str | None = None) -> str:
    """Return a colored status symbol for a workflow run or job.

    Examples: ✓ (green), ✗ (red), ● (yellow), ○ (gray), — (gray)
    """
    if conclusion:
        sym, color_fn = _CONCLUSION_MAP.get(conclusion, ("\u2014", _gray))
    else:
        sym, color_fn = _STATE_MAP.get(state, ("\u25cb", _gray))
    return color_fn(sym)


def format_duration(seconds: float | None) -> str:
    """Format a duration in seconds as a human-readable string.

    Examples: "14s", "2m 14s", "1h 5m 2s"
    """
    if seconds is None or seconds < 0:
        return "-"

    total = int(seconds)
    if total < 60:
        return f"{total}s"

    mins, secs = divmod(total, 60)
    if mins < 60:
        return f"{mins}m {secs:02d}s"

    hours, mins = divmod(mins, 60)
    return f"{hours}h {mins:02d}m {secs:02d}s"


def parse_runner_specs(labels: list[str]) -> dict[str, int | None]:
    """Extract CPU and memory from runner labels.

    Looks for patterns like 'avrea-ubuntu-latest-4-vcpu' and maps vCPU count
    to a memory tier.

    Returns dict with 'cpus' and 'memory_gb' keys (None if not detected).
    """
    for label in labels:
        m = _VCPU_RE.search(label)
        if m:
            cpus = int(m.group(1))
            return {"cpus": cpus, "memory_gb": _VCPU_TO_RAM.get(cpus)}
    return {"cpus": None, "memory_gb": None}


_CONCLUSION_COLOR: dict[str, str] = {
    "success": "green",
    "failure": "red",
    "cancelled": "bright_black",
    "skipped": "bright_black",
    "timed_out": "red",
    "startup_failure": "red",
    "neutral": "bright_black",
    "stale": "bright_black",
}


def format_conclusion_colored(status: str, conclusion: str | None, *, text: str | None = None) -> str:
    """Color a conclusion (or pre-padded cell) based on its terminal value.

    ``text`` overrides what gets rendered — useful for table cells that need
    fixed-width padding before the SGR escapes are applied. Falls through to
    the bare ``status`` when the run/job hasn't completed yet."""
    if status == "completed" and conclusion:
        color = _CONCLUSION_COLOR.get(conclusion)
        rendered = text if text is not None else conclusion
        return click.style(rendered, fg=color) if color else rendered
    return text if text is not None else status


def get_console_url(api_url: str) -> str:
    """Derive console URL from API URL. api.X -> console.X."""
    parsed = urlparse(api_url)
    host = parsed.hostname or ""
    if host.startswith("api."):
        host = "console." + host[4:]
    return urlunparse(parsed._replace(netloc=host, path=""))


class terminal_title:
    """Context manager that sets the terminal title (OSC 2) for the duration
    of a block and clears it on exit so the shell's prompt hook can re-set
    its own title on the next prompt.

    Useful for long-running watches — `avr run watch` sets the title to
    something like ``avr ▸ run 019de290 ▸ 3/7 jobs done`` so users with
    many tabs can spot the right one without alt-tabbing through six.
    tmux/screen pick this up via ``set-titles``; iTerm2 / GNOME Terminal /
    Konsole / Windows Terminal / WezTerm / Kitty / Alacritty all render it.

    No-op when stdout isn't a TTY — the OSC bytes would otherwise leak as
    visible garbage to pipes (click's ``strip_ansi`` only handles SGR/CSI).

    Usage::

        with terminal_title("avr ▸ run 019de290") as title:
            ...
            title.set("avr ▸ run 019de290 ▸ 3/7 jobs done")
    """

    # OSC 2 = set window title; ST (\x1b\\) terminator matches our OSC 8 form.
    _SET = "\x1b]2;{}\x1b\\"

    def __init__(self, initial: str = "", *, enabled: bool = True) -> None:
        # Gate on stderr.isatty(): OSC 2 is for the terminal, not the data
        # stream. Writing to stderr lets piped stdout still update the tab
        # title without poisoning redirect targets with escape bytes.
        self._enabled = False
        if enabled:
            try:
                self._enabled = sys.stderr.isatty()
            except AttributeError, ValueError:
                self._enabled = False
        if self._enabled and initial:
            self.set(initial)

    def set(self, title: str) -> None:
        """Update the title. Cheap to call every poll-tick — terminals dedupe
        identical writes, and the cost of an OSC sequence is dominated by
        the flush, not the formatting."""
        if not self._enabled:
            return
        sys.stderr.write(self._SET.format(title))
        sys.stderr.flush()

    def __enter__(self) -> terminal_title:
        return self

    def __exit__(self, *exc: object) -> None:
        # Empty title hands control back to the shell's prompt hook
        # (PROMPT_COMMAND / precmd / fish_title), which re-sets its own
        # default on the next prompt — the most graceful "restore".
        if self._enabled:
            sys.stderr.write(self._SET.format(""))
            sys.stderr.flush()


def hyperlink(text: str, url: str | None) -> str:
    """Wrap ``text`` in an OSC 8 terminal hyperlink, or return it unchanged
    when ``url`` is None/empty.

    Modern terminals (iTerm2, WezTerm, Kitty, GNOME Terminal, Konsole,
    Windows Terminal, Alacritty>=0.13) render OSC 8 as clickable; others
    ignore it. Caller is responsible for not passing a URL when stdout
    isn't a TTY — click's ``strip_ansi`` only handles SGR/CSI, not OSC,
    so the bytes would otherwise leak as visible garbage to pipes. The
    root CLI clamps ``ctx.obj['links_enabled']`` against ``isatty()``."""
    if not url:
        return text
    return f"\033]8;;{url}\033\\{text}\033]8;;\033\\"


# OSC 133 — FinalTerm "shell integration" semantic marks emitted around
# log step/job headers. Supporting terminals expose per-section navigation
# (jump-to-prev/next, gutter-color-on-failure); others strip them silently.
_OSC133_PROMPT_START: Final = "\x1b]133;A\x1b\\"
_OSC133_OUTPUT_START: Final = "\x1b]133;C\x1b\\"

_FAILURE_CONCLUSIONS: Final = frozenset(
    {"failure", "cancelled", "timed_out", "action_required", "startup_failure", "stale", "neutral"}
)


def _marks_supported() -> bool:
    try:
        return sys.stdout.isatty()
    except AttributeError, ValueError:
        return False


def osc133_prompt() -> str:
    """OSC 133;A — opens a new section (e.g. a step header is about to print)."""
    return _OSC133_PROMPT_START if _marks_supported() else ""


def osc133_output() -> str:
    """OSC 133;C — header has been printed, the section's body starts here."""
    return _OSC133_OUTPUT_START if _marks_supported() else ""


def osc133_done(exit_code: int = 0) -> str:
    """OSC 133;D;<code> — closes the current section. Non-zero codes drive
    failure-color gutter indicators in supporting terminals."""
    return f"\x1b]133;D;{exit_code}\x1b\\" if _marks_supported() else ""


def notify(message: str) -> None:
    """Fire an OSC 9 desktop notification through the terminal.

    iTerm2, Ghostty, WezTerm, kitty (and others) interpret the sequence
    ``ESC ] 9 ; <text> BEL`` as a request to show a system notification —
    handy when ``avr run watch`` or ``avr job logs --follow`` finishes
    while the user has tabbed away. Recipient terminals require the user
    to opt in to displaying notifications, so emitting unconditionally is
    safe; non-supporting terminals strip OSC sequences silently.

    Written to **stderr** so the notification still fires when stdout is
    being piped (``avr run watch --ndjson | jq``). Gated on
    ``stderr.isatty()`` so logs/files don't get the escape bytes."""
    try:
        if not sys.stderr.isatty():
            return
    except AttributeError, ValueError:
        return
    # Use BEL (\x07) terminator — broader compatibility than ST for OSC 9
    # specifically (iTerm's docs explicitly use BEL for the growl bridge).
    sys.stderr.write(f"\x1b]9;{message}\x07")
    sys.stderr.flush()


def conclusion_to_exit_code(conclusion: str | None) -> int:
    """Map a job/step conclusion string to a POSIX-style exit code so OSC
    133;D marks can drive failure highlighting. Anything not explicitly
    success/skipped is treated as failure (1)."""
    if not conclusion or conclusion in {"success", "skipped"}:
        return 0
    return 1 if conclusion in _FAILURE_CONCLUSIONS else 0


def run_url(console_url: str, slug: str, run_id: str) -> str:
    """Console deep-link for a workflow run."""
    return f"{console_url}/org/{slug}/runs/{run_id}"


def job_url(console_url: str, slug: str, job_id: str) -> str:
    """Console deep-link for a job."""
    return f"{console_url}/org/{slug}/jobs/{job_id}"


def workflow_url(console_url: str, slug: str, workflow_id: str) -> str:
    """Console deep-link for a workflow (matches ``avr workflow view --web``)."""
    return f"{console_url}/org/{slug}/workflows/{workflow_id}"


def repo_url(console_url: str, slug: str, repo_id: str) -> str:
    """Console deep-link for a repository — activity feed filtered to this repo."""
    return f"{console_url}/org/{slug}/activity?repositories={repo_id}"


def pr_url(console_url: str, slug: str, repo_id: str, number: int) -> str:
    """Console deep-link for a pull request."""
    return f"{console_url}/org/{slug}/repos/{repo_id}/pulls/{number}"


def hint(msg: str) -> None:
    """Print a dim, stderr-only hint line. Used for "next step" suggestions
    that shouldn't pollute stdout when the command output is piped."""
    click.echo(click.style(msg, fg=DIM_FG), err=True)


def open_or_print_url(url: str) -> None:
    """Open URL in a browser when stdout is a TTY; otherwise print it on stdout.

    `--web` over SSH or in a pipe (`avr run view --web > url.txt`) should
    not launch a browser — falling back to xdg-open in headless contexts
    hangs or errors and dumps "Opening: …" into the captured file."""
    if sys.stdout.isatty():
        click.echo(f"Opening: {url}")
        webbrowser.open(url)
    else:
        click.echo(url)


def truncate(s: str, width: int) -> str:
    """Truncate ``s`` to ``width`` columns with an ellipsis. Used to keep
    table cells fixed-width without ANSI-aware measurement; callers should
    style cells *after* truncating.

    For ``width <= 3`` the ellipsis itself wouldn't fit — fall back to a
    plain slice so the result never exceeds ``width``."""
    if len(s) <= width:
        return s
    if width <= 3:
        return s[:width]
    return s[: width - 3].rstrip() + "..."
