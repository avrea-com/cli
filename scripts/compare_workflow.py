"""Workflow discovery parity against the pinned Python command implementation."""

from copy import deepcopy
from datetime import UTC
from datetime import datetime
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from urllib.parse import parse_qsl
from urllib.parse import urlencode
from urllib.parse import urlsplit
from urllib.parse import urlunsplit
import sys

ORG, REPO = inspection.ORG, inspection.REPO
WF = "wfl-019a0000000070008000000000000004"
WF2 = "wfl-019a0000000070008000000000000008"
WORKFLOW = {
    "workflow_id": WF,
    "platform_id": 9007199254740993,
    "name": "Build web",
    "path": ".github/workflows/build.yml",
    "repository_full_name": "acme/web",
    "platform": "github",
}
JOB = {
    "job": {"name": "Build web"},
    "count": 10,
    "completed_count": 8,
    "failure_count": 2,
    "flaked_count": 1,
    "median_duration_seconds": 90.125,
    "p95_duration_seconds": 3800,
}
BUCKET = {
    "workflow": WORKFLOW,
    "count": 20,
    "completed_count": 10,
    "failure_count": 2,
    "flaked_count": 1,
    "median_duration_seconds": 63.75,
    "p95_duration_seconds": 121.125,
    "jobs": [
        JOB,
        JOB | {"job": {"name": "Lint"}, "median_duration_seconds": 12, "failure_count": 0, "completed_count": 0},
    ],
}
LIST = ("GET", f"/orgs/{ORG}/workflow-runs/aggregate?time_bucket=total")
VIEW = ("GET", f"/orgs/{ORG}/workflow-runs/aggregate?time_bucket=total&workflow_ids={WF}&include=jobs")
RECENT = ("GET", f"/orgs/{ORG}/workflow-runs?workflow_ids={WF}&limit=5&order=created_at.desc")
LOOKUP = ("GET", f"/orgs/{ORG}/repos/{REPO}/workflows")
BASE = {
    ("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS}),
    LIST: (200, {"data": [BUCKET | {"count": 1, "workflow": WORKFLOW | {"workflow_id": WF2, "name": "Lint"}}, BUCKET]}),
    VIEW: (200, {"data": [BUCKET]}),
    RECENT: (200, {"data": [inspection.RUN_DATA | {"created_at": "2026-01-01T00:00:00Z"}]}),
    LOOKUP: (200, {"data": [WORKFLOW]}),
}


def scenarios():
    cases = []
    for name, args, extra in [
        ("list human", ["list"], {}),
        ("list JSON", ["list", "--json", "*"], {"json": True}),
        ("list jq", ["list", "--json", "name,runs", "-q", "[.[]]"], {"json": True}),
        ("list limit", ["list", "-L", "1"], {}),
        ("list JSON limit", ["list", "-L", "1", "--json", "name,runs"], {"json": True}),
        ("view human", ["view", WF], {}),
        ("view JSON", ["view", WF, "--json", "*"], {"json": True}),
        ("view jq", ["view", WF, "--json", "jobs", "--jq", ".jobs[].job.name"], {}),
    ]:
        cases.append(
            {"name": "workflow " + name, "args": ["workflow", *args, "--since", "all"], "responses": BASE, **extra}
        )
    for cmd, args, endpoint in [("list", [], LIST), ("view", [WF], VIEW)]:
        for mode in ["human", "JSON"]:
            cases.append(
                {
                    "name": f"workflow {cmd} empty {mode}",
                    "args": ["workflow", cmd, *args, "--since", "all", *(["--json", "*"] if mode == "JSON" else [])],
                    "responses": BASE | {endpoint: (200, {"data": []})},
                    "json": mode == "JSON",
                }
            )
        for suffix, flags, extra in [
            ("fields", ["--json", "?"], {"env": {}}),
            ("unknown field", ["--json", "typo"], {"exit": 1, "error_terms": ["Unknown JSON field"]}),
            ("jq requires json", ["--jq", "."], {"exit": 2, "error_terms": ["--jq requires --json"]}),
            ("missing auth", [], {"env": {}, "exit": 4}),
            ("invalid since", ["--since", "oops"], {"exit": 1, "error_terms": ["--since", "oops"]}),
        ]:
            cases.append(
                {
                    "name": f"workflow {cmd} {suffix}",
                    "args": ["workflow", cmd, *args, *flags],
                    "responses": BASE,
                    **extra,
                }
            )
        for status in [401, 403, 500]:
            cases.append(
                {
                    "name": f"workflow {cmd} HTTP {status}",
                    "args": ["workflow", cmd, *args, "--since", "all"],
                    "responses": BASE | {endpoint: (status, {"detail": "fixture rejected"})},
                    "exit": 4 if status == 401 else 1,
                    **({} if status == 401 else {"error_terms": [str(status), "fixture rejected"]}),
                }
            )
        for flags, seconds in [
            ([], 30 * 86400),
            (["--since", "7d"], 7 * 86400),
            (["--since", "24h"], 86400),
            (["--since", "30m"], 1800),
        ]:
            cases.append(
                {
                    "name": f"workflow {cmd} relative window {seconds}",
                    "args": ["workflow", cmd, *args, *flags],
                    "since_seconds": seconds,
                }
            )
    for value in ["Build web", "BUILD WEB", "build.yml", "BUILD", "9007199254740993"]:
        cases.append(
            {
                "name": "workflow lookup " + value,
                "args": ["workflow", "view", value, "--repo", REPO, "--since", "all", "--json", "*"],
                "responses": BASE,
                "json": True,
            }
        )
    for value in ["missing", "9999"]:
        cases.append(
            {
                "name": "workflow lookup missing " + value,
                "args": ["workflow", "view", value, "--repo", REPO, "--since", "all"],
                "responses": BASE,
                "exit": 1,
                "error_terms": ["not found", value],
            }
        )
    cases.extend(
        [
            {
                "name": "workflow ambiguous",
                "args": ["workflow", "view", "Build web", "--repo", REPO],
                "responses": BASE | {LOOKUP: (200, {"data": [WORKFLOW, WORKFLOW | {"workflow_id": WF2}]})},
                "exit": 1,
                "error_terms": ["Ambiguous", "Matches:", "--wfl-id"],
            },
            {
                "name": "workflow lookup pagination",
                "args": ["workflow", "view", "build", "--repo", REPO, "--since", "all", "--json", "*"],
                "responses": BASE
                | {
                    LOOKUP: (200, {"data": [], "pagination": {"next_cursor": "page2"}}),
                    ("GET", LOOKUP[1] + "?cursor=page2"): (200, {"data": [WORKFLOW]}),
                },
                "json": True,
            },
            {
                "name": "workflow view requires repository",
                "args": ["workflow", "view", "build"],
                "exit": 1,
                "error_terms": ["determine repository", "--repo", "AVR_REPO"],
            },
            {
                "name": "workflow list repository",
                "args": ["workflow", "list", "--repo", REPO, "--repo", "rep-fixture2", "--since", "all", "--json", "*"],
                "responses": {("GET", LIST[1] + f"&repository_ids={REPO}&repository_ids=rep-fixture2"): BASE[LIST]},
                "json": True,
            },
            {
                "name": "workflow recent unavailable",
                "args": ["workflow", "view", WF, "--since", "all"],
                "responses": BASE | {RECENT: (503, {"detail": "unavailable"})},
            },
            {"name": "workflow web piped", "args": ["workflow", "view", WF, "--web"], "responses": BASE},
            {
                "name": "workflow web json conflict",
                "args": ["workflow", "view", WF, "--web", "--json", "*"],
                "exit": 2,
                "error_terms": ["--web", "--json"],
            },
            {
                "name": "workflow alias",
                "args": ["workflows", "list", "--since", "all", "--json", "name"],
                "responses": BASE,
                "json": True,
            },
        ]
    )
    for limit in ["0", "1001"]:
        cases.append(
            {
                "name": "workflow invalid limit " + limit,
                "args": ["workflow", "list", "-L", limit],
                "exit": 2,
                "error_terms": ["limit"],
            }
        )

    for code in [401, 403, 404, 500]:
        cases.append(
            {
                "name": f"workflow lookup HTTP {code}",
                "args": ["workflow", "view", "build", "--repo", REPO],
                "responses": BASE | {LOOKUP: (code, {"detail": "fixture rejected"})},
                "exit": 4 if code == 401 else 1,
                **(
                    {}
                    if code == 401
                    else {
                        "error_terms": [
                            str(code),
                            "fixture rejected",
                            *(["not in this org", "avr repo list"] if code == 404 else []),
                        ]
                    }
                ),
            }
        )
    resolved = {
        ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb&platform=github"): (
            200,
            {"data": inspection.REPOSITORIES[0]},
        )
    }
    for value in ["list", "view"]:
        args = ["workflow", value, *([WF] if value == "view" else []), "--since", "all", "--json", "*"]
        responses = BASE | ({("GET", LIST[1] + f"&repository_ids={REPO}"): BASE[LIST]} if value == "list" else {})
        cases.append(
            {
                "name": f"workflow {value} git context",
                "args": args,
                "git_remote": "git@github.com:acme/web.git",
                "responses": responses | resolved,
                "json": True,
            }
        )
    cases.append(
        {
            "name": "workflow name git context",
            "args": ["workflow", "view", "build", "--since", "all", "--json", "*"],
            "git_remote": "git@github.com:acme/web.git",
            "responses": BASE | resolved,
            "json": True,
        }
    )
    varied = []
    for index, failure in enumerate([0, 4, 5, 19, 20, 100]):
        varied.append(
            BUCKET
            | {
                "count": 100 - index,
                "completed_count": 100,
                "failure_count": failure,
                "median_duration_seconds": None if index == 0 else 60 * index,
                "workflow": WORKFLOW
                | {"name": "Build and deploy " + "界" * 45, "repository_full_name": "acme/" + "long-name-" * 8},
            }
        )
    cases.append(
        {
            "name": "workflow list rate colors and truncation",
            "args": ["workflow", "list", "--since", "all"],
            "responses": BASE | {LIST: (200, {"data": varied})},
            "tty": sys.platform != "win32",
            "presentation": sys.platform != "win32",
            "env": {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "NO_COLOR": "", "TERM": "xterm-256color"},
        }
    )

    for case in cases:
        case.setdefault("env", {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG})
    if sys.platform != "win32":
        names = {
            "workflow list human",
            "workflow list limit",
            "workflow view human",
            "workflow recent unavailable",
            "workflow web piped",
        }
        for original in [c for c in cases if c["name"] in names]:
            for no_color in [False, True]:
                case = deepcopy(original)
                case["name"] += " terminal" + (" no color" if no_color else "")
                case["tty"] = case["merge_terminal"] = case["presentation"] = True
                case["args"] = ["--no-links", *case["args"]]
                if no_color:
                    case["args"].insert(0, "--no-color")
                case["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
                cases.append(case)
        cases.append(
            {
                "name": "workflow view terminal links",
                "args": ["workflow", "view", WF, "--since", "all"],
                "responses": BASE,
                "tty": True,
                "merge_terminal": True,
                "presentation": True,
                "env": {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "NO_COLOR": "", "TERM": "xterm-256color"},
                "links": [
                    f"http://127.0.0.1/org/acme/workflows/{WF}",
                    f"http://127.0.0.1/org/acme/runs/{inspection.RUN}",
                    "https://github.com/acme/web/tree/main",
                ],
            }
        )
    return cases


def compare(scenario, python, go):
    differences = []
    if "since_seconds" in scenario:
        python, go = deepcopy(python), deepcopy(go)
        for result in (python, go):
            seen = False
            for request in result["requests"]:
                parts = urlsplit(request["path"])
                pairs = []
                for key, value in parse_qsl(parts.query):
                    if key == "created_after":
                        seen = True
                        cutoff = datetime.fromisoformat(value)
                        delta = (datetime.now(UTC) - cutoff).total_seconds()
                        if abs(delta - scenario["since_seconds"]) > 30:
                            differences.append("incorrect relative cutoff")
                        value = "<relative cutoff>"
                    pairs.append((key, value))
                request["path"] = urlunsplit(parts._replace(query=urlencode(pairs)))
            if not seen:
                differences.append("missing relative cutoff")
    return differences + inspection.compare(scenario, python, go)


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/workflow-parity.json")
    )
