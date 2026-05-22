"""Tests for `avr workflow view --json fields` (Tier 2 JSON refactor).

Covers the field-selection contract (camelCase wire names, `?` discoverability,
`*` for all fields) on the view path that previously took `--json` as a flag."""

from avrea_cli.main import cli
import httpx
import json

SAMPLE_BUCKET = {
    "data": [
        {
            "workflow": {
                "workflow_id": "wfl-abc",
                "platform_id": 12345,
                "name": "Build and Test",
                "path": ".github/workflows/build.yml",
                "repository_full_name": "acme/svc",
            },
            "count": 458,
            "completed_count": 442,
            "failure_count": 31,
            "flaked_count": 4,
            "median_duration_seconds": 187,
            "p95_duration_seconds": 412,
            "jobs": [{"job": {"name": "lint"}, "count": 458}, {"job": {"name": "test"}, "count": 458}],
        }
    ]
}


class TestWorkflowViewJson:
    def test_all_fields(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: SAMPLE_BUCKET,
        )
        result = runner.invoke(cli, ["workflow", "view", "wfl-abc", "--json", "*"])
        assert result.exit_code == 0, result.output
        parsed = json.loads(result.output)
        # Single object, not array
        assert isinstance(parsed, dict)
        assert parsed["workflow_id"] == "wfl-abc"
        assert parsed["name"] == "Build and Test"
        assert parsed["runs"] == 458

    def test_field_selection(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: SAMPLE_BUCKET,
        )
        result = runner.invoke(cli, ["workflow", "view", "wfl-abc", "--json", "name,runs,median_duration_seconds"])
        assert result.exit_code == 0, result.output
        parsed = json.loads(result.output)
        assert set(parsed.keys()) == {"name", "runs", "median_duration_seconds"}
        assert parsed["median_duration_seconds"] == 187

    def test_question_mark_lists_view_specific_fields(self, runner):
        # The view schema extends list with `jobs` and `p95_duration_seconds`;
        # pin those so a future schema split doesn't silently drop them from
        # discovery. The shared `--json '?'` mechanics live in test_json_output.
        result = runner.invoke(cli, ["workflow", "view", "wfl-abc", "--json", "?"])
        assert result.exit_code == 0
        assert "jobs" in result.output
        assert "p95_duration_seconds" in result.output

    def test_jq_filters_output(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: SAMPLE_BUCKET,
        )
        result = runner.invoke(cli, ["workflow", "view", "wfl-abc", "--json", "*", "-q", ".name"])
        # jq output is the filtered string + trailing newline
        assert result.exit_code == 0, result.output
        assert "Build and Test" in result.output

    def test_empty_buckets_returns_empty_object(self, runner, monkeypatch):
        """Defensive: when the aggregate endpoint returns no rows, --json
        should still produce valid JSON (an empty object) rather than fail."""
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: {"data": []},
        )
        result = runner.invoke(cli, ["workflow", "view", "wfl-missing", "--json", "*"])
        assert result.exit_code == 0, result.output
        parsed = json.loads(result.output)
        assert isinstance(parsed, dict)
        # All fields present, but values are None because the source dict was empty.
        assert all(v is None for v in parsed.values())


class TestWorkflowViewRecentRunsHint:
    """The RECENT RUNS section is auxiliary — a 5xx mid-view shouldn't kill
    the command. The aggregate fetch must succeed; the recent-runs fetch
    falling over should surface a dim hint and continue."""

    def test_recent_runs_5xx_renders_hint_not_crash(self, runner, monkeypatch):
        def fake_get(self, path, params=None):
            if path.endswith("/aggregate"):
                return SAMPLE_BUCKET
            # Recent runs lookup raises.
            req = httpx.Request("GET", f"https://api.example{path}")
            raise httpx.HTTPStatusError("502", request=req, response=httpx.Response(502, request=req))

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

        result = runner.invoke(cli, ["workflow", "view", "wfl-abc"])

        assert result.exit_code == 0, result.output
        # Hint surfaces the cause.
        assert "could not load recent runs" in result.output
        assert "HTTP 502" in result.output
        # No RECENT RUNS section was rendered (no data to show).
        assert "RECENT RUNS" not in result.output
