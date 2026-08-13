"""Tests for ``avr run diagnose``."""

from avrea_cli.main import cli
import json

DIAGNOSTICS = {
    "data": {
        "generated_at": "2025-06-01T12:03:00Z",
        "complete": False,
        "bounds": {
            "max_jobs": 100,
            "max_steps": 1000,
            "max_log_jobs": 10,
            "max_log_lines_per_job": 100,
            "max_log_bytes_per_job": 25600,
            "baseline_days": 30,
        },
        "run": {
            "run_id": "run-abc123",
            "display_title": "CI",
            "status": "completed",
            "conclusion": "failure",
            "run_number": 42,
            "run_attempt": 1,
            "repository": {"id": "rep-123", "full_name": "org/repo"},
            "workflow": {"workflow_id": "wfl-123", "name": "CI"},
        },
        "timing": {
            "queue_seconds": 2,
            "execution_seconds": 90,
            "start_offset_seconds": None,
            "execution_end_source": "updated_at",
        },
        "jobs": [
            {
                "job": {
                    "job_id": "job-123",
                    "job_name": "test",
                    "state": "completed",
                    "conclusion": "failure",
                    "running_on_avrea": True,
                },
                "timing": {
                    "queue_seconds": 3,
                    "execution_seconds": 45,
                    "start_offset_seconds": 4,
                    "execution_end_source": "completed_at",
                },
                "steps": [
                    {
                        "step": {
                            "name": "pytest",
                            "status": "completed",
                            "conclusion": "failure",
                            "number": 2,
                        },
                        "timing": {
                            "execution_seconds": 40,
                            "start_offset_seconds": 5,
                        },
                    }
                ],
                "steps_truncated": False,
                "metrics": {
                    "status": "complete",
                    "reason": None,
                    "sources": {
                        "cpu": {
                            "sample_count": 10,
                            "unit": "ratio",
                            "p95": 0.8,
                            "peak": 0.9,
                            "total": None,
                            "total_unit": None,
                        },
                        "memory": {
                            "sample_count": 10,
                            "unit": "bytes",
                            "p95": 1073741824,
                            "peak": 2147483648,
                            "total": None,
                            "total_unit": None,
                        },
                        "disk-io": {
                            "sample_count": 9,
                            "unit": "bytes/sec",
                            "p95": 2048,
                            "peak": 4096,
                            "total": 1048576,
                            "total_unit": "bytes",
                        },
                    },
                },
                "failed_logs": {
                    "status": "partial",
                    "reason": None,
                    "truncated": True,
                    "lines": [
                        {
                            "line_number": 18,
                            "content": "AssertionError: expected 2, got 3",
                            "stream": "stderr",
                            "level": "error",
                            "timestamp": "2025-06-01T12:02:30Z",
                            "step_name": "pytest",
                        }
                    ],
                },
            }
        ],
        "jobs_truncated": False,
        "baseline": {
            "status": "complete",
            "reason": None,
            "window_start": "2025-05-02T12:00:00Z",
            "window_end": "2025-06-01T12:00:00Z",
            "sample_count": 12,
            "median_duration_seconds": 60,
            "p95_duration_seconds": 75,
        },
        "warnings": [
            {
                "code": "logs_truncated",
                "component": "logs",
                "message": "Failed-job logs were truncated to the diagnostic response bounds.",
                "job_id": "job-123",
            }
        ],
    }
}


def test_avrea_id_json_uses_one_diagnostics_request(runner, monkeypatch) -> None:
    calls = []

    def fake_get(self, path, **kwargs):
        calls.append((path, kwargs.get("params")))
        return DIAGNOSTICS

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

    result = runner.invoke(cli, ["run", "diagnose", "run-abc123", "--json"])

    assert result.exit_code == 0, result.output
    assert json.loads(result.output) == DIAGNOSTICS["data"]
    assert calls == [("/orgs/org-default/workflow-runs/run-abc123/diagnostics", None)]


def test_github_id_resolves_without_fetching_run_details(runner, monkeypatch) -> None:
    calls = []

    def fake_get(self, path, **kwargs):
        calls.append((path, kwargs.get("params")))
        if path.endswith("/by-platform-id/123456789"):
            return {
                "data": {
                    "run_id": "run-abc123",
                    "repository": {"full_name": "org/repo"},
                }
            }
        return DIAGNOSTICS

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

    result = runner.invoke(cli, ["run", "diagnose", "123456789", "--json"])

    assert result.exit_code == 0, result.output
    assert calls == [
        (
            "/orgs/org-default/workflow-runs/by-platform-id/123456789",
            {"include": []},
        ),
        ("/orgs/org-default/workflow-runs/run-abc123/diagnostics", None),
    ]


def test_human_report_prioritizes_failure_evidence(runner, monkeypatch) -> None:
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, **kwargs: DIAGNOSTICS,
    )

    result = runner.invoke(cli, ["run", "diagnose", "run-abc123"])

    assert result.exit_code == 0, result.output
    assert "CI #42 (attempt 1)" in result.output
    assert "Run timing" in result.output
    assert "queued 2s" in result.output
    assert "12 successful runs" in result.output
    assert "test" in result.output
    assert "pytest" in result.output
    assert "CPU" in result.output
    assert "p95 80.0%" in result.output
    assert "Memory" in result.output
    assert "p95 1.0 GB" in result.output
    assert "Disk I/O" in result.output
    assert "total 1.0 MB" in result.output
    assert "AssertionError: expected 2, got 3" in result.output
    assert "logs_truncated" in result.output


def test_invalid_diagnostics_response_fails_cleanly(runner, monkeypatch) -> None:
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, **kwargs: {"data": []},
    )

    result = runner.invoke(cli, ["run", "diagnose", "run-abc123"])

    assert result.exit_code != 0
    assert "invalid diagnostics response" in result.output.lower()
