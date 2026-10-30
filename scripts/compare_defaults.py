"""Compare omitted and explicit declared value defaults with Python."""

from copy import deepcopy
from scripts import command_cases as catalog
from scripts import compare_auth as harness
from scripts import compare_job_watch as watch
from scripts.compare_option_aliases import FALLBACKS
import importlib
import sys

# A new installation appears on the first poll, so the production timeout
# can be exercised without waiting two minutes in each executable.
OVERRIDES = {
    ("avr org install add", "wait_seconds"): ("compare_installations", "installation add detected sorted distinct"),
}


def scenarios():
    inputs = []
    for suite, _, cases in catalog.executable_suites(include_derived=False):
        for case in cases:
            parsed = catalog.invocation(case["args"])
            if catalog.operational(case, parsed) or case.get("snapshots"):
                inputs.append((suite, case, parsed))
    cases = []
    for path, node in catalog.flatten(catalog.contract()["root"]).items():
        for parameter in node["parameters"]:
            if (
                parameter["kind"] != "option"
                or parameter["is_flag"]
                or parameter["default"] in (None, "Sentinel.UNSET")
            ):
                continue
            default = str(parameter["default"])
            candidates = []
            for suite, seed, parsed in inputs:
                if parsed.path != path:
                    continue
                selected = OVERRIDES.get((path, parameter["name"]))
                if selected is not None and (suite, seed["name"]) != selected:
                    continue
                occurrences = [o for o in parsed.options if o.path == path and o.parameter == parameter["name"]]
                if any(o.cluster for o in occurrences):
                    continue
                if (
                    occurrences
                    and parameter["name"] not in {"interval", "wait_seconds"}
                    and any(o.value != default for o in occurrences)
                ):
                    continue
                if path == "avr login" and "--email" in seed["args"]:
                    continue
                candidates.append((suite, seed, occurrences))
            if path == "avr login" and not candidates:
                seed = deepcopy(next(c for c in harness.scenarios() if c["name"] == "github login"))
                seed["args"] = ["login"]
                candidates.append(("compare_auth", seed, []))
            if not candidates:
                raise ValueError("No behavior fixture for default: " + path + " " + parameter["opts"][0])
            suite, seed, occurrences = min(
                candidates,
                key=lambda item: (
                    bool(item[2]),
                    bool(item[1].get("tty")),
                    item[1].get("exit", 0) != 0,
                    len(item[1]["args"]),
                ),
            )
            args = list(seed["args"])
            for occurrence in sorted(occurrences, key=lambda o: o.index, reverse=True):
                del args[occurrence.index : occurrence.index + (1 if occurrence.inline else 2)]
            for explicit in [False, True]:
                case = deepcopy(seed)
                case["name"] = (
                    path + " " + parameter["opts"][0] + (" explicit default" if explicit else " omitted default")
                )
                case["default_suite"] = suite
                case["default_seed_name"] = seed["name"]
                case["args"] = args + ([parameter["opts"][0], default] if explicit else [])
                cases.append(case)
    return cases


def compare(scenario, python, go):
    suite = scenario["default_suite"]
    source = importlib.import_module("scripts." + suite)
    comparator = getattr(source, "compare", FALLBACKS.get(suite, watch.compare))
    return comparator(scenario | {"name": scenario["default_seed_name"]}, python, go)


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/defaults-parity.json")
    )
