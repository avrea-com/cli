"""Unit tests for log display module."""

from avrea_cli.log_display import fetch_all_logs
from avrea_cli.log_display import format_log_line
from avrea_cli.log_display import print_failed_step_logs
from avrea_cli.log_display import print_logs_grouped
from unittest.mock import MagicMock
import pytest


class TestFormatLogLine:
    def test_plain_content(self):
        assert format_log_line({"content": "hello world", "level": "info"}) == "hello world"

    def test_group_marker_is_bold(self):
        result = format_log_line({"content": "##[group]Run actions/checkout@v4", "level": "info"})
        assert result is not None
        assert "Run actions/checkout@v4" in result
        assert "\x1b[" in result  # has styling

    def test_endgroup_returns_none(self):
        assert format_log_line({"content": "##[endgroup]", "level": "info"}) is None

    def test_command_marker_is_cyan(self):
        result = format_log_line({"content": "##[command]/usr/bin/git fetch", "level": "info"})
        assert result is not None
        assert "/usr/bin/git fetch" in result
        assert "\x1b[" in result  # has color

    def test_error_has_color(self):
        result = format_log_line({"content": "Error: failed", "level": "error"})
        assert result is not None
        assert "Error: failed" in result
        assert "\x1b[" in result

    def test_warning_has_color(self):
        result = format_log_line({"content": "some warning", "level": "warning"})
        assert result is not None
        assert "\x1b[" in result

    def test_empty_content(self):
        assert format_log_line({"content": "", "level": "info"}) == ""


class TestFetchAllLogs:
    def test_single_page(self):
        client = MagicMock()
        client.public_post.return_value = {
            "results": [
                {"line_number": 1, "content": "line 1", "level": "info", "step_name": "Build"},
                {"line_number": 2, "content": "line 2", "level": "info", "step_name": "Build"},
            ],
            "has_more": False,
        }
        entries = fetch_all_logs(client, "job-1")
        assert len(entries) == 2
        assert client.public_post.call_count == 1

    def test_multi_page(self):
        client = MagicMock()
        client.public_post.side_effect = [
            {
                "results": [{"line_number": 1, "content": "a", "level": "info"}],
                "has_more": True,
                "next_cursor": 1,
            },
            {
                "results": [{"line_number": 2, "content": "b", "level": "info"}],
                "has_more": False,
            },
        ]
        entries = fetch_all_logs(client, "job-1")
        assert len(entries) == 2
        assert client.public_post.call_count == 2

    def test_filters_diagnostic_by_default(self):
        client = MagicMock()
        client.public_post.return_value = {
            "results": [
                {"line_number": 1, "content": "visible", "level": "info"},
                {"line_number": 2, "content": "hidden", "level": "diagnostic"},
            ],
            "has_more": False,
        }
        entries = fetch_all_logs(client, "job-1")
        assert len(entries) == 1
        assert entries[0]["content"] == "visible"

    def test_show_all_levels_includes_diagnostic(self):
        client = MagicMock()
        client.public_post.return_value = {
            "results": [
                {"line_number": 1, "content": "visible", "level": "info"},
                {"line_number": 2, "content": "diag", "level": "diagnostic"},
            ],
            "has_more": False,
        }
        entries = fetch_all_logs(client, "job-1", show_all_levels=True)
        assert len(entries) == 2

    def test_step_name_filter(self):
        client = MagicMock()
        client.public_post.return_value = {
            "results": [
                {"line_number": 1, "content": "a", "level": "info", "step_name": "Build"},
                {"line_number": 2, "content": "b", "level": "info", "step_name": "Test"},
            ],
            "has_more": False,
        }
        entries = fetch_all_logs(client, "job-1", step_name="Build")
        assert len(entries) == 1
        assert entries[0]["step_name"] == "Build"


class TestPrintLogsGrouped:
    def test_groups_by_step(self, capsys):
        entries = [
            {"step_name": "Build", "content": "building...", "level": "info"},
            {"step_name": "Build", "content": "done", "level": "info"},
            {"step_name": "Test", "content": "testing...", "level": "info"},
        ]
        print_logs_grouped(entries)
        out = capsys.readouterr().out
        assert "--- Build ---" in out
        assert "--- Test ---" in out
        assert "building..." in out
        assert "testing..." in out

    def test_unknown_step(self, capsys):
        entries = [{"step_name": None, "content": "orphan", "level": "info"}]
        print_logs_grouped(entries)
        out = capsys.readouterr().out
        assert "(unknown step)" in out


class TestPrintFailedStepLogs:
    """The failed-step predicate (``conclusion in {failure, timed_out,
    cancelled, action_required, startup_failure}``) drives which steps get
    expanded under ``--log-failed``. ``success`` and ``skipped`` are
    explicitly NOT failed; everything else IS."""

    def _client(self, log_lines):
        client = MagicMock()
        client.public_post.return_value = {"results": log_lines, "has_more": False}
        return client

    def test_no_failed_steps_short_circuits(self, capsys):
        client = MagicMock()
        steps = [{"name": "Build", "conclusion": "success"}]
        print_failed_step_logs(client, "job-1", steps)
        out = capsys.readouterr().out
        assert "No failed steps." in out
        # Should never have hit the log search endpoint.
        client.public_post.assert_not_called()

    def test_skipped_is_not_failed(self, capsys):
        client = MagicMock()
        steps = [{"name": "Deploy", "conclusion": "skipped"}]
        print_failed_step_logs(client, "job-1", steps)
        assert "No failed steps." in capsys.readouterr().out
        client.public_post.assert_not_called()

    def test_failure_step_includes_sibling_success(self, capsys):
        """The success step should NOT render a header (only failed ones do)."""
        client = self._client([{"line_number": 1, "content": "boom", "level": "error", "step_name": "Test"}])
        steps = [
            {"name": "Build", "conclusion": "success"},
            {"name": "Test", "conclusion": "failure"},
        ]
        print_failed_step_logs(client, "job-1", steps)
        out = capsys.readouterr().out
        assert "Test (failure)" in out
        assert "boom" in out
        assert "Build" not in out

    @pytest.mark.parametrize("conclusion", ["failure", "timed_out"])
    def test_failed_conclusion_step_is_expanded(self, capsys, conclusion):
        client = self._client([{"line_number": 1, "content": "boom", "level": "info", "step_name": "Step"}])
        steps = [{"name": "Step", "conclusion": conclusion}]
        print_failed_step_logs(client, "job-1", steps)
        out = capsys.readouterr().out
        assert f"Step ({conclusion})" in out
        assert "boom" in out

    def test_step_with_no_logs_skipped_silently(self, capsys):
        # If a failed step has no captured logs, omit its header AND any
        # placeholder body rather than rendering an empty stanza. Other
        # failed steps still render normally.
        client = self._client([{"line_number": 1, "content": "found", "level": "info", "step_name": "WithLogs"}])
        steps = [
            {"name": "Empty", "conclusion": "failure"},
            {"name": "WithLogs", "conclusion": "failure"},
        ]
        print_failed_step_logs(client, "job-1", steps)
        out = capsys.readouterr().out
        # Empty step must be fully omitted — no header, no name reference,
        # no "(no logs)" placeholder.
        assert "Empty" not in out
        # WithLogs renders its header AND the captured log content.
        assert "WithLogs (failure)" in out
        assert "found" in out
        # Sanity: only one section header (--- WithLogs (failure) ---).
        assert out.count("(failure)") == 1

    def test_buffered_emit_replaces_click_echo(self):
        # With ``emit=buffer.append`` the function must not write to stdout —
        # this is how ``run logs`` gathers output for the pager.
        client = self._client([{"line_number": 1, "content": "bang", "level": "error", "step_name": "Test"}])
        steps = [{"name": "Test", "conclusion": "failure"}]
        buf: list[str] = []
        print_failed_step_logs(client, "job-1", steps, emit=buf.append)
        joined = "\n".join(buf)
        assert "Test (failure)" in joined
        assert "bang" in joined
