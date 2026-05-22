"""Unit tests for job view and job logs commands."""

from avrea_cli.main import cli
import json

SAMPLE_JOB = {
    "data": {
        "job_id": "job-abc123",
        "job_name": "Build",
        "repository_id": "rep-xyz",
        "repository_full_name": "org/repo",
        "state": "completed",
        "conclusion": "success",
        "duration_seconds": 42,
        "running_on_avrea": True,
        "job_labels": ["ubuntu-latest"],
        "platform_run_id": 12345,
        "platform_job_id": 67890,
        "created_at": "2025-06-01T12:00:00Z",
        "started_at": "2025-06-01T12:00:01Z",
        "completed_at": "2025-06-01T12:00:43Z",
        "steps": [
            {
                "name": "Set up job",
                "status": "completed",
                "conclusion": "success",
                "number": 1,
                "started_at": "2025-06-01T12:00:01Z",
                "completed_at": "2025-06-01T12:00:03Z",
            },
            {
                "name": "Build",
                "status": "completed",
                "conclusion": "success",
                "number": 2,
                "started_at": "2025-06-01T12:00:03Z",
                "completed_at": "2025-06-01T12:00:40Z",
            },
        ],
        "workflow_run": {"run_id": "run-xyz", "run_number": 42},
    }
}


class TestJobView:
    def test_displays_job_metadata(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_JOB,
        )
        result = runner.invoke(cli, ["job", "view", "job-abc123"])
        assert result.exit_code == 0
        assert "Build" in result.output
        assert "org/repo" in result.output
        assert "42s" in result.output

    def test_displays_steps(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_JOB,
        )
        result = runner.invoke(cli, ["job", "view", "job-abc123"])
        assert "Set up job" in result.output
        assert "STEPS" in result.output

    def test_json_output_all_fields(self, runner, monkeypatch):
        """`--json '*'` returns all known fields as a single object (not array)."""
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_JOB,
        )
        result = runner.invoke(cli, ["job", "view", "job-abc123", "--json", "*"])
        assert result.exit_code == 0, result.output
        parsed = json.loads(result.output)
        assert isinstance(parsed, dict)
        assert parsed["job_id"] == "job-abc123"

    def test_json_output_field_selection(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_JOB,
        )
        result = runner.invoke(cli, ["job", "view", "job-abc123", "--json", "job_id,conclusion,steps"])
        assert result.exit_code == 0, result.output
        parsed = json.loads(result.output)
        assert set(parsed.keys()) == {"job_id", "conclusion", "steps"}

    def test_requires_auth(self, runner, monkeypatch):
        monkeypatch.delenv("AVR_TOKEN", raising=False)
        result = runner.invoke(cli, ["job", "view", "job-abc123"])
        assert result.exit_code != 0


class TestJobLogs:
    def test_displays_grouped_logs(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_JOB,
        )
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, **kw: {
                "results": [
                    {"line_number": 1, "content": "Starting build", "level": "info", "step_name": "Build"},
                    {"line_number": 2, "content": "Done", "level": "info", "step_name": "Build"},
                ],
                "has_more": False,
            },
        )
        result = runner.invoke(cli, ["job", "logs", "job-abc123"])
        assert result.exit_code == 0
        assert "--- Build ---" in result.output
        assert "Starting build" in result.output

    def test_no_logs_found(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_JOB,
        )
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, **kw: {"results": [], "has_more": False},
        )
        result = runner.invoke(cli, ["job", "logs", "job-abc123"])
        assert result.exit_code == 0
        assert "No log entries found" in result.output

    def test_failed_filter(self, runner, monkeypatch):
        # Distinct step names + log content so the negative assertions
        # below can't be polluted by the surrounding job header (which
        # echoes the job_name "Build" from SAMPLE_JOB).
        job_with_failure = {
            "data": {
                **SAMPLE_JOB["data"],
                "steps": [
                    {
                        "name": "Compile",
                        "status": "completed",
                        "conclusion": "success",
                        "number": 1,
                        "started_at": "2025-06-01T12:00:01Z",
                        "completed_at": "2025-06-01T12:00:03Z",
                    },
                    {
                        "name": "Run tests",
                        "status": "completed",
                        "conclusion": "failure",
                        "number": 2,
                        "started_at": "2025-06-01T12:00:03Z",
                        "completed_at": "2025-06-01T12:00:40Z",
                    },
                ],
            }
        }
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: job_with_failure,
        )
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, **kw: {
                "results": [
                    {"line_number": 1, "content": "compiled-ok", "level": "info", "step_name": "Compile"},
                    {"line_number": 2, "content": "FAIL", "level": "error", "step_name": "Run tests"},
                ],
                "has_more": False,
            },
        )
        result = runner.invoke(cli, ["job", "logs", "job-abc123", "--failed"])
        assert result.exit_code == 0
        assert "Run tests" in result.output
        assert "FAIL" in result.output
        # --failed must exclude successful steps and their content.
        assert "Compile" not in result.output
        assert "compiled-ok" not in result.output

    def test_requires_auth(self, runner, monkeypatch):
        monkeypatch.delenv("AVR_TOKEN", raising=False)
        result = runner.invoke(cli, ["job", "logs", "job-abc123"])
        assert result.exit_code != 0
