"""Exercise meaningful option values that lacked a successful command fixture."""

from scripts import compare_auth as harness
from scripts import compare_git_mirrors as mirrors
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from scripts import compare_option_gaps as gaps
from scripts import compare_workflow as workflow
import json
import sys


def scenarios():
    cases = []
    for suite, name in [
        ("artifact", "artifact delete yes JSON"),
        ("artifact", "artifact download JSON"),
        ("inspection", "run list JSON"),
        ("settings", "settings schema JSON"),
    ]:
        for spelling in ["-q", "--jq"]:
            case = gaps.derive(suite, name, "filtered with " + spelling)
            case["args"] += [spelling, '{"kind":type,"value":.}']
            case["jq_wrapper"] = True
            cases.append(case)
    for verb in ["status", "enable", "disable", "place", "unplace"]:
        for platform in ["github", "gitlab"]:
            case = gaps.derive("git_mirrors", "git mirror " + verb + " JSON", "source " + platform)
            case["args"] = ["acme/web" if arg == mirrors.REPO else arg for arg in case["args"]]
            case["args"] += ["--source", platform]
            endpoint = mirrors.RESOLVE.replace("platform=github", "platform=" + platform)
            case["responses"][("GET", endpoint)] = (200, {"data": inspection.REPOSITORIES[0] | {"platform": platform}})
            case["source_platform"] = platform
            cases.append(case)
    for seconds, value in [(604800, "7d"), (86400, "24h"), (1800, "30m"), (0, "0m")]:
        case = gaps.derive("inspection", "run list JSON", "since " + value)
        case["args"] += ["--since", value]
        case["responses"] = {}
        case["dynamic_responses"] = {
            ("GET", f"/orgs/{inspection.ORG}/workflow-runs"): (200, {"data": [inspection.RUN_DATA]})
        }
        case["since_seconds"] = seconds
        cases.append(case)
    for provider in ["github", "google"]:
        case = gaps.derive("auth", "github login", "explicit provider " + provider)
        case["args"] += ["--provider", provider]
        cases.append(case)
    case = gaps.derive("workflow_run", "workflow run default watch", "interval and success exit status")
    case["args"] += ["--interval", "1", "--exit-status"]
    cases.append(case)
    return cases


def compare(scenario, python, go):
    original = scenario | {"name": scenario["origin_name"]}
    source = gaps.module(scenario["origin_suite"])
    comparator = getattr(source, "compare", watch.compare)
    if "since_seconds" in scenario:
        comparator = workflow.compare
    differences = comparator(original, python, go)
    for label, result in [("Python", python), ("Go", go)]:
        if scenario.get("jq_wrapper"):
            try:
                value = json.loads(result["stdout"])
            except ValueError, TypeError:
                value = None
            if not isinstance(value, dict) or set(value) != {"kind", "value"}:
                differences.append(label + " did not apply jq object transformation")
        if "source_platform" in scenario:
            expected = "platform=" + scenario["source_platform"]
            if not any("/repos/resolve?" in item["path"] and expected in item["path"] for item in result["requests"]):
                differences.append(label + " did not resolve the selected source platform")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/option-values-parity.json"
        )
    )
