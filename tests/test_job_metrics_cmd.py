"""Command-level smoke tests for `avr job metrics`."""

from avrea_cli.main import cli
from click.testing import CliRunner
import httpx
import json
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


def _make_get(routes):
    """Build a public_get fake that dispatches by path. Each route is path → response dict.

    Routes are matched longest-prefix-first so that more specific paths
    (e.g. ``/orgs/X/jobs/job-1/metrics/cpu``) win over a shorter prefix
    that would otherwise shadow them (``/orgs/X/jobs/job-1``)."""
    sorted_routes = sorted(routes.items(), key=lambda kv: len(kv[0]), reverse=True)

    def fake_get(self, path, params=None, **kw):
        for prefix, response in sorted_routes:
            if path == prefix or path.startswith(prefix + "?"):
                return response
            if path.startswith(prefix):
                return response
        raise AssertionError(f"unexpected GET {path}")

    return fake_get


JOB_RESPONSE = {
    "data": {
        "job_id": "job-1",
        "job_name": "Build",
        "job_labels": ["avrea-ubuntu-latest-4-vcpu"],  # → 16 GB RAM
    }
}

CPU_RESPONSE = {
    "unit": "ratio",
    "series": [
        {"labels": {"cpu": "cpu0", "state": "user"}, "values": [(100, 0.30), (110, 0.50)]},
        {"labels": {"cpu": "cpu0", "state": "system"}, "values": [(100, 0.05), (110, 0.10)]},
        {"labels": {"cpu": "cpu0", "state": "idle"}, "values": [(100, 0.65), (110, 0.40)]},
    ],
}

MEMORY_RESPONSE = {
    "unit": "bytes",
    "series": [{"labels": {"state": "used"}, "values": [(100, 4 * 1024**3)]}],
}


class TestJobMetricsCmd:
    def test_default_renders_cpu_and_memory(self, runner, monkeypatch):
        routes = {
            "/orgs/org-default/jobs/job-1/metrics/cpu": CPU_RESPONSE,
            "/orgs/org-default/jobs/job-1/metrics/memory": MEMORY_RESPONSE,
            "/orgs/org-default/jobs/job-1": JOB_RESPONSE,
        }
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _make_get(routes))
        result = runner.invoke(cli, ["job", "metrics", "job-1"])
        assert result.exit_code == 0, result.output
        assert "cpu" in result.output
        assert "memory" in result.output
        # cpu last: 1 - idle(0.40) = 0.60 → 60%
        assert "60%" in result.output
        # The header surfaces the VM spec so users can read the gauges in context.
        assert "4 vCPU, 16 GB RAM" in result.output
        assert "avrea-ubuntu-latest-4-vcpu" in result.output
        # memory uses VM RAM (16 GB) as denom
        assert "4.0 GB / 16.0 GB" in result.output

    def test_explicit_sources(self, runner, monkeypatch):
        routes = {
            "/orgs/org-default/jobs/job-1/metrics/cpu": CPU_RESPONSE,
            "/orgs/org-default/jobs/job-1": JOB_RESPONSE,
        }
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _make_get(routes))
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--source", "cpu"])
        assert result.exit_code == 0, result.output
        assert "cpu" in result.output
        # memory line should NOT appear
        assert "memory" not in result.output

    def test_invalid_source_rejected_by_click(self, runner):
        # No API mock — Click's choice validator should reject before any HTTP.
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--source", "bogus"])
        assert result.exit_code != 0
        assert "bogus" in result.output

    def test_json_output_passes_through(self, runner, monkeypatch):
        routes = {
            "/orgs/org-default/jobs/job-1/metrics/cpu": CPU_RESPONSE,
            "/orgs/org-default/jobs/job-1/metrics/memory": MEMORY_RESPONSE,
            "/orgs/org-default/jobs/job-1": JOB_RESPONSE,
        }
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _make_get(routes))
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--json"])
        assert result.exit_code == 0, result.output
        parsed = json.loads(result.output)
        assert set(parsed.keys()) == {"cpu", "memory"}
        assert parsed["cpu"]["unit"] == "ratio"


# ----------------------------------------------------------------------------
# Status header + --live fall-back on finished jobs
# ----------------------------------------------------------------------------


def _job_response(*, state: str, conclusion: str | None = None, completed_at: str | None = None) -> dict:
    return {
        "data": {
            "job_id": "job-1",
            "job_name": "Build",
            "job_labels": ["avrea-ubuntu-latest-4-vcpu"],
            "state": state,
            "conclusion": conclusion,
            "duration_seconds": 187,
            "completed_at": completed_at,
        }
    }


class TestJobMetricsStatus:
    def test_header_shows_completed_status(self, runner, monkeypatch):
        routes = {
            "/orgs/org-default/jobs/job-1/metrics/cpu": CPU_RESPONSE,
            "/orgs/org-default/jobs/job-1/metrics/memory": MEMORY_RESPONSE,
            "/orgs/org-default/jobs/job-1": _job_response(
                state="completed", conclusion="success", completed_at="2026-04-28T10:00:00Z"
            ),
        }
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _make_get(routes))
        result = runner.invoke(cli, ["job", "metrics", "job-1"])
        assert result.exit_code == 0, result.output
        assert "Status" in result.output
        assert "success" in result.output
        # Duration shows; "finished N ago" appended when completed_at is present.
        assert "3m 07s" in result.output
        assert "finished" in result.output

    def test_header_shows_running_status_without_finished_at(self, runner, monkeypatch):
        routes = {
            "/orgs/org-default/jobs/job-1/metrics/cpu": CPU_RESPONSE,
            "/orgs/org-default/jobs/job-1/metrics/memory": MEMORY_RESPONSE,
            "/orgs/org-default/jobs/job-1": _job_response(state="in_progress"),
        }
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _make_get(routes))
        result = runner.invoke(cli, ["job", "metrics", "job-1"])
        assert result.exit_code == 0, result.output
        assert "Status" in result.output
        assert "in_progress" in result.output
        # No "finished N ago" suffix while the job is still running.
        assert "finished" not in result.output

    def test_watch_completed_job_uses_stream(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            _make_get(
                {
                    "/orgs/org-default/jobs/job-1": _job_response(state="completed", conclusion="success"),
                }
            ),
        )
        monkeypatch.setattr("avrea_cli.commands.job.fetch_logs_after", lambda *a, **kw: ([], None))
        monkeypatch.setattr(
            "avrea_cli.commands.job.metrics_frames",
            lambda *a: iter(
                [
                    {"signal": "metrics", "type": "bundle", "metrics": {"cpu": CPU_RESPONSE}},
                    {"signal": "metrics", "type": "complete"},
                ]
            ),
        )
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--watch"])
        assert result.exit_code == 0, result.output
        assert "60%" in result.output


class TestJobMetricsStream:
    def setup_stream(self, monkeypatch, frames):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            _make_get(
                {
                    "/orgs/org-default/jobs/job-1": _job_response(state="in_progress"),
                }
            ),
        )
        monkeypatch.setattr("avrea_cli.commands.job.metrics_frames", lambda *a: iter(frames))
        monkeypatch.setattr("avrea_cli.commands.job.fetch_logs_after", lambda *a, **kw: ([], None))

    def test_json_replaces_snapshots_and_filters_sources(self, runner, monkeypatch):
        self.setup_stream(
            monkeypatch,
            [
                {"type": "bundle", "metrics": {"cpu": CPU_RESPONSE, "memory": MEMORY_RESPONSE}},
                {"type": "bundle", "metrics": {"cpu": {"series": []}}},
                {"type": "complete"},
            ],
        )
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--watch", "--json", "--source", "cpu"])
        assert result.exit_code == 0, result.output
        assert [json.loads(line) for line in result.stdout.splitlines()] == [
            {"cpu": json.loads(json.dumps(CPU_RESPONSE))},
            {"cpu": {"series": []}},
        ]

    def test_log_failure_surfaces_with_streamed_metrics(self, runner, monkeypatch):
        self.setup_stream(monkeypatch, [{"type": "bundle", "metrics": {"cpu": CPU_RESPONSE}}, {"type": "complete"}])
        monkeypatch.setattr(
            "avrea_cli.commands.job.fetch_logs_after",
            lambda *a, **kw: (_ for _ in ()).throw(httpx.TimeoutException("slow")),
        )
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--watch"])
        assert result.exit_code == 0, result.output
        assert "log fetch failed: TimeoutException" in result.output
        assert "60%" in result.output

    def test_completion_shows_logs_written_since_last_tick(self, runner, monkeypatch):
        self.setup_stream(monkeypatch, [{"type": "bundle", "metrics": {"cpu": CPU_RESPONSE}}, {"type": "complete"}])
        batches = iter([([], None), ([{"line_number": 1, "message": "final error", "level": "info"}], 1)])
        monkeypatch.setattr("avrea_cli.commands.job.fetch_logs_after", lambda *a, **kw: next(batches))
        monkeypatch.setattr("avrea_cli.commands.job.format_log_line", lambda entry: entry["message"])
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--watch"])
        assert result.exit_code == 0, result.output
        assert "final error" in result.output

    def test_empty_completion_reports_no_metrics(self, runner, monkeypatch):
        self.setup_stream(monkeypatch, [{"type": "complete"}])
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--watch"])
        assert result.exit_code == 0, result.output
        assert "No metrics available" in result.output

    def test_stream_path_encodes_job_id(self, runner, monkeypatch):
        paths = []
        self.setup_stream(monkeypatch, [])
        monkeypatch.setattr(
            "avrea_cli.commands.job.metrics_frames",
            lambda config, path: paths.append(path) or iter([{"type": "complete"}]),
        )
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda *a, **kw: _job_response(state="in_progress"),
        )
        result = runner.invoke(cli, ["job", "metrics", "../x?y", "--watch"])
        assert result.exit_code == 0, result.output
        assert paths == ["/vms/..%2Fx%3Fy/observability/stream"]

    def test_interrupt_closes_watch(self, runner, monkeypatch):
        def frames(*args):
            yield {"type": "bundle", "metrics": {"cpu": CPU_RESPONSE}}
            raise KeyboardInterrupt

        self.setup_stream(monkeypatch, [])
        monkeypatch.setattr("avrea_cli.commands.job.metrics_frames", frames)
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--watch", "--json"])
        assert result.exit_code == 130

    def test_explicit_window_refuses_watch(self, runner):
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--watch", "--start", "100"])
        assert result.exit_code == 2
        assert "single-shot" in result.output

    @pytest.mark.parametrize("detail,pending", [("No execution found for job", True), ("Job not found", False)])
    def test_logs_wait_for_vm_but_preserve_denial(self, runner, monkeypatch, detail, pending):
        self.setup_stream(monkeypatch, [{"type": "bundle", "metrics": {"cpu": CPU_RESPONSE}}, {"type": "complete"}])
        request = httpx.Request("GET", "https://api.example/logs")
        response = httpx.Response(404, json={"detail": detail}, request=request)
        calls = []

        def fetch(*args, **kwargs):
            calls.append(kwargs["after_line"])
            if len(calls) == 1:
                raise httpx.HTTPStatusError("404", request=request, response=response)
            return ([{"message": "VM started", "line_number": 1}], 1)

        monkeypatch.setattr("avrea_cli.commands.job.fetch_logs_after", fetch)
        monkeypatch.setattr("avrea_cli.commands.job.format_log_line", lambda entry: entry["message"])
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--watch"])
        if pending:
            assert result.exit_code == 0, result.output
            assert calls == [None, None]
            assert "VM started" in result.output
            assert "log fetch failed" not in result.output
        else:
            assert result.exit_code != 0
            assert calls == [None]
            assert "Job not found" in result.output
