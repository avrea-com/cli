"""Run lifecycle parity, including confirmation and mutation request bodies."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
import re
import shlex
import sys
import uuid

ORG, RUN = inspection.ORG, inspection.RUN
NEXT = "run-019a0000000070008000000000000009"
VIEW = ("GET", f"/orgs/{ORG}/workflow-runs/{RUN}")
RERUN = ("POST", f"/orgs/{ORG}/workflow-runs/{RUN}/rerun")
CANCEL = ("POST", f"/orgs/{ORG}/workflow-runs/{RUN}/cancel")
POLL = ("GET", f"/orgs/{ORG}/workflow-runs?platform_run_id=9007199254740993&limit=10&order=created_at.desc")
ORGS = {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})}
BASE = ORGS | {
    VIEW: (200, {"data": inspection.RUN_DATA}),
    RERUN: (200, {"data": {}}),
    CANCEL: (200, {"data": {"status": "cancel_requested"}}),
    POLL: (200, {"data": [inspection.RUN_DATA | {"run_id": NEXT, "run_attempt": 2}]}),
}


def scenarios():
    cases = []
    for command in ["cancel", "rerun"]:
        for name, args, extra in [
            ("yes", ["--yes"], {}),
            ("confirm", [], {"stdin": "y\n"}),
            ("decline", [], {"stdin": "n\n", "exit": 1}),
            ("default no", [], {"stdin": "\n", "exit": 1}),
            ("EOF", [], {"exit": 1}),
            ("invalid then yes", [], {"stdin": "maybe\nyes\n"}),
            (
                "disabled prompt",
                [],
                {"env": {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "AVR_PROMPT_DISABLED": "1"}, "exit": 1},
            ),
            (
                "disabled bypass",
                ["-y"],
                {"env": {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "AVR_PROMPT_DISABLED": "1"}},
            ),
            ("missing auth", ["-y"], {"env": {}, "exit": 4}),
        ]:
            cases.append(
                {"name": f"run {command} {name}", "args": ["run", command, RUN, *args], "responses": BASE, **extra}
            )
        for code in [401, 403, 404, 409, 500]:
            cases.append(
                {
                    "name": f"run {command} HTTP {code}",
                    "args": ["run", command, RUN, "--yes"],
                    "responses": BASE
                    | {(RERUN if command == "rerun" else CANCEL): (code, {"detail": "fixture rejected"})},
                    "exit": 4 if code == 401 else 1,
                    **({} if code == 401 else {"error_terms": [str(code), "fixture rejected"]}),
                }
            )
        platform = ("GET", f"/orgs/{ORG}/workflow-runs/by-platform-id/9007199254740993")
        for value in ["9007199254740993", "https://github.com/acme/web/actions/runs/9007199254740993"]:
            cases.append(
                {
                    "name": f"run {command} reference {value}",
                    "args": ["run", command, value, "-y"],
                    "responses": BASE | {platform: (200, {"data": inspection.RUN_DATA})},
                }
            )
    cases.extend(
        [
            {"name": "rerun failed", "args": ["run", "rerun", RUN, "--failed", "-y"], "responses": BASE},
            {
                "name": "rerun failed prompt",
                "args": ["run", "rerun", RUN, "--failed"],
                "stdin": "yes\n",
                "responses": BASE,
            },
            {
                "name": "cancel already terminal",
                "args": ["run", "cancel", RUN, "-y"],
                "responses": BASE | {CANCEL: (200, {"data": {"status": "already_terminal"}})},
            },
            {
                "name": "cancel default status",
                "args": ["run", "cancel", RUN, "-y"],
                "responses": BASE | {CANCEL: (200, {})},
            },
            {
                "name": "rerun without platform",
                "args": ["run", "rerun", RUN, "-y"],
                "responses": BASE | {VIEW: (200, {"data": inspection.RUN_DATA | {"platform_run_id": None}})},
            },
            {
                "name": "rerun poll retry",
                "args": ["run", "rerun", RUN, "-y"],
                "responses": BASE | {POLL: [(503, {"detail": "retry"}), BASE[POLL]]},
            },
            {
                "name": "rerun poll forbidden",
                "args": ["run", "rerun", RUN, "-y"],
                "responses": BASE | {POLL: (403, {"detail": "fixture rejected"})},
                "exit": 1,
                "error_terms": ["403", "fixture rejected"],
            },
        ]
    )
    for case in cases:
        case.setdefault("env", {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG})
    if sys.platform != "win32":
        for original in [cases[0], cases[16], *cases[32:37]]:
            case = deepcopy(original)
            case["name"] += " terminal"
            case["tty"] = case["merge_terminal"] = case["presentation"] = True
            case["env"] = case["env"] | {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(case)
    return cases + native_scenarios()


NATIVE_RUN = "awr-" + "a" * 32
NATIVE_NEXT = "awr-" + "b" * 32
NATIVE_JOB = "awj-" + "d" * 32
KEY = "c" * 32


def native_scenarios():
    view = ("GET", f"/orgs/{ORG}/workflow-runs/{NATIVE_RUN}")
    with_jobs = ("GET", view[1] + "?include=jobs")
    route = f"/orgs/{ORG}/workflow-runs/{NATIVE_RUN}/rerun"
    post = ("POST", route)
    options = ("GET", route)
    record = {
        "run_id": NATIVE_RUN,
        "ci_provider": "avrea_ci",
        "platform_run_id": None,
        "jobs": [{"job_id": NATIVE_JOB, "job_name": "build"}],
    }
    receipt = {"run_id": NATIVE_NEXT, "run_attempt": 2, "status": "rerun_requested"}
    base = ORGS | {
        view: (200, {"data": record}),
        with_jobs: (200, {"data": record}),
        options: (200, {"data": {"can_rerun": True, "can_rerun_subset": True}}),
        post: (200, {"data": receipt}),
    }
    cases = []

    def add(name, flags=None, **kw):
        cases.append(
            {
                "name": "native rerun " + name,
                "args": ["run", "rerun", NATIVE_RUN, "--idempotency-key", KEY, "--yes", *(flags or [])],
                "env": {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG},
                "responses": base,
                **kw,
            }
        )

    add("whole")
    add("failed", ["--failed"])
    add("selected name", ["--job", "build"])
    add("selected ID", ["--job", NATIVE_JOB])
    add("deduplicated", ["--job", "build", "--job", NATIVE_JOB, "--job", "build"])
    add("multiple", ["--job", NATIVE_JOB, "--job", "awj-" + "e" * 32])
    add("ID implies native", responses=base | {view: (200, {"data": record | {"ci_provider": None}})})
    add("generate key", args=["run", "rerun", NATIVE_RUN, "-y"], generated_key=True)
    for flags in ([], ["--failed"], ["--job", "build"]):
        add("confirm " + str(flags), args=["run", "rerun", NATIVE_RUN, "--idempotency-key", KEY, *flags], stdin="y\n")
        add(
            "decline " + str(flags),
            args=["run", "rerun", NATIVE_RUN, "--idempotency-key", KEY, *flags],
            stdin="n\n",
            exit=1,
        )
    add("conflicting subset", ["--failed", "--job", "build"], responses={}, exit=2)
    add("unknown job", ["--job", "missing"], exit=2)
    add(
        "ambiguous job",
        ["--job", "build"],
        responses=base | {with_jobs: (200, {"data": record | {"jobs": record["jobs"] * 2}})},
        exit=2,
    )
    add("missing capability", ["--failed"], responses=base | {options: (200, {"data": {}})}, exit=1)
    add(
        "disabled subset",
        args=["run", "rerun", NATIVE_RUN, "-y", "--failed"],
        responses=base | {options: (200, {"data": {"can_rerun_subset": False, "can_rerun": True}})},
        exit=1,
    )
    add(
        "replay despite subset disabled",
        ["--failed"],
        responses=base | {options: (200, {"data": {"can_rerun_subset": False, "can_rerun": True}})},
    )
    add(
        "server explains admission refusal",
        args=["run", "rerun", NATIVE_RUN, "-y", "--failed"],
        generated_key=True,
        responses=base
        | {
            options: (200, {"data": {"can_rerun_subset": False, "can_rerun": False}}),
            post: (409, {"detail": "Admission disabled"}),
        },
        exit=1,
    )
    for code in (401, 403, 404, 409, 422, 429, 500, 503):
        add(
            "HTTP " + str(code),
            responses=base | {post: (code, {"detail": "fixture rejection"})},
            exit=4 if code == 401 else 1,
        )
    add(
        "structured rejection",
        responses=base
        | {
            post: (
                409,
                {"detail": {"code": "subset_unavailable", "message": "Job cannot be rerun", "job_ids": [NATIVE_JOB]}},
            )
        },
        exit=1,
    )
    add("lost response recovered", responses=base | {post: [(0, None), (200, {"data": receipt})]})
    add("lost response twice", responses=base | {post: (0, None)}, exit=1)
    for change in (
        {"run_id": "run-" + "b" * 32},
        {"run_attempt": 1},
        {"run_attempt": True},
        {"run_attempt": 2.5},
        {"run_attempt": "2/1"},
        {"run_attempt": "2e0"},
        {"run_attempt": "2."},
        {"run_attempt": "2__0"},
        {"status": "queued"},
    ):
        add("invalid receipt " + str(change), responses=base | {post: (200, {"data": receipt | change})}, exit=1)
    add("missing receipt", responses=base | {post: (200, {})}, exit=1)
    for attempt in ("2", 2.0, "2.0", " +2.00 ", "2_0", "02"):
        add(
            "coerced receipt " + repr(attempt),
            responses=base | {post: (200, {"data": receipt | {"run_attempt": attempt}})},
        )
    for flags in (["--job", "build"], ["--idempotency-key", KEY]):
        add(
            "GitHub unsupported " + str(flags),
            args=["run", "rerun", RUN, "-y", *flags],
            responses=BASE | {("GET", VIEW[1] + "?include=jobs"): BASE[VIEW]},
            exit=2,
        )
    add("invalid UUID", args=["run", "rerun", NATIVE_RUN, "--idempotency-key", "wrong", "-y"], responses={}, exit=2)
    if sys.platform != "win32":
        for original in cases[:4]:
            cases.append(
                original
                | {
                    "name": original["name"] + " terminal",
                    "tty": True,
                    "merge_terminal": True,
                    "presentation": True,
                    "env": original["env"] | {"NO_COLOR": "", "TERM": "xterm-256color"},
                }
            )
    return cases


def compare(scenario, python, go):
    python, go = deepcopy(python), deepcopy(go)
    differences = []
    for label, result in (("Python", python), ("Go", go)):
        requests = [
            r
            for r in result["requests"]
            if r["method"] == "POST"
            and r["path"].endswith("/rerun")
            and isinstance(r.get("body"), dict)
            and "invocation_key" in r["body"]
        ]
        if not requests:
            continue
        body = requests[0]["body"]
        key = body["invocation_key"]
        text = result["stdout"] + result["stderr"]
        if not re.fullmatch("[0-9a-f]{32}", key) or "Invocation key: " + key not in text:
            differences.append(label + " invalid/missing invocation key")
        if len(requests) > 2 or any(r["body"] != body for r in requests):
            differences.append(label + " changed or excessive replay")
        for line in text.splitlines():
            if line.startswith("env AVR_HOST="):
                args = shlex.split(line)
                if (
                    args[2:5] != ["avr", "run", "rerun"]
                    or args[5] != NATIVE_RUN
                    or args[args.index("--idempotency-key") + 1] != key
                    or args[args.index("--org") + 1] != ORG
                    or "--yes" not in args
                ):
                    differences.append(label + " invalid recovery command")
                selected = [args[i + 1] for i, arg in enumerate(args) if arg == "--job"]
                if selected != body.get("job_ids", []) or ("--failed" in args) != body["failed_only"]:
                    differences.append(label + " changed recovery subset")
        if scenario.get("generated_key"):
            if uuid.UUID(key).version != 4:
                differences.append(label + " generated key is not UUID4")
            for request in requests:
                request["body"]["invocation_key"] = KEY
            for stream in ("stdout", "stderr"):
                result[stream] = result[stream].replace(key, KEY)
    return differences + inspection.compare(scenario, python, go)


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios,
            compare_results=compare,
            default_report="build/go/run-control-parity.json",
        )
    )
