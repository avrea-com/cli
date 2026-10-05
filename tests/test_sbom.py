"""Unit tests for the `avr sbom` command group."""

from avrea_cli.main import cli
from typing import Any
import httpx
import json
import pytest

SHA = "a" * 40
OTHER_SHA = "b" * 40
BASE = "/orgs/org-default/repos/rep-123/analysis/sbom"

ARTIFACTS = [
    {"filename": "bom.cyclonedx.json", "media_type": "application/vnd.cyclonedx+json", "raw_bytes": 2048},
    {"filename": "bom.spdx.json", "media_type": "application/spdx+json", "raw_bytes": 4096},
    {"filename": "inventory.json", "media_type": "application/json", "raw_bytes": 1024},
]

SUMMARY = {
    "total_dependencies": 42,
    "unresolved": 3,
    "lookup_failed": 0,
    "copyleft": ["gpl-thing"],
    "copyleft_omitted": 0,
    "licenses": {"MIT": 30, "Apache-2.0": 9, "NOASSERTION": 3},
    "ecosystems": {"python": 40, "docker": 2},
}

SNAPSHOT = {
    "type": "sbom",
    "commit_sha": SHA,
    "branch": "main",
    "is_release": False,
    "schema_version": 1,
    "generated_at": "2026-09-01T10:00:00Z",
    "recorded_at": "2026-09-01T10:05:00Z",
    "artifacts": ARTIFACTS,
    "summary": SUMMARY,
}

HISTORY = {
    "data": [
        {
            "commit_sha": SHA,
            "branch": "main",
            "is_release": True,
            "schema_version": 1,
            "generated_at": "2026-09-01T10:00:00Z",
            "recorded_at": "2026-09-01T10:05:00Z",
            "total_dependencies": 42,
            "unresolved": 3,
            "copyleft_count": 1,
            "artifacts": ARTIFACTS,
        },
        {
            "commit_sha": OTHER_SHA,
            "branch": "main",
            "is_release": False,
            "schema_version": 1,
            "generated_at": "2026-08-01T10:00:00Z",
            "recorded_at": "2026-08-01T10:05:00Z",
            "total_dependencies": None,
            "unresolved": None,
            "copyleft_count": None,
            "artifacts": [],
        },
    ],
    "pagination": {"next_cursor": "Y3Vyc29y"},
}


def _http_error(status: int, detail: str = "", headers: dict[str, str] | None = None) -> httpx.HTTPStatusError:
    request = httpx.Request("GET", "https://api.avrea.io/x")
    response = httpx.Response(status, json={"detail": detail}, headers=headers, request=request)
    return httpx.HTTPStatusError("err", request=request, response=response)


@pytest.fixture
def calls(monkeypatch):
    recorded: list[tuple[str, str, dict | None]] = []
    timeouts: list[tuple[str, float | None]] = []
    routes: dict[tuple[str, str], Any] = {}

    clock = {"now": 0.0}

    def get(path, params=None, timeout=None):
        timeouts.append((path, timeout))
        return respond("GET", path, params)

    def respond(method, path, payload):
        recorded.append((method, path, payload))
        result = routes[(method, path)]
        if isinstance(result, list):
            result = result.pop(0) if len(result) > 1 else result[0]
        if callable(result):
            result = result(clock["now"])
        if isinstance(result, Exception):
            raise result
        return result

    def sleep(seconds):
        clock["now"] += seconds

    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get",
        lambda self, path, params=None, timeout=None: get(path, params, timeout),
    )
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_get_bytes",
        lambda self, path, params=None: respond("GET_BYTES", path, params),
    )
    monkeypatch.setattr(
        "avrea_cli.api_client.ApiClient.public_post",
        lambda self, path, json=None, timeout=None, **_: respond("POST", path, json),
    )
    monkeypatch.setattr("avrea_cli.commands.sbom.time.sleep", sleep)
    monkeypatch.setattr("avrea_cli.commands.sbom.time.monotonic", lambda: clock["now"])

    class Calls:
        log = recorded
        request_timeouts = timeouts

        @property
        def now(self):
            return clock["now"]

        def route(self, method, path, result):
            routes[(method, path)] = result

    return Calls()


class TestSbomList:
    def test_table_output(self, runner, calls):
        calls.route("GET", BASE, HISTORY)
        result = runner.invoke(cli, ["sbom", "list", "--repo", "rep-123"])
        assert result.exit_code == 0, result.output
        assert SHA[:12] in result.output
        assert OTHER_SHA[:12] in result.output
        assert "42" in result.output
        assert "?" in result.output

    def test_next_cursor_hint(self, runner, calls):
        calls.route("GET", BASE, HISTORY)
        result = runner.invoke(cli, ["sbom", "list", "--repo", "rep-123"])
        assert "--cursor Y3Vyc29y" in result.output

    def test_passes_limit_and_cursor(self, runner, calls):
        calls.route("GET", BASE, {"data": [], "pagination": {"next_cursor": None}})
        result = runner.invoke(cli, ["sbom", "list", "--repo", "rep-123", "-L", "5", "--cursor", "abc"])
        assert result.exit_code == 0, result.output
        assert calls.log[-1][2] == {"limit": 5, "cursor": "abc"}

    def test_rejects_whitespace_cursor(self, runner, calls):
        result = runner.invoke(cli, ["sbom", "list", "--repo", "rep-123", "--cursor", " abc"])
        assert result.exit_code == 2
        assert calls.log == []

    def test_json_output(self, runner, calls):
        calls.route("GET", BASE, HISTORY)
        result = runner.invoke(cli, ["sbom", "list", "--repo", "rep-123", "--json", "commit_sha,copyleft_count"])
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == [
            {"commit_sha": SHA, "copyleft_count": 1},
            {"commit_sha": OTHER_SHA, "copyleft_count": None},
        ]

    def test_json_reports_next_cursor_on_stderr(self, runner, calls):
        calls.route("GET", BASE, HISTORY)
        result = runner.invoke(cli, ["sbom", "list", "--repo", "rep-123", "--json", "commit_sha"])
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == [{"commit_sha": SHA}, {"commit_sha": OTHER_SHA}]
        assert "next_cursor: Y3Vyc29y" in result.stderr

    def test_json_without_next_page_is_silent_on_stderr(self, runner, calls):
        calls.route("GET", BASE, {**HISTORY, "pagination": {"next_cursor": None}})
        result = runner.invoke(cli, ["sbom", "list", "--repo", "rep-123", "--json", "commit_sha"])
        assert result.stderr == ""

    def test_empty_history(self, runner, calls):
        calls.route("GET", BASE, {"data": [], "pagination": {"next_cursor": None}})
        result = runner.invoke(cli, ["sbom", "list", "--repo", "rep-123"])
        assert result.exit_code == 0
        assert "No data found." in result.output


class TestSbomView:
    def test_latest_summary(self, runner, calls):
        calls.route("GET", f"{BASE}/latest", SNAPSHOT)
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123"])
        assert result.exit_code == 0, result.output
        assert SHA in result.output
        assert "Apache-2.0" in result.output
        assert "python" in result.output
        assert "gpl-thing" in result.output
        assert "bom.spdx.json" in result.output

    def test_specific_commit(self, runner, calls):
        calls.route("GET", f"{BASE}/commits/{SHA}", SNAPSHOT)
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123", "--commit", SHA.upper()])
        assert result.exit_code == 0, result.output
        assert calls.log[-1][1] == f"{BASE}/commits/{SHA}"

    def test_rejects_short_commit(self, runner, calls):
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123", "--commit", "abc123"])
        assert result.exit_code == 2
        assert "40 or 64" in result.output
        assert calls.log == []

    def test_not_found_hints_generate(self, runner, calls):
        calls.route("GET", f"{BASE}/latest", _http_error(404, "analysis result not found"))
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123"])
        assert result.exit_code == 1
        assert "avr sbom generate" in result.output

    def test_omitted_summary_shows_unknown_counts(self, runner, calls):
        calls.route(
            "GET",
            f"{BASE}/latest",
            {**SNAPSHOT, "summary": {"omitted": "oversized", "serialized_bytes": 143362}},
        )
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123"])
        assert result.exit_code == 0, result.output
        assert "Dependencies  ?" in result.output
        assert "Unresolved    ?" in result.output
        assert "Copyleft      ?" in result.output
        assert "oversized" in result.output
        assert "--format inventory" in result.output

    def test_empty_summary_shows_unknown_counts(self, runner, calls):
        calls.route("GET", f"{BASE}/latest", {**SNAPSHOT, "summary": {}})
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123"])
        assert result.exit_code == 0, result.output
        assert "Dependencies  ?" in result.output
        assert "Copyleft      ?" in result.output

    def test_empty_copyleft_list_is_zero(self, runner, calls):
        summary = {**SUMMARY, "copyleft": [], "copyleft_omitted": 0}
        calls.route("GET", f"{BASE}/latest", {**SNAPSHOT, "summary": summary})
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123"])
        assert "Copyleft      0" in result.output

    def test_copyleft_count_includes_omitted(self, runner, calls):
        summary = {**SUMMARY, "copyleft": ["a", "b"], "copyleft_omitted": 7}
        calls.route("GET", f"{BASE}/latest", {**SNAPSHOT, "summary": summary})
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123"])
        assert "Copyleft      9" in result.output
        assert "and 7 more" in result.output

    def test_inventory_hints_pin_the_viewed_snapshot(self, runner, calls):
        omitted = {"omitted": "oversized", "serialized_bytes": 143362}
        calls.route("GET", f"{BASE}/commits/{SHA}", {**SNAPSHOT, "summary": omitted})
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123", "--commit", SHA])
        expected = f"avr sbom download --org org-default --repo rep-123 --commit {SHA} --format inventory"
        assert expected in result.output

    def test_copyleft_overflow_hint_pins_the_viewed_snapshot(self, runner, calls):
        summary = {**SUMMARY, "copyleft": ["a"], "copyleft_omitted": 3}
        calls.route("GET", f"{BASE}/latest", {**SNAPSHOT, "summary": summary})
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123"])
        expected = f"avr sbom download --org org-default --repo rep-123 --commit {SHA} --format inventory"
        assert expected in result.output

    def test_inventory_hint_quotes_values_for_the_shell(self, runner, calls):
        omitted = {"omitted": "oversized"}
        calls.route("GET", "/orgs/org-a b/repos/rep-123/analysis/sbom/latest", {**SNAPSHOT, "summary": omitted})
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123", "--org", "org-a b"])
        assert result.exit_code == 0, result.output
        assert "--org 'org-a b' --repo rep-123" in result.output

    @pytest.mark.parametrize(
        ("licenses", "ecosystems"),
        [
            (["MIT", "Apache-2.0"], "python"),
            ({"MIT": None, "Apache-2.0": "9", "BSD-3-Clause": True}, {"python": [1]}),
            (None, None),
        ],
    )
    def test_malformed_breakdowns_are_skipped(self, runner, calls, licenses, ecosystems):
        summary = {**SUMMARY, "licenses": licenses, "ecosystems": ecosystems}
        calls.route("GET", f"{BASE}/latest", {**SNAPSHOT, "summary": summary})
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123"])
        assert result.exit_code == 0, result.output
        assert "License" not in result.output
        assert "Ecosystem" not in result.output

    def test_breakdown_keeps_valid_entries_among_malformed_ones(self, runner, calls):
        summary = {**SUMMARY, "licenses": {"MIT": 30, "GPL-3.0": None}, "ecosystems": {"python": 40, "go": "x"}}
        calls.route("GET", f"{BASE}/latest", {**SNAPSHOT, "summary": summary})
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123"])
        assert result.exit_code == 0, result.output
        assert "MIT" in result.output
        assert "GPL-3.0" not in result.output
        assert "python" in result.output
        assert "| go " not in result.output

    def test_json_summary(self, runner, calls):
        calls.route("GET", f"{BASE}/latest", SNAPSHOT)
        result = runner.invoke(cli, ["sbom", "view", "--repo", "rep-123", "--json", "commit_sha,summary"])
        assert result.exit_code == 0, result.output
        data = json.loads(result.output)
        assert data["commit_sha"] == SHA
        assert data["summary"]["total_dependencies"] == 42


class TestSbomDownload:
    def test_latest_cyclonedx_to_default_file(self, runner, calls, tmp_path, monkeypatch):
        monkeypatch.chdir(tmp_path)
        calls.route("GET", f"{BASE}/latest", SNAPSHOT)
        calls.route("GET_BYTES", f"{BASE}/commits/{SHA}/artifacts/bom.cyclonedx.json", b'{"bomFormat":"CycloneDX"}')
        result = runner.invoke(cli, ["sbom", "download", "--repo", "rep-123"])
        assert result.exit_code == 0, result.output
        assert (tmp_path / "bom.cyclonedx.json").read_bytes() == b'{"bomFormat":"CycloneDX"}'

    @pytest.mark.parametrize(
        ("fmt", "filename"),
        [("spdx", "bom.spdx.json"), ("inventory", "inventory.json")],
    )
    def test_format_selects_artifact(self, runner, calls, tmp_path, fmt, filename):
        calls.route("GET_BYTES", f"{BASE}/commits/{SHA}/artifacts/{filename}", b"{}")
        out = tmp_path / "out.json"
        result = runner.invoke(
            cli, ["sbom", "download", "--repo", "rep-123", "--commit", SHA, "--format", fmt, "--out", str(out)]
        )
        assert result.exit_code == 0, result.output
        assert out.read_bytes() == b"{}"
        assert [c[0] for c in calls.log] == ["GET_BYTES"]

    def test_out_dash_writes_stdout(self, runner, calls):
        calls.route("GET_BYTES", f"{BASE}/commits/{SHA}/artifacts/bom.cyclonedx.json", b'{"ok":true}')
        result = runner.invoke(cli, ["sbom", "download", "--repo", "rep-123", "--commit", SHA, "--out", "-"])
        assert result.exit_code == 0, result.output
        assert result.stdout == '{"ok":true}'

    def test_missing_artifact(self, runner, calls, tmp_path):
        calls.route("GET_BYTES", f"{BASE}/commits/{SHA}/artifacts/bom.spdx.json", _http_error(404, "no artifact"))
        result = runner.invoke(
            cli,
            [
                "sbom",
                "download",
                "--repo",
                "rep-123",
                "--commit",
                SHA,
                "--format",
                "spdx",
                "--out",
                str(tmp_path / "x"),
            ],
        )
        assert result.exit_code == 1
        assert "no artifact" in result.output
        assert not (tmp_path / "x").exists()


STATE_PATH = "/orgs/org-default/repos/rep-123/ai-task/state"
LATEST = f"{BASE}/latest"
NEW_SHA = "c" * 40
NEW_SNAPSHOT = {**SNAPSHOT, "commit_sha": NEW_SHA, "recorded_at": "2026-10-05T12:00:00Z"}


class TestSbomGenerate:
    def test_requests_sbom_scope_only(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123"])
        assert result.exit_code == 0, result.output
        assert calls.log[-1][2] == {"scope": "sbom"}
        assert "task-1" in result.output

    def test_passes_ref(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--ref", "v1.2.0"])
        assert calls.log[-1][2] == {"scope": "sbom", "ref": "v1.2.0"}

    def test_already_running(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "already_running"})
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123"])
        assert result.exit_code == 0
        assert "already running" in result.output

    def test_cooldown_reports_retry_after(self, runner, calls):
        calls.route(
            "POST",
            f"{BASE}/generate",
            _http_error(429, "scanned 10 minutes ago; retry in 312s.", headers={"Retry-After": "312"}),
        )
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123"])
        assert result.exit_code == 1
        assert "312s" in result.output

    def test_conflict_surfaces_detail(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", _http_error(409, "Another analysis is already running"))
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123"])
        assert result.exit_code == 1
        assert "Another analysis is already running" in result.output

    def test_json_output(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--json", "*"])
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == {
            "ai_task_id": "task-1",
            "commit_sha": None,
            "error_code": None,
            "recorded_at": None,
            "status": "generating",
            "task_status": None,
        }

    @pytest.mark.parametrize("extra", [[], ["--wait"]])
    def test_unknown_json_field_rejected_before_request(self, runner, calls, extra):
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--json", "task_id", *extra])
        assert result.exit_code != 0
        assert "Unknown JSON field(s): task_id" in result.output
        assert calls.log == []


COMMIT_PATH = f"{BASE}/commits/{NEW_SHA}"
PRIOR_FOR_COMMIT = {**SNAPSHOT, "commit_sha": NEW_SHA, "recorded_at": "2026-09-20T08:00:00Z"}
UNRELATED_ARRIVAL = {**SNAPSHOT, "commit_sha": OTHER_SHA, "recorded_at": "2026-10-05T11:59:00Z"}


def _delivered_at(seconds: float):
    return lambda now: NEW_SNAPSHOT if now >= seconds else PRIOR_FOR_COMMIT


def _pinned_wait(*extra: str) -> list[str]:
    return [
        "sbom",
        "generate",
        "--repo",
        "rep-123",
        "--ref",
        NEW_SHA,
        "--wait",
        "--json",
        "status,task_status,error_code,commit_sha",
        *extra,
    ]


class TestSbomGenerateWaitWithoutCommit:
    def test_reports_task_outcome_without_claiming_a_snapshot(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route(
            "GET",
            STATE_PATH,
            [
                {"ai_task_id": "task-1", "status": "PENDING"},
                {"ai_task_id": "task-1", "status": "RUNNING"},
                {"ai_task_id": "task-1", "status": "COMPLETED"},
            ],
        )
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--wait", "--json", "*"])
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == {
            "ai_task_id": "task-1",
            "commit_sha": None,
            "error_code": None,
            "recorded_at": None,
            "status": "completed",
            "task_status": "completed",
        }
        assert [c[1] for c in calls.log] == [f"{BASE}/generate", STATE_PATH, STATE_PATH, STATE_PATH]
        assert calls.log[1][2] == {"scope": "sbom"}

    def test_human_output_points_to_commit_pinned_wait(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "COMPLETED"})
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--wait"])
        assert result.exit_code == 0, result.output
        assert "SBOM generated" not in result.output
        assert "task task-1 completed" in result.output
        assert "--ref <commit-sha>" in result.output

    def test_ignores_state_of_another_task(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route(
            "GET",
            STATE_PATH,
            [{"ai_task_id": "task-0", "status": "COMPLETED"}, {"ai_task_id": "task-1", "status": "COMPLETED"}],
        )
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--wait"])
        assert result.exit_code == 0, result.output
        assert len([c for c in calls.log if c[1] == STATE_PATH]) == 2

    def test_branch_ref_is_not_treated_as_commit(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "COMPLETED"})
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--ref", "main", "--wait"])
        assert result.exit_code == 0, result.output
        assert all("/commits/" not in c[1] for c in calls.log)

    @pytest.mark.parametrize("code", ["enrichment_unavailable", "vm_response_invalid", "clone_failed", None])
    def test_failed_task_is_reported_as_failed(self, runner, calls, code):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "already_running"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "FAILED", "error_code": code})
        result = runner.invoke(
            cli, ["sbom", "generate", "--repo", "rep-123", "--wait", "--json", "status,task_status,error_code"]
        )
        assert result.exit_code == 1
        assert json.loads(result.stdout) == {"status": "failed", "task_status": "failed", "error_code": code}
        assert all("/analysis/sbom/latest" not in c[1] and "/commits/" not in c[1] for c in calls.log)

    @pytest.mark.parametrize("code", ["enrichment_unavailable", "clone_failed", None])
    def test_failed_task_human_output(self, runner, calls, code):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "FAILED", "error_code": code})
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--wait"])
        assert result.exit_code == 1
        assert (code or "unknown error") in result.output
        assert "avr sbom list" in result.output

    def test_task_timeout(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "RUNNING"})
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--wait", "--wait-timeout", "250"])
        assert result.exit_code == 1
        assert "Timed out" in result.output
        assert calls.now >= 250

    def test_superseded_task_reports_unknown_outcome(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "already_running"})
        calls.route(
            "GET",
            STATE_PATH,
            [{"ai_task_id": "task-1", "status": "RUNNING"}, {"ai_task_id": "task-2", "status": "PENDING"}],
        )
        result = runner.invoke(
            cli, ["sbom", "generate", "--repo", "rep-123", "--wait", "--json", "status,task_status,error_code"]
        )
        assert result.exit_code == 1
        assert json.loads(result.stdout) == {"status": "unknown", "task_status": "unknown", "error_code": None}
        assert calls.now < 60

    def test_superseded_task_human_output(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route(
            "GET",
            STATE_PATH,
            [{"ai_task_id": "task-1", "status": "RUNNING"}, {"ai_task_id": "task-2", "status": "PENDING"}],
        )
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--wait"])
        assert result.exit_code == 1
        assert "superseded by task task-2" in result.output


class TestSbomGenerateWaitForCommit:
    def test_reads_commit_baseline_before_request(self, runner, calls):
        calls.route("GET", COMMIT_PATH, [PRIOR_FOR_COMMIT, NEW_SNAPSHOT])
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "COMPLETED"})
        runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--ref", NEW_SHA, "--wait"])
        assert [c[0:2] for c in calls.log[:2]] == [("GET", COMMIT_PATH), ("POST", f"{BASE}/generate")]

    def test_reports_delivered_snapshot_for_requested_commit(self, runner, calls):
        calls.route("GET", COMMIT_PATH, [PRIOR_FOR_COMMIT, PRIOR_FOR_COMMIT, NEW_SNAPSHOT])
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "already_running"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "COMPLETED"})
        result = runner.invoke(
            cli,
            [
                "sbom",
                "generate",
                "--repo",
                "rep-123",
                "--ref",
                NEW_SHA,
                "--wait",
                "--json",
                "status,commit_sha,recorded_at",
            ],
        )
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == {
            "status": "completed",
            "commit_sha": NEW_SHA,
            "recorded_at": NEW_SNAPSHOT["recorded_at"],
        }

    def test_first_sbom_for_commit(self, runner, calls):
        not_found = _http_error(404, "analysis result not found")
        calls.route("GET", COMMIT_PATH, [not_found, not_found, NEW_SNAPSHOT])
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "COMPLETED"})
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--ref", NEW_SHA, "--wait"])
        assert result.exit_code == 0, result.output
        assert f"SBOM generated for {NEW_SHA[:12]}" in result.output

    def test_unrelated_snapshot_arrival_does_not_satisfy_wait(self, runner, calls):
        calls.route("GET", COMMIT_PATH, PRIOR_FOR_COMMIT)
        calls.route("GET", LATEST, UNRELATED_ARRIVAL)
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "COMPLETED"})
        result = runner.invoke(
            cli, ["sbom", "generate", "--repo", "rep-123", "--ref", NEW_SHA, "--wait", "--wait-timeout", "250"]
        )
        assert result.exit_code == 1
        assert "SBOM generated" not in result.output
        assert f"no new SBOM for {NEW_SHA[:12]}" in result.output
        assert all(c[1] != LATEST for c in calls.log)

    @pytest.mark.parametrize("code", ["enrichment_unavailable", "vm_response_invalid", None])
    def test_failed_task_with_delivered_sbom_succeeds(self, runner, calls, code):
        calls.route("GET", COMMIT_PATH, [PRIOR_FOR_COMMIT, PRIOR_FOR_COMMIT, NEW_SNAPSHOT])
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "already_running"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "FAILED", "error_code": code})
        result = runner.invoke(
            cli,
            [
                "sbom",
                "generate",
                "--repo",
                "rep-123",
                "--ref",
                NEW_SHA,
                "--wait",
                "--json",
                "status,task_status,error_code,commit_sha",
            ],
        )
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == {
            "status": "completed",
            "task_status": "failed",
            "error_code": code,
            "commit_sha": NEW_SHA,
        }

    def test_failed_task_delivery_after_two_minutes_succeeds(self, runner, calls):
        calls.route("GET", COMMIT_PATH, _delivered_at(150))
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "already_running"})
        calls.route(
            "GET", STATE_PATH, {"ai_task_id": "task-1", "status": "FAILED", "error_code": "vm_response_invalid"}
        )
        result = runner.invoke(cli, _pinned_wait("--wait-timeout", "600"))
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == {
            "status": "completed",
            "task_status": "failed",
            "error_code": "vm_response_invalid",
            "commit_sha": NEW_SHA,
        }
        assert 150 <= calls.now < 600

    @pytest.mark.parametrize("code", ["enrichment_unavailable", "clone_failed"])
    def test_failed_task_without_delivery_uses_full_budget(self, runner, calls, code):
        calls.route("GET", COMMIT_PATH, PRIOR_FOR_COMMIT)
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "already_running"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "FAILED", "error_code": code})
        result = runner.invoke(cli, _pinned_wait("--wait-timeout", "600"))
        assert result.exit_code == 1
        assert json.loads(result.stdout) == {
            "status": "failed",
            "task_status": "failed",
            "error_code": code,
            "commit_sha": None,
        }
        assert calls.now >= 600

    def test_superseded_task_still_waits_for_commit_delivery(self, runner, calls):
        calls.route("GET", COMMIT_PATH, _delivered_at(10))
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "already_running"})
        calls.route(
            "GET",
            STATE_PATH,
            [{"ai_task_id": "task-1", "status": "RUNNING"}, {"ai_task_id": "task-2", "status": "PENDING"}],
        )
        result = runner.invoke(cli, _pinned_wait("--wait-timeout", "600"))
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == {
            "status": "completed",
            "task_status": "unknown",
            "error_code": None,
            "commit_sha": NEW_SHA,
        }
        assert calls.now < 60

    def test_delivery_before_task_finishes_succeeds(self, runner, calls):
        calls.route("GET", COMMIT_PATH, _delivered_at(5))
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "already_running"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "RUNNING"})
        result = runner.invoke(cli, _pinned_wait())
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout)["task_status"] == "running"

    def test_timeout_without_delivery_while_task_running(self, runner, calls):
        calls.route("GET", COMMIT_PATH, PRIOR_FOR_COMMIT)
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "RUNNING"})
        result = runner.invoke(
            cli, ["sbom", "generate", "--repo", "rep-123", "--ref", NEW_SHA, "--wait", "--wait-timeout", "250"]
        )
        assert result.exit_code == 1
        assert "Timed out" in result.output

    def test_uppercase_sha_is_normalised(self, runner, calls):
        calls.route("GET", COMMIT_PATH, [PRIOR_FOR_COMMIT, NEW_SNAPSHOT])
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "COMPLETED"})
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--ref", NEW_SHA.upper(), "--wait"])
        assert result.exit_code == 0, result.output
        assert calls.log[1][2] == {"scope": "sbom", "ref": NEW_SHA}

    def test_padded_sha_is_trimmed_and_pinned(self, runner, calls):
        calls.route("GET", COMMIT_PATH, [PRIOR_FOR_COMMIT, NEW_SNAPSHOT])
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, {"ai_task_id": "task-1", "status": "COMPLETED"})
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--ref", f"  {NEW_SHA}\n", "--wait"])
        assert result.exit_code == 0, result.output
        assert calls.log[0][1] == COMMIT_PATH
        assert calls.log[1][2] == {"scope": "sbom", "ref": NEW_SHA}
        assert f"SBOM generated for {NEW_SHA[:12]}" in result.output

    def test_padded_branch_is_trimmed_with_case_preserved(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--ref", " Release/V2 "])
        assert calls.log[-1][2] == {"scope": "sbom", "ref": "Release/V2"}

    def test_blank_ref_rejected_before_request(self, runner, calls):
        result = runner.invoke(cli, ["sbom", "generate", "--repo", "rep-123", "--ref", "   "])
        assert result.exit_code == 2
        assert "--ref" in result.output
        assert calls.log == []


RUNNING = {"ai_task_id": "task-1", "status": "RUNNING"}
COMPLETED = {"ai_task_id": "task-1", "status": "COMPLETED"}
UNPINNED_WAIT = ["sbom", "generate", "--repo", "rep-123", "--wait"]
PINNED_WAIT = ["sbom", "generate", "--repo", "rep-123", "--ref", NEW_SHA, "--wait"]
TRANSIENT_ERRORS = [
    _http_error(500),
    _http_error(502),
    _http_error(503),
    _http_error(408),
    _http_error(429, headers={"Retry-After": "7"}),
    httpx.ConnectError("connection reset"),
    httpx.ReadTimeout("read timed out"),
]


def _posts(calls) -> int:
    return len([c for c in calls.log if c[0] == "POST"])


def _timeouts_for(calls, path: str) -> list[float | None]:
    return [t for p, t in calls.request_timeouts if p == path]


class TestSbomGenerateWaitTransientErrors:
    @pytest.mark.parametrize("error", TRANSIENT_ERRORS)
    def test_task_state_error_is_retried(self, runner, calls, error):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, [error, COMPLETED])
        result = runner.invoke(cli, UNPINNED_WAIT)
        assert result.exit_code == 0, result.output
        assert len([c for c in calls.log if c[1] == STATE_PATH]) == 2
        assert _posts(calls) == 1

    @pytest.mark.parametrize("error", TRANSIENT_ERRORS)
    def test_commit_snapshot_error_is_retried(self, runner, calls, error):
        calls.route("GET", COMMIT_PATH, [PRIOR_FOR_COMMIT, error, NEW_SNAPSHOT])
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, COMPLETED)
        result = runner.invoke(cli, PINNED_WAIT)
        assert result.exit_code == 0, result.output
        assert f"SBOM generated for {NEW_SHA[:12]}" in result.output
        assert _posts(calls) == 1

    @pytest.mark.parametrize("error", TRANSIENT_ERRORS)
    def test_task_state_error_does_not_stop_pinned_delivery(self, runner, calls, error):
        calls.route("GET", COMMIT_PATH, [PRIOR_FOR_COMMIT, PRIOR_FOR_COMMIT, NEW_SNAPSHOT])
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, [error, COMPLETED])
        result = runner.invoke(cli, PINNED_WAIT)
        assert result.exit_code == 0, result.output
        assert _posts(calls) == 1

    def test_retry_after_sets_the_backoff(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, [_http_error(429, headers={"Retry-After": "12"}), COMPLETED])
        result = runner.invoke(cli, UNPINNED_WAIT)
        assert result.exit_code == 0, result.output
        assert calls.now == 12

    def test_retry_after_is_capped(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, [_http_error(429, headers={"Retry-After": "3600"}), COMPLETED])
        result = runner.invoke(cli, UNPINNED_WAIT)
        assert result.exit_code == 0, result.output
        assert calls.now == 30

    def test_transient_error_is_reported_on_stderr(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, [_http_error(503), COMPLETED])
        result = runner.invoke(cli, [*UNPINNED_WAIT, "--json", "status"])
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == {"status": "completed"}
        assert "HTTP 503" in result.stderr
        assert "retrying" in result.stderr

    def test_permanent_task_state_error_stops_the_wait(self, runner, calls):
        calls.route("GET", COMMIT_PATH, PRIOR_FOR_COMMIT)
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, _http_error(403, "access revoked"))
        result = runner.invoke(cli, PINNED_WAIT)
        assert result.exit_code == 1
        assert "access revoked" in result.output
        assert calls.now == 0

    def test_permanent_snapshot_error_stops_the_wait(self, runner, calls):
        calls.route("GET", COMMIT_PATH, [PRIOR_FOR_COMMIT, _http_error(403, "access revoked")])
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, RUNNING)
        result = runner.invoke(cli, PINNED_WAIT)
        assert result.exit_code == 1
        assert "access revoked" in result.output
        assert calls.now == 0

    def test_persistent_errors_time_out_naming_the_last_error(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, _http_error(503))
        result = runner.invoke(cli, [*UNPINNED_WAIT, "--wait-timeout", "20"])
        assert result.exit_code == 1
        assert "Timed out after 20s" in result.output
        assert "last error: HTTP 503" in result.output
        assert calls.now == 20


class TestSbomGenerateWaitBudget:
    def test_unpinned_wait_stops_at_the_timeout(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, RUNNING)
        result = runner.invoke(cli, [*UNPINNED_WAIT, "--wait-timeout", "7"])
        assert result.exit_code == 1
        assert calls.now == 7

    def test_pinned_delivery_after_the_timeout_is_not_reported(self, runner, calls):
        calls.route("GET", COMMIT_PATH, _delivered_at(5))
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, RUNNING)
        result = runner.invoke(cli, [*PINNED_WAIT, "--wait-timeout", "1"])
        assert result.exit_code == 1
        assert "SBOM generated" not in result.output
        assert calls.now == 1

    def test_poll_requests_are_bounded_by_the_remaining_budget(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, RUNNING)
        runner.invoke(cli, [*UNPINNED_WAIT, "--wait-timeout", "12"])
        assert _timeouts_for(calls, STATE_PATH) == [12.0, 7.0, 2.0, 1.0]

    def test_long_budget_keeps_the_client_request_timeout(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, COMPLETED)
        runner.invoke(cli, UNPINNED_WAIT)
        assert _timeouts_for(calls, STATE_PATH) == [30.0]

    def test_pinned_snapshot_polls_are_bounded_too(self, runner, calls):
        calls.route("GET", COMMIT_PATH, PRIOR_FOR_COMMIT)
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, RUNNING)
        runner.invoke(cli, [*PINNED_WAIT, "--wait-timeout", "8"])
        assert _timeouts_for(calls, COMMIT_PATH) == [None, 8.0, 3.0, 1.0]


class TestSbomGenerateWaitTimeoutOutput:
    def test_unpinned_timeout_emits_json(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, RUNNING)
        result = runner.invoke(cli, [*UNPINNED_WAIT, "--wait-timeout", "10", "--json", "*"])
        assert result.exit_code == 1
        assert json.loads(result.stdout) == {
            "ai_task_id": "task-1",
            "commit_sha": None,
            "error_code": None,
            "recorded_at": None,
            "status": "timeout",
            "task_status": "running",
        }

    def test_pinned_timeout_emits_json(self, runner, calls):
        calls.route("GET", COMMIT_PATH, PRIOR_FOR_COMMIT)
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "already_running"})
        calls.route("GET", STATE_PATH, RUNNING)
        result = runner.invoke(cli, [*PINNED_WAIT, "--wait-timeout", "10", "--json", "ai_task_id,status,task_status"])
        assert result.exit_code == 1
        assert json.loads(result.stdout) == {"ai_task_id": "task-1", "status": "timeout", "task_status": "running"}

    def test_timeout_before_the_task_is_observed_reports_unknown(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, _http_error(503))
        result = runner.invoke(cli, [*UNPINNED_WAIT, "--wait-timeout", "10", "--json", "status,task_status"])
        assert result.exit_code == 1
        assert json.loads(result.stdout) == {"status": "timeout", "task_status": "unknown"}

    def test_human_timeout_says_the_task_was_not_cancelled(self, runner, calls):
        calls.route("POST", f"{BASE}/generate", {"ai_task_id": "task-1", "status": "generating"})
        calls.route("GET", STATE_PATH, RUNNING)
        result = runner.invoke(cli, [*UNPINNED_WAIT, "--wait-timeout", "10"])
        assert result.exit_code == 1
        assert "not cancelled" in result.output
