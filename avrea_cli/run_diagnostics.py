"""Build bounded workflow-run diagnostics from existing public APIs."""

from avrea_cli.api_client import ApiClient
from avrea_cli.metrics_display import aggregate_cpu_utilization
from avrea_cli.metrics_display import aggregate_memory_used
from avrea_cli.metrics_display import aggregate_rates
from avrea_cli.metrics_display import aggregate_values
from concurrent.futures import Future
from concurrent.futures import ThreadPoolExecutor
from datetime import UTC
from datetime import datetime
from datetime import timedelta
from typing import Any
from typing import Literal
import click
import httpx
import math

MAX_DIAGNOSTIC_JOBS = 100
MAX_DIAGNOSTIC_STEPS = 1000
MAX_LOG_JOBS = 10
MAX_LOG_LINES_PER_JOB = 100
MAX_LOG_BYTES_PER_JOB = 25 * 1024
BASELINE_DAYS = 30

_METRIC_SOURCES = ("cpu", "memory", "disk-io", "network")
_COUNTER_SOURCES = frozenset({"disk-io", "network"})
_FAILED_JOB_CONCLUSIONS = frozenset({"failure", "timed_out", "action_required"})


def _warning(
    code: str,
    component: Literal["run", "jobs", "steps", "logs", "metrics", "baseline"],
    message: str,
    *,
    job_id: str | None = None,
) -> dict[str, Any]:
    warning: dict[str, Any] = {
        "code": code,
        "component": component,
        "message": message,
    }
    if job_id is not None:
        warning["job_id"] = job_id
    return warning


def _parse_datetime(value: object) -> datetime | None:
    if not isinstance(value, str):
        return None
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=UTC)
    return parsed.astimezone(UTC)


def _isoformat(value: datetime) -> str:
    return value.astimezone(UTC).isoformat().replace("+00:00", "Z")


def _seconds_between(start: datetime | None, end: datetime | None) -> float | None:
    if start is None or end is None:
        return None
    seconds = (end - start).total_seconds()
    return seconds if seconds >= 0 else None


def _timing(
    *,
    created_at: object,
    started_at: object,
    completed_at: object,
    parent_started_at: object,
    generated_at: datetime,
    completed: bool,
    completed_source: Literal["completed_at", "updated_at"],
) -> tuple[dict[str, Any], bool]:
    created = _parse_datetime(created_at)
    started = _parse_datetime(started_at)
    finished = _parse_datetime(completed_at)
    parent_started = _parse_datetime(parent_started_at)
    end = finished if completed else generated_at

    queue_seconds = _seconds_between(created, started)
    execution_seconds = _seconds_between(started, end)
    start_offset_seconds = _seconds_between(parent_started, started)
    clock_skew = any(
        first is not None and second is not None and value is None
        for first, second, value in (
            (created, started, queue_seconds),
            (started, end, execution_seconds),
            (parent_started, started, start_offset_seconds),
        )
    )
    if started is None or end is None:
        end_source = None
    elif completed:
        end_source = completed_source
    else:
        end_source = "generated_at"
    return (
        {
            "queue_seconds": queue_seconds,
            "execution_seconds": execution_seconds,
            "start_offset_seconds": start_offset_seconds,
            "execution_end_source": end_source,
        },
        clock_skew,
    )


def _percentile(values: list[float], quantile: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    if len(ordered) == 1:
        return ordered[0]
    rank = (len(ordered) - 1) * quantile
    lower = math.floor(rank)
    upper = math.ceil(rank)
    if lower == upper:
        return ordered[lower]
    return ordered[lower] + (ordered[upper] - ordered[lower]) * (rank - lower)


def _counter_total(series: list[dict[str, Any]]) -> float:
    total = 0.0
    for item in series:
        values = item.get("values") or []
        for index in range(1, len(values)):
            previous = float(values[index - 1][1])
            current = float(values[index][1])
            total += current - previous if current >= previous else current
    return total


def _summarize_metric(source: str, response: dict[str, Any]) -> dict[str, Any]:
    raw_series = response.get("series") or []
    if not isinstance(raw_series, list):
        raise ValueError("metrics series must be a list")
    series = [item for item in raw_series if isinstance(item, dict)]
    if source == "cpu":
        samples = aggregate_cpu_utilization(series)
    elif source == "memory":
        samples = aggregate_memory_used(series)
    elif source in _COUNTER_SOURCES:
        samples = aggregate_rates(series)
    else:
        samples = aggregate_values(series)

    values = [value for _, value in samples]
    is_counter = source in _COUNTER_SOURCES
    return {
        "sample_count": len(values),
        "unit": response.get("rate_unit") if is_counter and response.get("rate_unit") else response.get("unit"),
        "p95": _percentile(values, 0.95),
        "peak": max(values) if values else None,
        "total": _counter_total(series) if is_counter else None,
        "total_unit": response.get("unit") if is_counter else None,
    }


def _unavailable_metrics() -> dict[str, Any]:
    return {
        "status": "unavailable",
        "reason": "metrics_backend_unavailable",
        "sources": {},
    }


def _build_metrics(
    response: dict[str, Any] | None,
    jobs: list[dict[str, Any]],
    warnings: list[dict[str, Any]],
) -> dict[str, dict[str, Any]]:
    if response is None:
        warnings.append(
            _warning(
                "metrics_unavailable",
                "metrics",
                "Runner metrics are temporarily unavailable.",
            )
        )
        return {
            str(job.get("job_id")): (
                _unavailable_metrics()
                if job.get("running_on_avrea")
                else {"status": "not_applicable", "reason": "not_running_on_avrea", "sources": {}}
            )
            for job in jobs
        }

    entries = response.get("data")
    if not isinstance(entries, list):
        raise ValueError("metrics response data must be a list")
    entries_by_job = {
        str(entry.get("job_id")): entry
        for entry in entries
        if isinstance(entry, dict) and isinstance(entry.get("job_id"), str)
    }
    diagnostics: dict[str, dict[str, Any]] = {}
    for job in jobs:
        job_id = str(job.get("job_id"))
        if not job.get("running_on_avrea"):
            diagnostics[job_id] = {
                "status": "not_applicable",
                "reason": "not_running_on_avrea",
                "sources": {},
            }
            continue

        entry = entries_by_job.get(job_id)
        if entry is None or entry.get("error"):
            reason = str(entry.get("error")) if entry is not None else "metrics_missing"
            diagnostics[job_id] = {
                "status": "unavailable",
                "reason": reason,
                "sources": {},
            }
            warnings.append(
                _warning(
                    "metrics_no_execution" if reason == "no_execution" else "metrics_missing",
                    "metrics",
                    "Runner metrics are unavailable for this job.",
                    job_id=job_id,
                )
            )
            continue

        raw_metrics = entry.get("metrics")
        if not isinstance(raw_metrics, dict):
            raise ValueError("metrics entry must contain a metrics object")
        sources = {
            source: _summarize_metric(source, raw_metrics[source])
            for source in _METRIC_SOURCES
            if isinstance(raw_metrics.get(source), dict)
        }
        missing_sources = [source for source in _METRIC_SOURCES if source not in sources]
        diagnostics[job_id] = {
            "status": "partial" if missing_sources else "complete",
            "reason": "metrics_sources_missing" if missing_sources else None,
            "sources": sources,
        }
        if missing_sources:
            warnings.append(
                _warning(
                    "metrics_partial",
                    "metrics",
                    f"Runner metrics are missing sources: {', '.join(missing_sources)}.",
                    job_id=job_id,
                )
            )
    return diagnostics


def _bounded_log_lines(results: list[dict[str, Any]]) -> tuple[list[dict[str, Any]], bool]:
    selected: list[dict[str, Any]] = []
    remaining_bytes = MAX_LOG_BYTES_PER_JOB
    truncated = len(results) > MAX_LOG_LINES_PER_JOB
    for result in results[:MAX_LOG_LINES_PER_JOB]:
        content = result.get("content")
        if not isinstance(content, str):
            raise ValueError("log content must be a string")
        encoded = content.encode("utf-8")
        content_truncated = False
        if len(encoded) > remaining_bytes:
            truncated = True
            if remaining_bytes == 0:
                break
            content = encoded[:remaining_bytes].decode("utf-8", errors="ignore")
            content_truncated = True
        selected.append(
            {
                "line_number": result.get("line_number"),
                "content": content,
                "stream": result.get("stream"),
                "level": result.get("level"),
                "timestamp": result.get("timestamp"),
                "step_name": result.get("step_name"),
            }
        )
        remaining_bytes -= len(content.encode("utf-8"))
        if content_truncated or remaining_bytes == 0:
            break
    selected.reverse()
    return selected, truncated


def _build_log_excerpt(
    future: Future[dict[str, Any]],
    job_id: str,
    warnings: list[dict[str, Any]],
) -> dict[str, Any]:
    try:
        response = future.result()
        results = response.get("results")
        if not isinstance(results, list) or not all(isinstance(item, dict) for item in results):
            raise ValueError("log results must be a list")
        lines, truncated = _bounded_log_lines(results)
        truncated = truncated or bool(response.get("has_more"))
    except httpx.HTTPError, ValueError, AttributeError:
        warnings.append(
            _warning(
                "logs_unavailable",
                "logs",
                "Failed-job logs are temporarily unavailable.",
                job_id=job_id,
            )
        )
        return {
            "status": "unavailable",
            "reason": "logs_backend_unavailable",
            "truncated": False,
            "lines": [],
        }

    if truncated:
        warnings.append(
            _warning(
                "logs_truncated",
                "logs",
                "Failed-job logs were truncated to the diagnostic response bounds.",
                job_id=job_id,
            )
        )
    return {
        "status": "partial" if truncated else "complete",
        "reason": None,
        "truncated": truncated,
        "lines": lines,
    }


def _build_baseline(
    future: Future[dict[str, Any]] | None,
    *,
    window_start: datetime,
    window_end: datetime,
    warnings: list[dict[str, Any]],
) -> dict[str, Any]:
    if future is None:
        return {
            "status": "unavailable",
            "reason": "workflow_metadata_missing",
            "window_start": _isoformat(window_start),
            "window_end": _isoformat(window_end),
            "sample_count": 0,
            "median_duration_seconds": None,
            "p95_duration_seconds": None,
        }
    try:
        response = future.result()
        rows = response.get("data")
        if not isinstance(rows, list):
            raise ValueError("baseline response data must be a list")
        row = rows[0] if rows else {}
        if not isinstance(row, dict):
            raise ValueError("baseline row must be an object")
    except httpx.HTTPError, ValueError, AttributeError:
        warnings.append(
            _warning(
                "baseline_unavailable",
                "baseline",
                "The workflow baseline is temporarily unavailable.",
            )
        )
        return {
            "status": "unavailable",
            "reason": "baseline_backend_unavailable",
            "window_start": _isoformat(window_start),
            "window_end": _isoformat(window_end),
            "sample_count": 0,
            "median_duration_seconds": None,
            "p95_duration_seconds": None,
        }
    return {
        "status": "complete",
        "reason": None,
        "window_start": _isoformat(window_start),
        "window_end": _isoformat(window_end),
        "sample_count": row.get("success_count") or 0,
        "median_duration_seconds": row.get("success_median_duration_seconds"),
        "p95_duration_seconds": row.get("success_p95_duration_seconds"),
    }


def build_run_diagnostics(
    client: ApiClient,
    org_id: str,
    run: dict[str, Any],
    *,
    generated_at: datetime | None = None,
) -> dict[str, Any]:
    """Compose a bounded diagnostic report from existing public endpoints."""
    generated_at = generated_at or datetime.now(UTC)
    run_id = run.get("run_id")
    if not isinstance(run_id, str):
        raise click.ClickException("Avrea returned a workflow run without a run_id.")

    warnings: list[dict[str, Any]] = []
    target_created_at = _parse_datetime(run.get("created_at"))
    if target_created_at is None:
        target_created_at = generated_at
        warnings.append(
            _warning(
                "baseline_target_time_missing",
                "baseline",
                "The baseline window uses report generation time because run creation time is missing.",
            )
        )
    window_start = target_created_at - timedelta(days=BASELINE_DAYS)

    repository_id = run.get("repository_id")
    if not isinstance(repository_id, str):
        repository = run.get("repository")
        repository_id = repository.get("id") if isinstance(repository, dict) else None
    platform_workflow_id = run.get("platform_workflow_id")

    with ThreadPoolExecutor(max_workers=MAX_LOG_JOBS + 3) as executor:
        jobs_future = executor.submit(
            client.public_get,
            f"/orgs/{org_id}/workflow-runs/{run_id}/jobs",
            params={"limit": MAX_DIAGNOSTIC_JOBS, "order": "created_at.asc", "include": ["steps"]},
        )
        metrics_future = executor.submit(
            client.public_get,
            f"/orgs/{org_id}/workflow-runs/{run_id}/metrics",
            params={"source": list(_METRIC_SOURCES)},
        )
        baseline_future: Future[dict[str, Any]] | None = None
        if isinstance(repository_id, str) and isinstance(platform_workflow_id, int):
            baseline_future = executor.submit(
                client.public_get,
                f"/orgs/{org_id}/workflow-runs/aggregate",
                params={
                    "repository_ids": [repository_id],
                    "conclusions": ["success"],
                    "workflow_platform_ids": [platform_workflow_id],
                    "created_after": _isoformat(window_start),
                    "created_before": _isoformat(target_created_at - timedelta(microseconds=1)),
                    "time_bucket": "total",
                    "include": [],
                },
            )
        else:
            warnings.append(
                _warning(
                    "baseline_metadata_missing",
                    "baseline",
                    "The workflow baseline is unavailable because workflow metadata is missing.",
                )
            )

        jobs_response = jobs_future.result()
        raw_jobs = jobs_response.get("data")
        if not isinstance(raw_jobs, list) or not all(isinstance(job, dict) for job in raw_jobs):
            raise click.ClickException("Avrea returned an invalid workflow-run jobs response.")
        jobs = raw_jobs[:MAX_DIAGNOSTIC_JOBS]
        pagination = jobs_response.get("pagination")
        jobs_truncated = len(raw_jobs) > MAX_DIAGNOSTIC_JOBS or (
            isinstance(pagination, dict) and pagination.get("next_cursor") is not None
        )
        if jobs_truncated:
            warnings.append(
                _warning(
                    "jobs_truncated",
                    "jobs",
                    "Jobs were truncated to the diagnostic response bounds.",
                )
            )

        failed_jobs = [job for job in jobs if job.get("conclusion") in _FAILED_JOB_CONCLUSIONS]
        log_eligible_jobs = [job for job in failed_jobs if job.get("running_on_avrea") is not False]
        selected_failed_jobs = log_eligible_jobs[:MAX_LOG_JOBS]
        log_futures = {
            str(job.get("job_id")): executor.submit(
                client.public_post,
                "/logs/search",
                json={
                    "job_id": str(job.get("job_id")),
                    "limit": MAX_LOG_LINES_PER_JOB + 1,
                    "order": "line_number.desc",
                },
            )
            for job in selected_failed_jobs
        }

        metrics_warnings: list[dict[str, Any]] = []
        try:
            metrics_response = metrics_future.result()
            metrics_by_job = _build_metrics(metrics_response, jobs, metrics_warnings)
        except httpx.HTTPError, ValueError, AttributeError:
            metrics_warnings = []
            metrics_by_job = _build_metrics(None, jobs, metrics_warnings)
        warnings.extend(metrics_warnings)
        baseline = _build_baseline(
            baseline_future,
            window_start=window_start,
            window_end=target_created_at,
            warnings=warnings,
        )

        logs_by_job = {
            str(job.get("job_id")): {
                "status": "not_applicable",
                "reason": "job_did_not_fail",
                "truncated": False,
                "lines": [],
            }
            for job in jobs
        }
        for job in failed_jobs:
            if job.get("running_on_avrea") is False:
                logs_by_job[str(job.get("job_id"))] = {
                    "status": "not_applicable",
                    "reason": "not_running_on_avrea",
                    "truncated": False,
                    "lines": [],
                }
        for job in selected_failed_jobs:
            job_id = str(job.get("job_id"))
            logs_by_job[job_id] = _build_log_excerpt(log_futures[job_id], job_id, warnings)
        for job in log_eligible_jobs[MAX_LOG_JOBS:]:
            logs_by_job[str(job.get("job_id"))] = {
                "status": "partial",
                "reason": "log_job_limit",
                "truncated": True,
                "lines": [],
            }
        if len(log_eligible_jobs) > MAX_LOG_JOBS:
            warnings.append(
                _warning(
                    "log_jobs_truncated",
                    "logs",
                    "Failed-job log excerpts were limited to the first 10 failed jobs.",
                )
            )

    run_timing, run_clock_skew = _timing(
        created_at=run.get("created_at"),
        started_at=run.get("started_at"),
        completed_at=run.get("updated_at"),
        parent_started_at=None,
        generated_at=generated_at,
        completed=run.get("status") == "completed",
        completed_source="updated_at",
    )
    if run_clock_skew:
        warnings.append(
            _warning(
                "run_clock_skew",
                "run",
                "A run timestamp regression prevented one or more timing calculations.",
            )
        )

    remaining_steps = MAX_DIAGNOSTIC_STEPS
    job_diagnostics: list[dict[str, Any]] = []
    for job in jobs:
        job_id = str(job.get("job_id"))
        job_timing, job_clock_skew = _timing(
            created_at=job.get("created_at"),
            started_at=job.get("started_at"),
            completed_at=job.get("completed_at"),
            parent_started_at=run.get("started_at"),
            generated_at=generated_at,
            completed=job.get("state") == "completed",
            completed_source="completed_at",
        )
        if job_clock_skew:
            warnings.append(
                _warning(
                    "job_clock_skew",
                    "jobs",
                    "A job timestamp regression prevented one or more timing calculations.",
                    job_id=job_id,
                )
            )

        raw_steps = job.get("steps") or []
        if not isinstance(raw_steps, list):
            raw_steps = []
        selected_steps = raw_steps[:remaining_steps]
        steps_truncated = len(selected_steps) < len(raw_steps)
        remaining_steps -= len(selected_steps)
        step_diagnostics = []
        for step in selected_steps:
            if not isinstance(step, dict):
                continue
            step_timing, step_clock_skew = _timing(
                created_at=None,
                started_at=step.get("started_at"),
                completed_at=step.get("completed_at"),
                parent_started_at=run.get("started_at"),
                generated_at=generated_at,
                completed=step.get("status") == "completed",
                completed_source="completed_at",
            )
            if step_clock_skew:
                warnings.append(
                    _warning(
                        "step_clock_skew",
                        "steps",
                        "A step timestamp regression prevented one or more timing calculations.",
                        job_id=job_id,
                    )
                )
            step_diagnostics.append({"step": step, "timing": step_timing})
        if steps_truncated:
            warnings.append(
                _warning(
                    "steps_truncated",
                    "steps",
                    "Steps were truncated to the diagnostic response bounds.",
                    job_id=job_id,
                )
            )

        job_without_steps = {key: value for key, value in job.items() if key != "steps"}
        job_diagnostics.append(
            {
                "job": job_without_steps,
                "timing": job_timing,
                "steps": step_diagnostics,
                "steps_truncated": steps_truncated,
                "metrics": metrics_by_job[job_id],
                "failed_logs": logs_by_job[job_id],
            }
        )

    return {
        "generated_at": _isoformat(generated_at),
        "complete": not warnings,
        "bounds": {
            "max_jobs": MAX_DIAGNOSTIC_JOBS,
            "max_steps": MAX_DIAGNOSTIC_STEPS,
            "max_log_jobs": MAX_LOG_JOBS,
            "max_log_lines_per_job": MAX_LOG_LINES_PER_JOB,
            "max_log_bytes_per_job": MAX_LOG_BYTES_PER_JOB,
            "baseline_days": BASELINE_DAYS,
        },
        "run": run,
        "timing": run_timing,
        "jobs": job_diagnostics,
        "jobs_truncated": jobs_truncated,
        "baseline": baseline,
        "warnings": warnings,
    }
