"""Unit tests for piped/non-TTY output on `avr run list`, `avr job list`,
and `avr cache list`.

Each test opts back into piped mode (the autouse `_force_tty_mode` fixture
keeps everything else in TTY mode). The contract: a single header row of
snake_case column names followed by tab-separated rows — no color, no
truncation, ISO timestamps."""

from avrea_cli.main import cli
from click.testing import CliRunner
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


@pytest.fixture()
def piped(monkeypatch):
    """Opt into piped output for a single test."""
    for mod in ("avrea_cli.commands.run", "avrea_cli.commands.job", "avrea_cli.commands.cache"):
        monkeypatch.setattr(f"{mod}.is_piped", lambda: True)


class TestRunListPiped:
    def test_emits_tab_separated_rows_with_header(self, runner, piped, monkeypatch):
        runs = [
            {
                "run_id": "run-019d77762cfd7c7591ee8220b5c6b5eb",
                "status": "completed",
                "conclusion": "success",
                "display_title": "Add cache warming step",
                "head_branch": "feat/cache",
                "event": "pull_request",
                "duration_seconds": 134,
                "created_at": "2026-04-12T12:03:14Z",
                "workflow": {"name": "build-and-test"},
            },
            {
                "run_id": "run-019d7775f2a17c7591ee8220b5c6b5eb",
                "status": "completed",
                "conclusion": "failure",
                "display_title": "Fix auth token refresh",
                "head_branch": "fix/auth",
                "event": "push",
                "duration_seconds": 242,
                "created_at": "2026-04-12T11:30:00Z",
                "workflow": {"name": "build-and-test"},
            },
        ]
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: {"data": runs},
        )

        result = runner.invoke(cli, ["run", "list"])

        assert result.exit_code == 0, result.output
        # No ANSI escapes
        assert "\x1b[" not in result.output
        lines = result.output.splitlines()
        # Header row first, then data rows
        assert lines[0].split("\t") == [
            "status",
            "title",
            "workflow",
            "branch",
            "event",
            "run_id",
            "duration_seconds",
            "created_at",
        ]
        cols = lines[1].split("\t")
        assert cols[0] == "success"
        assert cols[1] == "Add cache warming step"
        assert cols[2] == "build-and-test"
        assert cols[3] == "feat/cache"
        assert cols[4] == "pull_request"
        # Full ULID, not truncated
        assert cols[5] == "run-019d77762cfd7c7591ee8220b5c6b5eb"
        assert cols[6] == "134"
        # ISO timestamp, not "3 hours ago"
        assert cols[7] == "2026-04-12T12:03:14Z"


class TestJobListPiped:
    def test_emits_tab_separated_rows_with_header(self, runner, piped, monkeypatch):
        jobs = [
            {
                "job_id": "job-019d7776b3cd789da094cef01d28f6a2",
                "job_name": "build",
                "repository_full_name": "avrea-com/avrea-core",
                "state": "completed",
                "conclusion": "success",
                "running_on_avrea": True,
                "duration_seconds": 134,
                "created_at": "2026-04-12T12:03:14Z",
            }
        ]
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: {"data": jobs},
        )

        result = runner.invoke(cli, ["job", "list"])

        assert result.exit_code == 0, result.output
        assert "\x1b[" not in result.output
        lines = result.output.splitlines()
        assert lines[0].split("\t") == [
            "status",
            "job_name",
            "repository",
            "on_avrea",
            "job_id",
            "duration_seconds",
            "created_at",
        ]
        assert lines[1].split("\t") == [
            "success",
            "build",
            "avrea-com/avrea-core",
            "yes",
            "job-019d7776b3cd789da094cef01d28f6a2",
            "134",
            "2026-04-12T12:03:14Z",
        ]


class TestCacheListPiped:
    def test_emits_tab_separated_rows_with_header(self, runner, piped, monkeypatch):
        entries = [
            {
                "id": "ent-1",
                "cache_type": "gha",
                "key": "node_modules-abcdef123456",
                "ref": "refs/heads/main",
                "size_bytes": 36003840,
                "created_at": "2026-04-12T12:03:14Z",
            }
        ]
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: {"data": entries, "total": 1},
        )

        result = runner.invoke(cli, ["cache", "list", "--repo", "rep-foo"])

        assert result.exit_code == 0, result.output
        assert "\x1b[" not in result.output
        lines = result.output.splitlines()
        assert lines[0].split("\t") == ["cache_type", "key", "ref", "size_bytes", "created_at"]
        assert lines[1].split("\t") == [
            "gha",
            "node_modules-abcdef123456",
            "refs/heads/main",
            "36003840",  # raw bytes, not "34.0 MB"
            "2026-04-12T12:03:14Z",
        ]
