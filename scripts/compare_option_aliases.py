"""Execute untested equivalent option spellings using existing behavioral fixtures."""

from copy import deepcopy
from scripts import command_cases as catalog
from scripts import compare_audit_health as audit
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import importlib
import sys

FALLBACKS = {
    "compare_jobs": inspection.compare,
    "compare_run_control": inspection.compare,
    "compare_vm_create": audit.compare,
    "compare_vm_lifecycle": audit.compare,
}


def replace_option(args, occurrence, spelling):
    """Keep values literal, including empty strings and leading dashes."""
    result = list(args)
    replacement = [spelling]
    if occurrence.inline:
        replacement.append(occurrence.value)
    result[occurrence.index : occurrence.index + 1] = replacement
    return result


def scenarios():
    inputs = []
    exercised = set()
    for suite, _, cases in catalog.executable_suites(include_derived=False):
        for case in cases:
            parsed = catalog.invocation(case["args"])
            if catalog.operational(case, parsed):
                inputs.append((suite, case, parsed))
                exercised.update((option.path, option.spelling) for option in parsed.options)
    nodes = catalog.flatten(catalog.contract()["root"])
    result = []
    for path, node in nodes.items():
        for parameter in node["parameters"]:
            if parameter["kind"] != "option" or parameter["name"] == "_click_default_help":
                continue
            # Positive and negative boolean spellings are separate equivalence groups.
            for spellings in (parameter["opts"], parameter["secondary_opts"]):
                for spelling in spellings:
                    if (path, spelling) in exercised:
                        continue
                    candidates = [
                        (suite, case, option)
                        for suite, case, parsed in inputs
                        for option in parsed.options
                        if option.path == path
                        and option.parameter == parameter["name"]
                        and option.spelling in spellings
                        and not option.cluster
                    ]
                    if not candidates:
                        continue
                    suite, seed, occurrence = min(
                        candidates, key=lambda candidate: (bool(candidate[1].get("tty")), len(candidate[1]["args"]))
                    )
                    case = deepcopy(seed)
                    case["name"] += " option spelling " + spelling
                    case["alias_suite"] = suite
                    case["alias_original_name"] = seed["name"]
                    case["args"] = replace_option(seed["args"], occurrence, spelling)
                    result.append(case)
    return result


def compare(scenario, python, go):
    suite = scenario["alias_suite"]
    source = importlib.import_module("scripts." + suite)
    comparator = getattr(source, "compare", FALLBACKS.get(suite, watch.compare))
    original = scenario | {"name": scenario["alias_original_name"]}
    return comparator(original, python, go)


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/option-aliases-parity.json"
        )
    )
