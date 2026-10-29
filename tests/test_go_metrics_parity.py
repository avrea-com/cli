"""Protect live metrics JSON comparison from normalizing away data loss."""

from copy import deepcopy
from scripts.compare_job_metrics import FOOTER
from scripts.compare_job_metrics import compare
from tests.test_go_inspection_parity import capture
import json
import pytest


def live_capture(value):
    result = capture({})
    result["stdout"] = json.dumps(value, indent=2) + "\n\n" + FOOTER + "\n"
    return result


@pytest.mark.parametrize("replacement", [9007199254740992, "9007199254740993", None])
def test_live_metrics_comparison_preserves_values_and_types(replacement):
    python = live_capture({"cpu": {"counter": 9007199254740993}})
    go = live_capture({"cpu": {"counter": replacement}})
    scenario = {"name": "metrics frames", "args": ["job", "metrics"], "json_frames": True}
    assert compare(scenario, python, go)


def test_live_metrics_comparison_retains_frames_and_footers():
    python = live_capture({"cpu": {"series": []}, "memory": {"series": []}})
    go = live_capture({"memory": {"series": []}, "cpu": {"series": []}})
    scenario = {"name": "metrics frames", "args": ["job", "metrics"], "json_frames": True}
    assert not compare(scenario, python, go)
    duplicate = deepcopy(go)
    duplicate["stdout"] *= 2
    assert compare(scenario, python, duplicate)
    go["stdout"] = go["stdout"].replace(FOOTER, "")
    assert compare(scenario, python, go)
