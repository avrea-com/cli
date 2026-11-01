"""VM lifecycle and --wait comparisons against the pinned Python CLI."""

from copy import deepcopy
from scripts import compare_audit_health as audit
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_vm_inspection as vm
import sys

VM, BASE, GET = vm.VM, vm.BASE, vm.SHOW
PATH = BASE + "/" + VM
PASSWORD = "fixtureOneTimePassword"
RUNNING = vm.ROW | {"state": "RUNNING", "enable_remote_desktop": True, "endpoints": vm.ENDPOINTS}
FAILED = vm.ROW | {"state": "FAILED", "state_reason": "fixture failure"}
INITIAL = {
    "start": {"data": {"vm": vm.ROW | {"enable_remote_desktop": True}, "password": PASSWORD}},
    "stop": {"data": {"vm": vm.ROW | {"state": "STOPPING"}, "password": None}},
    "pause": {"data": vm.ROW | {"state": "PAUSING", "desired_state": "PAUSED"}},
    "resume": {"data": vm.ROW | {"state": "STARTING"}},
    "delete": {"data": {"customer_vm_id": VM, "state": "DELETING"}},
}
READY = {
    "start": RUNNING,
    "stop": vm.ROW | {"state": "STOPPED"},
    "pause": vm.ROW | {"state": "PAUSED", "snapshot": {"memory_included": True}},
    "resume": RUNNING,
    "delete": vm.ROW | {"state": "DELETED"},
}
MUTATE = {
    "start": ("PATCH", PATH),
    "stop": ("PATCH", PATH),
    "pause": ("POST", PATH + "/pause"),
    "resume": ("POST", PATH + "/resume"),
    "delete": ("DELETE", PATH),
}


def scenarios():
    cases = []

    def case(name, operation, flags=None, **kwargs):
        cases.append(
            {
                "name": "vm life " + name,
                "args": ["vm", operation, VM, *(["--yes"] if operation == "delete" else []), *(flags or [])],
                "env": vm.ENV.copy(),
                "responses": {MUTATE[operation]: (200, INITIAL[operation]), GET: (200, {"data": READY[operation]})},
                **kwargs,
            }
        )

    for operation, initial in INITIAL.items():
        route = MUTATE[operation]
        case(operation + " human", operation, presentation=True)
        case(operation + " JSON", operation, ["--json"], json=True)
        case(operation + " no auth", operation, env={}, responses={}, exit=4)
        case(
            operation + " explicit org",
            operation,
            ["--org", vm.ORG],
            env=vm.ENV | {"AVR_ORG": "org-other"},
            presentation=True,
        )
        case(
            operation + " org slug",
            operation,
            ["--org", "AcMe"],
            responses={
                route: (200, initial),
                ("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS}),
            },
            presentation=True,
        )
        for status in [401, 403, 404, 409, 422, 429, 503]:
            case(
                operation + " HTTP " + str(status),
                operation,
                responses={route: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
        for as_json in [False, True]:
            flags = ["--wait", "--wait-timeout", "0", *(["--json"] if as_json else [])]
            case(operation + " wait ready " + str(as_json), operation, flags, json=as_json, presentation=not as_json)
            case(
                operation + " wait failed " + str(as_json),
                operation,
                flags,
                responses={route: (200, initial), GET: (200, {"data": FAILED})},
                exit=1,
                json=as_json,
                presentation=not as_json,
            )
            pending = vm.ROW | {
                "state": {
                    "start": "PENDING",
                    "stop": "STOPPING",
                    "pause": "PAUSING",
                    "resume": "STARTING",
                    "delete": "DELETING",
                }[operation]
            }
            case(
                operation + " wait timeout " + str(as_json),
                operation,
                flags,
                responses={route: (200, initial), GET: (200, {"data": pending})},
                exit=1,
                json=as_json,
                presentation=not as_json,
            )
            case(
                operation + " transient timeout " + str(as_json),
                operation,
                flags,
                responses={route: (200, initial), GET: (503, {"detail": "temporary"})},
                exit=1,
                json=as_json,
                presentation=not as_json,
            )
        case(
            operation + " rate limit retry",
            operation,
            ["--wait", "--wait-timeout", "10", "--json"],
            responses={
                route: (200, initial),
                GET: [(429, {"detail": "temporary"}, {"Retry-After": "0"}), (200, {"data": READY[operation]})],
            },
            json=True,
        )
        case(
            operation + " invalid timeout",
            operation,
            ["--wait-timeout", "abc"],
            responses={},
            exit=2,
            error_terms=["--wait-timeout"],
        )
        for status in [401, 403, 404, 422]:
            if operation == "delete" and status == 404:
                continue
            case(
                operation + " permanent poll " + str(status),
                operation,
                ["--wait"],
                responses={route: (200, initial), GET: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
        case(operation + " missing ID", operation, responses={}, exit=2, error_terms=["VM_ID"])
        cases[-1]["args"] = ["vm", operation]
        case(operation + " empty response human", operation, responses={route: (200, {})}, presentation=True)
        case(operation + " empty response JSON", operation, ["--json"], responses={route: (200, {})}, json=True)

    case("pause memory", "pause", ["--memory"], presentation=True)
    case("resume discard memory", "resume", ["--discard-memory"], presentation=True)
    case(
        "pause returns to running",
        "pause",
        ["--wait", "--wait-timeout", "0"],
        responses={
            MUTATE["pause"]: (200, INITIAL["pause"]),
            GET: (200, {"data": RUNNING | {"state_reason": "pause failed"}}),
        },
        exit=1,
        presentation=True,
    )
    case(
        "resume returns to paused",
        "resume",
        ["--wait", "--wait-timeout", "0", "--json"],
        responses={MUTATE["resume"]: (200, INITIAL["resume"]), GET: (200, {"data": READY["pause"]})},
        exit=1,
        json=True,
    )
    case(
        "start waits for desktop endpoint",
        "start",
        ["--wait", "--wait-timeout", "0", "--json"],
        responses={
            MUTATE["start"]: (200, INITIAL["start"]),
            GET: (200, {"data": RUNNING | {"endpoints": {"ssh": vm.ENDPOINTS["ssh"]}}}),
        },
        exit=1,
        json=True,
    )
    case(
        "start empty password JSON",
        "start",
        ["--wait", "--wait-timeout", "0", "--json"],
        responses={MUTATE["start"]: (200, {"data": {"vm": vm.ROW, "password": ""}}), GET: (200, {"data": RUNNING})},
        json=True,
    )
    case(
        "start quoted password",
        "start",
        responses={MUTATE["start"]: (200, {"data": {"vm": RUNNING, "password": "a'b c!"}})},
        presentation=True,
    )
    case(
        "start progress changes",
        "start",
        ["--wait", "--wait-timeout", "10", "--json"],
        responses={MUTATE["start"]: (200, INITIAL["start"]), GET: [(200, {"data": vm.ROW}), (200, {"data": RUNNING})]},
        json=True,
    )
    case(
        "pause retry 503",
        "pause",
        ["--wait", "--wait-timeout", "10", "--json"],
        responses={
            MUTATE["pause"]: (200, INITIAL["pause"]),
            GET: [(503, {"detail": "temporary"}), (200, {"data": READY["pause"]})],
        },
        json=True,
    )
    case(
        "resume retry 408",
        "resume",
        ["--wait", "--wait-timeout", "10", "--json"],
        responses={
            MUTATE["resume"]: (200, INITIAL["resume"]),
            GET: [(408, {"detail": "temporary"}), (200, {"data": RUNNING})],
        },
        json=True,
    )
    for as_json in [False, True]:
        case(
            "delete gone " + str(as_json),
            "delete",
            ["--wait", *(["--json"] if as_json else [])],
            responses={MUTATE["delete"]: (200, INITIAL["delete"]), GET: (404, {"detail": "gone"})},
            json=as_json,
            presentation=not as_json,
        )
        case(
            "delete 204 " + str(as_json),
            "delete",
            ["--json"] if as_json else [],
            responses={MUTATE["delete"]: (204, None)},
            json=as_json,
            presentation=not as_json,
        )
    for label, text in [("confirm", "yes\n"), ("decline", "n\n"), ("default no", "\n"), ("EOF", "")]:
        case(
            "delete " + label,
            "delete",
            stdin=text,
            exit=0 if label == "confirm" else 1,
            presentation=True,
            responses={MUTATE["delete"]: (200, INITIAL["delete"])} if label == "confirm" else {},
        )
        cases[-1]["args"].remove("--yes")
    case("delete disabled", "delete", env=vm.ENV | {"AVR_PROMPT_DISABLED": "1"}, responses={}, exit=1)
    cases[-1]["args"].remove("--yes")
    case("delete short yes", "delete", presentation=True)
    cases[-1]["args"][cases[-1]["args"].index("--yes")] = "-y"
    if sys.platform != "win32":
        for name in [
            "start human",
            "start wait ready False",
            "start wait failed False",
            "stop wait ready False",
            "pause memory",
            "resume discard memory",
            "delete confirm",
            "delete wait failed False",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "vm life " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=audit.compare, default_report="build/go/vm-lifecycle-parity.json"
        )
    )
