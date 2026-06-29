"""Smoke tests for --web on view/list commands.

`--web` paths build console URLs from the org slug and the resource ID, then
hand off to ``webbrowser.open``. This file pins the URL shape for every
command that supports the flag — a regression in slug resolution, console URL
derivation, or path layout would surface a broken link to the user without
any error from the CLI itself."""

from avrea_cli.main import cli
from click.testing import CliRunner
import copy
import pytest


def _run_view_sample() -> dict:
    """Fresh deepcopy per call so tests don't share state if the CLI
    mutates its input on the way through."""
    return copy.deepcopy(
        {
            "data": {
                "run_id": "run-abc123",
                "platform_run_id": 987654,
                "run_attempt": 1,
                "repository": {"full_name": "acme/svc"},
            }
        }
    )


def _job_view_sample() -> dict:
    """Fresh deepcopy per call. ``platform_run_id`` is the GitHub
    workflow_run.id — distinct from the Avrea ``job_id``."""
    return copy.deepcopy(
        {
            "data": {
                "job_id": "job-abc",
                "repository_full_name": "acme/svc",
                "platform_run_id": 987654,
                "platform_job_id": 5555555,
            }
        }
    )


@pytest.fixture()
def runner(monkeypatch):
    monkeypatch.setenv("AVR_TOKEN", "test-token")
    monkeypatch.setenv("AVR_ORG", "org-default")
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_default_host", lambda: None)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    # Stub out the actual browser launch.
    monkeypatch.setattr("webbrowser.open", lambda *a, **kw: True)
    # Resolve org slug deterministically. Each command module rebinds
    # `get_org_slug` at import time (`from avrea_cli.helpers import …`),
    # so we patch each consuming module's own binding.
    for mod in (
        "avrea_cli.commands.run",
        "avrea_cli.commands.job",
        "avrea_cli.commands.cache",
        "avrea_cli.commands.workflow",
        "avrea_cli.commands.settings",
    ):
        monkeypatch.setattr(f"{mod}.get_org_slug", lambda c, o: "acme", raising=False)
    return CliRunner()


def _stub_get(monkeypatch, payload):
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, **kw: payload,
    )


class TestRunListWeb:
    def test_url_uses_slug_and_workflows_view(self, runner):
        result = runner.invoke(cli, ["run", "list", "--web"])
        assert result.exit_code == 0, result.output
        assert "https://console.avrea.com/org/acme?view=workflows" in result.output


class TestRunViewWeb:
    def test_avrea_url_uses_slug(self, runner, monkeypatch):
        _stub_get(monkeypatch, _run_view_sample())
        result = runner.invoke(cli, ["run", "view", "run-abc123", "--web"])
        assert result.exit_code == 0, result.output
        assert "Avrea:  https://console.avrea.com/org/acme/runs/run-abc123" in result.output

    def test_github_url_uses_platform_run_id_not_workflow_id(self, runner, monkeypatch):
        _stub_get(monkeypatch, _run_view_sample())
        result = runner.invoke(cli, ["run", "view", "run-abc123", "--web"])
        assert result.exit_code == 0, result.output
        assert "GitHub: https://github.com/acme/svc/actions/runs/987654" in result.output
        # First-attempt URL has no /attempts/N suffix
        assert "/attempts/" not in result.output

    def test_github_url_appends_attempt_suffix_for_reruns(self, runner, monkeypatch):
        rerun_payload = {
            "data": {
                "run_id": "run-rerun",
                "platform_run_id": 987654,
                "run_attempt": 3,
                "repository": {"full_name": "acme/svc"},
            }
        }
        _stub_get(monkeypatch, rerun_payload)
        result = runner.invoke(cli, ["run", "view", "run-rerun", "--web"])
        assert result.exit_code == 0, result.output
        assert "/runs/987654/attempts/3" in result.output

    def test_github_url_omitted_when_platform_run_id_missing(self, runner, monkeypatch):
        no_platform = {"data": {"run_id": "run-x", "repository": {"full_name": "acme/svc"}}}
        _stub_get(monkeypatch, no_platform)
        result = runner.invoke(cli, ["run", "view", "run-x", "--web"])
        assert result.exit_code == 0
        assert "GitHub:" not in result.output


class TestJobViewWeb:
    def test_avrea_url_uses_slug(self, runner, monkeypatch):
        _stub_get(monkeypatch, _job_view_sample())
        result = runner.invoke(cli, ["job", "view", "job-abc", "--web"])
        assert result.exit_code == 0, result.output
        assert "Avrea:  https://console.avrea.com/org/acme/jobs/job-abc" in result.output

    def test_github_url_uses_platform_run_id_and_platform_job_id(self, runner, monkeypatch):
        _stub_get(monkeypatch, _job_view_sample())
        result = runner.invoke(cli, ["job", "view", "job-abc", "--web"])
        assert result.exit_code == 0, result.output
        assert "GitHub: https://github.com/acme/svc/actions/runs/987654/job/5555555" in result.output

    def test_github_url_omitted_without_platform_ids(self, runner, monkeypatch):
        _stub_get(monkeypatch, {"data": {"job_id": "job-x", "repository_full_name": "acme/svc"}})
        result = runner.invoke(cli, ["job", "view", "job-x", "--web"])
        assert result.exit_code == 0, result.output
        assert "GitHub:" not in result.output


class TestCacheListWeb:
    def test_url_includes_resolved_repo_id(self, runner, monkeypatch):
        _stub_get(monkeypatch, {"data": []})
        result = runner.invoke(cli, ["cache", "list", "--repo", "rep-xyz789", "--web"])
        assert result.exit_code == 0, result.output
        assert "https://console.avrea.com/org/acme/caches/rep-xyz789" in result.output


class TestWorkflowViewWeb:
    def test_url_includes_workflow_id(self, runner, monkeypatch):
        _stub_get(monkeypatch, {"data": []})
        result = runner.invoke(cli, ["workflow", "view", "wfl-abc", "--web"])
        assert result.exit_code == 0, result.output
        assert "https://console.avrea.com/org/acme/workflows/wfl-abc" in result.output


class TestSettingsListWeb:
    def test_org_scope_url(self, runner, monkeypatch):
        # No --repo: org-level settings URL.
        monkeypatch.setattr(
            "avrea_cli.commands.settings.resolve_repo_or_detect",
            lambda *a, **kw: None,
        )
        result = runner.invoke(cli, ["settings", "list", "--web"])
        assert result.exit_code == 0, result.output
        assert "https://console.avrea.com/org/acme/settings" in result.output

    def test_repo_scope_url(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.commands.settings.resolve_repo_or_detect",
            lambda *a, **kw: "rep-xyz789",
        )
        result = runner.invoke(cli, ["settings", "list", "--web", "--repo", "rep-xyz789"])
        assert result.exit_code == 0, result.output
        assert "https://console.avrea.com/org/acme/repos/rep-xyz789/settings" in result.output
