"""Compare native completion records with the pinned Python CLI, without API calls."""

from pathlib import Path
from scripts import compare_auth as harness
import json
import shlex
import sys


def nodes():
    contract = json.loads(Path("docs/go-python-command-contract.json").read_text())

    def walk(node):
        yield node
        for child in node.get("commands", []):
            yield from walk(child)

    return list(walk(contract["root"]))


def case(shell, name, args, incomplete="", raw=None):
    words = ["avr", *args]
    if shell == "fish":
        environment = {
            "COMP_WORDS": raw if raw is not None else shlex.join([*words, incomplete] if incomplete else words),
            "COMP_CWORD": shlex.quote(incomplete) if incomplete else "",
        }
    else:
        environment = {
            "COMP_WORDS": raw if raw is not None else shlex.join([*words, incomplete]),
            "COMP_CWORD": str(len(words)),
        }
    return {
        "name": shell + " " + name,
        "args": [],
        "env": environment | {"_AVR_COMPLETE": shell + "_complete"},
        "responses": {},
    }


def value_for(parameter):
    kind = parameter["type"]
    if kind["param_type"] == "Choice":
        return str(kind["choices"][0])
    if kind["param_type"] in {"Int", "IntRange"}:
        return str(max(1, kind.get("min") or 0))
    if kind["param_type"] == "DateTime":
        return "2026-10-01"
    return "fixture"


def scenarios():
    cases = []
    for node in nodes():
        args = node["path"].split()[1:]
        for shell in ["bash", "zsh", "fish"]:
            cases.append(case(shell, node["path"] + " options", args, "-"))
            cases.append(case(shell, node["path"] + " arguments", args))
        for param in node["parameters"]:
            if param["kind"] == "option":
                supplied = [*args, param["opts"][0]]
                if not param.get("is_flag"):
                    supplied.append(value_for(param))
                cases.append(case("bash", node["path"] + " used " + param["name"], supplied, "-"))
            if param["type"]["param_type"] not in {"Choice", "Path", "File"}:
                continue
            supplied = list(args)
            for prior in node["parameters"]:
                if prior is param:
                    break
                if prior["kind"] == "argument":
                    supplied.append(value_for(prior))
            if param["kind"] == "option":
                supplied.append(param["opts"][0])
            partial = str(param["type"]["choices"][0])[:2] if param["type"]["param_type"] == "Choice" else "./some dir/"
            for shell in ["bash", "zsh", "fish"]:
                for prefix in ["", partial]:
                    cases.append(
                        case(shell, node["path"] + " value " + param["name"] + " " + repr(prefix), supplied, prefix)
                    )
    special = [
        ("tests shorthand", ["tests", "job-" + "a" * 32], "--"),
        ("tests native shorthand", ["tests", "awj-" + "a" * 32, "--json", "*"], "--"),
        ("unknown command", ["no-such-command"], ""),
        ("unknown flag", ["repo", "list", "--unknown"], "-"),
        ("alias", ["repos"], ""),
        ("repeatable", ["job", "list", "--repo", "org/one"], "--repo"),
        ("repeatable project add", ["org", "install", "add", "--project-id", "11"], "--project-id"),
        ("repeatable project selection", ["org", "install", "set-selection", "--project-id", "11"], "--project-id"),
        ("boolean pair", ["workflow", "run", "--no-watch"], "--"),
        ("equals choice", ["run", "list"], "--status=su"),
        ("equals separate", ["run", "list", "--status"], "="),
        ("after separator", ["repo", "clone", "org/repo", "--"], "./"),
        ("ssh passthrough", ["vm", "ssh", "vm-fixture", "--", "echo"], "-"),
        ("quoted path", ["repo", "clone", "org/repo"], "some dir/"),
        ("partial quote", ["repo", "clone", "org/repo"], "some", "avr repo clone org/repo 'some"),
        ("partial escape", ["repo", "clone", "org/repo"], "some", "avr repo clone org/repo some\\"),
        ("option after positional", ["run", "view", "run-fixture"], "--"),
        ("no global leakage", ["auth", "status"], "--ver"),
        ("missing option value", ["repo", "list", "--repo"], "--"),
    ]
    for shell in ["bash", "zsh", "fish"]:
        for name, args, incomplete, *raw in special:
            cases.append(case(shell, name, args, incomplete, raw[0] if raw else None))
    for shell in ["bash", "zsh", "fish"]:
        quiet = case(shell, "completion bypasses command diagnostics", [], "rep")
        quiet["env"]["AVR_DEBUG"] = "timing"
        cases.append(quiet)
    for mode in ["bash_unknown", "unknown_complete", "unsupported"]:
        cases.append(
            {"name": "unsupported " + mode, "args": [], "env": {"_AVR_COMPLETE": mode}, "responses": {}, "exit": 1}
        )
    return cases


def compare(scenario, python, go):
    differences = harness.compare(scenario, python, go)
    # Completion is a machine protocol: record boundaries and ordering matter.
    if python["stdout"].replace("\r\n", "\n") != go["stdout"].replace("\r\n", "\n"):
        differences.append("completion records")
    for label, result in [("Python", python), ("Go", go)]:
        if result["requests"]:
            differences.append(label + " completion made an API request")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/completion-parity.json")
    )
