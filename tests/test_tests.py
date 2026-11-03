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
NATIVE_JOB = "awj-019a0000000070008000000000000007"
EXECUTION = "exe-019a0000000070008000000000000004"
JOB_PATH = f"/orgs/{ORG}/jobs/{JOB}"
RESULTS_PATH = f"/orgs/{ORG}/repos/{REPO}/jobs/{JOB}/tests"
SCOPE = ["--org", ORG]
ROW: dict[str, Any] = {
    "result_id": "result-one",
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
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--json", "*"])
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
    assert "Complete describes the collected reports" in result.output


def test_job_text_shows_the_reporting_reason(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], reporting_state="partial", reason="upload_failed"))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE])
    assert result.exit_code == 0, result.output
    assert "Reason: upload_failed" in result.stdout
    assert "Complete describes" not in result.stdout


@pytest.mark.parametrize("code", [403, 404, 409, 429, 503])
def test_job_page_failure_never_emits_partial_success(runner: CliRunner, job_api: FakeApi, code: int) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], cursor="next"))
    job_api.reply("GET", RESULTS_PATH, status=code, json={"detail": "Report changed or unavailable"})
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--json", "*"])
    assert result.exit_code == 1
    assert f"HTTP {code}" in result.stderr
    assert not result.stdout


def test_results_not_found_says_what_to_check(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply("GET", RESULTS_PATH, status=404, json={"detail": "Not Found"})
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--cursor", "stale"])
    assert result.exit_code == 1
    assert "restart without it" in result.stderr
    assert "Repository Tests are enabled" in result.stderr


def test_native_job_ids_are_read_like_job_ids(runner: CliRunner, api: FakeApi) -> None:
    native_results = f"/orgs/{ORG}/repos/{REPO}/jobs/{NATIVE_JOB}/tests"
    api.reply("GET", f"/orgs/{ORG}/jobs/{NATIVE_JOB}", json={"data": {"job_id": NATIVE_JOB, "repository_id": REPO}})
    api.reply("GET", native_results, json={**page([ROW]), "job_id": NATIVE_JOB})
    result = runner.invoke(cli, ["tests", NATIVE_JOB, *SCOPE, "--json", "job_id,data"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.stdout) == {"job_id": NATIVE_JOB, "data": [ROW]}
    assert len(api.sent("GET", native_results)) == 1


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


def test_large_limit_is_read_in_one_request_when_the_page_holds_it(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW] * 1000, total_count=1000))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--limit", "1000", "--json", "*"])
    assert result.exit_code == 0, result.output
    assert len(json.loads(result.stdout)["data"]) == 1000
    assert [dict(r.url.params) for r in job_api.sent("GET", RESULTS_PATH)] == [{"limit": "1000"}]


def test_failure_messages_name_the_class_of_same_named_tests(runner: CliRunner, job_api: FakeApi) -> None:
    rows = [
        {**ROW, "class_name": "TestCart", "name": "test_init", "failure_summary": "cart failed"},
        {**ROW, "class_name": "TestOrder", "name": "test_init", "failure_summary": "order failed"},
    ]
    job_api.reply("GET", RESULTS_PATH, json=page(rows))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE])
    assert result.exit_code == 0, result.output
    assert "TestCart.test_init: cart failed" in result.stdout
    assert "TestOrder.test_init: order failed" in result.stdout
    assert "avr tests view" not in result.stdout


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
        {"reporting_state": "partial"},
        {"reason": "upload_failed"},
        {"commit_sha": "c" * 40},
        {"observed_at": "2026-10-28T12:05:00Z"},
        {"total_count": 4},
    ],
)
def test_changed_job_pages_are_not_combined(runner: CliRunner, job_api: FakeApi, changes: dict[str, Any]) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], cursor="next"))
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], **changes))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--json", "*"])
    assert result.exit_code == 1
    assert "Job results changed while reading pages" in result.stderr
    assert not result.stdout


@pytest.mark.parametrize("page_number", [0, 1])
def test_a_page_for_another_job_is_rejected(runner: CliRunner, job_api: FakeApi, page_number: int) -> None:
    other = {"job_id": "job-019a0000000070008000000000000006"}
    pages = [page([ROW], cursor="next"), page([ROW])]
    pages[page_number] = {**pages[page_number], **other}
    for body in pages[: page_number + 1]:
        job_api.reply("GET", RESULTS_PATH, json=body)
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--json", "*"])
    assert result.exit_code == 1
    assert "invalid job test results" in result.stderr
    assert not result.stdout


def test_a_page_larger_than_requested_is_rejected(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW, ROW, ROW]))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--limit", "2", "--json", "*"])
    assert result.exit_code == 1
    assert "invalid test result page" in result.stderr
    assert not result.stdout


def test_job_lookup_for_another_job_is_rejected(runner: CliRunner, api: FakeApi) -> None:
    other = "job-019a0000000070008000000000000006"
    api.reply("GET", JOB_PATH, json={"data": {"job_id": other, "repository_id": REPO}})
    result = runner.invoke(cli, ["tests", JOB, *SCOPE])
    assert result.exit_code == 1
    assert "returned a different job" in result.stderr
    assert len(api.requests) == 1


@pytest.mark.parametrize(
    "bad_page,message",
    [
        (page([ROW], cursor="next"), "repeated a test-results cursor"),
        (page([ROW], cursor=" "), "invalid next cursor"),
        (page([ROW], cursor=""), "invalid next cursor"),
        (page([ROW], cursor="x" * 1025), "invalid next cursor"),
        (page([ROW], pagination={"next_cursor": 17}), "invalid next cursor"),
        (page([ROW], data="invalid"), "invalid job test results"),
    ],
)
def test_invalid_pagination_stops_without_partial_output(
    runner: CliRunner, job_api: FakeApi, bad_page: dict[str, Any], message: str
) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], cursor="next"))
    job_api.reply("GET", RESULTS_PATH, json=bad_page)
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--json", "*"])
    assert result.exit_code == 1
    assert message in result.stderr
    assert "--cursor" not in result.stderr
    assert not result.stdout
    assert len(job_api.sent("GET", RESULTS_PATH)) == 2


def test_cursor_resume_uses_the_exact_value(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW]))
    cursor = "x" * 900
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--cursor", cursor, "--json", "*"])
    assert result.exit_code == 0, result.output
    assert job_api.sent("GET", RESULTS_PATH)[0].url.params["cursor"] == cursor


@pytest.mark.parametrize(
    "args,exit_code",
    [
        (["list", "not-a-job"], 2),
        (["list", "awj-019a"], 2),
        ([JOB, "--cursor", " "], 2),
        ([JOB, "--cursor", "x" * 1025], 2),
        ([JOB, "--limit", "0"], 2),
        ([JOB, "--limit", "1001"], 2),
        ([JOB, "--output", "json"], 2),
        ([JOB, "--jq", ".data"], 2),
        ([JOB, "--json", "capture_id"], 1),
    ],
)
def test_invalid_options_make_no_requests(runner: CliRunner, api: FakeApi, args: list[str], exit_code: int) -> None:
    result = runner.invoke(cli, ["tests", *args, *SCOPE])
    assert result.exit_code == exit_code
    assert api.requests == []


@pytest.mark.parametrize(
    "state,total,message",
    [
        ("expired", None, "Missing or expired reports"),
        ("not_recorded", None, "Missing or expired reports"),
        ("available", 0, "retained report contains zero"),
        ("available", 3, "No more results."),
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
    assert ("Missing or expired" in result.stdout) == (state != "available")
    assert len(job_api.sent("GET", RESULTS_PATH)) == 1


def test_wrong_organization_does_not_fall_back_to_repository_scan(runner: CliRunner, api: FakeApi) -> None:
    api.reply("GET", JOB_PATH, status=404, json={"detail": "Job not found"})
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--json", "*"])
    assert result.exit_code == 1
    assert "read the job" in result.stderr
    assert "pass --org" in result.stderr
    assert not result.stdout
    assert len(api.requests) == 1


def test_json_preserves_raw_test_names_and_failure_text(runner: CliRunner, job_api: FakeApi) -> None:
    row = {**ROW, "name": "line\nbreak", "failure_summary": "\x1b[31merror"}
    job_api.reply("GET", RESULTS_PATH, json=page([row]))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--json", "*"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.stdout)["data"] == [row]


def test_help_only_advertises_the_shipped_job_journey(runner: CliRunner, api: FakeApi) -> None:
    result = runner.invoke(cli, ["tests", "--help"])
    assert result.exit_code == 0, result.output
    assert "avr tests RUN_OR_JOB" in result.stdout
    assert "history" not in result.stdout
    assert "view" not in result.stdout
    assert api.requests == []


def test_text_listing_keeps_result_handles_out_of_normal_workflow(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply(
        "GET",
        RESULTS_PATH,
        json=page([ROW, {**ROW, "result_id": "passed-result", "outcome": "passed", "failure_summary": None}]),
    )
    result = runner.invoke(cli, ["tests", JOB, *SCOPE])
    assert result.exit_code == 0, result.output
    assert "avr tests view" not in result.output
    assert "result-one" not in result.output


def test_job_filters_are_sent_and_failure_is_shown_without_result_lookup(runner: CliRunner, job_api: FakeApi) -> None:
    filters = {"name": ROW["name"], "suite": ROW["suite"], "class_name": ROW["class_name"], "outcome": "failed"}
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], filters=filters))
    result = runner.invoke(
        cli,
        [
            "tests",
            JOB,
            *SCOPE,
            "--test",
            ROW["name"],
            "--suite",
            ROW["suite"],
            "--class",
            ROW["class_name"],
            "--status",
            "failed",
        ],
    )
    assert result.exit_code == 0, result.output
    assert ROW["failure_summary"] in result.stdout
    assert "avr tests view" not in result.stdout
    assert "result-one" not in result.stdout
    params = job_api.sent("GET", RESULTS_PATH)[0].url.params
    for key, value in filters.items():
        assert params[key] == f"eq.{value}"


@pytest.mark.parametrize("applied", [None, {}, {"name": "different"}])
def test_cli_refuses_an_api_that_does_not_acknowledge_filters(
    runner: CliRunner, job_api: FakeApi, applied: Any
) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], filters=applied))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--test", ROW["name"]])
    assert result.exit_code != 0
    assert "did not confirm" in result.output
    assert ROW["failure_summary"] not in result.stdout


def test_filtered_search_budget_is_explicit_and_resumable(runner: CliRunner, job_api: FakeApi) -> None:
    for i in range(20):
        job_api.reply("GET", RESULTS_PATH, json=page([], cursor=f"scan-{i}", filters={"name": "absent"}))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--test", "absent"])
    assert result.exit_code == 0, result.output
    assert len(job_api.sent("GET", RESULTS_PATH)) == 20
    assert "No matches in the searched portion" in result.stdout
    assert "Search incomplete" in result.stderr
    assert "--cursor scan-19" in result.stderr
    assert "No more results" not in result.stdout


def test_filtered_search_follows_empty_pages_and_retains_duplicate_matches(runner: CliRunner, job_api: FakeApi) -> None:
    filters = {"name": ROW["name"], "suite": None, "class_name": None, "outcome": None}
    job_api.reply("GET", RESULTS_PATH, json=page([], cursor="next", filters=filters))
    job_api.reply("GET", RESULTS_PATH, json=page([ROW, ROW], filters=filters))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--test", ROW["name"], "--json", "*"])
    assert result.exit_code == 0, result.output
    body = json.loads(result.stdout)
    assert body["data"] == [ROW, ROW]
    assert body["pagination"]["next_cursor"] is None
    assert all(r.url.params["name"] == "eq." + ROW["name"] for r in job_api.sent("GET", RESULTS_PATH))


def test_filtered_final_empty_page_does_not_claim_missing_evidence(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([], filters={"name": "absent"}))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--test", "absent"])
    assert result.exit_code == 0, result.output
    assert "No matching tests in the remaining retained results" in result.stdout
    assert "Missing or expired" not in result.stdout


def test_native_job_omits_inapplicable_execution(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], execution_id=None))
    result = runner.invoke(cli, ["tests", JOB, *SCOPE])
    assert result.exit_code == 0, result.output
    assert "Execution:" not in result.stdout


def test_unfiltered_pagination_across_old_and_new_api_echoes(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], cursor="next"))
    job_api.reply(
        "GET",
        RESULTS_PATH,
        json=page([ROW], filters={"name": None, "suite": None, "class_name": None, "outcome": None}),
    )
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--json", "*"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.stdout)["data"] == [ROW, ROW]


def test_filter_cursor_mismatch_explains_recovery(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply("GET", RESULTS_PATH, status=400, json={"detail": "Invalid cursor"})
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--status", "failed", "--cursor", "previous"])
    assert result.exit_code != 0
    assert "same filters" in result.output
    assert "restart without --cursor" in result.output
    assert "Retained occurrences" not in result.stdout


RUN = "run-019a0000000070008000000000000008"
OTHER_JOB = "job-019a0000000070008000000000000009"
RUN_PATH = f"/orgs/{ORG}/workflow-runs/{RUN}"
JOBS = [{"job_id": JOB, "job_name": "Unit tests"}, {"job_id": OTHER_JOB, "job_name": "Integration tests"}]


def run_response(jobs: list[dict[str, Any]]) -> dict[str, Any]:
    return {"data": {"run_id": RUN, "run_attempt": 2, "repository": {"full_name": "acme/project"}, "jobs": jobs}}


@pytest.mark.parametrize(
    "reference,path",
    [
        (RUN, RUN_PATH),
        (RUN.replace("run-", "awr-"), RUN_PATH.replace("run-", "awr-")),
        ("12345", f"/orgs/{ORG}/workflow-runs/by-platform-id/12345"),
        ("https://github.com/acme/project/actions/runs/12345", f"/orgs/{ORG}/workflow-runs/by-platform-id/12345"),
        (f"https://console.avrea.com/org/acme/runs/{RUN}", RUN_PATH),
    ],
)
@pytest.mark.parametrize("explicit_list", [False, True])
def test_single_job_run_references(
    runner: CliRunner, job_api: FakeApi, reference: str, path: str, explicit_list: bool
) -> None:
    job_api.reply("GET", "/users/me/organizations", json={"data": [{"organization_id": ORG, "slug": "acme"}]})
    job_api.reply("GET", path, json=run_response(JOBS[:1]))
    job_api.reply("GET", RESULTS_PATH, json=page([ROW]))
    result = runner.invoke(cli, ["tests", *(["list"] if explicit_list else []), reference, *SCOPE, "--json", "*"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.stdout)["job_id"] == JOB
    assert json.loads(result.stdout)["data"] == [ROW]
    assert job_api.sent("GET", path)[0].url.params.get_list("include") == ["jobs"]
    assert len(job_api.sent("GET", RESULTS_PATH)) == 1


def test_run_job_name_is_case_insensitive_substring_and_keeps_test_filters(runner: CliRunner, job_api: FakeApi) -> None:
    job_api.reply("GET", RUN_PATH, json=run_response(JOBS))
    job_api.reply("GET", RESULTS_PATH, json=page([ROW], filters={"name": ROW["name"]}))
    result = runner.invoke(cli, ["tests", RUN, *SCOPE, "--job", "uNIt", "--test", ROW["name"]])
    assert result.exit_code == 0, result.output
    assert ROW["failure_summary"] in result.stdout
    assert job_api.sent("GET", RESULTS_PATH)[0].url.params["name"] == "eq." + ROW["name"]


@pytest.mark.parametrize("selection", [[], ["--job", "tests"], ["--job", "missing"]])
def test_run_requires_one_unambiguous_job(runner: CliRunner, api: FakeApi, selection: list[str]) -> None:
    api.reply("GET", RUN_PATH, json=run_response(JOBS))
    result = runner.invoke(cli, ["tests", RUN, *SCOPE, *selection, "--json", "*"])
    assert result.exit_code == 1
    assert not result.stdout
    assert "--job" in result.stderr
    assert "Unit tests" in result.stderr
    assert "Integration tests" in result.stderr
    assert JOB in result.stderr
    assert OTHER_JOB in result.stderr
    assert len(api.requests) == 1


@pytest.mark.parametrize("jobs", [[], [{"job_name": "Unit tests"}], [{"job_id": "invalid", "job_name": "Unit tests"}]])
def test_missing_job_never_reads_a_different_report(
    runner: CliRunner, api: FakeApi, jobs: list[dict[str, Any]]
) -> None:
    api.reply("GET", RUN_PATH, json=run_response(jobs))
    result = runner.invoke(cli, ["tests", RUN, *SCOPE, "--json", "*"])
    assert result.exit_code == 1
    assert not result.stdout
    assert len(api.requests) == 1


def test_job_selector_with_job_id_is_rejected_before_requests(runner: CliRunner, api: FakeApi) -> None:
    result = runner.invoke(cli, ["tests", JOB, *SCOPE, "--job", "Unit"])
    assert result.exit_code == 2
    assert "--job" in result.stderr
    assert api.requests == []


def test_attempt_url_selects_the_requested_attempt(runner: CliRunner, job_api: FakeApi) -> None:
    path = f"/orgs/{ORG}/workflow-runs"
    old = run_response(JOBS[:1])["data"]
    job_api.reply("GET", path, json={"data": [{**old, "run_attempt": 3, "jobs": JOBS[1:]}, old]})
    job_api.reply("GET", RESULTS_PATH, json=page([ROW]))
    result = runner.invoke(cli, ["tests", "https://github.com/acme/project/actions/runs/12345/attempts/2", *SCOPE])
    assert result.exit_code == 0, result.output
    assert job_api.sent("GET", path)[0].url.params["platform_run_id"] == "12345"
    assert len(job_api.sent("GET", RESULTS_PATH)) == 1


def test_console_url_cannot_override_selected_organization(runner: CliRunner, api: FakeApi) -> None:
    api.reply("GET", "/users/me/organizations", json={"data": [{"organization_id": ORG, "slug": "other"}]})
    result = runner.invoke(cli, ["tests", f"https://console.avrea.com/org/acme/runs/{RUN}", *SCOPE])
    assert result.exit_code == 1
    assert "does not match" in result.stderr
    assert len(api.requests) == 1


def test_github_url_cannot_select_a_different_repository(runner: CliRunner, api: FakeApi) -> None:
    api.reply("GET", f"/orgs/{ORG}/workflow-runs/by-platform-id/12345", json=run_response(JOBS[:1]))
    result = runner.invoke(cli, ["tests", "https://github.com/other/project/actions/runs/12345", *SCOPE])
    assert result.exit_code == 1
    assert "does not match" in result.stderr
    assert len(api.requests) == 1


def test_result_view_is_not_a_command(runner: CliRunner, api: FakeApi) -> None:
    result = runner.invoke(cli, ["tests", "view", "result-one"])
    assert result.exit_code == 2
    assert "No such command" in result.stderr
    assert api.requests == []


def test_wrong_environment_keeps_the_switch_environment_error(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv("AVR_HOST", "https://api.test.example.com")
    result = runner.invoke(cli, ["tests", f"https://console.avrea.com/org/acme/runs/{RUN}", *SCOPE])
    assert result.exit_code == 1
    assert "avr auth switch https://api.avrea.com" in result.stderr
    assert "RUN_OR_JOB" not in result.stderr
    assert api.requests == []


@pytest.mark.parametrize("target", ["not-a-job", "https://gitlab.com/acme/project/-/jobs/123"])
def test_invalid_targets_describe_test_run_and_job_references(runner: CliRunner, api: FakeApi, target: str) -> None:
    result = runner.invoke(cli, ["tests", "list", target, *SCOPE])
    assert result.exit_code == 2
    assert "RUN_OR_JOB" in result.stderr
    assert "Avrea run or job ID" in result.stderr
    assert "awj-" in result.stderr
    assert "avr job view" not in result.stderr
    assert api.requests == []
