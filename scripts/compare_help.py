"""Compare command help, group defaults, and bare CLI behavior against Python."""

from copy import deepcopy
from pathlib import Path
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import json
import sys


def scenarios():
    metadata = json.loads(Path("docgen/metadata.json").read_text())["terminal"]
    cases = []
    for path, spec in metadata.items():
        args = path.split()[1:]
        for flag in ["--help", "-h"]:
            cases.append(
                {
                    "name": "help " + path + " " + flag,
                    "args": [*args, flag],
                    "env": {},
                    "responses": {},
                    "presentation": True,
                }
            )
        if spec["no_args_is_help"]:
            cases.append(
                {
                    "name": "implicit group help " + path,
                    "args": args,
                    "env": {},
                    "responses": {},
                    "exit": 2,
                    "help_stderr": True,
                }
            )
    for args in [
        ["--help", "repo"],
        ["auth", "--help", "status"],
        ["vm", "--help", "bootstrap"],
        ["--help", "unknown"],
    ]:
        cases.append(
            {"name": "eager help " + " ".join(args), "args": args, "env": {}, "responses": {}, "presentation": True}
        )
    for name, env in [("welcome", {}), ("authenticated", {"AVR_TOKEN": harness.TOKEN})]:
        cases.append({"name": "bare CLI " + name, "args": [], "env": env, "responses": {}, "presentation": True})
    for alias in json.loads(Path("docs/go-python-command-contract.json").read_text())["aliases"]:
        cases.append(
            {"name": "alias help " + alias, "args": [alias, "--help"], "env": {}, "responses": {}, "presentation": True}
        )
    if sys.platform != "win32":
        selected = [
            case
            for case in cases
            if (
                case["name"].endswith(" --help")
                and (
                    metadata[" ".join(["avr", *case["args"][:-1]])]["sections"]
                    or " ".join(case["args"][:-1])
                    in {"vm", "vm bootstrap", "auth login", "org saml configure", "run view", "config", "internal docs"}
                )
            )
            or case["name"].startswith("bare CLI ")
        ]
        for original in selected:
            for color, extra in [
                ("color", {"NO_COLOR": "", "TERM": "xterm-256color"}),
                ("no color", {"NO_COLOR": "1", "TERM": "xterm-256color"}),
            ]:
                case = deepcopy(original)
                case["name"] += " terminal " + color
                case["tty"] = case["merge_terminal"] = True
                case["env"] |= extra
                cases.append(case)
        for path in ["auth", "vm", "org saml", "internal"]:
            cases.append(
                {
                    "name": "implicit " + path + " terminal",
                    "args": path.split(),
                    "env": {"NO_COLOR": "", "TERM": "xterm-256color"},
                    "responses": {},
                    "exit": 2,
                    "tty": True,
                    "merge_terminal": True,
                    "presentation": True,
                }
            )
        for path in ["avr", "avr auth", "avr vm bootstrap"]:
            for columns in ["60", "120"]:
                cases.append(
                    {
                        "name": "help width " + path + " " + columns,
                        "args": [*path.split()[1:], "--help"],
                        "env": {"NO_COLOR": "", "TERM": "xterm-256color", "COLUMNS": columns},
                        "responses": {},
                        "tty": True,
                        "merge_terminal": True,
                        "presentation": True,
                    }
                )
        for path in ["avr", "avr auth", "avr vm"]:
            cases.append(
                {
                    "name": "help explicit no-color " + path,
                    "args": ["--no-color", *path.split()[1:], "--help"],
                    "env": {"NO_COLOR": "", "TERM": "xterm-256color"},
                    "responses": {},
                    "tty": True,
                    "merge_terminal": True,
                    "presentation": True,
                }
            )
    return cases


def comparable(result):
    result = deepcopy(result)
    for channel in ["stdout", "stderr"]:
        result[channel] = result[channel].replace("avr-admin", "avr")
    return result


def compare(scenario, python, go):
    python, go = comparable(python), comparable(go)
    differences = watch.compare(scenario, python, go)
    if scenario.get("help_stderr"):
        if inspection.presentation(python["stderr"]) != inspection.presentation(go["stderr"]):
            differences.append("help stderr text/styles")
        if [line.rstrip() for line in inspection.terminal_text(python["stderr"]).splitlines()] != [
            line.rstrip() for line in inspection.terminal_text(go["stderr"]).splitlines()
        ]:
            differences.append("help stderr layout")
    for label, result in [("Python", python), ("Go", go)]:
        if result["requests"]:
            differences.append(label + " help made an API call")
    return differences


if __name__ == "__main__":
    sys.exit(harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/help-parity.json"))
