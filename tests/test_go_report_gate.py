"""Exercise missing/empty/stale reports and deliberately mismatching processes."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts.validate_parity_reports import validate_report
from typing import Any
import json
import os
import pytest
import subprocess
import sys


def test_report_gate_rejects_false_greens():
    report = {
        "python_baseline": "baseline",
        "go_binary_sha256": "binary",
        "scenarios": [{"name": "case", "differences": []}],
    }
    validate_report(report, 1, "baseline", "binary")
    for change in [
        {"scenarios": []},
        {"scenarios": [{"name": "case", "differences": ["stdout"]}]},
        {"scenarios": [{"name": "case"}]},
        {"scenarios": report["scenarios"] * 2},
        {"python_baseline": "old"},
        {"go_binary_sha256": "old"},
    ]:
        with pytest.raises(ValueError):
            validate_report(report | change, 1, "baseline", "binary")
    with pytest.raises(ValueError):
        validate_report(report, 2, "baseline", "binary")


def test_empty_suite_fails_before_tools_are_invoked(monkeypatch):
    monkeypatch.setattr(sys, "argv", ["compare", "--go", "not-invoked"])
    with pytest.raises(SystemExit, match="at least one scenario"):
        harness.main(case_factory=list)


def test_secret_boundary_ignores_loose_error_comparison():
    python: dict[str, Any] = {"stdout": "", "stderr": "HTTP 403 creating an API key", "hosts": {}}
    scenario = {
        "error_terms": ["HTTP 403"],
        "responses": {("POST", "/fixture"): (403, {"secret": "fixture-sensitive-value"})},
    }
    for secret in ["fixture-sensitive-value", "session-fixture", "credential-fixture"]:
        go = deepcopy(python)
        go["stderr"] += " " + secret
        assert harness.has_fixture_disclosure(scenario, python, go) is True
    # Explicit secret-delivery commands may match Python's stdout, but must not
    # move that secret to stderr.
    python["stdout"] = "fixture-sensitive-value"
    assert harness.has_fixture_disclosure(scenario, python, python) is False
    assert harness.has_fixture_disclosure(scenario, python, python | {"stderr": "fixture-sensitive-value"}) is True


@pytest.mark.skipif(os.name == "nt", reason="executable script fixture uses a POSIX shebang")
def test_main_process_returns_failure_for_mismatching_binary(tmp_path):
    # Use the actual Python oracle and process runner. The stub claims to be
    # jq only for version checking; this scenario never invokes jq.
    jq = tmp_path / "jq"
    jq.write_text("#!/bin/sh\nprintf 'jq-1.7.1\\n'\n")
    jq.chmod(0o700)
    binary = tmp_path / "different-cli"
    binary.write_text("#!/bin/sh\nprintf 'deliberately wrong output\\n'\n")
    binary.chmod(0o700)
    report = tmp_path / "report.json"
    # This gate test must also work in shallow CI checkouts. Only this synthetic
    # subprocess uses HEAD; production parity retains its pinned Python baseline.
    code = (
        "from scripts import compare_auth as h; import sys; "
        "h.BASELINE = 'HEAD'; "
        "sys.exit(h.main(case_factory=lambda: [{'name':'different config', "
        "'args':['config','get','org'], 'env':{'AVR_ORG':'expected'}}]))"
    )
    result = subprocess.run(
        [sys.executable, "-c", code, "--go", str(binary), "--jq-dir", str(tmp_path), "--report", str(report)],
        check=False,
        capture_output=True,
        text=True,
        timeout=30,
    )
    assert result.returncode == 1, result.stderr
    assert report.exists(), result.stderr
    assert "stdout" in json.loads(report.read_text())["scenarios"][0]["differences"]


def test_all_scenarios_capture_browser_without_opt_in(tmp_path, monkeypatch):
    monkeypatch.setenv("BROWSER", "/must/not/launch/inherited/browser")
    executable = [sys.executable, "-c", "import webbrowser; webbrowser.open('https://example.test/fixture')"]
    result = harness.run(executable, {"args": [], "capture_files": ["."]}, tmp_path, "http://127.0.0.1:1")
    assert result["exit"] == 0
    assert result["browser"] == "https://example.test/fixture"
    assert result["files"] == {}


def test_fixture_server_does_not_require_reverse_dns(monkeypatch):
    import socket

    def reject_lookup(*_args):
        raise AssertionError("fixture attempted reverse DNS")

    monkeypatch.setattr(socket, "getfqdn", reject_lookup)
    with harness.FixtureServer(("127.0.0.1", 0), harness.Fixture) as server:
        assert server.server_name == "localhost"
        assert server.server_port > 0
