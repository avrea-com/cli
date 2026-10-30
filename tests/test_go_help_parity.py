"""Help parity must detect terminal regressions without requiring identical bytes."""

from copy import deepcopy
from scripts.compare_help import compare
import pytest


def capture():
    return {
        "exit": 2,
        "stdout": "",
        "stderr": "\x1b[1mUSAGE\x1b[0m\n  avr auth [OPTIONS] COMMAND [ARGS]...\n",
        "hosts": None,
        "mode": None,
        "requests": [],
        "browser": None,
    }


CASE = {"name": "implicit help", "args": ["auth"], "exit": 2, "help_stderr": True}


@pytest.mark.parametrize("change", ["color", "indent", "command", "exit", "request"])
def test_help_comparison_detects_user_visible_changes(change):
    python = capture()
    go = deepcopy(python)
    if change == "color":
        go["stderr"] = go["stderr"].replace("\x1b[1m", "\x1b[31m")
    elif change == "indent":
        go["stderr"] = go["stderr"].replace("  avr", "avr")
    elif change == "command":
        go["stderr"] = go["stderr"].replace("auth", "repo")
    elif change == "exit":
        go["exit"] = 0
    else:
        go["requests"] = [{"method": "GET", "path": "/users/me"}]
    assert compare(CASE, python, go)


def test_help_allows_equivalent_ansi_and_composed_program_name():
    python = capture()
    go = deepcopy(python)
    go["stderr"] = go["stderr"].replace("\x1b[1m", "\x1b[0;1m").replace("avr auth", "avr-admin auth")
    assert compare(CASE, python, go) == []
