"""SBOM generation parity: request selection, task outcomes and commit delivery."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_job_watch as watch
from scripts import compare_sbom as sbom
import sys

TASK = "atk-019a0000000070008000000000000010"
OTHER_TASK = "atk-019a0000000070008000000000000011"
POST = ("POST", sbom.BASE + "/generate")
STATE = ("GET", f"/orgs/{sbom.ORG}/repos/{sbom.REPO}/ai-task/state?scope=sbom")
STARTED = {"ai_task_id": TASK, "status": "generating"}
RUNNING = {"ai_task_id": TASK, "status": "RUNNING", "error_code": None}
COMPLETED = RUNNING | {"status": "COMPLETED"}
FAILED = RUNNING | {"status": "FAILED", "error_code": "ANALYSIS_FAILED"}
NEW = sbom.SNAPSHOT | {"recorded_at": "2026-09-01T11:00:00Z"}
WAIT = ["--wait", "--wait-timeout", "1"]


def scenarios():
    cases = []

    def case(name, args=(), **kwargs):
        cases.append(
            {
                "name": "sbom generate " + name,
                "args": ["sbom", "generate", "--repo", sbom.REPO, *args],
                "env": sbom.ENV.copy(),
                "responses": {POST: (200, STARTED), STATE: (200, COMPLETED)},
                **kwargs,
            }
        )

    case("default", presentation=True)
    case("joined", responses={POST: (200, STARTED | {"status": "already_running"})}, presentation=True)
    for ref in ["v1.2.0", " feature/UPPER ", sbom.SHA.upper(), "c" * 64, "aaaaaaa"]:
        case("ref " + repr(ref), [ref], presentation=True)
    case("ref option", ["--ref", " release/v1 "], presentation=True)
    case("same ref twice", ["main", "--ref", "main"], presentation=True)
    case("conflicting refs", ["main", "--ref", "other"], responses={}, exit=2, error_terms=["Pass the ref"])
    for ref in ["", "  "]:
        case("blank argument " + repr(ref), [ref], responses={}, exit=2, error_terms=["must not be empty"])
        case("blank option " + repr(ref), ["--ref", ref], responses={}, exit=2, error_terms=["must not be empty"])
    case("JSON", ["--json", "*"], json=True)
    case("empty JSON", ["--json", ""], json=True)
    case("jq", ["--json", "ai_task_id,status", "-q", ".status"])
    case("fields", ["--json", "?"], responses={}, env={})
    case("fields before conflict", ["main", "--ref", "other", "--json", "?"], responses={}, env={})
    case("invalid field", ["--json", "invalid"], responses={}, exit=1, error_terms=["Unknown JSON field"])
    case("jq requires JSON", ["--jq", ".status"], responses={}, exit=2, error_terms=["--jq requires --json"])
    case("timeout zero", ["--wait-timeout", "0"], responses={}, exit=2, error_terms=["--wait-timeout"])
    case("missing auth", responses={}, env={}, exit=4)
    case("pinned JSON", [sbom.SHA, "--json", "*"], json=True)
    for expansion in [sbom.SHA, "b" * 64, None, "not-a-sha", sbom.SHA.upper(), 42]:
        case(
            "expansion " + repr(expansion),
            ["aaaaaaa", "--json", "*"],
            responses={POST: (200, STARTED | {"commit_sha": expansion})},
            json=True,
        )
    for status in [401, 403, 409, 422, 429, 503]:
        case(
            "POST HTTP " + str(status),
            responses={POST: (status, {"detail": "fixture denied"}, {"Retry-After": "17"})},
            exit=4 if status == 401 else 1,
        )
    case("wait task completed", WAIT, presentation=True)
    case("wait jq failure", [*WAIT, "--json", "status", "--jq", ".["], exit=1, error_terms=["Error:"])
    case("wait branch completed", ["main", *WAIT, "--json", "*"], json=True)
    case("wait task failed", WAIT, responses={POST: (200, STARTED), STATE: (200, FAILED)}, exit=1, presentation=True)
    case(
        "wait failed JSON",
        [*WAIT, "--json", "*"],
        responses={POST: (200, STARTED), STATE: (200, FAILED)},
        exit=1,
        json=True,
    )
    case("wait timeout", WAIT, responses={POST: (200, STARTED), STATE: (200, RUNNING)}, exit=1, presentation=True)
    case(
        "wait timeout JSON",
        [*WAIT, "--json", "*"],
        responses={POST: (200, STARTED), STATE: (200, RUNNING)},
        exit=1,
        json=True,
    )
    case(
        "wait older ignored",
        WAIT,
        responses={POST: (200, STARTED), STATE: [(200, COMPLETED | {"ai_task_id": OTHER_TASK}), (200, COMPLETED)]},
        presentation=True,
    )
    case(
        "wait superseded",
        WAIT,
        responses={POST: (200, STARTED), STATE: [(200, RUNNING), (200, COMPLETED | {"ai_task_id": OTHER_TASK})]},
        exit=1,
        presentation=True,
    )
    for status in [403, 404, 422]:
        case(
            "permanent poll " + str(status),
            WAIT,
            responses={POST: (200, STARTED), STATE: (status, {"detail": "denied"})},
            exit=1,
        )
    for status in [408, 429, 503]:
        case(
            "retry poll " + str(status),
            WAIT,
            responses={
                POST: (200, STARTED),
                STATE: [(status, {"detail": "retry"}, {"Retry-After": "99"}), (200, COMPLETED)],
            },
            presentation=True,
        )
    case(
        "retry timeout",
        WAIT,
        responses={POST: (200, STARTED), STATE: (503, {"detail": "retry"})},
        exit=1,
        presentation=True,
    )

    def pinned(name, snapshots, state=COMPLETED, extra=(), **kwargs):
        case(
            name,
            [sbom.SHA, *WAIT, *extra],
            responses={POST: (200, STARTED), sbom.COMMIT: snapshots, STATE: (200, state)},
            **kwargs,
        )

    pinned("commit delivered", [(200, sbom.SNAPSHOT), (200, NEW)], presentation=True)
    pinned("commit JSON", [(200, sbom.SNAPSHOT), (200, NEW)], extra=["--json", "*"], json=True)
    pinned("first commit delivery", [(404, {"detail": "missing"}), (200, NEW)], presentation=True)
    pinned(
        "failed task later delivery",
        [(200, sbom.SNAPSHOT), (200, sbom.SNAPSHOT), (200, NEW)],
        state=FAILED,
        presentation=True,
    )
    pinned("task completed no delivery", (200, sbom.SNAPSHOT), exit=1, presentation=True)
    pinned("task failed no delivery", (200, sbom.SNAPSHOT), state=FAILED, exit=1, presentation=True)
    pinned("commit timeout JSON", (200, sbom.SNAPSHOT), state=RUNNING, extra=["--json", "*"], json=True, exit=1)
    pinned(
        "commit transient then delivery",
        [(200, sbom.SNAPSHOT), (503, {"detail": "retry"}), (200, NEW)],
        presentation=True,
    )
    pinned("commit permanent poll error", [(200, sbom.SNAPSHOT), (403, {"detail": "denied"})], exit=1)
    pinned("baseline error prevents POST", (403, {"detail": "denied"}), exit=1)
    case(
        "expanded waits for delivery",
        ["aaaaaaa", *WAIT],
        responses={POST: (200, STARTED | {"commit_sha": sbom.SHA}), sbom.COMMIT: [(200, sbom.SNAPSHOT), (200, NEW)]},
        presentation=True,
    )
    case(
        "expanded baseline retries",
        ["aaaaaaa", *WAIT],
        responses={
            POST: (200, STARTED | {"commit_sha": sbom.SHA}),
            sbom.COMMIT: [(503, {"detail": "retry"}), (200, sbom.SNAPSHOT), (200, NEW)],
        },
        presentation=True,
    )
    case(
        "expanded baseline timeout",
        ["aaaaaaa", *WAIT, "--json", "*"],
        responses={POST: (200, STARTED | {"commit_sha": sbom.SHA}), sbom.COMMIT: (503, {"detail": "retry"})},
        json=True,
        exit=1,
    )
    if sys.platform != "win32":
        for name in [
            "default",
            "joined",
            "wait task completed",
            "wait task failed",
            "commit delivered",
            "failed task later delivery",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "sbom generate " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


def compare(scenario, python, go):
    if scenario.get("json") and scenario.get("exit", 0):
        # Failure records are still typed JSON contracts, not text snapshots.
        differences = watch.compare(scenario | {"exit": 0}, python | {"exit": 0}, go | {"exit": 0})
        if python["exit"] != scenario["exit"] or go["exit"] != scenario["exit"]:
            differences.append(f"expected exit {scenario['exit']}")
        return differences
    return watch.compare(scenario, python, go)


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/sbom-generate-parity.json"
        )
    )
