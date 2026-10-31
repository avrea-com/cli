"""Native dispatch parity. Every submission targets an isolated loopback fixture."""

from copy import deepcopy
from decimal import Decimal
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_workflow as workflow
from scripts import compare_workflow_run as github
import json
import re
import shlex
import sys
import uuid

ORG, REPO = workflow.ORG, workflow.REPO
WF = "awf-019a0000000070008000000000000004"
DISPATCH = "wap-019a0000000070008000000000000005"
RUN = "awr-019a0000000070008000000000000006"
KEY = "019a0000000070008000000000000007"
BASE = f"/orgs/{ORG}/repos/{REPO}"
POST = ("POST", BASE + "/workflow-dispatches")
GET = ("GET", BASE + "/workflow-dispatches/" + DISPATCH)
META = ("GET", BASE + "/workflows/" + WF + "/dispatch-metadata")
NATIVE = workflow.WORKFLOW | {
    "workflow_id": WF,
    "ci_provider": "avrea_ci",
    "path": ".avrea/workflows/build.yml",
    "platform_id": None,
}
METADATA = {
    "ref": "refs/heads/trunk",
    "dispatchable": True,
    "can_dispatch": True,
    "inputs": [
        {"name": "enabled", "type": "boolean"},
        {"name": "count", "type": "number"},
        {"name": "label", "type": "string"},
    ],
}
RECEIPT = {
    "dispatch_id": DISPATCH,
    "state": "pending",
    "reason": None,
    "run_ids": [],
    "ref": "refs/heads/main",
    "commit_sha": "a" * 40,
}
ADMITTED = RECEIPT | {"state": "admitted", "run_ids": [RUN]}
RESPONSES = {
    workflow.LOOKUP: (200, {"data": [NATIVE]}),
    META: (200, {"data": METADATA}),
    (META[0], META[1] + "?ref=main"): (200, {"data": METADATA}),
    POST: (202, {"data": RECEIPT}),
    GET: (200, {"data": ADMITTED}),
    ("GET", "/users/me/organizations"): workflow.BASE[("GET", "/users/me/organizations")],
    ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}?include=jobs&include=workflow"): (
        200,
        {"data": inspection.FINISHED_RUN | {"run_id": RUN, "ci_provider": "avrea_ci"}},
    ),
}


def scenarios():
    result = []

    def add(name, flags=(), **extra):
        args = extra.pop(
            "args",
            [
                "workflow",
                "run",
                extra.pop("workflow", WF),
                "--repo",
                REPO,
                "--ref",
                "main",
                "--idempotency-key",
                KEY,
                "--no-watch",
                *flags,
            ],
        )
        result.append(
            {
                "name": "native dispatch " + name,
                "args": args,
                "env": {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG},
                "responses": RESPONSES,
                **extra,
            }
        )

    add("pending receipt")
    add("default ref", args=["workflow", "run", WF, "--repo", REPO, "--idempotency-key", KEY, "--no-watch"])
    for value in ("bad", ""):
        add(
            "invalid UUID " + repr(value),
            args=["workflow", "run", WF, "--idempotency-key", value],
            responses={},
            exit=2,
        )
    add("JSON large exponent", ["--json"], stdin='{"count":1e10000}')
    add("duplicate JSON ignores earlier invalid value", ["--json"], stdin='{"count":[],"count":2}')
    add("typed fields", ["-f", "enabled=FALSE", "-f", "count=1.00000000000000001", "-f", "label=false"])
    add(
        "JSON scalars",
        ["--json"],
        stdin='{"enabled":false,"count":0,"label":"false","precise":1.00000000000000001,"large":9007199254740993}',
    )
    add(
        "JSON unicode quoting",
        ["--json"],
        stdin=json.dumps({"quote": "a'b\"c", "unicode": "世界🦫", "control": "a\nb"}),
    )
    add("JSON empty", ["--json"], stdin="")
    add("duplicate JSON", ["--json"], stdin='{"count":1,"count":2,"label":"x"}')
    add("duplicate fields", ["-f", "count=invalid", "-f", "count=2"])
    for number in ["+001.20", ".5", "1.", "-0", "-0.00", "1e2", "1e-8", "1.2300e+3", "1_000", "1e10000"]:
        add("decimal " + number, ["-f", "count=" + number])
    add("admitted no watch", responses=RESPONSES | {POST: (202, {"data": ADMITTED})})
    add("watch exact run", ["--watch"])
    add(
        "watch exit status",
        ["--watch", "--exit-status"],
        responses=RESPONSES
        | {
            ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}?include=jobs&include=workflow"): (
                200,
                {"data": inspection.FINISHED_RUN | {"run_id": RUN, "ci_provider": "avrea_ci", "conclusion": "failure"}},
            )
        },
        exit=1,
    )
    add(
        "generated invocation key",
        args=["workflow", "run", WF, "--repo", REPO, "--ref", "main", "--no-watch"],
        generated_key=True,
    )
    add(
        "UUID spelling",
        args=[
            "workflow",
            "run",
            WF,
            "--repo",
            REPO,
            "--ref",
            "main",
            "--no-watch",
            "--idempotency-key",
            str(uuid.UUID(KEY)).upper(),
        ],
    )
    for identifier in [".avrea/workflows/build.yml", "Build web"]:
        add("resolve " + identifier, workflow=identifier)
    for state in ["rejected", "admitted"]:
        add(
            "invalid outcome " + state,
            responses=RESPONSES
            | {POST: (202, {"data": RECEIPT | {"state": state, "reason": "writer_access_revoked"}})},
            exit=1,
        )
    for changed in [
        {"dispatch_id": "bad"},
        {"state": "future"},
        {"run_ids": None},
        {"run_ids": ["run-bad"]},
        {"ref": None},
        {"commit_sha": None},
        {"reason": 12},
    ]:
        add(
            "malformed receipt " + str(changed),
            responses=RESPONSES | {POST: (202, {"data": RECEIPT | changed})},
            exit=1,
        )
    for code in [401, 403, 404, 422, 429, 503]:
        add(
            "POST HTTP " + str(code),
            responses=RESPONSES | {POST: (code, {"detail": "fixture refused"})},
            exit=4 if code == 401 else 1,
        )
    for code in [403, 404, 429, 503]:
        response = (
            [(code, {"detail": "fixture refused"}), (200, {"data": ADMITTED})]
            if code >= 429
            else (code, {"detail": "fixture refused"})
        )
        add("follow HTTP " + str(code), ["--watch"], responses=RESPONSES | {GET: response}, exit=1 if code < 429 else 0)
    add(
        "follow changed receipt",
        ["--watch"],
        responses=RESPONSES | {GET: (200, {"data": ADMITTED | {"dispatch_id": "wap-" + "b" * 32}})},
        exit=1,
    )
    for data in [
        None,
        METADATA | {"inputs": [{}]},
        METADATA | {"dispatchable": False},
        METADATA | {"can_dispatch": False},
        METADATA | {"ref": ""},
    ]:
        add(
            "metadata " + str(data),
            args=["workflow", "run", WF, "--repo", REPO, "--idempotency-key", KEY, "--no-watch"],
            responses=RESPONSES | {META: (200, {"data": data})},
            exit=1,
        )
    for raw in ['{"x":null}', '{"x":[]}', '{"x":{}}']:
        add("invalid scalar " + raw, ["--json"], stdin=raw, exit=1)
    for field in ["enabled=yes", "count=no", "count=NaN", "count=Infinity"]:
        add("invalid field " + field, ["-f", field], exit=1)
    add(
        "unsupported input type",
        ["-f", "count=1"],
        responses=RESPONSES
        | {
            (META[0], META[1] + "?ref=main"): (
                200,
                {"data": METADATA | {"inputs": [{"name": "count", "type": "future"}]}},
            )
        },
        exit=1,
    )
    add("lost response recovered", responses=RESPONSES | {POST: [(0, None), (202, {"data": RECEIPT})]})
    add("lost response twice", responses=RESPONSES | {POST: (0, None)}, exit=1)
    add(
        "metadata transport",
        args=["workflow", "run", WF, "--repo", REPO, "--idempotency-key", KEY, "--no-watch"],
        responses=RESPONSES | {META: (0, None)},
        exit=1,
    )
    add("follow transport", ["--watch"], responses=RESPONSES | {GET: [(0, None), (200, {"data": ADMITTED})]})
    add("GitHub key refused", workflow="build.yml", responses=github.BASE, exit=1)
    for code in [404, 422]:
        add(
            "GitHub filename hint " + str(code),
            args=["workflow", "run", "build.yml", "--repo", REPO, "--ref", "main", "--no-watch"],
            responses=github.BASE | {github.DISPATCH: (code, {"detail": "missing workflow"})},
            exit=1,
        )
    for provider in ["gitlab_ci", "future"]:
        add(
            "provider " + provider,
            responses=RESPONSES | {workflow.LOOKUP: (200, {"data": [NATIVE | {"ci_provider": provider}]})},
            exit=1,
        )
    if sys.platform != "win32":
        for name in ["pending receipt", "typed fields", "watch exact run", "POST HTTP 503", "invalid outcome rejected"]:
            c = deepcopy(next(c for c in result if c["name"] == "native dispatch " + name))
            c["name"] += " tty"
            c["tty"] = c["merge_terminal"] = True
            c["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            result.append(c)
    return result


def compare(scenario, python, go):
    python, go = deepcopy(python), deepcopy(go)
    differences = []
    for label, result in [("Python", python), ("Go", go)]:
        submissions = [
            r for r in result["requests"] if r["method"] == "POST" and r["path"].endswith("/workflow-dispatches")
        ]
        if submissions:
            key = submissions[0].get("idempotency_key", "")
            if not re.fullmatch("[0-9a-f]{32}", key):
                differences.append(label + " invalid invocation key")
            if "Invocation key: " + key not in result["stdout"] + result["stderr"]:
                differences.append(label + " missing invocation key diagnostic")
            if any(
                r.get("idempotency_key") != key or r.get("raw_body") != submissions[0].get("raw_body")
                for r in submissions
            ):
                differences.append(label + " changed keyed retry")
            if scenario.get("generated_key"):
                if uuid.UUID(key).version != 4:
                    differences.append(label + " generated key is not UUID4")
                for stream in ["stdout", "stderr"]:
                    result[stream] = result[stream].replace(key, KEY)
                for request in submissions:
                    request["idempotency_key"] = KEY
            for request in submissions:
                # Validate exact decimal value/type independently of the float-decoded capture.
                parsed = json.loads(request.pop("raw_body"), parse_float=Decimal)
                request["exact_body"] = repr(sorted((k, type(v).__name__, str(v)) for k, v in parsed["inputs"].items()))
        text = result["stdout"] + result["stderr"]
        for line in text.splitlines():
            if line.startswith("printf '%s' "):
                words = shlex.split(line)
                if (
                    words[:2] != ["printf", "%s"]
                    or words[3:5] != ["|", "env"]
                    or "--json" not in words
                    or "--idempotency-key" not in words
                ):
                    differences.append(label + " invalid replay command")
    for result in (python, go):
        for stream in ("stdout", "stderr"):
            result[stream] = re.sub(r"\d{2}:\d{2}:\d{2} UTC", "<clock>", result[stream])
    for stream in ["stdout", "stderr"]:
        if inspection.presentation(python[stream]) != inspection.presentation(go[stream]):
            differences.append(stream + " text/styles")
        if inspection.terminal_text(python[stream]) != inspection.terminal_text(go[stream]):
            differences.append(stream + " layout")
    return differences + inspection.compare(scenario | {"presentation": True}, python, go)


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/native-dispatch-parity.json"
        )
    )
