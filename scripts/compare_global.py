"""Root flags, diagnostics, environment parsing and terminal-link precedence."""

from scripts import compare_auth as harness
from scripts import compare_option_gaps as gaps
import re
import sys

VERSION = re.compile(r"avr(?:-admin)? version (?:[0-9][A-Za-z0-9.+_-]*|dev)(?: \(development\))?\r?\n")


def scenarios():
    cases = []
    for spelling in ["--version", "-V"]:
        for flags in [[], ["--verbose", "--no-color"]]:
            cases.append(
                {
                    "name": "root version " + spelling + " " + str(flags),
                    "args": [*flags, spelling],
                    "env": {"AVR_DEBUG": "unknown", "AVR_LINKS": "invalid"},
                    "version_output": True,
                    "offline": True,
                }
            )
    for spelling in ["--verbose", "-v"]:
        for offset in [0, 1, 2]:
            case = gaps.derive("auth", "verbose status", f"global position {offset} {spelling}")
            case["args"] = ["auth", "status"]
            case["args"].insert(offset, spelling)
            cases.append(case)
    for value in ["1", "true", "YES", "on", " api ", "api, api", "", "unknown", "zeta,alpha,zeta", "api,unknown"]:
        case = gaps.derive("auth", "status human", "debug " + repr(value))
        case["env"] = {"AVR_DEBUG": value}
        cases.append(case)
    for value in ["invalid", "2"]:
        case = gaps.derive("auth", "status human", "invalid links environment " + value)
        case["env"] = {"AVR_LINKS": value}
        case.update(exit=2, error_terms=["--links", "boolean"], responses={})
        case["offline"] = True
        cases.append(case)
    if sys.platform != "win32":
        for value in ["0", "false", "F", "no", "n", "off", "1", "true", "T", "yes", "y", "on", " true ", "", " "]:
            case = gaps.derive("inspection", "run terminal links default", "environment " + repr(value))
            case["env"]["AVR_LINKS"] = value
            if value and value.strip().lower() in {"", "0", "false", "f", "no", "n", "off"}:
                case["links"] = []
            cases.append(case)
        for flags, enabled in [
            (["--links", "--no-links"], False),
            (["--no-links", "--links"], True),
            (["--links", "--no-links", "--links"], True),
        ]:
            case = gaps.derive("inspection", "run terminal links default", "order " + " ".join(flags))
            case["args"] += flags
            if not enabled:
                case["links"] = []
            cases.append(case)
        for flags in [["--links"], ["--no-links"]]:
            case = gaps.derive("inspection", "run terminal links default", "override invalid env " + flags[0])
            case["args"] += flags
            case["env"]["AVR_LINKS"] = "invalid"
            if flags == ["--no-links"]:
                case["links"] = []
            cases.append(case)
    return cases


def compare(scenario, python, go):
    if scenario.get("version_output"):
        differences = []
        for label, result in [("Python", python), ("Go", go)]:
            if result["exit"] != 0 or not VERSION.fullmatch(result["stdout"]) or result["stderr"]:
                differences.append(label + " invalid version output")
            if result["requests"] or result["hosts"] or result["browser"]:
                differences.append(label + " version caused side effects")
        return differences
    differences = gaps.compare(scenario, python, go)
    if scenario.get("offline"):
        for label, result in [("Python", python), ("Go", go)]:
            if result["requests"]:
                differences.append(label + " sent API requests before invalid global options failed")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/global-parity.json")
    )
