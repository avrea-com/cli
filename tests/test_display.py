"""Unit tests for display module — status indicators, duration, console URL."""

from avrea_cli.display import format_conclusion_colored
from avrea_cli.display import format_duration
from avrea_cli.display import get_console_url
from avrea_cli.display import hyperlink
from avrea_cli.display import job_url
from avrea_cli.display import page_output
from avrea_cli.display import pr_url
from avrea_cli.display import run_url
from avrea_cli.display import status_indicator
from avrea_cli.display import terminal_title
from io import StringIO
from unittest.mock import patch


class TestStatusIndicator:
    def test_success(self):
        assert "\u2713" in status_indicator("completed", "success")

    def test_failure(self):
        assert "\u2717" in status_indicator("completed", "failure")

    def test_unknown_fallback(self):
        # Unmapped states fall through to the queued symbol. The remaining
        # state/conclusion paths are direct dict lookups; success/failure
        # plus the fallback cover the two non-trivial branches.
        assert "\u25cb" in status_indicator("some_unknown_state")


class TestFormatDuration:
    def test_none(self):
        assert format_duration(None) == "-"

    def test_seconds(self):
        assert format_duration(45) == "45s"

    def test_minutes(self):
        assert format_duration(125) == "2m 05s"

    def test_hours(self):
        assert format_duration(3720) == "1h 02m 00s"

    def test_zero(self):
        assert format_duration(0) == "0s"


class TestFormatConclusion:
    def test_success_colored(self):
        result = format_conclusion_colored("completed", "success")
        assert "success" in result

    def test_not_completed(self):
        assert format_conclusion_colored("in_progress", None) == "in_progress"

    def test_unknown_conclusion(self):
        # Truly unmapped conclusions render uncolored. "neutral" and "stale"
        # are mapped (bright_black) so the table cell color matches what the
        # job-watch view uses; pick something not in the map for this test.
        result = format_conclusion_colored("completed", "unrecognized_value")
        assert result == "unrecognized_value"


class TestGetConsoleUrl:
    def test_api_to_console(self):
        assert get_console_url("https://api.avrea.com") == "https://console.avrea.com"

    def test_dev_url(self):
        assert get_console_url("https://api.dev.example.com") == "https://console.dev.example.com"


class TestHyperlink:
    """OSC 8 hyperlinks render as ESC]8;;<url>ESC\\<text>ESC]8;;ESC\\.
    Caller passes None / empty when stdout isn't a TTY — the function then
    must return text unchanged so click's strip_ansi doesn't have to know
    about OSC."""

    def test_returns_text_unchanged_when_url_is_none(self):
        assert hyperlink("run-abc", None) == "run-abc"

    def test_returns_text_unchanged_when_url_is_empty(self):
        assert hyperlink("run-abc", "") == "run-abc"

    def test_wraps_text_in_osc8_when_url_provided(self):
        result = hyperlink("run-abc", "https://example.com/run")
        assert result == "\033]8;;https://example.com/run\033\\run-abc\033]8;;\033\\"

    def test_url_with_query_and_fragment_passes_through(self):
        # No URL escaping in the helper itself — caller is responsible for
        # building a safe URL. Verify we don't double-encode or split.
        url = "https://x.test/run?attempt=2#step-3"
        result = hyperlink("run-abc", url)
        assert url in result


class TestEntityUrls:
    def test_run_url_template(self):
        assert (
            run_url("https://console.avrea.com", "acme", "run-abc") == "https://console.avrea.com/org/acme/runs/run-abc"
        )

    def test_job_url_template(self):
        assert (
            job_url("https://console.avrea.com", "acme", "job-xyz") == "https://console.avrea.com/org/acme/jobs/job-xyz"
        )

    def test_pr_url_template(self):
        assert (
            pr_url("https://console.avrea.com", "acme", "rep-xyz", 42)
            == "https://console.avrea.com/org/acme/repos/rep-xyz/pulls/42"
        )


class TestTerminalTitle:
    """OSC 2 sets the terminal window title. The format is
    `ESC ] 2 ; <title> ESC \\` — terminator is ST (\x1b\\\\) here, not BEL,
    matching our OSC 8 helper. Caller-side gating (TTY check + ``enabled``)
    determines whether anything is written at all."""

    def _capture(self, *, isatty: bool, enabled: bool = True) -> StringIO:
        """Run the context manager against a fake stderr; return everything
        the manager wrote (initial set + clear-on-exit). The class writes
        to stderr (not stdout) so a redirected stdout — `avr run watch >
        log` or `... | jq` — still updates the tab title without poisoning
        the data stream."""
        buf = StringIO()
        # StringIO doesn't implement isatty by default — fake it.
        buf.isatty = lambda: isatty  # type: ignore[method-assign]
        with patch("avrea_cli.display.sys.stderr", buf):
            with terminal_title("avr ▸ run abc", enabled=enabled) as t:
                t.set("avr ▸ run abc ▸ 2/3 jobs done")
        return buf

    def test_writes_set_and_clear_when_tty(self):
        buf = self._capture(isatty=True)
        out = buf.getvalue()
        assert "\x1b]2;avr ▸ run abc\x1b\\" in out
        assert "\x1b]2;avr ▸ run abc ▸ 2/3 jobs done\x1b\\" in out
        # Cleared on exit.
        assert out.endswith("\x1b]2;\x1b\\")

    def test_no_op_when_not_tty(self):
        buf = self._capture(isatty=False)
        assert buf.getvalue() == ""

    def test_force_disabled_via_enabled_false(self):
        # Even on a TTY, ``enabled=False`` skips all writes — used by JSON
        # streaming modes where stdout is intended for downstream parsing.
        buf = self._capture(isatty=True, enabled=False)
        assert buf.getvalue() == ""


class TestPageOutputBypass:
    """``bypass=True`` skips paging unconditionally, including on a TTY with
    a configured pager. Wired to ``--no-pager`` on log-paging commands so
    demos and scripts can opt out of ``less`` without setting AVR_PAGER=''."""

    def test_bypass_true_writes_directly_even_on_tty(self, monkeypatch):
        """On a TTY with PAGER set, ``bypass=True`` must NOT invoke the pager."""
        monkeypatch.setenv("PAGER", "less")
        monkeypatch.delenv("AVR_PAGER", raising=False)
        # Force ``is_piped()`` to report False so the only thing keeping the
        # pager out of the picture is ``bypass``.
        monkeypatch.setattr("avrea_cli.display.is_piped", lambda: False)
        called = {"echo_via_pager": 0}
        monkeypatch.setattr(
            "avrea_cli.display.click.echo_via_pager",
            lambda *a, **kw: called.__setitem__("echo_via_pager", called["echo_via_pager"] + 1),
        )

        with patch("sys.stdout", new=StringIO()) as fake_stdout:
            page_output("hello\nworld", bypass=True)

        assert called["echo_via_pager"] == 0
        assert "hello" in fake_stdout.getvalue()

    def test_bypass_false_uses_pager_on_tty(self, monkeypatch):
        """Default behaviour (``bypass=False``) keeps the pager when stdout is
        a TTY and a pager is configured — pin so the bypass plumbing doesn't
        accidentally short-circuit the normal path."""
        monkeypatch.setenv("PAGER", "less")
        monkeypatch.delenv("AVR_PAGER", raising=False)
        monkeypatch.setattr("avrea_cli.display.is_piped", lambda: False)
        called = {"echo_via_pager": 0}
        monkeypatch.setattr(
            "avrea_cli.display.click.echo_via_pager",
            lambda *a, **kw: called.__setitem__("echo_via_pager", called["echo_via_pager"] + 1),
        )

        page_output("hello\nworld", bypass=False)

        assert called["echo_via_pager"] == 1
