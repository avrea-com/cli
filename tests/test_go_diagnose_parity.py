"""Ensure diagnostic comparison normalizes clocks, not semantic evidence."""

from copy import deepcopy
from scripts.compare_run_diagnose import compare
from tests.test_go_inspection_parity import capture
import pytest


def report():
    return {
        "generated_at": "2026-10-29T12:00:30Z",
        "run": {"started_at": "2026-10-29T12:00:00Z", "platform_run_id": 9007199254740993},
        "timing": {"execution_end_source": "generated_at", "execution_seconds": 30.0},
        "jobs": [],
        "warnings": [],
        "complete": True,
    }


SCENARIO = {"name": "diagnose", "args": ["run", "diagnose"], "json": True, "diagnostic_report": True}


def test_generation_time_variation_requires_correct_elapsed_time():
    python = report()
    go = deepcopy(python)
    go["generated_at"] = "2026-10-29T12:00:31Z"
    go["timing"]["execution_seconds"] = 31.0
    assert not compare(SCENARIO, capture(python), capture(go))
    go["timing"]["execution_seconds"] = 30.0
    assert "incorrect elapsed time" in compare(SCENARIO, capture(python), capture(go))


@pytest.mark.parametrize("field", ["warnings", "complete", "id"])
def test_diagnostics_differences_survive_clock_normalization(field):
    python = report()
    go = deepcopy(python)
    if field == "id":
        go["run"]["platform_run_id"] -= 1
    else:
        go[field] = ["missing"] if field == "warnings" else False
    assert compare(SCENARIO, capture(python), capture(go))


def test_parallel_request_comparison_keeps_count_and_authorization():
    python = capture(report())
    python["requests"] = [
        {"method": "GET", "path": "/jobs", "authorization": "fixture", "body": None},
        {"method": "GET", "path": "/metrics", "authorization": "fixture", "body": None},
    ]
    go = deepcopy(python)
    go["requests"].reverse()
    assert not compare(SCENARIO, python, go)
    go["requests"][0]["authorization"] = None
    assert compare(SCENARIO, python, go)
    go = deepcopy(python)
    go["requests"].append(go["requests"][0])
    assert compare(SCENARIO, python, go)
