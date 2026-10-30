"""Exercise option-use gaps found by the final public-command coverage audit."""

from copy import deepcopy
from functools import lru_cache
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_jobs as jobs
from scripts import compare_workflow as workflow
import importlib
import json
import shlex
import sys

ORG = inspection.ORG
WRONG_ORG = "org-019a0000000070008000000000000099"
ORG_LIST = ("GET", "/users/me/organizations")
ORG_RESPONSE = (200, {"data": inspection.ORGANIZATIONS})
ORG_BASES = [
    ("artifact", "artifact list JSON"),
    ("artifact", "artifact get JSON"),
    ("artifact", "artifact download JSON"),
    ("artifact", "artifact delete yes JSON"),
    ("cache", "cache list JSON"),
    ("cache", "cache usage JSON"),
    ("cache", "cache delete all count 5"),
    ("jobs", "job list JSON"),
    ("jobs", "job view JSON"),
    ("jobs", "job logs"),
    ("jobs", "run logs"),
    ("log_search", "log search JSON"),
    ("run_control", "run cancel yes"),
    ("run_control", "run rerun yes"),
    ("inspection", "run list JSON"),
    ("inspection", "view JSON"),
    ("inspection", "watch completed stream"),
    ("sbom", "sbom list JSON"),
    ("sbom", "sbom view JSON"),
    ("sbom_download", "sbom download default"),
    ("sbom_generate", "sbom generate JSON"),
    ("workflow", "workflow list JSON"),
    ("workflow", "workflow view JSON"),
    ("workflow_run", "workflow run filename"),
]


@lru_cache
def module(name):
    return importlib.import_module("scripts.compare_" + name)


@lru_cache
def originals(name):
    return {case["name"]: case for case in module(name).scenarios()}


def derive(suite, name, suffix):
    case = deepcopy(originals(suite)[name])
    case["name"] = name + " " + suffix
    case["origin_suite"] = suite
    case["origin_name"] = name
    return case


def scenarios():
    cases = []
    for suite, name in ORG_BASES:
        for selection, value in [("ID", ORG), ("slug", "AcMe")]:
            case = derive(suite, name, "explicit org " + selection)
            case["args"] += ["--org", value]
            case["env"]["AVR_ORG"] = WRONG_ORG
            case.setdefault("responses", {})[ORG_LIST] = ORG_RESPONSE
            case["selected_org"] = ORG
            cases.append(case)
        case = derive(suite, name, "unknown explicit org")
        case["args"] += ["--org", "absent-organization"]
        case["responses"] = {ORG_LIST: ORG_RESPONSE}
        case["env"]["AVR_ORG"] = ORG
        case["exit"] = 1
        case["error_terms"] = ["No organization matching", "absent-organization"]
        case.pop("json", None)
        case.pop("events", None)
        case.pop("expected_files", None)
        case.pop("exact_stdout", None)
        case["unknown_org"] = True
        cases.append(case)
    for value, seconds in [("7d", 604800), ("24h", 86400), ("30m", 1800), ("0m", 0)]:
        case = derive("jobs", "job list JSON", "since " + value)
        case["args"] += ["--since", value]
        case["responses"] = {}
        case["dynamic_responses"] = {("GET", f"/orgs/{ORG}/jobs"): (200, {"data": [jobs.JOB_DATA]})}
        case["since_seconds"] = seconds
        cases.append(case)
    for value in ["yesterday", "-1d", "1.5h"]:
        case = derive("jobs", "job list JSON", "invalid since " + value)
        case["args"] += ["--since", value]
        case.update(exit=1, responses={}, error_terms=["Invalid --since value"])
        case.pop("json", None)
        cases.append(case)
    case = derive("jobs", "job view JSON", "jq object")
    case["args"] += ["--jq", "{name:.job_name,platform:.platform_job_id}"]
    cases.append(case)
    case = derive("jobs", "run logs", "all levels")
    case["args"] += ["--all-levels"]
    case["stdout_terms"] = ["hidden diagnostic", "compile failed"]
    cases.append(case)
    case = derive("inspection", "run list TSV", "web piped")
    case["args"] += ["--web"]
    case["responses"] = {ORG_LIST: ORG_RESPONSE}
    cases.append(case)
    case = derive("auth", "SSO login", "hidden alias email")
    case["args"] = ["login", *case["args"][2:]]
    cases.append(case)
    if sys.platform != "win32":
        for command in ["job logs", "job view full logs", "run logs"]:
            for color in [False, True]:
                case = derive("jobs", command, "no pager terminal " + str(color))
                case["args"] += ["--no-pager", "--no-links"]
                case["tty"] = case["merge_terminal"] = case["presentation"] = True
                case["env"] |= {
                    "NO_COLOR": "" if color else "1",
                    "TERM": "xterm-256color",
                    "AVR_PAGER": shlex.join([sys.executable, "-c", "print('UNEXPECTED PAGER INVOCATION')"]),
                }
                case["stdout_absent"] = ["UNEXPECTED PAGER INVOCATION"]
                case["stdout_terms"] = ["compile failed"]
                cases.append(case)
        case = derive("jobs", "run logs", "all levels terminal")
        case["args"] += ["--all-levels", "--no-pager", "--no-links"]
        case["tty"] = case["merge_terminal"] = case["presentation"] = True
        case["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color", "AVR_PAGER": ""}
        case["stdout_terms"] = ["hidden diagnostic", "compile failed"]
        cases.append(case)
        case = derive("inspection", "run list TSV", "web terminal")
        case["args"] += ["--web"]
        case["responses"] = {ORG_LIST: ORG_RESPONSE}
        case["tty"] = case["merge_terminal"] = case["presentation"] = True
        case["browser_capture"] = True
        case["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
        cases.append(case)
    return cases


def compare(scenario, python, go):
    original = scenario | {"name": scenario["origin_name"]}
    source = module(scenario["origin_suite"])
    comparator = getattr(source, "compare", inspection.compare)
    if "since_seconds" in scenario:
        comparator = workflow.compare
    differences = comparator(original, python, go)
    for label, result in [("Python", python), ("Go", go)]:
        if scenario.get("selected_org") and WRONG_ORG in json.dumps(result["requests"]):
            differences.append(label + " used environment org instead of explicit option")
        if scenario.get("unknown_org") and any(request["path"] != ORG_LIST[1] for request in result["requests"]):
            differences.append(label + " continued after unknown explicit org")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/option-gaps-parity.json")
    )
