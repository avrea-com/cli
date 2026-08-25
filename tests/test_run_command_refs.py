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
