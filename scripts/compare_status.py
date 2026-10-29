"""Status dashboard parity with validated current and previous time windows."""

from copy import deepcopy
from datetime import UTC
from datetime import datetime
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from urllib.parse import parse_qsl
from urllib.parse import urlencode
from urllib.parse import urlsplit
from urllib.parse import urlunsplit
import sys

ORG, REPO, RUN = inspection.ORG, inspection.REPO, inspection.RUN
ORGS = {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})}
RUNS = ("GET", f"/orgs/{ORG}/workflow-runs?limit=5&order=created_at.desc&include=workflow")
WF = ("GET", f"/orgs/{ORG}/workflow-runs/aggregate")
JOB = ("GET", f"/orgs/{ORG}/jobs/aggregate")
CACHE = ("GET", f"/orgs/{ORG}/repos/{REPO}/cache")
REPO_VIEW = ("GET", f"/orgs/{ORG}/repos/{REPO}")
CACHE_DATA = {
    "total_size_bytes": 3 * 1024**3,
    "quota_bytes": 10 * 1024**3,
    "by_type": [
        {"cache_type": "actions", "size_bytes": 2 * 1024**3, "entry_count": 42},
        {"cache_type": "buildkit", "size_bytes": 1024**3, "entry_count": 7},
    ],
}
RUN_DATA = inspection.RUN_DATA | {"created_at": "2026-01-01T00:00:00Z"}
WORKFLOWS = [
    {
        "workflow_id": f"wfl-019a0000000070008000{i:012x}",
        "workflow": {
            "workflow_id": f"wfl-019a0000000070008000{i:012x}",
            "name": "Build " + str(i) + " very long workflow display name for truncation",
            "repository_id": REPO,
            "repository_full_name": "acme/a-long-repository-name-with-truncation",
        },
        "success_count": count,
        "success_median_duration_seconds": median,
        "median_duration_seconds": 99999.0,
    }
    for i, (count, median) in enumerate(
        [(4, 150.0), (2, 900.0), (1, 999.0), (3, 0.0), (3, 200.0), (5, 300.0), (4, 300.0), (3, 250.0)]
    )
]
PREVIOUS = [item | {"success_count": 4, "success_median_duration_seconds": 100.0} for item in WORKFLOWS]
JOBS = [item | {"job": {"name": f"Job {i}"}} for i, item in enumerate(WORKFLOWS)]
JOB_PREVIOUS = [item | {"success_count": 4, "success_median_duration_seconds": 100.0} for item in JOBS]


def responses(repo=False, runs=None):
    run_path = RUNS[1] + (f"&repository_ids={REPO}" if repo else "")
    result = ORGS | {("GET", run_path): (200, {"data": [RUN_DATA] if runs is None else runs})}
    if repo:
        result |= {CACHE: (200, {"data": CACHE_DATA}), REPO_VIEW: (200, {"data": inspection.REPOSITORIES[0]})}
    return result


def aggregates(wf=WORKFLOWS, previous=PREVIOUS, job=JOBS, job_previous=JOB_PREVIOUS):
    return {
        WF: [(200, {"data": wf}), (200, {"data": previous})],
        JOB: [(200, {"data": job}), (200, {"data": job_previous})],
    }


def scenarios():
    cases = []

    def case(name, extra=None, **kwargs):
        cases.append(
            {
                "name": "status " + name,
                "args": ["status", *(extra or [])],
                "responses": responses(),
                "dynamic_responses": aggregates(),
                "windows": True,
                "window_seconds": 7 * 86400,
                **kwargs,
            }
        )

    case("organization JSON", ["--json"], json=True)
    case("organization human", presentation=True)
    case("repository human", ["--repo", REPO], responses=responses(True), presentation=True)
    case("repository JSON", ["--repo", REPO, "--json"], responses=responses(True), json=True)
    case("empty human", responses=responses(runs=[]), dynamic_responses=aggregates([], [], [], []), presentation=True)
    case(
        "empty JSON", ["--json"], responses=responses(runs=[]), dynamic_responses=aggregates([], [], [], []), json=True
    )
    case("org slug", ["--org", "acme", "--json"], json=True)
    case(
        "repository name",
        ["--repo", "acme/web", "--json"],
        responses=responses(True)
        | {
            ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb"): (200, {"data": inspection.REPOSITORIES[0]}),
        },
        json=True,
    )
    case(
        "Git repository",
        ["--json"],
        responses=responses(True)
        | {
            ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb&platform=github"): (
                200,
                {"data": inspection.REPOSITORIES[0]},
            ),
        },
        git_remote="git@github.com:acme/web.git",
        json=True,
    )
    case(
        "Git fallback",
        ["--json"],
        responses=responses()
        | {
            ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb&platform=github"): (404, {"detail": "missing"}),
        },
        git_remote="git@github.com:acme/web.git",
        json=True,
    )
    for since, seconds in [("0d", 0), ("24h", 86400), ("30m", 1800)]:
        case("window " + since, ["--since", since, "--json"], json=True, window_seconds=seconds)
    for since in ["", "-7d", "1.5d", "invalid"]:
        case(
            "invalid since " + repr(since),
            ["--since", since],
            responses=ORGS,
            dynamic_responses={},
            windows=False,
            exit=1,
            error_terms=["--since"],
        )
    case("missing auth", responses={}, dynamic_responses={}, windows=False, env={}, exit=4)
    for code in [401, 403, 503]:
        case(
            f"runs HTTP {code}",
            responses=ORGS | {RUNS: (code, {"detail": "fixture denied"})},
            dynamic_responses={},
            windows=False,
            exit=4 if code == 401 else 1,
        )
    for endpoint, label in [(WF, "workflow"), (JOB, "job")]:
        for index in [0, 1]:
            dynamic = aggregates()
            dynamic[endpoint][index] = (503, {"detail": "fixture unavailable"})
            case(f"{label} period {index} unavailable", dynamic_responses=dynamic, presentation=True)
    for endpoint, label in [(CACHE, "cache"), (REPO_VIEW, "metadata")]:
        for code in [401, 503]:
            case(
                f"{label} HTTP {code}",
                ["--repo", REPO],
                responses=responses(True) | {endpoint: (code, {"detail": "fixture unavailable"})},
                presentation=True,
            )
    case(
        "zero quota",
        ["--repo", REPO],
        responses=responses(True)
        | {CACHE: (200, {"data": CACHE_DATA | {"quota_bytes": 0, "total_size_bytes": 0, "by_type": []}})},
        presentation=True,
    )
    case(
        "GitLab footer",
        ["--repo", REPO],
        responses=responses(True) | {REPO_VIEW: (200, {"data": inspection.REPOSITORIES[0] | {"platform": "gitlab"}})},
        presentation=True,
    )
    case(
        "insufficient previous samples",
        ["--json"],
        dynamic_responses=aggregates(
            previous=[item | {"success_count": 2} for item in PREVIOUS],
            job_previous=[item | {"success_count": 2} for item in JOB_PREVIOUS],
        ),
        json=True,
    )
    case(
        "faster current period",
        ["--json"],
        dynamic_responses=aggregates(
            previous=[item | {"success_median_duration_seconds": 1000.0} for item in PREVIOUS],
            job_previous=[item | {"success_median_duration_seconds": 1000.0} for item in JOB_PREVIOUS],
        ),
        json=True,
    )
    case(
        "missing metadata",
        dynamic_responses=aggregates(
            wf=[{"success_count": 2, "success_median_duration_seconds": 30.0}],
            previous=[],
            job=[{"success_count": 2, "success_median_duration_seconds": 30.0}],
            job_previous=[],
        ),
        presentation=True,
    )
    case(
        "missing age",
        responses=responses(runs=[RUN_DATA | {"created_at": None, "head_branch": None, "duration_seconds": None}]),
        presentation=True,
    )
    if sys.platform != "win32":
        for name in [
            "organization human",
            "repository human",
            "empty human",
            "workflow period 0 unavailable",
            "cache HTTP 503",
            "missing age",
            "missing metadata",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "status " + name))
            item["name"] += " terminal"
            item["args"] = ["--no-links", *item["args"]]
            item["tty"] = item["merge_terminal"] = True
            item["env"] = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
            if name == "repository human":
                cases.append(item | {"name": item["name"] + " no color", "args": ["--no-color", *item["args"]]})
        item = deepcopy(next(c for c in cases if c["name"] == "status repository human terminal"))
        item["name"] = "status repository links terminal"
        item["args"] = item["args"][1:]
        item["links"] = [
            f"http://127.0.0.1/org/acme/runs/{RUN}",
            "https://github.com/acme/web/tree/main",
            f"http://127.0.0.1/org/acme/activity?repositories={REPO}",
            *[f"http://127.0.0.1/org/acme/workflows/{WORKFLOWS[i]['workflow_id']}" for i in [0, 1, 4, 5, 6, 7]],
        ]
        cases.append(item)
    for item in cases:
        item.setdefault("env", {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG})
    return cases


def compare(scenario, python, go):
    python, go = deepcopy(python), deepcopy(go)
    differences = []
    if scenario.get("windows"):
        for result in (python, go):
            panels = {}
            for request in result["requests"]:
                parts = urlsplit(request["path"])
                if not parts.path.endswith(("/workflow-runs/aggregate", "/jobs/aggregate")):
                    continue
                params = dict(parse_qsl(parts.query))
                try:
                    after = datetime.fromisoformat(params["created_after"])
                    before = datetime.fromisoformat(params["created_before"]) if "created_before" in params else None
                    if after.tzinfo is None or (before is not None and before.tzinfo is None):
                        raise ValueError("missing timezone")
                except KeyError, ValueError:
                    differences.append("invalid aggregate window")
                    continue
                panels.setdefault(parts.path, []).append((after, before))
                fields = [
                    (
                        key,
                        ("<previous>" if before is not None else "<current>")
                        if key == "created_after"
                        else "<current>"
                        if key == "created_before"
                        else value,
                    )
                    for key, value in parse_qsl(parts.query, keep_blank_values=True)
                ]
                request["path"] = urlunsplit(parts._replace(query=urlencode(fields)))
            if len(panels) != 2 or any(len(v) != 2 for v in panels.values()):
                differences.append("missing aggregate window")
                continue
            windows = list(panels.values())
            if windows[0] != windows[1]:
                differences.append("inconsistent workflow/job windows")
            for current, previous in windows:
                seconds = scenario["window_seconds"]
                age = (datetime.now(UTC) - current[0]).total_seconds()
                if not seconds - 1 <= age <= seconds + 10:
                    differences.append("wrong current lookback")
                if (
                    current[1] is not None
                    or previous[1] != current[0]
                    or abs((current[0] - previous[0]).total_seconds() - seconds) > 1
                ):
                    differences.append("wrong previous lookback")
    return differences + watch.compare(scenario, python, go)


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/status-parity.json")
    )
