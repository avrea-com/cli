"""Verify inspection comparisons detect data loss and terminal omissions."""

from copy import deepcopy
from scripts.compare_inspection import compare
import json
import pytest
import sys


def capture(event):
    return {
        "exit": 0,
        "stdout": json.dumps(event) + "\n",
        "stderr": "",
        "hosts": None,
        "requests": [],
        "browser": None,
        "mode": None,
    }


@pytest.mark.parametrize("replacement", ["12.125", None, 12.5, True])
def test_watch_event_values_and_types_are_strict(replacement):
    event = {
        "event": "run_completed",
        "timestamp": "2026-10-29T12:00:00Z",
        "run_id": "run-019a0000000070008000000000000003",
        "duration_seconds": 12.125,
    }
    python = capture(event)
    go = capture(event | {"duration_seconds": replacement})
    scenario = {"name": "events", "args": ["run", "watch"], "events": ["run_completed"]}
    assert compare(scenario, python, go)


def test_watch_timestamp_variation_is_allowed_but_required():
    event = {"event": "run_completed", "timestamp": "2026-10-29T12:00:00Z"}
    python = capture(event)
    go = capture(event | {"timestamp": "2026-10-29T12:00:01.123Z"})
    scenario = {"name": "events", "args": ["run", "watch"], "events": ["run_completed"]}
    assert not compare(scenario, python, go)
    go = capture({"event": "run_completed"})
    assert "invalid NDJSON event" in compare(scenario, python, go)


def test_event_duplicates_and_missing_events_are_detected():
    python = capture({"event": "job_started", "timestamp": "2026-10-29T12:00:00Z"})
    go = deepcopy(python)
    go["stdout"] *= 2
    scenario = {"name": "events", "args": ["run", "watch"], "events": ["job_started"]}
    assert "event sequence" in compare(scenario, python, go)
    go["stdout"] = ""
    assert "event sequence" in compare(scenario, python, go)


def test_terminal_comparison_requires_all_information_and_filters():
    python = capture({})
    python["stdout"] = "Title: Build web\nJOB Unit tests 12s\n"
    go = deepcopy(python)
    go["stdout"] = "Build web\nUnit tests\n12s\n"
    scenario = {
        "name": "view",
        "args": ["run", "view"],
        "stdout_terms": ["Build web", "Unit tests", "12s"],
        "stdout_absent": ["hidden diagnostic"],
    }
    assert not compare(scenario, python, go)
    go["stdout"] = "Build web\nUnit tests\n"
    assert "stdout missing: 12s" in compare(scenario, python, go)
    go["stdout"] = python["stdout"] + "hidden diagnostic"
    assert "stdout contains: hidden diagnostic" in compare(scenario, python, go)


@pytest.mark.skipif(sys.platform == "win32", reason="POSIX PTY")
def test_terminal_capture_drains_large_output_before_child_exit(tmp_path):
    from scripts.compare_auth import run

    expected = "terminal output " * 2000
    scenario = {"args": ["-c", "import sys; sys.stdout.write(" + repr(expected) + ")"], "tty": True}
    result = run([sys.executable], scenario, tmp_path, "http://127.0.0.1")
    assert result["exit"] == 0
    assert result["stdout"] == expected


def test_hidden_link_target_cannot_replace_visible_information():
    python = capture({})
    python["stdout"] = "job-id"
    go = deepcopy(python)
    go["stdout"] = "\x1b]8;;https://example.test/job-id\x1b\\label\x1b]8;;\x1b\\"
    scenario = {"name": "visible IDs", "args": ["run", "view"], "stdout_terms": ["job-id"]}
    assert "stdout missing: job-id" in compare(scenario, python, go)


@pytest.mark.parametrize("codes", ["38;5;245", "4", "38;5;244;4", "38;5;245;4;1"])
def test_terminal_comparison_detects_missing_or_wrong_header_style(codes):
    python = capture({})
    python["stdout"] = "\x1b[38;5;245m\x1b[4mREPOSITORY\x1b[0m\n"
    go = deepcopy(python)
    go["stdout"] = f"\x1b[{codes}mREPOSITORY\x1b[0m\n"
    scenario = {"name": "styled repo", "args": ["repo", "list"], "presentation": True}
    assert "terminal text/styles" in compare(scenario, python, go)


def test_terminal_comparison_accepts_equivalent_escape_encoding_and_trailing_padding():
    python = capture({})
    python["stdout"] = "\x1b[36mrepo-id\x1b[0m  \n"
    go = deepcopy(python)
    go["stdout"] = "\x1b[38;5;6mrepo-id\x1b[39m\n"
    scenario = {"name": "styled repo", "args": ["repo", "list"], "presentation": True}
    assert not compare(scenario, python, go)


def test_terminal_comparison_detects_bad_indentation_and_truncation():
    python = capture({})
    python["stdout"] = "  REPOSITORY\n  long-repo...\n"
    go = deepcopy(python)
    go["stdout"] = "REPOSITORY\nlong-repo...\n"
    scenario = {"name": "styled repo", "args": ["repo", "list"], "presentation": True}
    assert "terminal layout" in compare(scenario, python, go)
    go["stdout"] = "  REPOSITORY\n  long-repository-name\n"
    assert "terminal text/styles" in compare(scenario, python, go)


@pytest.mark.parametrize("codes", ["90", "90;41", "90;100;49"])
def test_terminal_comparison_detects_missing_or_wrong_background(codes):
    python = capture({})
    python["stdout"] = "\x1b[90;100m░\x1b[0m"
    go = deepcopy(python)
    go["stdout"] = f"\x1b[{codes}m░\x1b[0m"
    scenario = {"name": "metric track", "args": ["job", "metrics"], "presentation": True}
    assert "terminal text/styles" in compare(scenario, python, go)


def test_terminal_comparison_accepts_equivalent_background_encoding():
    python = capture({})
    python["stdout"] = "\x1b[90;100m░\x1b[0mX"
    go = deepcopy(python)
    go["stdout"] = "\x1b[38;5;8;48;5;8m░\x1b[39;49mX"
    scenario = {"name": "metric track", "args": ["job", "metrics"], "presentation": True}
    assert not compare(scenario, python, go)
