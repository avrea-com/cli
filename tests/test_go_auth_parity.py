"""Ensure the differential gate catches material changes, not just happy paths."""

from copy import deepcopy
from scripts.compare_auth import compare
from scripts.compare_auth import run_captured
from scripts.compare_auth import windows_literal_json_args
import pytest
import sys


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


def test_json_wildcard_is_literal_without_rewriting_other_arguments():
    assert windows_literal_json_args(["auth", "status", "--json", "*"]) == ["auth", "status", "--json=*"]
    args = ["--json=email", "--jq", "*", "--json", "email", "*"]
    assert windows_literal_json_args(args) == args


def test_protocol_input_bytes_are_not_translated_by_the_host():
    value = "protocol=https\nhost=mirror.test\n\n"
    result = run_captured(
        [sys.executable, "-c", "import sys; print(sys.stdin.buffer.read().hex())"],
        stdin=value,
        timeout=5,
    )
    assert result.returncode == 0
    assert result.stdout.strip() == value.encode("utf-8").hex()


def test_captured_output_is_utf8_with_terminal_newline_normalization():
    result = run_captured(
        [sys.executable, "-c", "import sys; sys.stdout.buffer.write(bytes.fromhex('e29c930d0a'))"],
        stdin="",
        timeout=5,
    )
    assert result.returncode == 0
    assert result.stdout == "✓\n"


@pytest.mark.parametrize("changed", ["a b\tc\n", "a\tb c\n", "a\tb\nc\n"])
def test_tsv_column_and_row_changes_are_detected(changed):
    python = captured()
    python["stdout"] = "a\tb\tc\n"
    go = deepcopy(python)
    go["stdout"] = changed
    assert "TSV rows/columns" in compare({"args": ["cache", "list"], "tsv": True}, python, go)


def test_notification_loss_is_detected_before_terminal_normalization():
    from scripts.compare_inspection import compare as inspection_compare

    python = captured()
    python["stdout"] = "completed\x1b]9;Build: success\a"
    go = deepcopy(python)
    go["stdout"] = "completed"
    assert "terminal completion notifications" in inspection_compare(
        {"name": "notification fixture", "args": ["run", "watch"], "stdout_terms": ["completed"]}, python, go
    )


def test_windows_oracle_pipe_exception_does_not_hide_go_errors(monkeypatch):
    from scripts import compare_inspection as inspection

    monkeypatch.setattr(inspection.sys, "platform", "win32")
    python = captured()
    python["exit"] = 120
    python["stderr"] = "sys.stdout.flush()\nOSError: [Errno 22] Invalid argument\n"
    go = deepcopy(python)
    go["exit"], go["stderr"] = 0, ""
    scenario = {"name": "pipe fixture", "args": ["run", "watch"], "python_windows_pipe_flush_bug": True}
    assert not inspection.compare(scenario, python, go)
    go["exit"] = 1
    assert inspection.compare(scenario, python, go)
    go["exit"] = 0
    python["stderr"] = "unexpected failure"
    assert inspection.compare(scenario, python, go)
