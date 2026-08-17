"""Tests for ``avr run diagnose``."""

from avrea_cli.main import cli
from avrea_cli.run_diagnostics import _bounded_log_lines
from typing import Any
import httpx
import json

RUN: dict[str, Any] = {
    "data": {
        "run_id": "run-abc123",
        "platform_run_id": 123456789,
        "repository_id": "rep-123",
        "platform_workflow_id": 456,
        "display_title": "CI",
        "status": "completed",
        "conclusion": "failure",
        "run_number": 42,
        "run_attempt": 1,
        "created_at": "2025-06-01T11:59:58Z",
        "started_at": "2025-06-01T12:00:00Z",
        "updated_at": "2025-06-01T12:01:30Z",
        "repository": {"id": "rep-123", "full_name": "org/repo"},
        "workflow": {"workflow_id": "wfl-123", "name": "CI"},
    }
}

JOBS: dict[str, Any] = {
    "data": [
        {
            "job_id": "job-123",
            "repository_id": "rep-123",
            "job_name": "test",
            "state": "completed",
            "conclusion": "failure",
            "running_on_avrea": True,
            "created_at": "2025-06-01T12:00:01Z",
            "started_at": "2025-06-01T12:00:04Z",
            "completed_at": "2025-06-01T12:00:49Z",
            "steps": [
                {
                    "name": "pytest",
                    "status": "completed",
                    "conclusion": "failure",
                    "number": 2,
                    "started_at": "2025-06-01T12:00:05Z",
                    "completed_at": "2025-06-01T12:00:45Z",
                }
            ],
        }
    ],
    "pagination": {"next_cursor": None},
}

METRICS: dict[str, Any] = {
    "data": [
        {
            "job_id": "job-123",
            "metrics": {
                "cpu": {
                    "unit": "ratio",
                    "series": [
                        {
                            "labels": {"cpu": "cpu0", "state": "idle"},
                            "values": [[100, 0.2]],
                        }
                    ],
                },
                "memory": {
                    "unit": "bytes",
                    "series": [
                        {
                            "labels": {"state": "used"},
                            "values": [[100, 1073741824]],
                        }
                    ],
                },
                "disk-io": {
                    "unit": "bytes",
                    "rate_unit": "bytes/sec",
                    "series": [
                        {
                            "labels": {"device": "vda"},
                            "values": [[100, 0], [110, 1048576]],
                            "rates": [[110, 2048]],
                        }
                    ],
                },
                "network": {
                    "unit": "bytes",
                    "rate_unit": "bytes/sec",
                    "series": [],
                },
            },
        }
    ]
}

BASELINE: dict[str, Any] = {
    "data": [
        {
            "success_count": 12,
            "success_median_duration_seconds": 60,
            "success_p95_duration_seconds": 75,
        }
    ]
}

LOGS: dict[str, Any] = {
    "results": [
        {
            "line_number": 18,
            "content": "AssertionError: expected 2, got 3",
            "stream": "stderr",
            "level": "error",
            "timestamp": "2025-06-01T12:00:44Z",
            "step_name": "pytest",
        }
    ],
    "has_more": True,
}


def _install_api(
    monkeypatch,
    *,
    jobs_response: dict[str, Any] = JOBS,
    metrics_response: dict[str, Any] = METRICS,
    metrics_error: bool = False,
):
    get_calls: list[tuple[str, dict | None]] = []
    post_calls: list[tuple[str, dict | None]] = []

    def fake_get(self, path, params=None, **kwargs):
        get_calls.append((path, params))
        if path.endswith("/by-platform-id/123456789") or path.endswith("/workflow-runs/run-abc123"):
            return RUN
        if path.endswith("/workflow-runs/run-abc123/jobs"):
            return jobs_response
        if path.endswith("/workflow-runs/run-abc123/metrics"):
            if metrics_error:
                request = httpx.Request("GET", "https://api.avrea.com/metrics")
                response = httpx.Response(503, request=request)
                raise httpx.HTTPStatusError("metrics unavailable", request=request, response=response)
            return metrics_response
        if path.endswith("/workflow-runs/aggregate"):
            return BASELINE
        raise AssertionError(f"unexpected GET {path}")

    def fake_post(self, path, json=None, **kwargs):
        post_calls.append((path, json))
        if path == "/logs/search":
            return LOGS
        raise AssertionError(f"unexpected POST {path}")

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", fake_post)
    return get_calls, post_calls


def test_avrea_id_composes_existing_apis(runner, monkeypatch) -> None:
    get_calls, post_calls = _install_api(monkeypatch)

    result = runner.invoke(cli, ["run", "diagnose", "run-abc123", "--json"])

    assert result.exit_code == 0, result.output
    report = json.loads(result.output)
    assert report["run"]["run_id"] == "run-abc123"
    assert report["timing"]["queue_seconds"] == 2
    assert report["timing"]["execution_seconds"] == 90
    assert report["jobs"][0]["timing"]["queue_seconds"] == 3
    assert report["jobs"][0]["timing"]["start_offset_seconds"] == 4
    assert report["jobs"][0]["steps"][0]["timing"]["execution_seconds"] == 40
    assert report["jobs"][0]["metrics"]["sources"]["cpu"]["peak"] == 0.8
    assert report["jobs"][0]["metrics"]["sources"]["disk-io"]["total"] == 1048576
    assert report["jobs"][0]["failed_logs"]["lines"][0]["line_number"] == 18
    assert report["baseline"]["sample_count"] == 12
    assert report["complete"] is False
    assert {warning["code"] for warning in report["warnings"]} == {"logs_truncated"}

    paths = {path for path, _ in get_calls}
    assert paths == {
        "/orgs/org-default/workflow-runs/run-abc123",
        "/orgs/org-default/workflow-runs/run-abc123/jobs",
        "/orgs/org-default/workflow-runs/run-abc123/metrics",
        "/orgs/org-default/workflow-runs/aggregate",
    }
    assert all(not path.endswith("/diagnostics") for path in paths)
    params_by_path = dict(get_calls)
    assert params_by_path["/orgs/org-default/workflow-runs/run-abc123"] == {"include": ["workflow"]}
    assert params_by_path["/orgs/org-default/workflow-runs/run-abc123/jobs"] == {
        "limit": 100,
        "order": "created_at.asc",
        "include": ["steps"],
    }
    assert params_by_path["/orgs/org-default/workflow-runs/run-abc123/metrics"] == {
        "source": ["cpu", "memory", "disk-io", "network"]
    }
    assert params_by_path["/orgs/org-default/workflow-runs/aggregate"] == {
        "repository_ids": ["rep-123"],
        "conclusions": ["success"],
        "workflow_platform_ids": [456],
        "created_after": "2025-05-02T11:59:58Z",
        "created_before": "2025-06-01T11:59:57.999999Z",
        "time_bucket": "total",
        "include": [],
    }
    assert post_calls == [
        (
            "/logs/search",
            {
                "job_id": "job-123",
                "limit": 101,
                "order": "line_number.desc",
            },
        )
    ]


def test_github_id_resolves_directly_then_composes(runner, monkeypatch) -> None:
    get_calls, _ = _install_api(monkeypatch)

    result = runner.invoke(cli, ["run", "diagnose", "123456789", "--json"])

    assert result.exit_code == 0, result.output
    assert (
        "/orgs/org-default/workflow-runs/by-platform-id/123456789",
        {"include": ["workflow"]},
    ) in get_calls
    assert not any(path.endswith("/workflow-runs/run-abc123") for path, _ in get_calls)


def test_human_report_prioritizes_failure_evidence(runner, monkeypatch) -> None:
    _install_api(monkeypatch)

    result = runner.invoke(cli, ["run", "diagnose", "run-abc123"])

    assert result.exit_code == 0, result.output
    assert "CI #42 (attempt 1)" in result.output
    assert "queued 2s" in result.output
    assert "12 successful runs" in result.output
    assert "pytest" in result.output
    assert "p95 80.0%" in result.output
    assert "p95 1.0 GB" in result.output
    assert "total 1.0 MB" in result.output
    assert "AssertionError: expected 2, got 3" in result.output
    assert "logs_truncated" in result.output


def test_optional_metrics_failure_returns_partial_report(runner, monkeypatch) -> None:
    _install_api(monkeypatch, metrics_error=True)

    result = runner.invoke(cli, ["run", "diagnose", "run-abc123", "--json"])

    assert result.exit_code == 0, result.output
    report = json.loads(result.output)
    assert report["complete"] is False
    assert report["jobs"][0]["metrics"] == {
        "status": "unavailable",
        "reason": "metrics_backend_unavailable",
        "sources": {},
    }
    assert "metrics_unavailable" in {warning["code"] for warning in report["warnings"]}


def test_invalid_metrics_discard_warnings_from_partial_parse(runner, monkeypatch) -> None:
    first_job = {**JOBS["data"][0], "job_id": "job-no-execution", "conclusion": "success"}
    second_job = {**JOBS["data"][0], "job_id": "job-invalid", "conclusion": "success"}
    metrics_response = {
        "data": [
            {"job_id": "job-no-execution", "error": "no_execution"},
            {
                "job_id": "job-invalid",
                "metrics": {
                    source: ({"unit": "ratio", "series": "invalid"} if source == "cpu" else value)
                    for source, value in METRICS["data"][0]["metrics"].items()
                },
            },
        ]
    }
    _install_api(
        monkeypatch,
        jobs_response={"data": [first_job, second_job], "pagination": {"next_cursor": None}},
        metrics_response=metrics_response,
    )

    result = runner.invoke(cli, ["run", "diagnose", "run-abc123", "--json"])

    assert result.exit_code == 0, result.output
    report = json.loads(result.output)
    metric_warnings = [warning["code"] for warning in report["warnings"] if warning["component"] == "metrics"]
    assert metric_warnings == ["metrics_unavailable"]


def test_log_byte_limit_stops_after_partial_utf8_line(monkeypatch) -> None:
    monkeypatch.setattr("avrea_cli.run_diagnostics.MAX_LOG_BYTES_PER_JOB", 5)

    lines, truncated = _bounded_log_lines(
        [
            {"line_number": 2, "content": "ééé"},
            {"line_number": 1, "content": "older"},
        ]
    )

    assert truncated is True
    assert lines == [
        {
            "line_number": 2,
            "content": "éé",
            "stream": None,
            "level": None,
            "timestamp": None,
            "step_name": None,
        }
    ]


def test_invalid_run_response_fails_cleanly(runner, monkeypatch) -> None:
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, **kwargs: {"data": []},
    )

    result = runner.invoke(cli, ["run", "diagnose", "run-abc123"])

    assert result.exit_code != 0
    assert "invalid workflow-run response" in result.output.lower()
