"""Workflow dispatch and watch parity; writes only reach loopback fixtures."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_workflow as workflow
import sys

ORG, REPO, WF, RUN = workflow.ORG, workflow.REPO, workflow.WF, inspection.RUN
REPOSITORY = ("GET", f"/orgs/{ORG}/repos/{REPO}")
DISPATCH = ("POST", f"/orgs/{ORG}/repos/{REPO}/dispatch-workflow")
POLL = ("GET", f"/orgs/{ORG}/workflow-runs?repository_ids={REPO}&limit=100&order=created_at.desc")
WATCH = ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}?include=jobs&include=workflow")
RUN_DATA = inspection.RUN_DATA | {
    "created_at": "2099-01-01T00:00:00Z",
    "platform_workflow_id": 9007199254740993,
    "jobs": [],
}
BASE = workflow.BASE | {
    REPOSITORY: (200, {"data": inspection.REPOSITORIES[0] | {"default_branch": "trunk"}}),
    DISPATCH: (200, {"data": {"workflow": "build.yml", "ref": "trunk", "platform_run_id": 9007199254740995}}),
    POLL: (200, {"data": [RUN_DATA]}),
    WATCH: (200, {"data": RUN_DATA}),
}


def scenarios():
    cases = []

    def add(name, flags=None, **extra):
        cases.append(
            {
                "name": "workflow run " + name,
                "args": [
                    "workflow",
                    "run",
                    extra.pop("workflow", "build.yml"),
                    "--repo",
                    REPO,
                    *(flags or []),
                    "--no-watch",
                ],
                "responses": BASE,
                **extra,
            }
        )

    add("filename")
    add("explicit ref", ["-r", "release/v2"])
    add("raw inputs", ["-f", " env =prod", "-f", "data=a=b", "--raw-field", "empty="])
    add("duplicate raw inputs", ["-f", "env=one", "-f", "env=two"])
    add(
        "JSON inputs",
        ["--json"],
        stdin='{"env":"prod","number":9007199254740993,"enabled":true,"none":null,"fraction":1.0}',
    )
    add(
        "JSON nested inputs",
        ["--json"],
        stdin='{"list":[1,true,null,"hi"],"object":{"z":"last","a":"first","z":"new"},"float":1000000.0}',
    )
    add("JSON empty", ["--json"], stdin="")
    add("JSON empty object", ["--json"], stdin="{}")
    for identifier in [WF, "build", "Build web", "9007199254740993"]:
        add("resolve " + identifier, workflow=identifier)
    for name, flags, extra in [
        ("invalid raw", ["-f", "missing"], {"error_terms": ["Invalid -f value", "expected key=value"]}),
        ("empty raw key", ["-f", " =x"], {"error_terms": ["key is empty"]}),
        ("JSON invalid", ["--json"], {"stdin": "{", "error_terms": ["Invalid JSON on stdin"]}),
        ("JSON array", ["--json"], {"stdin": "[]", "error_terms": ["JSON inputs must be an object"]}),
        ("JSON trailing", ["--json"], {"stdin": "{} {}", "error_terms": ["Invalid JSON on stdin"]}),
        ("JSON raw conflict", ["--json", "-f", "env=prod"], {"error_terms": ["Use either -f/--raw-field or --json"]}),
        ("interval invalid", ["--interval", "0"], {"error_terms": ["--interval must be at least 1 second"]}),
    ]:
        add(name, flags, exit=1, **extra)
    add("missing auth", env={}, exit=4)
    add(
        "default branch missing",
        responses=BASE | {REPOSITORY: (200, {"data": inspection.REPOSITORIES[0]})},
        exit=1,
        error_terms=["default branch", "Pass --ref"],
    )
    add(
        "no workflow path",
        workflow="Build web",
        responses=BASE | {workflow.LOOKUP: (200, {"data": [workflow.WORKFLOW | {"path": ""}]})},
        exit=1,
        error_terms=["no file path", "can't be dispatched"],
    )
    add("missing workflow", workflow="wfl-missing", exit=1, error_terms=["not found in this repository"])
    add(
        "no platform run id",
        responses=BASE
        | {DISPATCH: (200, {"data": {"workflow": "build.yml", "ref": "trunk", "platform_run_id": None}})},
    )
    for code in [401, 403, 404, 422, 500]:
        add(
            f"HTTP {code}",
            responses=BASE | {DISPATCH: (code, {"detail": "fixture rejected"})},
            exit=4 if code == 401 else 1,
            **({} if code == 401 else {"error_terms": [str(code), "fixture rejected"]}),
        )
    for name, flags, responses, extra in [
        ("default watch", [], BASE, {}),
        ("watch flags last wins", ["--no-watch", "--watch"], BASE, {}),
        ("watch short flags", ["-W", "-w"], BASE, {}),
        (
            "watch failed",
            ["--exit-status"],
            BASE | {WATCH: (200, {"data": RUN_DATA | {"conclusion": "failure"}})},
            {"exit": 1},
        ),
        ("watch retry", [], BASE | {POLL: [(503, {"detail": "retry"}), BASE[POLL]]}, {}),
        (
            "watch skip stale and unrelated",
            [],
            BASE
            | {
                POLL: (
                    200,
                    {
                        "data": [
                            RUN_DATA | {"created_at": "2020-01-01T00:00:00Z"},
                            RUN_DATA | {"platform_workflow_id": 123},
                            RUN_DATA,
                        ]
                    },
                )
            },
            {},
        ),
        (
            "watch forbidden",
            [],
            BASE | {POLL: (403, {"detail": "fixture rejected"})},
            {"exit": 1, "error_terms": ["403", "fixture rejected"]},
        ),
    ]:
        cases.append(
            {
                "name": "workflow run " + name,
                "args": ["workflow", "run", "build", "--repo", REPO, *flags],
                "responses": responses,
                "presentation": True,
                "no_terminal_controls": True,
                **extra,
            }
        )
    # Negative toggle wins when supplied last.
    cases.append(
        {
            "name": "workflow run no watch last",
            "args": ["workflow", "run", "build.yml", "--repo", REPO, "-w", "-W"],
            "responses": BASE,
        }
    )
    for case in cases:
        case.setdefault("env", {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG})
    if sys.platform != "win32":
        for name in [
            "workflow run filename",
            "workflow run resolve build",
            "workflow run default watch",
            "workflow run watch failed",
            "workflow run no platform run id",
        ]:
            case = deepcopy(next(c for c in cases if c["name"] == name))
            case["name"] += " terminal"
            case["args"] = ["--no-links", *case["args"]]
            case["tty"] = case["merge_terminal"] = case["presentation"] = True
            case.pop("no_terminal_controls", None)
            case["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(case)
        case = deepcopy(cases[0])
        case["name"] = "workflow run terminal links"
        case["tty"] = case["merge_terminal"] = case["presentation"] = True
        case["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
        case["links"] = ["https://github.com/acme/web/actions/runs/9007199254740995"]
        cases.append(case)
    return cases


def compare(scenario, python, go):
    differences = inspection.compare(scenario, python, go)
    if scenario.get("no_terminal_controls") and any("\x1b" in result["stdout"] for result in (python, go)):
        differences.append("terminal escapes in piped watch")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/workflow-run-parity.json"
        )
    )
