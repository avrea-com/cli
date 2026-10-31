"""Ensure the differential gate catches material changes, not just happy paths."""

from copy import deepcopy
from scripts.compare_auth import compare
from scripts.compare_auth import run_captured
from scripts.compare_auth import windows_literal_json_args
from typing import Any
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


def test_hidden_prompt_oracle_uses_standard_fallback(monkeypatch):
    from scripts import parity_prompt_oracle
    import getpass

    original = getpass.getpass
    fallback = getpass.fallback_getpass  # ty: ignore[unresolved-attribute] - omitted by stdlib stubs
    seen = []
    monkeypatch.setattr(
        parity_prompt_oracle.runpy,
        "run_module",
        lambda name, run_name: seen.append((name, run_name, getpass.getpass)),
    )
    parity_prompt_oracle.main()
    assert seen == [("avrea_cli.main", "__main__", fallback)]
    assert getpass.getpass is original


def test_ssh_text_newline_tolerance_does_not_hide_content_or_binary_changes(tmp_path):
    from scripts import compare_auth

    def capture(payload, normalize):
        script = "from pathlib import Path; Path('config.txt').write_bytes(" + repr(payload) + ")"
        scenario = {"args": [], "capture_files": ["config.txt"], "normalize_file_newlines": normalize}
        return compare_auth.run([sys.executable, "-c", script], scenario, tmp_path, "http://127.0.0.1")["files"]

    expected = capture(b"Host fixture\n", True)
    assert capture(b"Host fixture\r\n", True) == expected
    assert capture(b"Host changed\r\n", True) != expected
    assert capture(b"Host fixture\r\n", False) != expected


@pytest.mark.parametrize("source", ["getpass.py", "termui.py"])
def test_hidden_prompt_warning_location_is_not_command_output(source):
    from scripts import compare_billing_card
    from scripts import compare_gitlab

    python = captured()
    go = deepcopy(python)
    python["stderr"] = (
        f"/fixture/{source}:82: GetPassWarning: Can not control echo on the terminal.\n"
        "  return getpass.getpass(prompt)\nWarning: Password input may be echoed.\nToken: "
    )
    go["stderr"] = "Warning: Password input may be echoed.\nToken: \n"
    scenario = {"name": "hidden prompt", "args": [], "hidden_prompt": True}
    for comparator in (compare_billing_card.compare, compare_gitlab.compare):
        assert not comparator(scenario, python, go)
        changed = deepcopy(go)
        changed["stderr"] += "unexpected warning"
        assert "stderr" in comparator(scenario, python, changed)


def test_bootstrap_terminal_newlines_keep_script_content_checks():
    from scripts import compare_vm_bootstrap

    python = captured()
    go = deepcopy(python)
    python["stdout"] = "echo one\r\r\n"
    go["stdout"] = "echo one\r\n"
    scenario = {"name": "bootstrap print", "args": ["vm", "bootstrap", "--print"], "tty": True}
    assert not compare_vm_bootstrap.compare(scenario, python, go)
    go["stdout"] = "echo  one\r\n"
    assert "printed script content differs" in compare_vm_bootstrap.compare(scenario, python, go)
    go["stdout"] = "echo one\r\n"
    scenario["tty"] = False
    assert "printed script content differs" in compare_vm_bootstrap.compare(scenario, python, go)


def test_only_redundant_successful_organization_reads_can_be_removed():
    python = captured()
    request = {"method": "GET", "path": "/users/me/organizations", "authorization": "Bearer fixture", "body": None}
    python["requests"] = [request, {"method": "GET", "path": "/repos", "body": None}, deepcopy(request)]
    go = deepcopy(python)
    go["requests"].pop()
    scenario: dict[str, Any] = {
        "args": ["repo", "list"],
        "responses": {("GET", "/users/me/organizations"): (200, {"data": []})},
    }
    assert not compare(scenario, python, go)
    # Additional Go reads, missing non-org reads, changed credentials and failures
    # must not disappear under the performance exception.
    assert "requests" in compare(scenario, go, python)
    for mutation in ("missing", "auth", "write", "error", "moving"):
        case, changed = deepcopy(scenario), deepcopy(go)
        if mutation == "missing":
            changed["requests"].pop()
        elif mutation == "auth":
            changed["requests"][0]["authorization"] = "Bearer other"
        elif mutation == "write":
            changed["requests"][1]["method"] = "POST"
        elif mutation == "error":
            case["responses"][("GET", "/users/me/organizations")] = (503, {})
        else:
            case["responses"][("GET", "/users/me/organizations")] = [(200, {}), (200, {})]
        assert "requests" in compare(case, python, changed)


def test_org_lookup_reuse_preserves_log_search_body_and_types():
    python = captured()
    org = {"method": "GET", "path": "/users/me/organizations", "body": None}
    search = {"method": "POST", "path": "/logs/search", "body": {"limit": 1}}
    python["requests"] = [org, search, deepcopy(org)]
    go = deepcopy(python)
    go["requests"].pop()
    scenario = {"args": ["job", "view"], "responses": {("GET", "/users/me/organizations"): (200, {"data": []})}}
    assert not compare(scenario, python, go)
    go["requests"][1]["body"]["limit"] = True
    assert "requests" in compare(scenario, python, go)


def test_org_lookup_reuse_never_removes_or_changes_writes():
    python = captured()
    org = {"method": "GET", "path": "/users/me/organizations", "body": None}
    write = {"method": "POST", "path": "/runs/run-fixture/cancel", "body": {"force": False}}
    python["requests"] = [org, write, deepcopy(org)]
    go = deepcopy(python)
    go["requests"].pop()
    scenario = {"args": ["run", "cancel"], "responses": {("GET", "/users/me/organizations"): (200, {"data": []})}}
    assert not compare(scenario, python, go)
    go["requests"][1]["body"]["force"] = True
    assert "requests" in compare(scenario, python, go)
    go["requests"].pop()
    assert "requests" in compare(scenario, python, go)
