"""Bounded run diagnostics parity, including report data and terminal styles."""

from copy import deepcopy
from datetime import datetime
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_metrics as metrics
from scripts import compare_job_watch as watch
from scripts import compare_jobs as jobs
from typing import Any
from urllib.parse import urlencode
import json
import sys

ORG, RUN, JOB, REPO = inspection.ORG, inspection.RUN, inspection.JOB, inspection.REPO
VIEW = ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}?include=workflow")
JOBS = ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}/jobs?limit=100&order=created_at.asc&include=steps")
METRICS = ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}/metrics?source=cpu&source=memory&source=disk-io&source=network")
BASELINE = (
    "GET",
    f"/orgs/{ORG}/workflow-runs/aggregate?"
    + urlencode(
        {
            "repository_ids": REPO,
            "conclusions": "success",
            "workflow_platform_ids": 123,
            "created_after": "2025-12-02T00:00:00Z",
            "created_before": "2025-12-31T23:59:59.999999Z",
            "time_bucket": "total",
        }
    ),
)
RUN_DATA = inspection.RUN_DATA | {
    "repository_id": REPO,
    "platform_workflow_id": 123,
    "conclusion": "failure",
    "created_at": "2026-01-01T00:00:00Z",
    "started_at": "2026-01-01T00:00:05Z",
    "updated_at": "2026-01-01T00:01:05Z",
}
STEP = jobs.STEP | {"started_at": "2026-01-01T00:00:10Z", "completed_at": "2026-01-01T00:00:25Z"}
JOB_DATA = jobs.JOB_DATA | {
    "created_at": "2026-01-01T00:00:05Z",
    "started_at": "2026-01-01T00:00:08Z",
    "completed_at": "2026-01-01T00:01:05Z",
    "steps": [STEP],
}
SOURCES: dict[str, Any] = {s: metrics.METRICS[s] for s in ("cpu", "memory", "disk-io", "network")}
SOURCES["cpu"] = SOURCES["cpu"] | {"unit": "ratio"}
SOURCES["disk-io"] = {
    "unit": "bytes",
    "rate_unit": "bytes/sec",
    "series": [
        metrics.series([100, 160, 20, 55], rates=[10, 12, 20]),
        metrics.series([1000, 1500, 1600], rates=[100, 150, 160]),
    ],
}
METRIC_ENTRY = {"job_id": JOB, "metrics": SOURCES, "error": None}
LOG_RESPONSE = (200, {"results": list(reversed(jobs.LINES)), "has_more": False})
BASE_DATA = {
    "data": [{"success_count": 5, "success_median_duration_seconds": 30.0, "success_p95_duration_seconds": 50.0}]
}


def responses(run=RUN_DATA, job_list=None, metric_entries=None):
    return {
        VIEW: (200, {"data": run}),
        JOBS: (200, {"data": [JOB_DATA] if job_list is None else job_list, "pagination": {}}),
        METRICS: (200, {"data": [METRIC_ENTRY] if metric_entries is None else metric_entries}),
        BASELINE: (200, BASE_DATA),
        jobs.LOGS: LOG_RESPONSE,
    }


def scenarios():
    cases = []

    def case(name, **kwargs):
        cases.append(
            {
                "name": "run diagnose " + name,
                "args": ["run", "diagnose", RUN, "--json"],
                "responses": responses(),
                "json": True,
                "diagnostic_report": True,
                **kwargs,
            }
        )

    case("complete report")
    case("human report", args=["run", "diagnose", RUN], json=False, presentation=True, diagnostic_report=False)
    case("org slug", args=["run", "diagnose", RUN, "--org", "acme", "--json"], responses=jobs.ORGS | responses())
    platform_path = ("GET", f"/orgs/{ORG}/workflow-runs/by-platform-id/9007199254740993?include=workflow")
    for ref in ["9007199254740993", "https://github.com/acme/web/actions/runs/9007199254740993"]:
        case(
            "reference " + ref,
            args=["run", "diagnose", ref, "--json"],
            responses=responses() | {platform_path: (200, {"data": RUN_DATA})},
        )
    case("baseline malformed", responses=responses() | {BASELINE: (200, {})})
    invalid = deepcopy(SOURCES)
    invalid["cpu"]["series"][0]["values"][0][1] = "not-a-number"
    case("metrics invalid sample", responses=responses(metric_entries=[METRIC_ENTRY | {"metrics": invalid}]))
    case("empty jobs", responses=responses(job_list=[], metric_entries=[]))
    case("baseline empty", responses=responses() | {BASELINE: (200, {"data": []})})
    case(
        "baseline one success",
        responses=responses()
        | {BASELINE: (200, {"data": [{"success_count": 1, "success_median_duration_seconds": 0.0}]})},
    )
    case("baseline missing metadata", responses=responses(run=RUN_DATA | {"platform_workflow_id": None}))
    case(
        "baseline nested repository",
        responses=responses(run={k: v for k, v in RUN_DATA.items() if k != "repository_id"}),
    )
    for code in [401, 403, 404, 503]:
        for component, endpoint in [("metrics", METRICS), ("baseline", BASELINE), ("logs", jobs.LOGS)]:
            case(f"{component} HTTP {code}", responses=responses() | {endpoint: (code, {"detail": "fixture denied"})})
        case(
            f"jobs HTTP {code}",
            responses=responses() | {JOBS: (code, {"detail": "fixture denied"})},
            exit=4 if code == 401 else 1,
            diagnostic_report=False,
            json=False,
        )
    case("metrics no execution", responses=responses(metric_entries=[{"job_id": JOB, "error": "no_execution"}]))
    case("metrics missing", responses=responses(metric_entries=[]))
    case("metrics partial", responses=responses(metric_entries=[METRIC_ENTRY | {"metrics": {"cpu": SOURCES["cpu"]}}]))
    case(
        "metrics malformed", responses=responses() | {METRICS: (200, {"data": [{"job_id": JOB, "metrics": "invalid"}]})}
    )
    case("logs malformed", responses=responses() | {jobs.LOGS: (200, {"results": [{"content": None}]})})
    case(
        "logs has more",
        responses=responses() | {jobs.LOGS: (200, {"results": list(reversed(jobs.LINES)), "has_more": True})},
    )
    line_list = [{"line_number": i, "content": f"line-{i}", "level": "error"} for i in range(101, 0, -1)]
    case("logs line bound", responses=responses() | {jobs.LOGS: (200, {"results": line_list, "has_more": False})})
    case(
        "logs UTF8 byte bound",
        responses=responses()
        | {
            jobs.LOGS: (
                200,
                {
                    "results": [
                        {"line_number": 2, "content": "€" * 9000, "level": "error"},
                        {"line_number": 1, "content": "old"},
                    ],
                    "has_more": False,
                },
            )
        },
    )
    case("non Avrea", responses=responses(job_list=[JOB_DATA | {"running_on_avrea": False}]))
    case("successful job", responses=responses(job_list=[JOB_DATA | {"conclusion": "success"}]))
    case("job clock skew", responses=responses(job_list=[JOB_DATA | {"started_at": "2025-12-31T23:59:00Z"}]))
    case("run clock skew", responses=responses(run=RUN_DATA | {"started_at": "2025-12-31T23:59:00Z"}))
    case(
        "step clock skew",
        responses=responses(job_list=[JOB_DATA | {"steps": [STEP | {"completed_at": "2026-01-01T00:00:01Z"}]}]),
    )
    case("missing timestamps", responses=responses(job_list=[JOB_DATA | {"started_at": None, "completed_at": None}]))
    case(
        "active elapsed time",
        responses=responses(
            run=RUN_DATA | {"status": "in_progress", "conclusion": None},
            job_list=[
                JOB_DATA
                | {
                    "state": "in_progress",
                    "conclusion": None,
                    "steps": [STEP | {"status": "in_progress", "conclusion": None}],
                }
            ],
        ),
    )
    many = [
        JOB_DATA
        | {
            "job_id": f"job-019a0000000070008000{i:012x}",
            "job_name": f"job-{i}",
            "running_on_avrea": False,
            "steps": [STEP] * 11,
        }
        for i in range(101)
    ]
    case("jobs and global steps bounds", responses=responses(job_list=many, metric_entries=[]))
    case(
        "jobs cursor bound",
        responses=responses() | {JOBS: (200, {"data": [JOB_DATA], "pagination": {"next_cursor": "more"}})},
    )
    eleven = [JOB_DATA | {"job_id": f"job-019a0000000070008000{i:012x}", "job_name": f"job-{i}"} for i in range(11)]
    case("failed job bound", responses=responses(job_list=eleven, metric_entries=[]))
    partial = responses() | {
        METRICS: (503, {"detail": "down"}),
        BASELINE: (503, {"detail": "down"}),
        jobs.LOGS: (503, {"detail": "down"}),
    }
    case(
        "partial human",
        args=["run", "diagnose", RUN],
        responses=partial,
        json=False,
        diagnostic_report=False,
        presentation=True,
    )
    case("missing auth", responses={}, env={}, exit=4, diagnostic_report=False, json=False)
    case(
        "invalid reference",
        args=["run", "diagnose", "bad"],
        responses={},
        exit=1,
        diagnostic_report=False,
        json=False,
        error_terms=["run reference"],
    )
    if sys.platform != "win32":
        for name in ["human report", "partial human"]:
            item = deepcopy(next(c for c in cases if c["name"] == "run diagnose " + name))
            item["tty"] = item["merge_terminal"] = True
            item["env"] = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "NO_COLOR": "", "TERM": "xterm-256color"}
            item["name"] += " terminal"
            cases.append(item)
            cases.append(item | {"name": item["name"] + " no color", "args": ["--no-color", *item["args"]]})
    for item in cases:
        item.setdefault("env", {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG})
    return cases


def compare(scenario, python, go):
    python, go = deepcopy(python), deepcopy(go)
    differences = []
    generated_times = []
    for result in (python, go):
        # Independent evidence requests are concurrent. Preserve their complete
        # bodies/counts, but do not assert the scheduler's arrival order.
        result["requests"] = sorted(result["requests"], key=lambda r: json.dumps(r, sort_keys=True))
        if scenario.get("diagnostic_report") and result["exit"] == 0:
            report = json.loads(result["stdout"])
            generated = datetime.fromisoformat(report["generated_at"].replace("Z", "+00:00"))
            generated_times.append(generated)
            report["generated_at"] = "<generated>"
            pairs = [(report["timing"], report["run"])]
            for entry in report["jobs"]:
                pairs.append((entry["timing"], entry["job"]))
                pairs.extend((step["timing"], step["step"]) for step in entry["steps"])
            for timing, entity in pairs:
                if timing["execution_end_source"] != "generated_at":
                    continue
                started = datetime.fromisoformat(entity["started_at"].replace("Z", "+00:00"))
                expected = (generated - started).total_seconds()
                if abs(timing["execution_seconds"] - expected) > 0.00001:
                    differences.append("incorrect elapsed time")
                timing["execution_seconds"] = "<elapsed>"
            result["stdout"] = json.dumps(report)
    if len(generated_times) == 2 and abs((generated_times[1] - generated_times[0]).total_seconds()) > 5:
        differences.append("report generation time drift")
    return differences + watch.compare(scenario, python, go)


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/run-diagnose-parity.json"
        )
    )
