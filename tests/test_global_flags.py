"""Unit tests for the top-level CLI options: --no-color, --verbose, NO_COLOR.

These flags affect cross-cutting behavior (color stripping, request logging),
so they have no command of their own — exercising them through `health`
which is the lightest path that touches the API client."""

from avrea_cli.main import _hoist_global_flags
from avrea_cli.main import cli
from click.testing import CliRunner
from unittest.mock import MagicMock
from unittest.mock import patch
import pytest


@pytest.fixture()
def runner(monkeypatch):
    monkeypatch.setenv("AVR_TOKEN", "tok")
    monkeypatch.setenv("AVR_ORG", "org-default")
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.delenv("NO_COLOR", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    return CliRunner()


def _ok_health_response() -> MagicMock:
    resp = MagicMock(status_code=200)
    resp.json.return_value = {"status": "ok"}
    resp.request.url = "https://api.example/health"
    return resp


class TestVerbose:
    def test_verbose_logs_request_line(self, runner):
        with patch("avrea_cli.api_client.httpx.Client.get", return_value=_ok_health_response()):
            result = runner.invoke(cli, ["--verbose", "health"])
        assert result.exit_code == 0
        assert "GET" in result.output
        assert "https://api.example/health" in result.output
        assert "[200]" in result.output

    def test_short_v_alias(self, runner):
        with patch("avrea_cli.api_client.httpx.Client.get", return_value=_ok_health_response()):
            result = runner.invoke(cli, ["-v", "health"])
        assert result.exit_code == 0
        assert "GET" in result.output

    def test_default_no_request_line(self, runner):
        with patch("avrea_cli.api_client.httpx.Client.get", return_value=_ok_health_response()):
            result = runner.invoke(cli, ["health"])
        assert result.exit_code == 0
        # Without --verbose, the request URL must not appear in output
        assert "https://api.example/health" not in result.output


class TestNoColor:
    """Force ``color=True`` on CliRunner so click doesn't strip ANSI just
    because stdout isn't a TTY in the test harness — without this the
    assertion below is vacuous (no ANSI to start with)."""

    @staticmethod
    def _runs_payload():
        return {
            "data": [
                {
                    "run_id": "run-1",
                    "display_title": "Build",
                    "status": "completed",
                    "conclusion": "success",
                    "head_branch": "main",
                    "event": "push",
                    "duration_seconds": 60,
                    "created_at": "2025-06-01T12:00:00Z",
                    "workflow": {"name": "CI"},
                }
            ],
            "pagination": {"next_cursor": None},
        }

    def test_baseline_emits_ansi(self, runner, monkeypatch):
        # Sanity check — without --no-color and with color forced, the run
        # list table should contain ANSI escape sequences (status indicator,
        # header underlines, dim age column).
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: TestNoColor._runs_payload(),
        )
        result = runner.invoke(cli, ["run", "list"], color=True)
        assert result.exit_code == 0, result.output
        assert "\x1b[" in result.output

    def test_no_color_flag_strips_ansi(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: TestNoColor._runs_payload(),
        )
        result = runner.invoke(cli, ["--no-color", "run", "list"], color=True)
        assert result.exit_code == 0, result.output
        assert "\x1b[" not in result.output

    def test_no_color_env_var_strips_ansi(self, runner, monkeypatch):
        monkeypatch.setenv("NO_COLOR", "1")
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: TestNoColor._runs_payload(),
        )
        result = runner.invoke(cli, ["run", "list"], color=True)
        assert result.exit_code == 0
        assert "\x1b[" not in result.output


class TestHoistGlobalFlags:
    """The entrypoint reorders argv so global flags work after subcommands —
    `avr run list --verbose` shouldn't error with 'No such option'."""

    def test_verbose_after_subcommand_hoisted_to_front(self):
        assert _hoist_global_flags(["run", "list", "--verbose"]) == ["--verbose", "run", "list"]

    def test_short_v_after_subcommand_hoisted(self):
        assert _hoist_global_flags(["run", "list", "-v"]) == ["-v", "run", "list"]

    def test_no_color_anywhere_hoisted(self):
        assert _hoist_global_flags(["run", "list", "--no-color"]) == ["--no-color", "run", "list"]

    def test_already_at_front_is_a_noop(self):
        assert _hoist_global_flags(["--verbose", "run", "list"]) == ["--verbose", "run", "list"]

    def test_multiple_globals_preserve_relative_order(self):
        assert _hoist_global_flags(["run", "--verbose", "list", "--no-color"]) == [
            "--verbose",
            "--no-color",
            "run",
            "list",
        ]

    def test_double_dash_stops_hoisting(self):
        # POSIX: anything after `--` is a positional, even if it looks like a flag.
        assert _hoist_global_flags(["run", "view", "--", "--verbose"]) == ["run", "view", "--", "--verbose"]

    def test_empty_argv(self):
        assert _hoist_global_flags([]) == []

    def test_unrelated_flags_untouched(self):
        argv = ["run", "list", "--limit", "10", "--json", "*"]
        assert _hoist_global_flags(argv) == argv

    def test_links_and_no_links_hoisted(self):
        # --links/--no-links is a paired boolean; both forms hoist for the
        # same reason --verbose does — users place global flags after the
        # subcommand.
        assert _hoist_global_flags(["run", "list", "--no-links"]) == ["--no-links", "run", "list"]
        assert _hoist_global_flags(["run", "list", "--links"]) == ["--links", "run", "list"]
