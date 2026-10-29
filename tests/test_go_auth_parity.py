"""Ensure the differential gate catches material changes, not just happy paths."""

from copy import deepcopy
from scripts.compare_auth import compare
import pytest


def captured():
    return {
        "exit": 0,
        "stdout": '{"email":null,"items":[1,2]}\n',
        "stderr": "",
        "hosts": {"hosts": {"fixture": {"token": "fixture-only", "enabled": False}}},
        "requests": [{"method": "GET", "path": "/users/me", "body": None}],
        "browser": None,
        "mode": 0o600,
    }


@pytest.mark.parametrize(
    "changed",
    [
        '{"items":[1,2]}',
        '{"email":null,"items":[2,1]}',
        '{"email":null,"items":[true,2]}',
        '{"email":null,"items":[1,3]}',
    ],
)
def test_structured_regressions_are_detected(changed):
    python = captured()
    go = deepcopy(python)
    go["stdout"] = changed
    assert "JSON values" in compare({"args": ["auth", "status"], "json": True}, python, go)


def test_protocol_bytes_are_not_normalized():
    python = captured()
    python["stdout"] = "username=fixture\npassword=fixture\n\n"
    go = deepcopy(python)
    go["stdout"] = python["stdout"].rstrip() + "\n"
    assert "Git protocol stdout" in compare({"args": ["auth", "git-credential"]}, python, go)


def test_human_spacing_is_allowed_but_missing_information_is_not():
    python = captured()
    python["stdout"] = "Email   fixture@example.test\n"
    go = deepcopy(python)
    go["stdout"] = "Email fixture@example.test\n"
    scenario = {"args": ["auth", "status"]}
    assert not compare(scenario, python, go)
    go["stdout"] = "Email\n"
    assert "stdout" in compare(scenario, python, go)


def test_api_and_local_state_regressions_are_detected():
    python = captured()
    go = deepcopy(python)
    go["requests"][0]["path"] = "/wrong"
    go["hosts"]["hosts"]["fixture"]["enabled"] = 0
    differences = compare({"args": ["auth", "status"], "json": True}, python, go)
    assert "requests" in differences
    assert "hosts" in differences
