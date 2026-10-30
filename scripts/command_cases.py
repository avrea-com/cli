"""Catalog executable parity inputs without treating input coverage as proof of parity."""

from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path
from scripts.audit_go_commands import flatten
from typing import TypedDict
import importlib
import json


class OptionCoverage(TypedDict):
    parameter: str
    cases: list[str]
    successful_cases: list[str]


class ArgumentCoverage(TypedDict):
    name: str
    required: bool
    variadic: bool
    cases: list[str]


class NodeCoverage(TypedDict):
    path: str
    group: bool
    cases: list[str]
    options: dict[str, OptionCoverage]
    arguments: list[ArgumentCoverage]


@dataclass
class Occurrence:
    path: str
    parameter: str
    spelling: str
    index: int
    value: str | None = None
    inline: bool = False
    cluster: bool = False


@dataclass
class Invocation:
    path: str
    options: list[Occurrence]
    arguments: list[str]


@lru_cache
def contract():
    return json.loads(Path("docs/go-python-command-contract.json").read_text())


def option_map(node):
    return {
        spelling: parameter
        for parameter in node["parameters"]
        if parameter["kind"] == "option"
        for spelling in parameter["opts"] + parameter["secondary_opts"]
    }


def invocation(args):
    data = contract()
    root = node = data["root"]
    global_options = option_map(root)
    occurrences = []
    arguments = []
    index = 0
    stopped = False
    while index < len(args):
        value = args[index]
        if not stopped and value == "--":
            stopped = True
            index += 1
            continue
        options = option_map(node)
        if not stopped and value.startswith("-") and value != "-":
            name, equal, inline = value.partition("=")
            owner = node if name in options else root
            parameter = options.get(name, global_options.get(name))
            if parameter is not None:
                option = Occurrence(owner["path"], parameter["name"], name, index)
                if not parameter.get("is_flag"):
                    if equal:
                        option.value, option.inline = inline, True
                    elif index + 1 < len(args):
                        index += 1
                        option.value = args[index]
                occurrences.append(option)
            elif not value.startswith("--"):
                for offset, character in enumerate(value[1:], start=1):
                    name = "-" + character
                    owner = node if name in options else root
                    parameter = options.get(name, global_options.get(name))
                    if parameter is None:
                        break
                    option = Occurrence(owner["path"], parameter["name"], name, index, cluster=True)
                    if not parameter.get("is_flag"):
                        if offset + 1 < len(value):
                            option.value, option.inline = value[offset + 1 :], True
                        elif index + 1 < len(args):
                            index += 1
                            option.value = args[index]
                        occurrences.append(option)
                        break
                    occurrences.append(option)
            index += 1
            continue
        children = {child["path"].split()[-1]: child for child in node.get("commands", [])}
        target = data["aliases"].get(value, value) if node is root else value
        if target in children and not arguments:
            node = children[target]
            stopped = False
        else:
            arguments.append(value)
        index += 1
    return Invocation(node["path"], occurrences, arguments)


def executable_suites(include_derived=True):
    excluded = {"compare_help", "compare_completion"}
    if not include_derived:
        excluded |= {"compare_option_aliases", "compare_argument_contract"}
    for path in sorted(Path("scripts").glob("compare_*.py")):
        if path.stem in excluded:
            continue
        module = importlib.import_module("scripts." + path.stem)
        if hasattr(module, "scenarios"):
            yield path.stem, module, module.scenarios()


def operational(case, parsed):
    # Help and field discovery can exit before the other supplied options are used.
    return case.get("exit", 0) == 0 and not any(
        option.spelling in {"-h", "--help", "-V", "--version"} or (option.spelling == "--json" and option.value == "?")
        for option in parsed.options
    )


def coverage():
    nodes = flatten(contract()["root"])
    rows: dict[str, NodeCoverage] = {
        path: {
            "path": path,
            "group": bool(node.get("commands")),
            "cases": [],
            "options": {
                spelling: {"parameter": parameter["name"], "cases": [], "successful_cases": []}
                for spelling, parameter in option_map(node).items()
                if parameter["name"] != "_click_default_help"
            },
            "arguments": [
                {
                    "name": parameter["name"],
                    "required": parameter["required"],
                    "variadic": parameter["nargs"] == -1,
                    "cases": [],
                }
                for parameter in node["parameters"]
                if parameter["kind"] == "argument"
            ],
        }
        for path, node in nodes.items()
    }
    count = 0
    for suite, _, cases in executable_suites():
        for case in cases:
            parsed = invocation(case["args"])
            key = suite + ":" + case["name"]
            row = rows[parsed.path]
            row["cases"].append(key)
            good = operational(case, parsed)
            for option in parsed.options:
                entry = rows[option.path]["options"].get(option.spelling)
                if entry is None:
                    continue
                entry["cases"].append(key)
                if good:
                    entry["successful_cases"].append(key)
            for index, arg in enumerate(row["arguments"]):
                if len(parsed.arguments) > index:
                    arg["cases"].append(key)
            count += 1
    missing = [(path, name) for path, row in rows.items() for name, info in row["options"].items() if not info["cases"]]
    unexercised = [
        (path, name)
        for path, row in rows.items()
        for name, info in row["options"].items()
        if not info["successful_cases"]
    ]
    return {
        "description": (
            "Fixture-input coverage only; passing paired execution and contract review are separate requirements."
        ),
        "case_count": count,
        "nodes": list(rows.values()),
        "missing_spellings": missing,
        "no_successful_use": unexercised,
    }


if __name__ == "__main__":
    report = coverage()
    print("Fixture cases:", report["case_count"])
    print("Missing option spellings:", len(report["missing_spellings"]))
    for entry in report["missing_spellings"]:
        print(*entry)
    print("No successful non-discovery use:", len(report["no_successful_use"]))
