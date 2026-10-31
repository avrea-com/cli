"""Current-main native CI and GitLab inspection parity, using local fixtures."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_jobs as jobs
from scripts import compare_run_control as control
from scripts import compare_workflow as workflow
import sys

ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": inspection.ORG}


def native(value):
    if isinstance(value, str):
        for old, new in [("run-019a", "awr-019a"), ("job-019a", "awj-019a"), ("wfl-019a", "awf-019a")]:
            value = value.replace(old, new)
        return value
    if isinstance(value, list):
        return [native(v) for v in value]
    if isinstance(value, tuple):
        return tuple(native(v) for v in value)
    if not isinstance(value, dict):
        return value
    result = {native(k): native(v) for k, v in value.items()}
    if "job_id" in result:
        result |= {"ci_provider": "avrea_ci", "platform_job_id": None, "platform_run_id": None, "platform": "gitlab"}
    if "run_id" in result and ("run_number" in result or "status" in result):
        old = result.get("workflow") or {}
        result |= {
            "ci_provider": "avrea_ci",
            "platform_run_id": None,
            "run_number": None,
            "run_attempt": None,
            "workflow": None,
            "workflow_name": old.get("name", "Native build"),
            "workflow_path": old.get("path", ".avrea/workflows/build.yml"),
        }
    if "workflow_id" in result and "platform_id" in result:
        result |= {"ci_provider": "avrea_ci", "platform_id": None}
    return result


def scenarios():
    cases = []
    selections = [
        (
            inspection,
            {
                "run list JSON",
                "run list TSV",
                "run list jq",
                "view JSON",
                "view metadata and jobs",
                "view steps",
                "watch completed stream",
                "watch transitions suppress duplicate jobs",
                "watch failure exit status",
                "watch live terminal",
                "watch terminal already finished",
                "styled run list TSV",
                "styled run status matrix",
                "styled view steps",
                "styled recent runs",
                "run terminal links default",
                "styled view metadata and jobs",
            },
        ),
        (
            jobs,
            {
                "job list JSON",
                "job list TSV",
                "job view JSON",
                "job view human",
                "job logs",
                "job logs failed",
                "run logs",
                "run logs failed",
                "job follow completion",
                "run follow selects active",
                "styled job list TSV",
                "styled job view human",
                "styled run logs failed",
            },
        ),
        (workflow, {"workflow list JSON", "workflow view JSON", "workflow view human", "workflow view human terminal"}),
    ]
    for module, names in selections:
        for original in module.scenarios():
            if original["name"] not in names:
                continue
            case = native(deepcopy(original))
            case["name"] = "native " + original["name"]
            case.pop("stdout_contains", None)
            case.pop("stdout_terms", None)
            if "links" in case:
                case["links"] = [link for link in case["links"] if "github.com" not in link]
                case["presentation"] = True
            if "watch live terminal" in original["name"]:
                case["presentation"] = True
            cases.append(case)
    for module, seed_name, prefix in [(inspection, "run list JSON", "run"), (jobs, "job list JSON", "job")]:
        seed = next(c for c in module.scenarios() if c["name"] == seed_name)
        for provider in ("avrea_ci", "github_actions", "gitlab_ci"):
            case = deepcopy(seed)
            case["name"] = prefix + " provider " + provider
            case["args"] += ["--ci-provider", provider]
            case["responses"] = {
                (method, path + "&ci_provider=" + provider if "?" in path else path): value
                for (method, path), value in case["responses"].items()
            }
            cases.append(case)
        cases.append(
            {
                "name": prefix + " invalid provider",
                "args": [prefix, "list", "--ci-provider", "unknown"],
                "env": ENV,
                "exit": 2,
                "error_terms": ["--ci-provider"],
            }
        )
    job_views = ["job view human", "job view web piped", "job list TSV"]
    if sys.platform != "win32":
        job_views += ["styled job view human", "styled job list TSV"]
    for provider in (None, "gitlab_ci"):
        for name in job_views:
            case = deepcopy(next(c for c in jobs.scenarios() if c["name"] == name))
            for response in case["responses"].values():
                if response[0] == 200 and isinstance(response[1], dict):
                    data = response[1].get("data", [])
                    for row in data if isinstance(data, list) else [data]:
                        if "job_id" in row:
                            row["platform"] = "gitlab"
                            if provider:
                                row["ci_provider"] = provider
            case["name"] = f"GitLab {provider} {name}"
            cases.append(case)
    for command, include, feature in [("rerun", "", "Reruns"), ("diagnose", "?include=workflow", "Diagnostics")]:
        rid = native(inspection.RUN)
        cases.append(
            {
                "name": "native unavailable " + command,
                "args": ["run", command, rid, *(["--yes"] if command == "rerun" else [])],
                "env": ENV,
                "responses": {
                    ("GET", f"/orgs/{inspection.ORG}/workflow-runs/{rid}{include}"): (
                        200,
                        {"data": native(inspection.RUN_DATA)},
                    )
                },
                "exit": 1,
                "error_terms": [feature, "not available for Avrea CI yet"],
            }
        )
    cancel = deepcopy(next(c for c in control.scenarios() if c["name"] == "run cancel yes"))
    cases.append(native(cancel) | {"name": "native cancel yes"})
    for steps in ([], [jobs.STEP | {"conclusion": None}]):
        case = native(deepcopy(next(c for c in jobs.scenarios() if c["name"] == "job logs failed")))
        case["name"] = "native unknown step outcomes " + str(len(steps))
        case["responses"][native(jobs.LOG_JOB)] = (200, {"data": native(jobs.JOB_DATA | {"steps": steps})})
        cases.append(case)
    case = native(deepcopy(next(c for c in jobs.scenarios() if c["name"] == "job logs failed")))
    case["name"] = "native duplicate step names use record IDs"
    steps = [jobs.STEP | {"log_record_id": "first", "conclusion": "success"}, jobs.STEP | {"log_record_id": "second"}]
    case["responses"][native(jobs.LOG_JOB)] = (200, {"data": native(jobs.JOB_DATA | {"steps": steps})})
    case["responses"][jobs.LOGS] = (
        200,
        {
            "results": [
                {
                    "line_number": 1,
                    "step_name": "Build",
                    "step_record_id": "first",
                    "content": "successful first invocation",
                    "level": "info",
                },
                {
                    "line_number": 2,
                    "step_name": "Build",
                    "step_record_id": "second",
                    "content": "failed second invocation",
                    "level": "error",
                },
            ],
            "has_more": False,
            "next_cursor": 2,
        },
    )
    cases.append(case)
    case = native(deepcopy(next(c for c in jobs.scenarios() if c["name"] == "job follow final drain")))
    case["name"] = "native queued job completes without execution"
    case["responses"][jobs.LOGS] = (404, {"detail": "No execution found for job"})
    cases.append(case)
    for value in (inspection.JOB, "https://gitlab.com/acme/web/-/pipelines/42"):
        cases.append(
            {
                "name": "run reference rejected " + value,
                "args": ["run", "view", value],
                "env": ENV,
                "exit": 2,
                "error_terms": ["avr job view"],
            }
        )
    if sys.platform != "win32":
        for key in (None, ""):
            cases.append(
                {
                    "name": "SSH refuses missing key " + repr(key),
                    "args": ["job", "ssh", jobs.JOB],
                    "env": ENV,
                    "responses": {jobs.SSH: (200, jobs.SSH_DATA | {"host_key": key})},
                    "ssh_stub": 0,
                    "exit": 1,
                }
            )
    cases.append(
        {
            "name": "native workflow dispatch unavailable",
            "args": ["workflow", "run", native(workflow.WF), "--repo", inspection.REPO, "--no-watch"],
            "env": ENV,
            "responses": {workflow.LOOKUP: (200, {"data": [native(workflow.WORKFLOW)]})},
            "exit": 1,
            "error_terms": ["Manual workflow dispatch", "not available for Avrea CI yet"],
        }
    )
    exact = deepcopy(next(c for c in workflow.scenarios() if c["name"] == "workflow view human"))
    exact["name"] = "workflow full path wins over same display name"
    exact["args"][2] = ".github/workflows/build.yml"
    exact["args"] += ["--repo", inspection.REPO]
    exact["responses"][workflow.LOOKUP] = (
        200,
        {
            "data": [
                workflow.WORKFLOW,
                workflow.WORKFLOW
                | {
                    "workflow_id": workflow.WF2,
                    "name": ".github/workflows/build.yml",
                    "path": ".github/workflows/other.yml",
                },
            ]
        },
    )
    cases.append(exact)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=inspection.compare, default_report="build/go/native-ci-parity.json"
        )
    )
