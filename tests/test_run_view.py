"""Unit tests for run view and run watch commands."""

from avrea_cli.commands.run import _active_job_name
from avrea_cli.commands.run import _emit_ndjson_event
from avrea_cli.commands.run import _watch_title
from avrea_cli.main import cli
from io import StringIO
import click
import httpx
import json
import pytest

SAMPLE_RUN = {
    "data": {
        "run_id": "run-abc123",
        "display_title": "Fix CI build",
        "status": "completed",
        "conclusion": "success",
        "head_branch": "main",
        "head_sha": "abc123def456",
        "event": "push",
        "run_number": 42,
        "run_attempt": 1,
        "duration_seconds": 154,
        "created_at": "2025-06-01T12:00:00Z",
        "updated_at": "2025-06-01T12:02:34Z",
        "workflow_id": 789,
        "workflow": {"name": "CI"},
        "repository": {"full_name": "org/repo"},
        "triggering_actor": {"login": "octocat"},
        "jobs": [
            {
                "job_id": "job-111",
                "job_name": "Build",
                "state": "completed",
                "conclusion": "success",
                "duration_seconds": 12,
                "repository_id": "rep-xyz",
                "started_at": "2025-06-01T12:00:01Z",
                "steps": [
                    {
                        "name": "Set up job",
                        "status": "completed",
                        "conclusion": "success",
                        "started_at": "2025-06-01T12:00:01Z",
                        "completed_at": "2025-06-01T12:00:03Z",
                    },
                    {
                        "name": "Build",
                        "status": "completed",
                        "conclusion": "success",
                        "started_at": "2025-06-01T12:00:03Z",
                        "completed_at": "2025-06-01T12:00:12Z",
                    },
                ],
            },
            {
                "job_id": "job-222",
                "job_name": "Deploy",
                "state": "completed",
                "conclusion": "failure",
                "duration_seconds": 45,
                "repository_id": "rep-xyz",
                "started_at": "2025-06-01T12:00:13Z",
                "steps": [
                    {
                        "name": "Deploy",
                        "status": "completed",
                        "conclusion": "failure",
                        "started_at": "2025-06-01T12:00:13Z",
                        "completed_at": "2025-06-01T12:00:58Z",
                    },
                ],
            },
        ],
    }
}

SAMPLE_RUNS_LIST = {
    "data": [
        {
            "run_id": "run-abc123",
            "display_title": "Fix CI build",
            "status": "completed",
            "conclusion": "success",
            "head_branch": "main",
            "run_number": 42,
            "duration_seconds": 154,
            "created_at": "2025-06-01T12:00:00Z",
            "repository": {"full_name": "org/repo"},
        }
    ],
    "pagination": {"next_cursor": None},
}


class TestRunView:
    def test_no_run_id_lists_recent(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_RUNS_LIST,
        )
        result = runner.invoke(cli, ["run", "view"])
        assert result.exit_code == 0
        assert "Fix CI build" in result.output
        assert "avr run view <run-id>" in result.output

    def test_displays_run_metadata(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_RUN,
        )
        result = runner.invoke(cli, ["run", "view", "run-abc123"])
        assert result.exit_code == 0
        assert "Fix CI build" in result.output
        assert "push" in result.output
        assert "octocat" in result.output
        assert "#42" in result.output

    def test_displays_jobs(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_RUN,
        )
        result = runner.invoke(cli, ["run", "view", "run-abc123"])
        assert "Build" in result.output
        assert "Deploy" in result.output
        assert "JOBS" in result.output

    def test_steps_flag_expands_each_job(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_RUN,
        )
        result = runner.invoke(cli, ["run", "view", "run-abc123", "--steps"])
        assert result.exit_code == 0
        assert "Set up job" in result.output

    def test_json_output_all_fields(self, runner, monkeypatch):
        """`--json '*'` returns all known fields as a single object (not array)."""
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_RUN,
        )
        result = runner.invoke(cli, ["run", "view", "run-abc123", "--json", "*"])
        assert result.exit_code == 0, result.output
        parsed = json.loads(result.output)
        # Output is a JSON object, not array — view commands return one record.
        assert isinstance(parsed, dict)
        assert parsed["run_id"] == "run-abc123"

    def test_json_output_field_selection(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_RUN,
        )
        result = runner.invoke(cli, ["run", "view", "run-abc123", "--json", "run_id,conclusion"])
        assert result.exit_code == 0, result.output
        parsed = json.loads(result.output)
        assert set(parsed.keys()) == {"run_id", "conclusion"}

    def test_jq_requires_json(self, runner):
        result = runner.invoke(cli, ["run", "view", "run-abc123", "-q", ".run_id"])
        assert result.exit_code != 0
        assert "--jq requires --json" in result.output

    def test_requires_auth(self, runner, monkeypatch):
        monkeypatch.delenv("AVR_TOKEN", raising=False)
        result = runner.invoke(cli, ["run", "view", "run-abc123"])
        assert result.exit_code != 0

    def test_job_filter_keeps_only_matching_jobs(self, runner, monkeypatch):
        """--job <name> narrows the JOBS section to the matching job(s)."""
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_RUN,
        )
        result = runner.invoke(cli, ["run", "view", "run-abc123", "--job", "Build"])
        assert result.exit_code == 0
        assert "Build" in result.output
        assert "Deploy" not in result.output  # filtered out

    def test_job_filter_no_match_warns(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: SAMPLE_RUN,
        )
        result = runner.invoke(cli, ["run", "view", "run-abc123", "--job", "nosuch"])
        assert result.exit_code == 0
        assert "No job matching" in result.output


class TestRunWatch:
    def test_no_run_id_no_active_runs(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: {"data": [], "pagination": {"next_cursor": None}},
        )
        result = runner.invoke(cli, ["run", "watch"])
        assert result.exit_code == 0
        assert "No in-progress" in result.output

    def test_requires_auth(self, runner, monkeypatch):
        monkeypatch.delenv("AVR_TOKEN", raising=False)
        result = runner.invoke(cli, ["run", "watch", "run-abc123"])
        assert result.exit_code != 0

    def test_explicit_repo_not_found_propagates(self, runner, monkeypatch):
        """A typo in --repo (no run_id positional) must fail loudly. Earlier
        versions swallowed the resolver error and silently fell through to
        org-wide auto-select, picking up a run from a different repo —
        confusingly close to working but watching the wrong thing."""

        def boom(client, config, org_id, names):
            raise click.ClickException(f"Repository '{names!r}' not found")

        monkeypatch.setattr("avrea_cli.commands.run.resolve_repos_or_detect", boom)
        result = runner.invoke(cli, ["run", "watch", "--repo", "avrea-com/typo"])
        assert result.exit_code != 0
        assert "not found" in result.output


class TestRunWatchJson:
    """NDJSON event stream: one JSON object per line, transitions only."""

    def _stateful_get(self, snapshots):
        """Return a public_get fake that returns each snapshot in turn."""
        idx = {"i": 0}

        def fake_get(self, path, **kw):
            i = min(idx["i"], len(snapshots) - 1)
            idx["i"] += 1
            return snapshots[i]

        return fake_get

    def test_emits_events_in_order_and_exits_clean(self, runner, monkeypatch):
        # Three snapshots: queued → in_progress → completed/success.
        snapshots = [
            {
                "data": {
                    "run_id": "run-x",
                    "status": "in_progress",
                    "jobs": [
                        {"job_id": "job-1", "platform_job_id": 100, "job_name": "Build", "state": "queued"},
                    ],
                }
            },
            {
                "data": {
                    "run_id": "run-x",
                    "status": "in_progress",
                    "jobs": [
                        {"job_id": "job-1", "platform_job_id": 100, "job_name": "Build", "state": "in_progress"},
                    ],
                }
            },
            {
                "data": {
                    "run_id": "run-x",
                    "status": "completed",
                    "conclusion": "success",
                    "duration_seconds": 30,
                    "jobs": [
                        {
                            "job_id": "job-1",
                            "platform_job_id": 100,
                            "job_name": "Build",
                            "state": "completed",
                            "conclusion": "success",
                        },
                    ],
                }
            },
        ]
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", self._stateful_get(snapshots))
        monkeypatch.setattr("time.sleep", lambda _: None)

        result = runner.invoke(cli, ["run", "watch", "run-x", "--ndjson"])
        assert result.exit_code == 0, result.output

        events = [json.loads(line) for line in result.output.strip().splitlines() if line.startswith("{")]
        kinds = [e["event"] for e in events]
        assert kinds == ["job_started", "job_completed", "run_completed"]
        # Identifier fields land in the events
        assert events[0]["avrea_job_id"] == "job-1"
        assert events[0]["platform_job_id"] == 100
        assert events[2]["conclusion"] == "success"

    def test_exit_status_propagates_failure(self, runner, monkeypatch):
        snapshot = {
            "data": {
                "run_id": "run-x",
                "status": "completed",
                "conclusion": "failure",
                "jobs": [],
            }
        }
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, **kw: snapshot)
        monkeypatch.setattr("time.sleep", lambda _: None)
        result = runner.invoke(cli, ["run", "watch", "run-x", "--ndjson", "--exit-status"])
        assert result.exit_code == 1
        events = [json.loads(line) for line in result.output.strip().splitlines() if line.startswith("{")]
        assert events[-1]["event"] == "run_completed"
        assert events[-1]["conclusion"] == "failure"

    def test_terminates_on_4xx_instead_of_looping(self, runner, monkeypatch):
        """A 4xx HTTP error mid-watch (auth lost, run deleted) must be
        terminal — looping forever would leave the consumer hanging without
        any useful event stream."""

        def raise_404(self, path, **kw):
            req = httpx.Request("GET", f"https://api.example{path}")
            raise httpx.HTTPStatusError("404", request=req, response=httpx.Response(404, request=req))

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", raise_404)
        monkeypatch.setattr("time.sleep", lambda _: None)
        result = runner.invoke(cli, ["run", "watch", "run-x", "--ndjson"])
        # handle_http_error exits 1 for non-401 4xx
        assert result.exit_code == 1
        # Must not have looped indefinitely emitting "Error fetching run" lines.
        assert result.output.count("Error fetching run") == 0

    def test_5xx_keeps_retrying(self, runner, monkeypatch):
        """5xx is transient (e.g. brief gateway hiccup); the loop should
        retry and recover when the next snapshot lands."""
        calls = {"i": 0}

        def maybe_500(self, path, **kw):
            calls["i"] += 1
            if calls["i"] == 1:
                req = httpx.Request("GET", f"https://api.example{path}")
                raise httpx.HTTPStatusError("502", request=req, response=httpx.Response(502, request=req))
            return {"data": {"run_id": "run-x", "status": "completed", "conclusion": "success", "jobs": []}}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", maybe_500)
        monkeypatch.setattr("time.sleep", lambda _: None)
        result = runner.invoke(cli, ["run", "watch", "run-x", "--ndjson"])
        assert result.exit_code == 0, result.output
        # Saw the transient error AND eventually completed.
        assert "Error fetching run" in result.output
        events = [json.loads(line) for line in result.output.strip().splitlines() if line.startswith("{")]
        assert events[-1]["event"] == "run_completed"
        assert events[-1]["conclusion"] == "success"

    def test_emit_systemexit_propagates_through_runner(self, runner, monkeypatch):
        """End-to-end: when ``_emit_ndjson_event`` raises ``SystemExit(0)``
        (its real BrokenPipe handler does), the watch loop must surface
        cleanly as exit-code 0 — pins the contract that a future change
        which "just returns" would break the real ``| head -n1`` path."""
        snapshot = {
            "data": {
                "run_id": "run-x",
                "status": "completed",
                "conclusion": "success",
                "jobs": [],
            }
        }
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, **kw: snapshot)
        monkeypatch.setattr("time.sleep", lambda _: None)

        def _exit_clean(_event):
            raise SystemExit(0)

        monkeypatch.setattr("avrea_cli.commands.run._emit_ndjson_event", _exit_clean)
        result = runner.invoke(cli, ["run", "watch", "run-x", "--ndjson"])
        assert result.exit_code == 0, result.output


class TestNdjsonBrokenPipe:
    """``--ndjson`` events are designed to feed into ``head``, ``jq``, etc.
    A consumer closing the pipe (`avr run watch ... --ndjson | head -n1`)
    must result in a clean exit instead of a stack trace."""

    def _broken_stdout(self, *, fail_on: str):
        """Build a fake stdout whose write/flush raises BrokenPipeError on the
        configured method ('write' or 'flush'). Replacing sys.stdout wholesale
        is more reliable than patching attributes on the captured pytest
        stream (pytest re-wraps its capture in ways that defeat setattr)."""

        class _FakeStdout(StringIO):
            def write(self, s):
                if fail_on == "write":
                    raise BrokenPipeError("downstream closed")
                return super().write(s)

            def flush(self):
                if fail_on == "flush":
                    raise BrokenPipeError("downstream closed")

            def close(self):
                pass

        return _FakeStdout()

    def test_broken_pipe_during_write_exits_cleanly(self, monkeypatch):
        monkeypatch.setattr("sys.stdout", self._broken_stdout(fail_on="write"))
        with pytest.raises(SystemExit) as excinfo:
            _emit_ndjson_event({"event": "job_started"})
        assert excinfo.value.code == 0

    def test_broken_pipe_during_flush_exits_cleanly(self, monkeypatch):
        monkeypatch.setattr("sys.stdout", self._broken_stdout(fail_on="flush"))
        with pytest.raises(SystemExit) as excinfo:
            _emit_ndjson_event({"event": "job_started"})
        assert excinfo.value.code == 0


class TestActiveJobName:
    """The terminal title surfaces the running job name (``tools: sleep``)
    rather than the opaque run id. Picking the *most informative* job is
    a small ranking exercise — the helper centralizes the logic so tests
    can pin it without monkey-patching the watch loop."""

    def test_in_progress_wins_over_queued(self):
        jobs = [
            {"job_name": "lint", "state": "queued"},
            {"job_name": "build", "state": "in_progress"},
        ]
        assert _active_job_name(jobs) == "build"

    def test_oldest_in_progress_first(self):
        # When multiple jobs are running, surface the one that started
        # earliest — it's the one that's been "live" longest, which
        # matches what users intuitively expect to see.
        jobs = [
            {"job_name": "later", "state": "in_progress", "started_at": "2026-05-01T12:05:00Z"},
            {"job_name": "earlier", "state": "in_progress", "started_at": "2026-05-01T12:00:00Z"},
        ]
        assert _active_job_name(jobs) == "earlier"

    def test_falls_back_to_queued_when_nothing_running(self):
        jobs = [
            {"job_name": "lint", "state": "queued"},
            {"job_name": "build", "state": "completed", "conclusion": "success"},
        ]
        assert _active_job_name(jobs) == "lint"

    def test_returns_none_when_no_active_or_queued(self):
        # Caller falls back to a progress fraction or run-state label.
        jobs = [{"job_name": "build", "state": "completed", "conclusion": "success"}]
        assert _active_job_name(jobs) is None

    def test_empty_input(self):
        assert _active_job_name([]) is None


class TestWatchTitle:
    """Title format: ``avr ▸ <workflow>: <stage>`` — the ``avr ▸`` prefix
    marks the tab as belonging to this CLI; workflow name disambiguates
    multiple concurrent watch tabs; ``<stage>`` is the active job, the
    conclusion, or a progress fraction."""

    def test_running_with_active_job(self):
        run = {
            "workflow": {"name": "tools"},
            "status": "in_progress",
            "jobs": [{"job_name": "sleep", "state": "in_progress"}],
        }
        assert _watch_title(run) == "avr ▸ tools: sleep"

    def test_completed_shows_conclusion(self):
        run = {
            "workflow": {"name": "tools"},
            "status": "completed",
            "conclusion": "success",
            "jobs": [{"job_name": "sleep", "state": "completed", "conclusion": "success"}],
        }
        assert _watch_title(run) == "avr ▸ tools: success"

    def test_completed_without_conclusion_says_done(self):
        run = {"workflow": {"name": "ci"}, "status": "completed", "jobs": []}
        assert _watch_title(run) == "avr ▸ ci: done"

    def test_falls_back_to_queued_job_when_nothing_running(self):
        run = {
            "workflow": {"name": "tools"},
            "status": "in_progress",
            "jobs": [
                {"state": "completed", "job_name": "lint"},
                {"state": "completed", "job_name": "build"},
                {"state": "queued", "job_name": "deploy"},
            ],
        }
        # ``deploy`` is queued so it surfaces as the next-up stage;
        # progress-fraction fallback triggers only with no active and no
        # queued either.
        assert _watch_title(run) == "avr ▸ tools: deploy"

    def test_progress_fraction_when_only_completed_jobs_so_far(self):
        # Edge case: jobs done but run not marked completed yet (Avrea
        # ingest race). Show fraction so the title isn't misleading.
        run = {
            "workflow": {"name": "tools"},
            "status": "in_progress",
            "jobs": [
                {"state": "completed", "job_name": "lint"},
                {"state": "completed", "job_name": "build"},
            ],
        }
        assert _watch_title(run) == "avr ▸ tools: 2/2 jobs done"

    def test_no_jobs_yet_shows_run_state(self):
        run = {"workflow": {"name": "tools"}, "status": "queued", "jobs": []}
        assert _watch_title(run) == "avr ▸ tools: queued"

    def test_missing_workflow_falls_back_to_run(self):
        run = {"status": "in_progress", "jobs": [{"job_name": "build", "state": "in_progress"}]}
        assert _watch_title(run) == "avr ▸ run: build"
