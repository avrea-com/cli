"""Job results are read through the authorized repository and exact API cursor."""

from avrea_cli.main import cli
from click.testing import CliRunner
from tests.conftest import FakeApi
from typing import Any
import json
import pytest

ORG = "org-019a0000000070008000000000000001"
REPO = "rep-019a0000000070008000000000000002"
JOB = "job-019a0000000070008000000000000003"
EXECUTION = "exe-019a0000000070008000000000000004"
JOB_PATH = f"/orgs/{ORG}/jobs/{JOB}"
RESULTS_PATH = f"/orgs/{ORG}/repos/{REPO}/jobs/{JOB}/tests"
SCOPE = ["--org", ORG]
ROW = {
    "test_id": "a" * 64,
    "suite": "checkout",
    "class_name": "TestCheckout",
    "name": "test_payment",
    "outcome": "failed",
    "duration_ms": 21,
    "failure_summary": "expected 200, got 500",
}


def page(rows: list[dict[str, Any]], *, cursor: str | None = None, **changes: Any) -> dict[str, Any]:
    return {
        "job_id": JOB,
        "execution_id": EXECUTION,
        "status": "available",
        "reporting_state": "complete",
        "reason": None,
        "commit_sha": "b" * 40,
        "observed_at": "2026-10-28T12:00:00Z",
        "total_count": 3,
        "data": rows,
        "pagination": {"next_cursor": cursor},
        **changes,
    }


@pytest.fixture
def job_api(api: FakeApi) -> FakeApi:
    api.reply("GET", JOB_PATH, json={"data": {"job_id": JOB, "repository_id": REPO, "job_name": "Unit tests"}})
    return api


def test_job_short_and_empty_pages_continue_without_dropping_repeats(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], cursor="first"))
    job_api.reply("GET", RESULTS_PATH, json=page([], cursor="second"))
    job_api.reply("GET", RESULTS_PATH, json=page([ROW, {**ROW, "outcome": "passed"}]))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--output", "json"])
    assert result.exit_code == 0, result.output
    data = json.loads(result.output)
    assert data["organization_id"] == ORG
    assert data["repository_id"] == REPO
    assert data["job_id"] == JOB
    assert data["execution_id"] == EXECUTION
    assert data["data"] == [ROW, ROW, {**ROW, "outcome": "passed"}]
    assert data["pagination"]["next_cursor"] is None
    assert [r.url.params.get("cursor") for r in job_api.sent("GET", RESULTS_PATH)] == [None, "first", "second"]


@pytest.mark.parametrize(
    "state", ["pending", "missing", "skipped", "no_report", "partial", "complete", "failed", "not_recorded"]
)
def test_job_reporting_is_separate_from_availability(runner: CliRunner, job_api: FakeApi, state: str) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([], reporting_state=state, status="not_recorded", total_count=None))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--json", "*"])
    assert result.exit_code == 0, result.output
    data = json.loads(result.output)
    assert data["reporting_state"] == state
    assert data["status"] == "not_recorded"
    assert data["total_count"] is None


def test_job_text_shows_failure_and_escapes_terminal_controls(runner: CliRunner, job_api: FakeApi) -> None:
    row = {**ROW, "name": "test_\x1b[31mpayment", "failure_summary": "bad\r\nline\x1b[2J"}
    job_api.reply("GET", RESULTS_PATH, json=page([row]))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE])
    assert result.exit_code == 0, result.output
    assert "payment" in result.output
    assert "failed" in result.output
    assert "21" in result.output
    assert "bad\\r\\nline" in result.output
    assert "\x1b" not in result.output
    assert "Reporting" in result.output


@pytest.mark.parametrize("code", [403, 404, 409, 429, 503])
def test_job_page_failure_never_emits_partial_success(runner: CliRunner, job_api: FakeApi, code: int) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], cursor="next"))
    job_api.reply("GET", RESULTS_PATH, status=code, json={"detail": "Report changed or unavailable"})
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--output", "json"])
    assert result.exit_code != 0
    assert not result.stdout


def test_explicit_repository_does_not_require_job_metadata(runner: CliRunner, api: FakeApi) -> None:
    api.reply("GET", RESULTS_PATH, json=page([ROW]))
    result = runner.invoke(cli, ["tests", "list", JOB, *SCOPE, "--repo", REPO, "--json", "data,status"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.stdout) == {"data": [ROW], "status": "available"}
    assert len(api.requests) == 1


def test_limit_preserves_next_cursor_and_request_sizes(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], cursor="one"))
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], cursor="two"))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--limit", "2", "--json", "*"])
    assert result.exit_code == 0, result.output
    response = json.loads(result.stdout)
    assert len(response["data"]) == 2
    assert response["pagination"] == {"next_cursor": "two"}
    assert "--cursor two" in result.stderr
    assert [dict(r.url.params) for r in job_api.sent("GET", RESULTS_PATH)] == [
        {"limit": "2"},
        {"limit": "1", "cursor": "one"},
    ]


def test_page_budget_returns_a_resume_cursor(runner: CliRunner, job_api: FakeApi) -> None:
    for i in range(20):
        job_api.reply("GET", RESULTS_PATH, json=page([], cursor=f"page-{i}"))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--json", "*"])
    assert result.exit_code == 0, result.output
    assert len(job_api.sent("GET", RESULTS_PATH)) == 20
    assert json.loads(result.stdout)["pagination"]["next_cursor"] == "page-19"
    assert "page-19" in result.stderr


@pytest.mark.parametrize(
    "changes",
    [
        {"execution_id": "exe-019a0000000070008000000000000005"},
        {"status": "expired"},
        {"total_count": 4},
        {"job_id": "job-019a0000000070008000000000000006"},
    ],
)
def test_changed_job_pages_are_not_combined(runner: CliRunner, job_api: FakeApi, changes: dict[str, Any]) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], cursor="next"))
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], **changes))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--json", "*"])
    assert result.exit_code != 0
    assert not result.stdout


@pytest.mark.parametrize(
    "bad_page",
    [
        page([ROW], cursor="next"),
        page([ROW], cursor=" "),
        page([ROW], pagination={"next_cursor": 17}),
        page([ROW], data="invalid"),
    ],
)
def test_invalid_pagination_stops_without_partial_output(
    runner: CliRunner, job_api: FakeApi, bad_page: dict[str, Any]
) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], cursor="next"))
    job_api.reply("GET", RESULTS_PATH, json=bad_page)
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--json", "*"])
    assert result.exit_code != 0
    assert not result.stdout
    assert len(job_api.sent("GET", RESULTS_PATH)) == 2


def test_cursor_resume_uses_the_exact_value(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW]))
    cursor = "x" * 900
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--cursor", cursor, "--output", "json"])
    assert result.exit_code == 0, result.output
    assert job_api.sent("GET", RESULTS_PATH)[0].url.params["cursor"] == cursor


@pytest.mark.parametrize(
    "args",
    [
        ["list", "not-a-job"],
        [JOB, "--cursor", " "],
        [JOB, "--cursor", "x" * 1025],
        [JOB, "--limit", "0"],
        [JOB, "--limit", "1001"],
        [JOB, "--json", "capture_id"],
        [JOB, "--output", "text", "--json", "*"],
        [JOB, "--jq", ".data"],
    ],
)
def test_invalid_options_make_no_requests(runner: CliRunner, api: FakeApi, args: list[str]) -> None:
    result = runner.invoke(cli, ["tests", *args, *SCOPE])
    assert result.exit_code != 0
    assert api.requests == []


@pytest.mark.parametrize(
    "state,total,message",
    [
        ("expired", None, "Missing or expired reports"),
        ("not_recorded", None, "Missing or expired reports"),
        ("available", 0, "retained report contains zero"),
    ],
)
def test_empty_results_explain_availability(
    runner: CliRunner, job_api: FakeApi, state: str, total: int | None, message: str
) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([], status=state, total_count=total))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE])
    assert result.exit_code == 0, result.output
    assert message in result.stdout
    assert state in result.stdout
    assert len(job_api.sent("GET", RESULTS_PATH)) == 1


def test_wrong_organization_does_not_fall_back_to_repository_scan(runner: CliRunner, api: FakeApi) -> None:
    api.reply("GET", JOB_PATH, status=404, json={"detail": "Job not found"})
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--json", "*"])
    assert result.exit_code != 0
    assert not result.stdout
    assert len(api.requests) == 1


def test_json_preserves_raw_test_names_and_failure_text(runner: CliRunner, job_api: FakeApi) -> None:
    row = {**ROW, "name": "line\nbreak", "failure_summary": "\x1b[31merror"}
    job_api.reply("GET", RESULTS_PATH, json=page([row]))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--output", "json"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.stdout)["data"] == [row]


def test_help_only_advertises_the_shipped_job_journey(runner: CliRunner, api: FakeApi) -> None:
    result = runner.invoke(cli, ["tests", "--help"])
    assert result.exit_code == 0, result.output
    assert "avr tests JOB" in result.stdout
    assert "history" not in result.stdout
    assert "view" not in result.stdout
    assert api.requests == []
