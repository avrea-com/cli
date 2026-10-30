"""Run the inherited plural aliases against Python using established local fixtures."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from scripts import compare_jobs
from scripts import compare_log_search
from scripts import compare_pr
from scripts import compare_token
from scripts import compare_vm_inspection
from scripts import compare_workflow
import sys


def scenarios():
    result = []
    for alias, target, factory in [
        ("jobs", "job", compare_jobs.scenarios),
        ("logs", "log", compare_log_search.scenarios),
        ("orgs", "org", inspection.scenarios),
        ("prs", "pr", compare_pr.scenarios),
        ("repos", "repo", inspection.scenarios),
        ("tokens", "token", compare_token.scenarios),
        ("vms", "vm", compare_vm_inspection.scenarios),
        ("workflows", "workflow", compare_workflow.scenarios),
    ]:
        original = next(
            row for row in factory() if row["args"][0] == target and row.get("exit", 0) == 0 and not row.get("tty")
        )
        case = deepcopy(original)
        case["name"] = "plural alias " + alias
        case["args"][0] = alias
        result.append(case)
    return result


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=watch.compare, default_report="build/go/aliases-parity.json"
        )
    )
