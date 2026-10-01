"""Human-readable rendering for bounded workflow-run diagnostics."""

from avrea_cli.display import DIM_FG
from avrea_cli.display import format_duration
from avrea_cli.display import status_indicator
from avrea_cli.log_display import format_log_line
from avrea_cli.metrics_display import format_bytes
from avrea_cli.metrics_display import format_rate
from avrea_cli.output import format_timestamp
from typing import Any
import click

_METRIC_LABELS = {
    "cpu": "CPU",
    "memory": "Memory",
    "disk-io": "Disk I/O",
    "network": "Network",
}


def _timing_parts(timing: dict[str, Any]) -> list[str]:
    parts = []
    if timing.get("queue_seconds") is not None:
        parts.append(f"queued {format_duration(timing['queue_seconds'])}")
    if timing.get("start_offset_seconds") is not None:
        parts.append(f"started +{format_duration(timing['start_offset_seconds'])}")
    if timing.get("execution_seconds") is not None:
        parts.append(f"ran {format_duration(timing['execution_seconds'])}")
    return parts


def _format_metric_value(value: float | None, unit: str | None) -> str:
    if value is None:
        return "-"
    if unit == "ratio":
        return f"{value * 100:.1f}%"
    if unit == "bytes":
        return format_bytes(value)
    if unit in {"bytes/sec", "operations/sec"}:
        return format_rate(value, unit)
    return f"{value:.2f} {unit or ''}".rstrip()


def _render_metric(source: str, summary: dict[str, Any]) -> str:
    unit = summary.get("unit")
    parts = [
        f"p95 {_format_metric_value(summary.get('p95'), unit)}",
        f"peak {_format_metric_value(summary.get('peak'), unit)}",
    ]
    if summary.get("total") is not None:
        parts.append(f"total {_format_metric_value(summary['total'], summary.get('total_unit'))}")
    parts.append(f"{summary.get('sample_count', 0)} samples")
    return f"    {_METRIC_LABELS.get(source, source)}: {', '.join(parts)}"


def _render_baseline(report: dict[str, Any]) -> str | None:
    baseline = report.get("baseline") or {}
    if baseline.get("status") != "complete":
        return None

    sample_count = baseline.get("sample_count", 0)
    sample_label = "successful run" if sample_count == 1 else "successful runs"
    parts = [
        f"{format_timestamp(baseline.get('window_start'))} to {format_timestamp(baseline.get('window_end'))}",
        f"{sample_count} {sample_label}",
    ]
    median = baseline.get("median_duration_seconds")
    p95 = baseline.get("p95_duration_seconds")
    if median is not None:
        parts.append(f"median {format_duration(median)}")
    if p95 is not None:
        parts.append(f"p95 {format_duration(p95)}")

    duration = (report.get("timing") or {}).get("execution_seconds")
    if duration is not None and median not in {None, 0}:
        delta = (duration - median) / median * 100
        parts.append(f"this run {delta:+.1f}% vs median")
    return " · ".join(parts)


def render_run_diagnostics(report: dict[str, Any]) -> str:
    """Render the diagnostic evidence in failure-first order."""
    run = report.get("run") or {}
    status = run.get("status", "unknown")
    conclusion = run.get("conclusion")
    indicator = status_indicator(status, conclusion)
    title = run.get("display_title") or run.get("run_id") or "Workflow run"
    run_number = run.get("run_number")
    run_attempt = run.get("run_attempt", 1)
    number = f" #{run_number}" if run_number is not None else ""

    lines = [
        f"{indicator} {click.style(f'{title}{number} (attempt {run_attempt})', bold=True)}",
        f"  {run.get('run_id', '-')}",
    ]

    run_timing = _timing_parts(report.get("timing") or {})
    if run_timing:
        lines.append(f"  Run timing: {' · '.join(run_timing)}")
    baseline = _render_baseline(report)
    if baseline:
        lines.append(f"  Baseline: {baseline}")

    jobs = report.get("jobs") or []
    if jobs:
        lines.extend(["", click.style("JOBS", bold=True, fg="bright_white")])
    for entry in jobs:
        job = entry.get("job") or {}
        timing = _timing_parts(entry.get("timing") or {})
        suffix = f" · {' · '.join(timing)}" if timing else ""
        lines.append(
            f"  {status_indicator(job.get('state', 'unknown'), job.get('conclusion'))} "
            f"{click.style(job.get('job_name', '?'), bold=True)}{suffix}"
        )

        failed_steps = [
            step
            for step in entry.get("steps") or []
            if (step.get("step") or {}).get("conclusion") not in {None, "success", "skipped"}
        ]
        for step_entry in failed_steps:
            step = step_entry.get("step") or {}
            step_timing = _timing_parts(step_entry.get("timing") or {})
            timing_suffix = f" · {' · '.join(step_timing)}" if step_timing else ""
            lines.append(f"    Failed step: {step.get('name', '?')}{timing_suffix}")

        metrics = entry.get("metrics") or {}
        for source, summary in (metrics.get("sources") or {}).items():
            if summary.get("sample_count", 0):
                lines.append(_render_metric(source, summary))

        failed_logs = entry.get("failed_logs") or {}
        log_lines = failed_logs.get("lines") or []
        if log_lines:
            truncated = " (truncated)" if failed_logs.get("truncated") else ""
            lines.append(f"    {click.style('LOG TAIL', bold=True)}{truncated}")
            for log_entry in log_lines:
                rendered = format_log_line(log_entry)
                if rendered is not None:
                    lines.append(f"      {rendered}")

    warnings = report.get("warnings") or []
    if warnings:
        lines.extend(["", click.style("INCOMPLETE DATA", fg="yellow", bold=True)])
        for warning in warnings:
            job_suffix = f" ({warning['job_id']})" if warning.get("job_id") else ""
            lines.append(
                click.style(
                    f"  {warning.get('code', 'warning')}{job_suffix}: {warning.get('message', '')}",
                    fg="yellow",
                )
            )

    if not report.get("complete", False) and not warnings:
        lines.extend(["", click.style("Diagnostic data is incomplete.", fg="yellow")])

    bounds = report.get("bounds") or {}
    if bounds:
        lines.extend(
            [
                "",
                click.style(
                    "Response bounds: "
                    f"{bounds.get('max_jobs', '?')} jobs, "
                    f"{bounds.get('max_steps', '?')} steps, "
                    f"{bounds.get('max_log_lines_per_job', '?')} log lines per failed job.",
                    fg=DIM_FG,
                ),
            ]
        )

    return "\n".join(lines)
