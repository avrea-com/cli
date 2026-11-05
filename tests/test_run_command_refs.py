"""Run-reference coverage for commands beyond ``run view`` and ``diagnose``."""

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
    monkeypatch.setattr("avrea_cli.commands.run.get_org_slug", lambda *args, **kwargs: "my-org")
    monkeypatch.setattr("avrea_cli.commands.run.page_output", lambda content, **kwargs: click.echo(content))
    return CliRunner()


def test_watch_resolves_github_run_id(runner, monkeypatch):
    get_paths: list[str] = []

    def fake_get(self, path, **kwargs):
        get_paths.append(path)
        if path.endswith("/by-platform-id/123"):
            return {"data": {"run_id": "run-resolved", "platform_run_id": 123}}
        if path.endswith("/workflow-runs/run-resolved"):
            return {
                "data": {
                    "run_id": "run-resolved",
                    "status": "completed",
                    "conclusion": "success",
                    "jobs": [],
                }
            }
        raise AssertionError(f"unexpected GET: {path}")

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)
    monkeypatch.setattr("avrea_cli.commands.run.time.sleep", lambda _: None)

    result = runner.invoke(cli, ["run", "watch", "123", "--ndjson"])

    assert result.exit_code == 0, result.output
    assert get_paths == [
        "/orgs/org-default/workflow-runs/by-platform-id/123",
        "/orgs/org-default/workflow-runs/run-resolved",
    ]


def test_logs_resolves_github_run_id(runner, monkeypatch):
    get_calls: list[tuple[str, object]] = []

    def fake_get(self, path, **kwargs):
        get_calls.append((path, kwargs.get("params")))
        return {
            "data": {
                "run_id": "run-resolved",
                "platform_run_id": 123,
                "jobs": [],
            }
        }

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

    result = runner.invoke(cli, ["run", "logs", "123"])

    assert result.exit_code == 0, result.output
    assert "No matching jobs" in result.output
    assert get_calls == [
        (
            "/orgs/org-default/workflow-runs/by-platform-id/123",
            {"include": ["jobs"]},
        )
    ]


def test_cancel_resolves_github_run_id(runner, monkeypatch):
    post_paths: list[str] = []
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, **kwargs: {"data": {"run_id": "run-resolved", "platform_run_id": 123}},
    )

    def fake_post(self, path, **kwargs):
        post_paths.append(path)
        return {"data": {"status": "cancel_requested"}}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", fake_post)

    result = runner.invoke(cli, ["run", "cancel", "123", "--yes"])

    assert result.exit_code == 0, result.output
    assert post_paths == ["/orgs/org-default/workflow-runs/run-resolved/cancel"]
    assert "/runs/run-resolved" in result.output


def test_rerun_resolves_github_run_id(runner, monkeypatch):
    post_paths: list[str] = []
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, **kwargs: {
            "data": {
                "run_id": "run-resolved",
                "platform_run_id": 123,
                "run_attempt": 1,
            }
        },
    )

    def fake_post(self, path, **kwargs):
        post_paths.append(path)
        return {"status": "rerun_requested"}

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", fake_post)
    monkeypatch.setattr("avrea_cli.commands.run._poll_for_new_attempt", lambda *args, **kwargs: None)

    result = runner.invoke(cli, ["run", "rerun", "123", "--yes"])

    assert result.exit_code == 0, result.output
    assert post_paths == ["/orgs/org-default/workflow-runs/run-resolved/rerun"]
    assert "/runs/run-resolved" in result.output


JOB_URL = "https://github.com/acme/widgets/actions/runs/123/job/456?pr=78#step:3:10"


def _install_two_attempts(monkeypatch) -> list[tuple[str, dict]]:
    """Attempt 2 is the latest; job 456 belongs to attempt 1."""
    calls: list[tuple[str, dict]] = []

    def attempt(number, job_id, conclusion):
        return {
            "run_id": f"run-attempt-{number}",
            "platform_run_id": 123,
            "run_attempt": number,
            "status": "completed",
            "conclusion": conclusion,
            "repository": {"full_name": "acme/widgets"},
            "jobs": [{"job_id": f"job-{job_id}", "platform_job_id": job_id, "job_name": "Build"}],
        }

    attempts = {"run-attempt-2": attempt(2, 789, "success"), "run-attempt-1": attempt(1, 456, "failure")}
    attempts["run-attempt-1"]["jobs"].append({"job_id": "job-457", "platform_job_id": 457, "job_name": "Lint"})

    def fake_get(self, path, **kwargs):
        params = kwargs.get("params") or {}
        calls.append((path, params))
        with_jobs = "jobs" in params.get("include", [])
        if path == "/orgs/org-default/workflow-runs":
            return {"data": [{**a, "jobs": a["jobs"] if with_jobs else None} for a in attempts.values()]}
        run_id = path.removeprefix("/orgs/org-default/workflow-runs/")
        if run_id in attempts:
            return {"data": {**attempts[run_id], "jobs": attempts[run_id]["jobs"] if with_jobs else None}}
        raise AssertionError(f"unexpected GET: {path}")

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)
    return calls


def test_view_job_url_shows_the_attempt_containing_the_job(runner, monkeypatch):
    calls = _install_two_attempts(monkeypatch)

    result = runner.invoke(cli, ["run", "view", JOB_URL, "--json", "run_id,run_attempt,conclusion"])

    assert result.exit_code == 0, result.output
    assert '"run_id": "run-attempt-1"' in result.output
    assert '"conclusion": "failure"' in result.output
    assert [path for path, _ in calls] == [
        "/orgs/org-default/workflow-runs",
        "/orgs/org-default/workflow-runs/run-attempt-2",
        "/orgs/org-default/workflow-runs/run-attempt-1",
    ]
    assert "jobs" not in calls[0][1]["include"]


def test_view_job_url_not_found_keeps_json_stdout_empty(runner, monkeypatch):
    _install_two_attempts(monkeypatch)
    url = "https://github.com/acme/widgets/actions/runs/123/job/999"

    result = runner.invoke(cli, ["run", "view", url, "--json", "run_id"])

    assert result.exit_code == 1
    assert result.stdout == ""
    assert "GitHub job 999 is not listed in any visible attempt of run 123" in result.stderr


def test_view_rejects_non_step_fragment_with_exit_code_1(runner):
    result = runner.invoke(cli, ["run", "view", "https://github.com/acme/widgets/actions/runs/123/job/456#other"])

    assert result.exit_code == 1
    assert "Invalid run reference" in result.stderr


@pytest.mark.parametrize("command", ["rerun", "cancel"])
def test_whole_run_commands_refuse_job_urls_without_calling_the_api(runner, monkeypatch, command):
    def fail(self, path, **kwargs):
        raise AssertionError(f"unexpected request: {path}")

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fail)
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", fail)

    result = runner.invoke(cli, ["run", command, JOB_URL, "--yes"])

    assert result.exit_code == 1
    assert f"Pass the run instead: avr run {command} 123" in result.stderr


def test_logs_job_url_shows_only_that_job(runner, monkeypatch):
    _install_two_attempts(monkeypatch)
    fetched: list[str] = []

    def fake_fetch(client, job_id, **kwargs):
        fetched.append(job_id)
        return []

    monkeypatch.setattr("avrea_cli.commands.run.fetch_all_logs", fake_fetch)

    result = runner.invoke(cli, ["run", "logs", JOB_URL, "--no-pager"])

    assert result.exit_code == 0, result.output
    assert fetched == ["job-456"]


def test_watch_job_url_follows_the_attempt_containing_the_job(runner, monkeypatch):
    calls = _install_two_attempts(monkeypatch)
    monkeypatch.setattr("avrea_cli.commands.run.time.sleep", lambda _: None)

    result = runner.invoke(cli, ["run", "watch", JOB_URL, "--ndjson"])

    assert result.exit_code == 0, result.output
    assert calls[0][0] == "/orgs/org-default/workflow-runs"
    assert "run-attempt-1" in result.output


def test_diagnose_job_url_reports_on_the_attempt_containing_the_job(runner, monkeypatch):
    _install_two_attempts(monkeypatch)
    diagnosed: list[str] = []

    def fake_build(client, org_id, resolved):
        diagnosed.append(resolved["run_id"])
        return {"run_id": resolved["run_id"]}

    monkeypatch.setattr("avrea_cli.commands.run.build_run_diagnostics", fake_build)

    result = runner.invoke(cli, ["run", "diagnose", JOB_URL, "--json"])

    assert result.exit_code == 0, result.output
    assert diagnosed == ["run-attempt-1"]
