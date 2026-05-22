"""Tests for `avr job list` and the shared sectioned-table renderer."""

from avrea_cli.main import cli
import json

SAMPLE_JOBS = {
    "data": [
        {
            "job_id": "job-success-1",
            "job_name": "build",
            "repository_full_name": "acme/core",
            "state": "completed",
            "conclusion": "success",
            "running_on_avrea": True,
            "created_at": "2026-04-28T07:00:00Z",
        },
        {
            "job_id": "job-failure-1",
            "job_name": "lint",
            "repository_full_name": "acme/core",
            "state": "completed",
            "conclusion": "failure",
            "running_on_avrea": True,
            "created_at": "2026-04-28T07:00:00Z",
        },
        {
            "job_id": "job-running-1",
            "job_name": "test-suite",
            "repository_full_name": "acme/core",
            "state": "in_progress",
            "conclusion": None,
            "running_on_avrea": False,
            "created_at": "2026-04-28T07:00:00Z",
        },
        {
            "job_id": "job-skipped-1",
            "job_name": "vuln-scan",
            "repository_full_name": "acme/core",
            "state": "completed",
            "conclusion": "skipped",
            "running_on_avrea": False,
            "created_at": "2026-04-28T07:00:00Z",
        },
    ],
    "pagination": {"next_cursor": None},
}


class TestJobList:
    def test_renders_sectioned_table(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_JOBS,
        )
        result = runner.invoke(cli, ["job", "list"])
        assert result.exit_code == 0, result.output
        # Section headers in the new style
        assert "NAME" in result.output
        assert "STATUS" in result.output
        assert "ID" in result.output
        # Body rows
        assert "build" in result.output
        assert "success" in result.output
        assert "failure" in result.output
        assert "skipped" in result.output
        assert "job-success-1" in result.output

    def test_status_column_falls_back_to_state_when_in_progress(self, runner, monkeypatch):
        """Running jobs have no conclusion yet — show the state instead."""
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_JOBS,
        )
        result = runner.invoke(cli, ["job", "list"])
        assert "in_progress" in result.output

    def test_on_column_distinguishes_avrea_vs_shadow(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_JOBS,
        )
        result = runner.invoke(cli, ["job", "list"])
        assert "yes" in result.output
        assert "shadow" in result.output

    def test_empty_list_shows_friendly_message(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: {"data": [], "pagination": {"next_cursor": None}},
        )
        result = runner.invoke(cli, ["job", "list"])
        assert result.exit_code == 0
        assert "No jobs found" in result.output

    def test_json_with_field_subset(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_JOBS,
        )
        result = runner.invoke(cli, ["job", "list", "--json", "job_id,state,conclusion"])
        assert result.exit_code == 0, result.output
        parsed = json.loads(result.output)
        assert len(parsed) == 4
        assert set(parsed[0].keys()) == {"job_id", "state", "conclusion"}
        assert parsed[0]["job_id"] == "job-success-1"

    def test_pagination_cursor_surfaced(self, runner, monkeypatch):
        page = dict(SAMPLE_JOBS)
        page["pagination"] = {"next_cursor": "abc-123"}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: page,
        )
        result = runner.invoke(cli, ["job", "list"])
        assert "abc-123" in result.output

    def test_since_sets_created_after_param(self, runner, monkeypatch):
        """--since 24h should resolve to a created_after ISO timestamp on the API call."""
        captured = {}

        def fake_get(self, path, params=None, **kw):
            captured["params"] = params or {}
            return SAMPLE_JOBS

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)
        result = runner.invoke(cli, ["job", "list", "--since", "24h"])
        assert result.exit_code == 0, result.output
        assert "created_after" in captured["params"]
        # Round-trippable ISO timestamp (any timezone offset; just a smoke check)
        assert "T" in captured["params"]["created_after"]
