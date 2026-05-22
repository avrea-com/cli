"""Unit tests for attempt-aware behavior on `avr run rerun` and `avr run view`.

Avrea models each (workflow_run, attempt) pair as its own row with its own
Avrea run_id, so:
- After `avr run rerun`, the user wants the *new* run_id to navigate to.
- During `avr run view`, sibling attempts of the same platform_run_id should
  surface so the user can drill into a specific one.
"""

from avrea_cli.commands.run import _poll_for_new_attempt
from avrea_cli.main import cli
from click.testing import CliRunner
from unittest.mock import MagicMock
import httpx
import pytest


@pytest.fixture()
def runner(monkeypatch):
    monkeypatch.setenv("AVR_TOKEN", "tok")
    monkeypatch.setenv("AVR_ORG", "org-default")
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    # Each command module does `from avrea_cli.helpers import get_org_slug`,
    # so the binding is local — patch the consumers, not the source.
    for mod in ("avrea_cli.helpers", "avrea_cli.commands.run"):
        monkeypatch.setattr(f"{mod}.get_org_slug", lambda *a, **kw: "myorg", raising=False)
    # Don't actually sleep between poll iterations.
    monkeypatch.setattr("avrea_cli.commands.run.time.sleep", lambda _: None)
    return CliRunner()


# ----------------------------------------------------------------------------
# avr run rerun — poll for new attempt
# ----------------------------------------------------------------------------


class TestRunRerunPoll:
    def test_prints_new_run_id_when_attempt_appears(self, runner, monkeypatch):
        # 1st GET: fetch the existing run (attempt 1, platform_run_id=42)
        # POST: rerun
        # 2nd GET: workflow-runs?platform_run_id=42 → returns attempt 2 with new run_id
        get_calls: list[str] = []

        def fake_get(self, path, params=None):
            get_calls.append(path)
            if path == "/orgs/org-default/workflow-runs/run-old":
                return {"data": {"run_id": "run-old", "platform_run_id": 42, "run_attempt": 1}}
            if path == "/orgs/org-default/workflow-runs":
                return {
                    "data": [
                        {"run_id": "run-new", "platform_run_id": 42, "run_attempt": 2},
                        {"run_id": "run-old", "platform_run_id": 42, "run_attempt": 1},
                    ]
                }
            raise AssertionError(f"unexpected GET: {path}")

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, **kw: {"status": "rerun_requested"},
        )

        result = runner.invoke(cli, ["run", "rerun", "run-old", "--yes"])

        assert result.exit_code == 0, result.output
        assert "Re-run requested" in result.output
        assert "run-new" in result.output
        assert "attempt 2" in result.output
        # And the console URL hint should point at the *new* run, not the old one
        assert "/runs/run-new" in result.output

    def test_failed_only_passes_through_to_post(self, runner, monkeypatch):
        post_bodies: list[dict] = []

        def fake_post(self, path, **kw):
            post_bodies.append(kw.get("json", {}))
            return {"status": "rerun_requested"}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", fake_post)
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: (
                {"data": {"run_id": "run-old", "platform_run_id": 42, "run_attempt": 1}}
                if path.endswith("/run-old")
                else {"data": [{"run_id": "run-new", "platform_run_id": 42, "run_attempt": 2}]}
            ),
        )

        result = runner.invoke(cli, ["run", "rerun", "run-old", "--failed", "--yes"])
        assert result.exit_code == 0
        assert post_bodies and post_bodies[0] == {"failed_only": True}

    def test_poll_timeout_falls_back_to_hint(self, runner, monkeypatch):
        """If the new attempt doesn't land within the poll window, exit cleanly
        with a hint instead of failing or hanging forever."""

        def fake_get(self, path, params=None):
            if path == "/orgs/org-default/workflow-runs/run-old":
                return {"data": {"run_id": "run-old", "platform_run_id": 42, "run_attempt": 1}}
            # Poll always returns only the old attempt
            return {"data": [{"run_id": "run-old", "platform_run_id": 42, "run_attempt": 1}]}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, **kw: {"status": "rerun_requested"},
        )
        # Force the poll to time out immediately
        monkeypatch.setattr("avrea_cli.commands.run._poll_for_new_attempt", lambda *a, **kw: None)

        result = runner.invoke(cli, ["run", "rerun", "run-old", "--yes"])

        assert result.exit_code == 0
        assert "not yet visible" in result.output
        # Console URL hint falls back to the old run_id
        assert "/runs/run-old" in result.output

    def test_transient_network_error_retries_until_attempt_appears(self, monkeypatch):
        """Network blips during the rerun poll must not abort the command —
        the loop is bounded by ``deadline`` and the user expects to see the
        new run_id once it lands. Pin both transient httpx exceptions so a
        future regression that re-narrows the catch fails this test."""
        monkeypatch.setattr("avrea_cli.commands.run.time.sleep", lambda _: None)

        client = MagicMock()
        request = httpx.Request("GET", "https://api.example.com/x")
        # 1st poll: ConnectError. 2nd poll: TimeoutException. 3rd poll: success.
        client.public_get.side_effect = [
            httpx.ConnectError("dns blip", request=request),
            httpx.ReadTimeout("read timeout", request=request),
            {"data": [{"run_id": "run-new", "platform_run_id": 42, "run_attempt": 2}]},
        ]
        run = _poll_for_new_attempt(client, "org-1", platform_run_id=42, current_attempt=1, timeout=10.0)
        assert run is not None
        assert run["run_id"] == "run-new"
        assert client.public_get.call_count == 3

    def test_skips_poll_when_no_platform_run_id(self, runner, monkeypatch):
        """Defensive: if the run record is missing platform_run_id, don't poll."""
        polled = False

        def fake_poll(*a, **kw):
            nonlocal polled
            polled = True
            return None

        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: {"data": {"run_id": "run-old", "run_attempt": 1}},
        )
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, **kw: {"status": "rerun_requested"},
        )
        monkeypatch.setattr("avrea_cli.commands.run._poll_for_new_attempt", fake_poll)

        result = runner.invoke(cli, ["run", "rerun", "run-old", "--yes"])
        assert result.exit_code == 0
        assert polled is False
        assert "Re-run requested" in result.output


# ----------------------------------------------------------------------------
# avr run view — OTHER ATTEMPTS section
# ----------------------------------------------------------------------------


class TestRunViewOtherAttempts:
    def test_lists_sibling_attempts(self, runner, monkeypatch):
        def fake_get(self, path, params=None):
            if path == "/orgs/org-default/workflow-runs/run-2":
                return {
                    "data": {
                        "run_id": "run-2",
                        "platform_run_id": 42,
                        "run_attempt": 2,
                        "status": "completed",
                        "conclusion": "success",
                        "display_title": "Fix auth",
                        "head_branch": "main",
                        "head_sha": "abcdef12",
                        "event": "push",
                        "duration_seconds": 240,
                        "run_number": 7,
                        "workflow": {"name": "build"},
                        "repository": {"full_name": "acme/svc"},
                        "jobs": [],
                    }
                }
            if path == "/orgs/org-default/workflow-runs":
                # Three attempts; viewing run-2 — expect run-1 and run-3 listed
                return {
                    "data": [
                        {
                            "run_id": "run-3",
                            "platform_run_id": 42,
                            "run_attempt": 3,
                            "status": "completed",
                            "conclusion": "success",
                            "duration_seconds": 230,
                        },
                        {
                            "run_id": "run-2",
                            "platform_run_id": 42,
                            "run_attempt": 2,
                            "status": "completed",
                            "conclusion": "success",
                            "duration_seconds": 240,
                        },
                        {
                            "run_id": "run-1",
                            "platform_run_id": 42,
                            "run_attempt": 1,
                            "status": "completed",
                            "conclusion": "failure",
                            "duration_seconds": 260,
                        },
                    ]
                }
            raise AssertionError(f"unexpected GET: {path}")

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

        result = runner.invoke(cli, ["run", "view", "run-2"])

        assert result.exit_code == 0, result.output
        assert "OTHER ATTEMPTS" in result.output
        # run-1 and run-3 are siblings; run-2 (the one being viewed) must not appear in the section
        assert "run-1" in result.output
        assert "run-3" in result.output
        assert "attempt 1" in result.output
        assert "attempt 3" in result.output
        # The being-viewed run shouldn't appear *as a bullet* under OTHER
        # ATTEMPTS — but it does appear in the header and the trailing console
        # URL. Slice the section to just the bulleted lines (until "View this
        # run on Avrea") and check there.
        after_header = result.output.split("OTHER ATTEMPTS", 1)[1]
        bullets = after_header.split("View this run on Avrea", 1)[0]
        assert "run-2" not in bullets

    def test_no_siblings_skips_section(self, runner, monkeypatch):
        def fake_get(self, path, params=None):
            if path == "/orgs/org-default/workflow-runs/run-only":
                return {
                    "data": {
                        "run_id": "run-only",
                        "platform_run_id": 99,
                        "run_attempt": 1,
                        "status": "completed",
                        "conclusion": "success",
                        "display_title": "First and only",
                        "duration_seconds": 60,
                        "run_number": 1,
                        "workflow": {"name": "build"},
                        "repository": {"full_name": "acme/svc"},
                        "jobs": [],
                    }
                }
            if path == "/orgs/org-default/workflow-runs":
                return {"data": [{"run_id": "run-only", "platform_run_id": 99, "run_attempt": 1}]}
            raise AssertionError(f"unexpected GET: {path}")

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

        result = runner.invoke(cli, ["run", "view", "run-only"])

        assert result.exit_code == 0
        assert "OTHER ATTEMPTS" not in result.output

    def test_skips_section_when_no_platform_run_id(self, runner, monkeypatch):
        """If the run record lacks platform_run_id, don't bother making the
        sibling lookup — there's no key to filter on."""
        sibling_lookup_called = False

        def fake_get(self, path, params=None):
            nonlocal sibling_lookup_called
            if path.endswith("/run-x"):
                return {
                    "data": {
                        "run_id": "run-x",
                        # platform_run_id intentionally missing
                        "run_attempt": 1,
                        "status": "completed",
                        "conclusion": "success",
                        "display_title": "no plat",
                        "duration_seconds": 12,
                        "run_number": 1,
                        "workflow": {"name": "build"},
                        "repository": {"full_name": "acme/svc"},
                        "jobs": [],
                    }
                }
            sibling_lookup_called = True
            return {"data": []}

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

        result = runner.invoke(cli, ["run", "view", "run-x"])

        assert result.exit_code == 0
        assert sibling_lookup_called is False
        assert "OTHER ATTEMPTS" not in result.output

    @pytest.mark.parametrize(
        "exc_factory,fingerprint",
        [
            (
                lambda path: httpx.HTTPStatusError(
                    "500",
                    request=httpx.Request("GET", f"https://api.example{path}"),
                    response=httpx.Response(500, request=httpx.Request("GET", f"https://api.example{path}")),
                ),
                "HTTP 500",
            ),
            (lambda path: httpx.ConnectError("dns"), "ConnectError"),
            (lambda path: httpx.TimeoutException("slow"), "TimeoutException"),
        ],
    )
    def test_sibling_lookup_failure_renders_dim_hint(self, runner, monkeypatch, exc_factory, fingerprint):
        """When the auxiliary sibling fetch fails, ``run view`` must still
        render the primary record and surface a dim hint to stderr — not
        crash the whole command."""

        def fake_get(self, path, params=None):
            if path.endswith("/run-2"):
                return {
                    "data": {
                        "run_id": "run-2",
                        "platform_run_id": 42,
                        "run_attempt": 1,
                        "status": "completed",
                        "conclusion": "success",
                        "display_title": "primary still works",
                        "duration_seconds": 20,
                        "run_number": 1,
                        "workflow": {"name": "build"},
                        "repository": {"full_name": "acme/svc"},
                        "jobs": [],
                    }
                }
            # Sibling lookup raises.
            raise exc_factory(path)

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", fake_get)

        result = runner.invoke(cli, ["run", "view", "run-2"])

        # Primary record still rendered.
        assert result.exit_code == 0, result.output
        assert "primary still works" in result.output
        # Hint surfaces the cause to stderr.
        assert "could not load other attempts" in result.output
        assert fingerprint in result.output
        # OTHER ATTEMPTS heading must NOT appear (no data to show).
        assert "OTHER ATTEMPTS" not in result.output
