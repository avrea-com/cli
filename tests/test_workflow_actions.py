"""Unit tests for the CLI side of the workflow control endpoints —
the wire-level pieces that bridge the API PR's responses into UX.
"""

from avrea_cli.commands.workflow import _poll_for_run
from avrea_cli.commands.workflow import _resolve_repo_full_name
from avrea_cli.main import cli
from click.testing import CliRunner
from datetime import UTC
from datetime import datetime
from unittest.mock import MagicMock
import httpx
import pytest


@pytest.fixture()
def runner(monkeypatch):
    monkeypatch.setenv("AVR_TOKEN", "test-token")
    monkeypatch.setenv("AVR_ORG", "org-default")
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.helpers.get_org_slug", lambda *a, **kw: "my-org")
    return CliRunner()


class TestRunCancelResponse:
    """Differentiate `cancel_requested` (we cancelled it) from `already_terminal`
    (run had already finished). The earlier code always echoed 'Cancel requested'
    regardless of the API status."""

    def test_cancel_requested_branch(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, **kw: {"data": {"run_id": "run-1", "status": "cancel_requested"}},
        )
        # public_get is hit by get_org_slug; the override above bypasses it.
        result = runner.invoke(cli, ["run", "cancel", "run-1", "--yes"])
        assert result.exit_code == 0
        assert "Cancel requested" in result.output
        assert "had already finished" not in result.output

    def test_already_terminal_branch(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, **kw: {"data": {"run_id": "run-1", "status": "already_terminal"}},
        )
        result = runner.invoke(cli, ["run", "cancel", "run-1", "--yes"])
        assert result.exit_code == 0
        assert "had already finished" in result.output
        assert "Cancel requested" not in result.output

    def test_missing_status_defaults_to_cancel_requested(self, runner, monkeypatch):
        """Defensive: legacy/empty payloads should fall through the success branch
        rather than masquerade as already_terminal."""
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, **kw: {},
        )
        result = runner.invoke(cli, ["run", "cancel", "run-1", "--yes"])
        assert result.exit_code == 0
        assert "Cancel requested" in result.output


class TestPollForRun:
    """Match runs by ``created_at >= dispatch_time`` and ``workflow_id``
    (when known). The list endpoint's ``platform_run_id`` is unreliable on
    freshly-ingested rows, so we don't rely on it."""

    def test_finds_run_by_workflow_and_created_after(self):
        client = MagicMock()
        dispatch_time = datetime(2026, 5, 4, 10, 0, 0, tzinfo=UTC)
        client.public_get.return_value = {
            "data": [
                {"run_id": "run-x", "platform_workflow_id": 200, "created_at": "2026-05-04T10:00:03Z"},
                {"run_id": "run-old", "platform_workflow_id": 200, "created_at": "2026-05-04T09:55:00Z"},
            ]
        }

        run = _poll_for_run(
            client,
            "org-1",
            repo_id="rep-abc",
            platform_workflow_id=200,
            dispatch_time=dispatch_time,
            timeout=5.0,
        )

        assert run == {"run_id": "run-x", "platform_workflow_id": 200, "created_at": "2026-05-04T10:00:03Z"}
        client.public_get.assert_called_once_with(
            "/orgs/org-1/workflow-runs",
            params={"repository_ids": ["rep-abc"], "limit": 100, "order": "created_at.desc"},
        )

    def test_skips_runs_for_other_workflows(self, monkeypatch):
        client = MagicMock()
        dispatch_time = datetime(2026, 5, 4, 10, 0, 0, tzinfo=UTC)
        client.public_get.return_value = {
            "data": [
                {"run_id": "run-other", "platform_workflow_id": 999, "created_at": "2026-05-04T10:00:05Z"},
            ]
        }
        monkeypatch.setattr("time.sleep", lambda _: None)

        run = _poll_for_run(
            client,
            "org-1",
            repo_id="rep-abc",
            platform_workflow_id=200,
            dispatch_time=dispatch_time,
            timeout=0.001,
        )
        assert run is None

    def test_skips_runs_created_before_dispatch(self, monkeypatch):
        client = MagicMock()
        dispatch_time = datetime(2026, 5, 4, 10, 0, 0, tzinfo=UTC)
        client.public_get.return_value = {
            "data": [
                {"run_id": "run-old", "platform_workflow_id": 200, "created_at": "2026-05-04T09:00:00Z"},
            ]
        }
        monkeypatch.setattr("time.sleep", lambda _: None)

        run = _poll_for_run(
            client,
            "org-1",
            repo_id="rep-abc",
            platform_workflow_id=200,
            dispatch_time=dispatch_time,
            timeout=0.001,
        )
        assert run is None

    def test_returns_none_on_timeout(self, monkeypatch):
        client = MagicMock()
        client.public_get.return_value = {"data": []}
        monkeypatch.setattr("time.sleep", lambda _: None)
        run = _poll_for_run(
            client,
            "org-1",
            repo_id="rep-abc",
            platform_workflow_id=200,
            dispatch_time=datetime.now(UTC),
            timeout=0.001,
        )
        assert run is None


class TestResolveRepoFullName:
    """Resolves a repo's ``org/name`` for the GitHub Actions hyperlink. Three
    branches: short-circuit on org/name input, lookup by rep-xxx id, and
    transport-failure fallback to None."""

    def test_short_circuits_when_repo_flag_is_org_name(self):
        client = MagicMock()
        result = _resolve_repo_full_name(client, "org-1", "rep-abc", "avrea-com/avrea-core")
        assert result == "avrea-com/avrea-core"
        # Short-circuit must NOT touch the API.
        client.public_get.assert_not_called()

    def test_looks_up_full_name_by_rep_id(self):
        client = MagicMock()
        # New behaviour: single round-trip to GET /orgs/{org}/repos/{repo_id}.
        client.public_get.return_value = {"data": {"repository_id": "rep-abc", "full_name": "avrea-com/avrea-core"}}
        result = _resolve_repo_full_name(client, "org-1", "rep-abc", repo_flag=None)
        assert result == "avrea-com/avrea-core"
        client.public_get.assert_called_once_with("/orgs/org-1/repos/rep-abc")

    def test_returns_none_on_transport_failure(self):
        client = MagicMock()
        client.public_get.side_effect = httpx.ConnectError("boom")
        result = _resolve_repo_full_name(client, "org-1", "rep-abc", repo_flag=None)
        assert result is None
