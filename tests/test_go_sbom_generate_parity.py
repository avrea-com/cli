"""Failed SBOM waits must retain typed JSON and exit-code parity."""

from scripts.compare_sbom_generate import compare
from tests.test_go_inspection_parity import capture
import pytest


@pytest.mark.parametrize("replacement", [False, "0", 0, ""])
def test_failed_wait_json_keeps_null_distinct(replacement):
    scenario = {"name": "failed generation", "args": ["sbom", "generate"], "json": True, "exit": 1}
    python = capture({"status": "timeout", "commit_sha": None}) | {"exit": 1}
    go = capture({"status": "timeout", "commit_sha": replacement}) | {"exit": 1}
    assert "JSON values" in compare(scenario, python, go)


def test_failed_wait_comparison_still_checks_exit():
    scenario = {"name": "failed generation", "args": ["sbom", "generate"], "json": True, "exit": 1}
    result = capture({"status": "timeout"}) | {"exit": 1}
    assert not compare(scenario, result, result)
    assert "expected exit 1" in compare(scenario, result, result | {"exit": 0})
