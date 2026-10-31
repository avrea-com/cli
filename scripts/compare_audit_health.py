"""Audit event and health output comparisons with the pinned Python executable."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from urllib.parse import urlencode
import sys

ORG = inspection.ORG
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
BASE = f"/orgs/{ORG}/audit-events"
GET = ("GET", BASE + "?limit=100")
HEALTH = ("GET", "/health")
ROW = {
    "event_id": 9007199254740993,
    "created_at": "2026-10-01T12:30:45.789123Z",
    "actor_user_id": "usr-019a0000000070008000000000000003",
    "actor_type": "USER",
    "acting_api_key_id": None,
    "client_ip": "192.0.2.1",
    "resource_type": "api_key",
    "resource_id": None,
    "action": "CREATE",
    "event_data": {"z": True, "a": {"name": "Helsinki 🦫 <script>", "count": 9007199254740993}, "list": [None, 1, "é"]},
    "organization_id": ORG,
}
ROWS = [ROW, ROW | {"event_id": 9007199254740995, "actor_user_id": None, "actor_type": "SYSTEM", "event_data": None}]
DEFAULT = {GET: (200, {"data": ROWS, "pagination": {"next_cursor": "next/cursor+="}}), HEALTH: (200, {"status": "ok"})}


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append({"name": name, "args": args, "env": ENV.copy(), "responses": DEFAULT.copy(), **kwargs})

    audit = ["audit-events", "list"]
    for label, args in [("audit", audit), ("health", ["health"])]:
        field = "event_id" if label == "audit" else "status"
        case(label + " human", args, presentation=True)
        case(label + " JSON", args + ["--json", "*"], json=True)
        case(label + " projection", args + ["--json", field], json=True)
        case(label + " jq", args + ["--json", field, "-q", (".[]." if label == "audit" else ".") + field])
        case(label + " empty fields", args + ["--json", ""], json=True)
        case(label + " discovery", args + ["--json", "?"], env={}, responses={})
        case(
            label + " bad fields", args + ["--json", "wrong"], responses={}, exit=1, error_terms=["Unknown JSON field"]
        )
        case(
            label + " jq without JSON", args + ["--jq", "."], responses={}, exit=2, error_terms=["--jq requires --json"]
        )
    case("audit no auth", audit, env={}, responses={}, exit=4)
    case("health no auth", ["health"], env={}, presentation=True)
    case(
        "audit org slug",
        audit + ["--org", "AcMe"],
        responses=DEFAULT | {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})},
        presentation=True,
    )
    case("audit explicit org", audit + ["--org", ORG], env=ENV | {"AVR_ORG": "org-other"}, presentation=True)
    for status in [401, 403, 404, 409, 422, 429, 503]:
        case(
            "audit HTTP " + str(status),
            audit,
            responses={GET: (status, {"detail": "fixture denied"})},
            exit=4 if status == 401 else 1,
        )
        case(
            "health HTTP " + str(status),
            ["health"],
            responses={HEALTH: (status, {"detail": "fixture denied"})},
            exit=1,
            presentation=True,
        )
        case(
            "health failure JSON " + str(status),
            ["health", "--json", "*"],
            responses={HEALTH: (status, {"detail": "fixture denied"})},
            exit=1,
            json=True,
        )
    case("health failure jq", ["health", "--json", "status", "-q", ".status"], responses={HEALTH: (503, {})}, exit=1)
    case("health failure empty fields", ["health", "--json", ""], responses={HEALTH: (503, {})}, exit=1, json=True)
    for status in ["degraded", None]:
        case(
            "health status " + repr(status),
            ["health"],
            responses={HEALTH: (200, {"status": status})},
            presentation=True,
        )
    case("health status missing", ["health"], responses={HEALTH: (200, {})}, presentation=True)
    case(
        "audit empty",
        audit,
        responses={GET: (200, {"data": [], "pagination": {"next_cursor": None}})},
        presentation=True,
    )
    case(
        "audit empty with cursor",
        audit,
        responses={GET: (200, {"data": [], "pagination": {"next_cursor": "next"}})},
        presentation=True,
    )
    case(
        "audit missing optional",
        audit,
        responses={
            GET: (
                200,
                {
                    "data": [
                        {
                            k: v
                            for k, v in ROW.items()
                            if k not in {"actor_type", "actor_user_id", "resource_type", "resource_id", "event_data"}
                        }
                    ]
                },
            )
        },
        presentation=True,
    )
    for flag, value, key in [
        ("--resource-type", "api_key", "resource_type"),
        ("--action", "DELETE", "action"),
        ("--actor-user-id", ROW["actor_user_id"], "actor_user_id"),
        ("--from", "2026-10-01T12:30:00+03:00", "created_after"),
        ("--created-after", "2026-10-01", "created_after"),
        ("--to", "2026-11-01", "created_before"),
        ("--created-before", "2026-10-30T00:00:00Z", "created_before"),
        ("--cursor", "opaque /?+", "cursor"),
        ("-L", "1", "limit"),
        ("--limit", "1000", "limit"),
    ]:
        query = {"limit": "100", key: value}
        case(
            "audit filter " + flag,
            audit + [flag, value],
            responses={("GET", BASE + "?" + urlencode(query)): (200, {"data": [ROW]})},
            presentation=True,
        )
    case(
        "audit date alias last wins",
        audit + ["--from", "old", "--created-after", "new", "--created-before", "first", "--to", "last"],
        responses={("GET", BASE + "?limit=100&created_after=new&created_before=last"): (200, {"data": [ROW]})},
        presentation=True,
    )
    case(
        "audit empty filters omitted", audit + ["--action", "", "--from", "", "--resource-type", ""], presentation=True
    )
    for limit in ["0", "1001"]:
        case("audit invalid limit " + limit, audit + ["--limit", limit], responses={}, exit=2, error_terms=["--limit"])
    for cursor in ["", " space", "space ", "x" * 513]:
        case(
            "audit invalid cursor " + repr(cursor[:10]),
            audit + ["--cursor", cursor],
            responses={},
            exit=2,
            error_terms=["--cursor"],
        )
    case("audit discovery before cursor", audit + ["--cursor", "", "--json", "?"], env={}, responses={})
    if sys.platform != "win32":
        for name in ["audit human", "audit empty", "health human", "health HTTP 503", "health no auth"]:
            item = deepcopy(next(c for c in cases if c["name"] == name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


def compare(scenario, python, go):
    if scenario.get("json") and scenario.get("exit", 0) != 0:
        expected = scenario["exit"]
        differences = [] if python["exit"] == go["exit"] == expected else ["failure exit code"]
        python, go = deepcopy(python), deepcopy(go)
        python["exit"] = go["exit"] = 0
        return differences + watch.compare(scenario | {"exit": 0}, python, go)
    return watch.compare(scenario, python, go)


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/audit-health-parity.json"
        )
    )
