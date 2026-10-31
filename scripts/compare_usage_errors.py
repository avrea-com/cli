"""Strict usage diagnostics for read and write commands, using local fixtures only."""

from copy import deepcopy
from scripts import compare_argument_contract as arguments
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
import sys


def scenarios():
    cases = [c for c in arguments.scenarios() if c["parser_kind"] == "missing_option"]
    for group, target in [("job", inspection.JOB), ("run", inspection.RUN)]:
        cases.append(
            {
                "name": group + " logs incompatible flags",
                "args": [group, "logs", target, "--follow", "--failed"],
                "exit": 2,
                "env": {},
            }
        )
    result = []
    for original in cases:
        for tty in (False, True) if sys.platform != "win32" else (False,):
            case = deepcopy(original)
            case["name"] += " tty" if tty else " pipe"
            case["tty"] = case["merge_terminal"] = tty
            case["responses"] = {}
            if tty:
                case["env"] = case.get("env", {}) | {"NO_COLOR": "", "TERM": "xterm-256color"}
            result.append(case)
    return result


def compare(case, python, go):
    differences = []
    for language, value in [("Python", python), ("Go", go)]:
        if value["exit"] != 2:
            differences.append(language + " expected usage exit 2")
        if value["requests"] or value["hosts"] or value["browser"]:
            differences.append(language + " usage error caused side effects")
    for stream in ("stdout", "stderr"):
        a, b = python[stream], go[stream]
        if inspection.terminal_text(a) != inspection.terminal_text(b):
            differences.append(stream + " usage text/layout")
        if inspection.presentation(a) != inspection.presentation(b):
            differences.append(stream + " styles")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/usage-errors-parity.json"
        )
    )
