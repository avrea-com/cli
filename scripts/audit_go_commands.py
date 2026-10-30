"""Audit the registered Go command/flag surface against the pinned Python contract.

Registration coverage does not prove behavior parity. Defaults and positional
usage are emitted for explicit review, and executable suites remain necessary.
"""

from pathlib import Path
import argparse
import json


def flatten(node):
    return {node["path"]: node} | {
        key: value for child in node.get("commands", []) for key, value in flatten(child).items()
    }


def compatible_default(expected, actual):
    if expected in (None, "Sentinel.UNSET"):
        # These require execution checks of absence/explicit values, since Go
        # represents absent strings/numbers with zero values plus Changed().
        return True
    if str(expected).lower() == actual.lower() or (expected == [] and actual == "[]"):
        return True
    try:
        return float(expected) == float(actual)
    except TypeError, ValueError:
        return False


def parameter_issues(parameter, flag):
    issues = []
    name = parameter["opts"][0]
    if parameter["is_flag"] != bool(flag["no_opt_default"]):
        issues.append(name + " flag/value arity differs")
    if parameter["multiple"] != (flag["type"] in {"stringArray", "stringSlice"}):
        issues.append(name + " repeatability differs")
    if not compatible_default(parameter["default"], flag["default"]):
        issues.append(name + " declared default differs")
    return issues


def audit(python, go):
    expected = flatten(python["root"])
    actual = flatten(go)
    rows = []
    for path, command in expected.items():
        item = {
            "path": path,
            "issues": [],
            "options": [],
            "arguments": [p for p in command["parameters"] if p["kind"] == "argument"],
        }
        native = actual.get(path)
        if native is None:
            item["issues"].append("missing command")
            rows.append(item)
            continue
        item["go_use"] = native["use"]
        usage_arguments = native["use"].split()[1:]
        if len(usage_arguments) != len(item["arguments"]):
            item["issues"].append("declared positional argument count differs")
        else:
            for usage, param in zip(usage_arguments, item["arguments"], strict=True):
                if (not usage.startswith("[")) != param["required"]:
                    item["issues"].append(param["name"] + " declared positional requirement differs")
                if ("..." in usage) != (param["nargs"] == -1):
                    item["issues"].append(param["name"] + " declared positional variadic arity differs")
        item["go_extra_flags"] = []
        if command["hidden"] != native["hidden"]:
            item["issues"].append("hidden visibility differs")
        flags = {}
        for flag in native["flags"]:
            flags["--" + flag["name"]] = flag
            if flag.get("shorthand"):
                flags["-" + flag["shorthand"]] = flag
        seen = set()
        for param in command["parameters"]:
            if param["kind"] != "option":
                continue
            names = param["opts"] + param["secondary_opts"]
            seen.update(names)
            missing = sorted(set(names) - flags.keys())
            if missing:
                item["issues"].append("missing flags: " + ", ".join(missing))
            if primary := flags.get(param["opts"][0]):
                item["issues"].extend(parameter_issues(param, primary))
            item["options"].append({"python": param, "go": {name: flags[name] for name in names if name in flags}})
        item["go_extra_flags"] = sorted(flags.keys() - seen)
        rows.append(item)
    aliases = {}
    for child in go.get("commands", []):
        aliases.update({alias: child["path"].split()[-1] for alias in child["aliases"] or []})
    missing_aliases = {name: target for name, target in python["aliases"].items() if aliases.get(name) != target}
    return {
        "python_baseline": python["baseline"],
        "python_nodes": len(expected),
        "go_nodes": len(actual),
        "missing_aliases": missing_aliases,
        "extra_go_commands": sorted(actual.keys() - expected.keys()),
        "commands": rows,
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--inventory", required=True)
    parser.add_argument("--report", default="build/go/command-surface-audit.json")
    args = parser.parse_args()
    report = audit(
        json.loads(Path("docs/go-python-command-contract.json").read_text()),
        json.loads(Path(args.inventory).read_text()),
    )
    target = Path(args.report)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps(report, indent=2) + "\n")
    for row in report["commands"]:
        if row["issues"]:
            print(row["path"] + ": " + "; ".join(row["issues"]))
    if report["missing_aliases"]:
        print("Missing aliases:", report["missing_aliases"])
    print(f"Compared {report['python_nodes']} Python nodes with {report['go_nodes']} Go nodes; details: {target}")
    raise SystemExit(int(bool(report["missing_aliases"]) or any(row["issues"] for row in report["commands"])))
