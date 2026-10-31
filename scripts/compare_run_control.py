"""Run lifecycle parity, including confirmation and mutation request bodies."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
import sys

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
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios,
            compare_results=inspection.compare,
            default_report="build/go/run-control-parity.json",
        )
    )
