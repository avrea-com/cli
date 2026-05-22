"""Unit tests for `avr run logs` — the long-form log fetcher.

Mirrors the inline `avr run view --log/--log-failed` paths but as a standalone
command with --follow support. The follow path is exercised separately so we
don't need to spin up a real polling loop here."""

from avrea_cli.main import cli
from click.testing import CliRunner
import click
import pytest


@pytest.fixture()
def runner(monkeypatch):
    monkeypatch.setenv("AVR_TOKEN", "test-token")
    monkeypatch.setenv("AVR_ORG", "org-default")
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    # `run logs` buffers output and calls page_output. The pager respects TTY
    # detection but CliRunner's stdout is a fake — patch to plain echo so the
    # test asserts can inspect the rendered content directly.
    monkeypatch.setattr("avrea_cli.commands.run.page_output", lambda content, **kw: click.echo(content))
    return CliRunner()


def _run_with_jobs(jobs: list[dict]) -> dict:
    return {"data": {"run_id": "run-1", "jobs": jobs}}


class TestRunLogsFiltering:
    def test_default_prints_all_jobs(self, runner, monkeypatch):
        called: list[str] = []
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: _run_with_jobs(
                [
                    {"job_id": "job-a", "job_name": "build", "repository_id": "rep-1", "conclusion": "success"},
                    {"job_id": "job-b", "job_name": "test", "repository_id": "rep-1", "conclusion": "failure"},
                ]
            ),
        )
        monkeypatch.setattr(
            "avrea_cli.commands.run.fetch_all_logs",
            lambda client, jid, **kw: (called.append(jid), [])[1],
        )
        monkeypatch.setattr("avrea_cli.commands.run.print_logs_grouped", lambda entries, **kw: None)

        result = runner.invoke(cli, ["run", "logs", "run-1"])

        assert result.exit_code == 0, result.output
        assert called == ["job-a", "job-b"]
        assert "build" in result.output
        assert "test" in result.output

    def test_failed_filter_skips_succeeding_jobs(self, runner, monkeypatch):
        called: list[str] = []
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: _run_with_jobs(
                [
                    {
                        "job_id": "job-a",
                        "job_name": "build",
                        "repository_id": "rep-1",
                        "conclusion": "success",
                        "steps": [],
                    },
                    {
                        "job_id": "job-b",
                        "job_name": "test",
                        "repository_id": "rep-1",
                        "conclusion": "failure",
                        "steps": [{"name": "run tests", "conclusion": "failure"}],
                    },
                ]
            ),
        )
        monkeypatch.setattr(
            "avrea_cli.commands.run.print_failed_step_logs",
            lambda client, jid, steps, **kw: called.append(jid),
        )

        result = runner.invoke(cli, ["run", "logs", "run-1", "--failed"])

        assert result.exit_code == 0, result.output
        assert called == ["job-b"]
        assert "build" not in result.output
        assert "test" in result.output

    def test_failed_filter_with_no_failures_prints_message(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: _run_with_jobs(
                [{"job_id": "job-a", "job_name": "build", "repository_id": "rep-1", "conclusion": "success"}]
            ),
        )
        result = runner.invoke(cli, ["run", "logs", "run-1", "--failed"])
        assert result.exit_code == 0
        assert "No failed jobs" in result.output

    def test_job_name_filter_narrows_results(self, runner, monkeypatch):
        called: list[str] = []
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: _run_with_jobs(
                [
                    {"job_id": "job-a", "job_name": "build", "repository_id": "rep-1", "conclusion": "success"},
                    {"job_id": "job-b", "job_name": "test", "repository_id": "rep-1", "conclusion": "failure"},
                ]
            ),
        )
        monkeypatch.setattr(
            "avrea_cli.commands.run.fetch_all_logs",
            lambda client, jid, **kw: (called.append(jid), [])[1],
        )
        monkeypatch.setattr("avrea_cli.commands.run.print_logs_grouped", lambda entries, **kw: None)

        result = runner.invoke(cli, ["run", "logs", "run-1", "--job", "test"])

        assert result.exit_code == 0, result.output
        assert called == ["job-b"]

    def test_follow_and_failed_are_mutually_exclusive(self, runner):
        result = runner.invoke(cli, ["run", "logs", "run-1", "--follow", "--failed"])
        assert result.exit_code != 0
        assert "cannot be combined" in result.output

    def test_no_jobs_prints_message(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: _run_with_jobs([]),
        )
        result = runner.invoke(cli, ["run", "logs", "run-1"])
        assert result.exit_code == 0
        assert "No matching jobs" in result.output


class TestRunLogsFollow:
    def test_follow_picks_running_job(self, runner, monkeypatch):
        followed: list[str] = []
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: _run_with_jobs(
                [
                    # JobResponse uses `state`, not `status`. Pinning fixtures
                    # to the real shape so the --follow predicate gets exercised.
                    {"job_id": "job-a", "job_name": "build", "repository_id": "rep-1", "state": "completed"},
                    {"job_id": "job-b", "job_name": "test", "repository_id": "rep-1", "state": "in_progress"},
                ]
            ),
        )
        monkeypatch.setattr(
            "avrea_cli.commands.run.follow_logs",
            lambda client, org_id, job_id, **kw: followed.append(job_id),
        )
        result = runner.invoke(cli, ["run", "logs", "run-1", "--follow"])
        assert result.exit_code == 0, result.output
        assert followed == ["job-b"]

    def test_follow_falls_back_to_first_job_when_none_running(self, runner, monkeypatch):
        followed: list[str] = []
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: _run_with_jobs(
                [{"job_id": "job-a", "job_name": "build", "repository_id": "rep-1", "state": "completed"}]
            ),
        )
        monkeypatch.setattr(
            "avrea_cli.commands.run.follow_logs",
            lambda client, org_id, job_id, **kw: followed.append(job_id),
        )
        result = runner.invoke(cli, ["run", "logs", "run-1", "--follow"])
        assert result.exit_code == 0, result.output
        assert followed == ["job-a"]


class TestFailedConclusionsContract:
    """``--failed`` keeps any job whose conclusion is in ``_FAILED_CONCLUSIONS``.

    This pins the predicate so future API additions to the conclusion enum
    don't silently slip through (a new enum value defaults to "not failed",
    which is the safe-but-surprising direction)."""

    @pytest.fixture
    def setup(self, runner, monkeypatch):
        called: list[tuple[str, str]] = []

        def _stub(client, jid, steps, **kw):
            called.append((jid, kw.get("show_all_levels", False)))

        monkeypatch.setattr("avrea_cli.commands.run.print_failed_step_logs", _stub)
        return called

    # Job-level conclusions only — `startup_failure` and `stale` are
    # run-level only and never appear on a job, so they're correctly
    # absent from _FAILED_CONCLUSIONS.
    @pytest.mark.parametrize("conclusion", ["failure", "timed_out"])
    def test_each_failed_conclusion_kept(self, conclusion, runner, monkeypatch, setup):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: _run_with_jobs(
                [
                    {
                        "job_id": "job-x",
                        "job_name": "test",
                        "repository_id": "rep-1",
                        "conclusion": conclusion,
                        "steps": [{"name": "run", "conclusion": conclusion}],
                    }
                ]
            ),
        )
        result = runner.invoke(cli, ["run", "logs", "run-1", "--failed"])
        assert result.exit_code == 0, result.output
        assert setup, f"--failed should have included a job with conclusion={conclusion!r}"

    @pytest.mark.parametrize("conclusion", ["success", "skipped", "neutral"])
    def test_non_failed_conclusions_filtered(self, conclusion, runner, monkeypatch, setup):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: _run_with_jobs(
                [
                    {
                        "job_id": "job-x",
                        "job_name": "test",
                        "repository_id": "rep-1",
                        "conclusion": conclusion,
                        "steps": [],
                    }
                ]
            ),
        )
        result = runner.invoke(cli, ["run", "logs", "run-1", "--failed"])
        assert result.exit_code == 0
        assert not setup, f"--failed should NOT have included conclusion={conclusion!r}"
        assert "No failed jobs" in result.output
