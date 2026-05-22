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

    def test_live_falls_back_to_static_for_completed_job(self, runner, monkeypatch):
        """`--live` on a finished job has nothing to watch — render the static
        post-mortem and exit instead of looping forever."""
        routes = {
            "/orgs/org-default/jobs/job-1/metrics/cpu": CPU_RESPONSE,
            "/orgs/org-default/jobs/job-1/metrics/memory": MEMORY_RESPONSE,
            "/orgs/org-default/jobs/job-1": _job_response(
                state="completed", conclusion="success", completed_at="2026-04-28T10:00:00Z"
            ),
        }
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _make_get(routes))
        # If the live loop ever fires, time.sleep would be called — fail loudly.
        monkeypatch.setattr(
            "avrea_cli.commands.job.time.sleep",
            lambda _: pytest.fail("--live should not enter the polling loop on a finished job"),
        )
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--watch"])
        assert result.exit_code == 0, result.output
        assert "already finished" in result.output
        # Static gauge output (avg/peak/last) should be present.
        assert "avg " in result.output
        assert "peak " in result.output


class TestJobMetricsLiveResilience:
    """``--live`` polls on a tight loop; transient httpx errors must not kill
    the watcher. We exit the loop after one iteration via KeyboardInterrupt
    so we can assert the rendered frame's footer carries the error label."""

    def _running_job(self):
        return _job_response(state="in_progress")

    def test_log_fetch_failure_surfaces_in_footer(self, runner, monkeypatch):
        routes = {
            "/orgs/org-default/jobs/job-1/metrics/cpu": CPU_RESPONSE,
            "/orgs/org-default/jobs/job-1/metrics/memory": MEMORY_RESPONSE,
            "/orgs/org-default/jobs/job-1": self._running_job(),
        }
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _make_get(routes))
        # Make the log search blow up.
        monkeypatch.setattr(
            "avrea_cli.commands.job.fetch_logs_after",
            lambda *a, **kw: (_ for _ in ()).throw(httpx.TimeoutException("slow")),
        )
        # Exit the loop after the first frame draws.
        monkeypatch.setattr(
            "avrea_cli.commands.job.time.sleep",
            lambda _: (_ for _ in ()).throw(KeyboardInterrupt()),
        )
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--watch"])
        assert result.exit_code == 0, result.output
        assert "log fetch failed: TimeoutException" in result.output

    def test_metrics_fetch_failure_surfaces_in_footer(self, runner, monkeypatch):
        # The metrics fetch raises but the watcher must keep running and show
        # the error in the footer instead of dying with a traceback.
        def fake_get(self, path, params=None, **kw):
            if path == "/orgs/org-default/jobs/job-1":
                return _job_response(state="in_progress")
            if "/metrics/" in path:
                req = httpx.Request("GET", f"https://api.example{path}")
                raise httpx.HTTPStatusError("503", request=req, response=httpx.Response(503, request=req))
            raise AssertionError(f"unexpected GET {path}")

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)
        monkeypatch.setattr("avrea_cli.commands.job.fetch_logs_after", lambda *a, **kw: ([], None))
        monkeypatch.setattr(
            "avrea_cli.commands.job.time.sleep",
            lambda _: (_ for _ in ()).throw(KeyboardInterrupt()),
        )
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--watch"])
        assert result.exit_code == 0, result.output
        # 5xx is treated as soft (transient backend hiccup) — surface in
        # the footer with the status code, keep the watcher alive.
        assert "metrics fetch failed: HTTP 503" in result.output

    def test_metrics_fetch_4xx_exits_rather_than_loops_on_stale_frame(self, runner, monkeypatch):
        """A 401/403/410 on metrics is terminal — auth lost or job deleted.
        Looping forever rendering the previous metrics frame would lie about
        the job's current state. Exit non-zero via handle_http_error."""

        def fake_get(self, path, params=None, **kw):
            if path == "/orgs/org-default/jobs/job-1":
                return _job_response(state="in_progress")
            if "/metrics/" in path:
                req = httpx.Request("GET", f"https://api.example{path}")
                raise httpx.HTTPStatusError("401", request=req, response=httpx.Response(401, request=req))
            raise AssertionError(f"unexpected GET {path}")

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)
        monkeypatch.setattr("avrea_cli.commands.job.fetch_logs_after", lambda *a, **kw: ([], None))
        result = runner.invoke(cli, ["job", "metrics", "job-1", "--watch"])
        assert result.exit_code != 0, result.output
        assert "metrics fetch failed: HTTP" not in result.output  # never reached the soft-fail branch
