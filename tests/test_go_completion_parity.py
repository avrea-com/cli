"""Completion comparisons retain machine-protocol boundaries and side effects."""

from copy import deepcopy
from scripts.compare_completion import compare
import pytest


def capture(stdout="plain,repo\nplain,run\n"):
    return {"exit": 0, "stdout": stdout, "stderr": "", "hosts": None, "requests": [], "browser": None}


CASE = {"name": "bash completion", "args": []}


@pytest.mark.parametrize(
    "changed",
    ["plain,run\nplain,repo\n", "plain,repo plain,run\n", "file,repo\nplain,run\n", "plain,repo\n"],
)
def test_completion_record_changes_are_detected(changed):
    assert "completion records" in compare(CASE, capture(), capture(changed))


def test_completion_platform_newlines_are_equivalent():
    assert compare(CASE, capture(), capture("plain,repo\r\nplain,run\r\n")) == []


def test_completion_must_not_call_the_api():
    python = capture()
    go = deepcopy(python)
    go["requests"] = [{"method": "GET", "path": "/users/me"}]
    assert "Go completion made an API request" in compare(CASE, python, go)
