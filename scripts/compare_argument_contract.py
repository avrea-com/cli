"""Check parser rejection across the pinned public command contract."""

from copy import deepcopy
from scripts import command_cases as catalog
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
import re
import sys

UNKNOWN = "--definitely-not-an-avr-option"
EXTRA = "UNEXPECTED_ARG_FIXTURE"

DIAGNOSTICS = {
    "missing_argument": "missing arg|requires.*arg|accepts.*arg|expected.*arg|must.*arg",
    "extra_argument": "unexpected extra arg|unknown command|accepts.*arg|requires.*arg",
    "missing_value": "requires an argument|needs an argument|expected.*argument",
    "missing_option": "missing option|required flag|option.*required|requires.*--|--.*(?:are|is) required",
    "invalid_value": "invalid|not.*range|must|not one of|expected",
    "unknown_option": "no such option|unknown(?: shorthand)? flag|unknown option|unrecognized option",
    "unknown_command": "no such command|unknown command",
}


def examples():
    result = {}
    for _, _, cases in catalog.executable_suites(include_derived=False):
        for case in cases:
            parsed = catalog.invocation(case["args"])
            if not catalog.operational(case, parsed):
                continue
            previous = result.get(parsed.path)
            if previous is None or len(parsed.arguments) > len(previous[1].arguments):
                result[parsed.path] = (case, parsed)
    return result


def scenarios():
    result = []
    seeds = examples()
    for path, node in catalog.flatten(catalog.contract()["root"]).items():
        args = path.split()[1:]
        parameters = [p for p in node["parameters"] if p["kind"] == "argument"]
        seed, parsed = seeds.get(path, ({}, catalog.Invocation(path, [], [])))
        required = {}
        for parameter in node["parameters"]:
            if parameter["kind"] == "option" and parameter["required"]:
                occurrence = next(option for option in parsed.options if option.parameter == parameter["name"])
                required[parameter["name"]] = [parameter["opts"][0], occurrence.value]
        required_args = [arg for pair in required.values() for arg in pair]
        values = [
            parsed.arguments[index]
            if index < len(parsed.arguments)
            else parameter["type"].get("choices", ["fixture-value"])[0]
            for index, parameter in enumerate(parameters)
            if parameter["nargs"] != -1
        ]

        def add(suffix, invocation, kind, flags=None, *, path=path, seed=seed):
            case = {
                "name": path + " " + suffix,
                "args": invocation,
                "env": {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": inspection.ORG},
                "exit": 2,
                "parser_kind": kind,
                "parser_flags": flags or [],
                "browser_capture": True,
            }
            for key in ["fixture_files", "stdin"]:
                if key in seed:
                    case[key] = deepcopy(seed[key])
            result.append(case)

        if node.get("commands"):
            add("unknown command", args + ["__unknown_command__"], "unknown_command")
            continue
        for index, parameter in enumerate(parameters):
            if parameter["required"]:
                add("missing " + parameter["name"], args + required_args + values[:index], "missing_argument")
        # vm ssh deliberately forwards unknown flags and variadic arguments.
        if path != "avr vm ssh":
            add("unknown option", args + required_args + [UNKNOWN], "unknown_option", [UNKNOWN])
        if not any(p["nargs"] == -1 for p in parameters):
            add("extra argument", args + required_args + values + [EXTRA], "extra_argument")
        for parameter in node["parameters"]:
            if parameter["kind"] != "option" or parameter["is_flag"]:
                continue
            other_required = [arg for name, pair in required.items() if name != parameter["name"] for arg in pair]
            add(
                parameter["opts"][0] + " missing value",
                args + other_required + values + [parameter["opts"][0]],
                "missing_value",
                parameter["opts"],
            )
            # Card fields intentionally prompt when absent; dedicated card tests
            # cover those interactive and piped-input flows.
            if parameter["required"] and path != "avr billing payment-methods add":
                add(
                    parameter["opts"][0] + " missing option",
                    args + other_required + values,
                    "missing_option",
                    parameter["opts"],
                )
            spec = parameter["type"]
            invalid = []
            match spec["param_type"]:
                case "Choice":
                    invalid = ["__invalid_choice__"]
                case "Int" | "IntRange":
                    invalid = ["not-a-number"]
                    if spec.get("min") is not None:
                        invalid.append(str(spec["min"] - 1))
                    if spec.get("max") is not None:
                        invalid.append(str(spec["max"] + 1))
                case "DateTime":
                    invalid = ["not-a-date"]
            for value in invalid:
                add(
                    parameter["opts"][0] + " invalid " + value,
                    args + other_required + values + [parameter["opts"][0], value],
                    "invalid_value",
                    parameter["opts"],
                )
    return result


def compare(scenario, python, go):
    differences = []
    for label, result in [("Python", python), ("Go", go)]:
        if result["exit"] != 2:
            differences.append(label + " expected parser exit 2")
        if result["stdout"]:
            differences.append(label + " unexpected stdout on parser rejection")
        if result["requests"] or result["hosts"] or result["browser"]:
            differences.append(label + " parser rejection caused side effects")
        message = inspection.terminal_text(result["stderr"])
        if not message.strip():
            differences.append(label + " missing parser diagnostic")
        if scenario["parser_flags"]:
            if not any(
                flag in message
                or scenario["parser_kind"] == "missing_option"
                and re.search(r"\b" + re.escape(flag.lstrip("-")) + r"\b", message)
                for flag in scenario["parser_flags"]
            ):
                differences.append(label + " diagnostic omitted the rejected option")
        if not re.search(DIAGNOSTICS[scenario["parser_kind"]], message, re.IGNORECASE):
            differences.append(label + " diagnostic reported the wrong parser problem")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/argument-contract-parity.json"
        )
    )
