"""Customer test-evidence commands preserve scope and bounded API responses."""

from avrea_cli.main import cli
from click.testing import CliRunner
from tests.conftest import FakeApi
from typing import Any
import json
import pytest

ORG = "org-019a0000000070008000000000000001"
REPO = "rep-019a0000000070008000000000000002"
TEST = "a" * 64
CONFIG = "b" * 64
CAPTURE = "tec-019a0000000070008000000000000003"
BASE = f"/orgs/{ORG}/repos/{REPO}/tests"
SCOPE = ["--org", ORG, "--repo", REPO]
PAGE: dict[str, Any] = {
    "status": "available",
    "pending": True,
    "reason": None,
    "as_of_revision": 1,
    "current_revision": 2,
    "data": [{"test_id": TEST, "configuration_digest": CONFIG, "failed_count": 1, "passed_count": 2}],
    "pagination": {"next_cursor": "c" * 900},
}


def test_list_preserves_page_status_and_context(runner: CliRunner, api: FakeApi) -> None:
    api.reply("GET", f"{BASE}/history", json=PAGE)
    result = runner.invoke(cli, ["tests", "list", *SCOPE, "--limit", "7", "--cursor", "c" * 900, "--json", "*"])
    assert result.exit_code == 0, result.output
    response = json.loads(result.output)
    assert response["organization_id"] == ORG
    assert response["repository_id"] == REPO
    assert response["data"] == PAGE["data"]
    assert response["pending"] is True
    assert response["pagination"] == PAGE["pagination"]
    assert len(api.requests) == 1
    assert dict(api.requests[0].url.params) == {"limit": "7", "cursor": "c" * 900}


def test_history_passes_exact_configuration(runner: CliRunner, api: FakeApi) -> None:
    api.reply("GET", f"{BASE}/history/{TEST}", json={**PAGE, "data": [{"capture_id": CAPTURE}]})
    result = runner.invoke(cli, ["tests", "history", TEST, "--configuration", CONFIG, *SCOPE, "--json", "*"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.output)["configuration_digest"] == CONFIG
    assert api.requests[0].url.params["configuration"] == CONFIG


@pytest.mark.parametrize("state", ["available", "expired", "not_recorded"])
def test_view_keeps_exact_capture_without_fallback(runner: CliRunner, api: FakeApi, state: str) -> None:
    rows = (
        [{"name": "test_checkout", "outcome": "failed", "failure_summary": "expected 200, got 500"}]
        if state == "available"
        else []
    )
    api.reply(
        "GET",
        f"{BASE}/history/{TEST}/detail/{CAPTURE}",
        json={"status": state, "data": rows, "truncated": state == "available"},
    )
    result = runner.invoke(cli, ["tests", "view", TEST, "--capture", CAPTURE, *SCOPE, "--limit", "500", "--json", "*"])
    assert result.exit_code == 0, result.output
    response = json.loads(result.output)
    assert (response["test_id"], response["capture_id"], response["status"]) == (TEST, CAPTURE, state)
    assert response["data"] == rows
    assert response["truncated"] == (state == "available")
    assert len(api.requests) == 1


@pytest.mark.parametrize("status", [403, 404, 409, 503])
def test_denied_or_failed_reads_are_not_empty_success(runner: CliRunner, api: FakeApi, status: int) -> None:
    api.reply("GET", f"{BASE}/history", status=status, json={"detail": "Evidence is unavailable"})
    result = runner.invoke(cli, ["tests", "list", *SCOPE, "--json", "*"])
    assert result.exit_code != 0
    assert len(api.requests) == 1
    assert "Evidence is unavailable" in result.output


def test_status_reads_control_without_mutation(runner: CliRunner, api: FakeApi) -> None:
    state = {"capture_enabled": False, "can_manage": True, "repository_generation": 2, "purges": [], "events": []}
    api.reply("GET", f"{BASE}/control", json=state)
    result = runner.invoke(cli, ["tests", "status", *SCOPE, "--json", "*"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.output)["data"] == state
    assert len(api.requests) == 1


def test_captures_preserves_missing_report_reason(runner: CliRunner, api: FakeApi) -> None:
    rows = [{"capture_id": CAPTURE, "state": "no_report", "reason": "no_supported_report"}]
    api.reply("GET", f"{BASE}/captures", json={"data": rows, "pagination": {"next_cursor": None}})
    result = runner.invoke(cli, ["tests", "captures", *SCOPE, "--json", "*"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.output)["data"] == rows


def test_name_terminal_controls_are_escaped(runner: CliRunner, api: FakeApi) -> None:
    row = {**PAGE["data"][0], "identity": {"name": "test_\x1b[31mcheckout"}}
    api.reply("GET", f"{BASE}/history", json={**PAGE, "data": [row]})
    result = runner.invoke(cli, ["tests", "list", *SCOPE])
    assert result.exit_code == 0, result.output
    assert "\x1b" not in result.output
    assert "checkout" in result.output


@pytest.mark.parametrize(
    "args",
    [
        ["list", "--cursor", " "],
        ["history", "bad", "--configuration", CONFIG],
        ["view", TEST, "--capture", "bad"],
        ["list", "--json", "no_such_field"],
    ],
)
def test_bad_arguments_make_no_requests(runner: CliRunner, api: FakeApi, args: list[str]) -> None:
    result = runner.invoke(cli, ["tests", *args, *SCOPE])
    assert result.exit_code != 0
    assert api.requests == []


def test_empty_capture_page_and_partial_detail_are_explicit(runner: CliRunner, api: FakeApi) -> None:
    api.reply("GET", f"{BASE}/captures", json={"data": [], "pagination": {"next_cursor": None}})
    result = runner.invoke(cli, ["tests", "captures", *SCOPE])
    assert result.exit_code == 0, result.output
    assert "No recorded results in this page" in result.output
    api.reply(
        "GET", f"{BASE}/history/{TEST}/detail/{CAPTURE}", json={"status": "available", "data": [], "truncated": True}
    )
    result = runner.invoke(cli, ["tests", "view", TEST, "--capture", CAPTURE, *SCOPE, "--limit", "500"])
    assert result.exit_code == 0, result.output
    assert "This result is partial" in result.output
    assert "Increase --limit" not in result.output
