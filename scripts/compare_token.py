"""Scoped token inspection and revocation parity without minting credentials."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import sys

ORG, REPO = inspection.ORG, inspection.REPO
KEY = "key-" + "019a0000000070008000000000000004"
OTHER_KEY = "key-" + "019a0000000070008000000000000005"
OWNER = "usr-019a0000000070008000000000000006"
VM = "cvm-019a0000000070008000000000000007"
BASE = f"/orgs/{ORG}/access-tokens"
LIST = ("GET", BASE + "?limit=50")
VIEW = ("GET", BASE + "/" + KEY)
DELETE = ("DELETE", BASE + "/" + KEY)
ROW = {
    "id": KEY,
    "organization_id": ORG,
    "user_id": OWNER,
    "name": "CI read",
    "created_at": "2026-09-01T10:00:00Z",
    "expires_at": "2050-01-01T10:00:00Z",
    "last_used_at": None,
    "revoked_at": None,
    "revoked_reason": None,
    "allow_vm_create": False,
    "vm_create_limit": None,
    "vm_create_count": 0,
    "grants": [{"resource_type": "repository", "resource_id": REPO, "access_level": "read"}],
    "token": "avs_must_never_print_fixture",
}
MIXED = ROW | {
    "name": "mixed\nscope\tname",
    "allow_vm_create": True,
    "vm_create_limit": 3,
    "vm_create_count": 1,
    "last_used_at": "2026-09-01T11:00:00Z",
    "grants": ROW["grants"] * 2 + [{"resource_type": "customer_vm", "resource_id": VM, "access_level": "admin"}],
}


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "token " + name,
                "args": ["token", *args],
                "env": {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG},
                "responses": {LIST: (200, {"data": [ROW]}), VIEW: (200, ROW), DELETE: (204, b"")},
                **kwargs,
            }
        )

    case("list TSV", ["list"])
    case("list JSON", ["list", "--json", "*"], json=True)
    case("list jq", ["list", "--json", "name", "-q", ".[].name"])
    case("list empty", ["list"], responses={LIST: (200, {"data": []})})
    case("list mixed grants", ["list"], responses={LIST: (200, {"data": [MIXED]})})
    case(
        "list multiple owners",
        ["list"],
        responses={
            LIST: (200, {"data": [ROW, MIXED | {"id": OTHER_KEY, "user_id": "usr-019a0000000070008000000000000008"}]})
        },
    )
    case(
        "list all VMs",
        ["list"],
        responses={
            LIST: (
                200,
                {
                    "data": [
                        ROW
                        | {
                            "grants": [{"resource_type": "customer_vm", "resource_id": None, "access_level": "admin"}],
                            "allow_vm_create": True,
                        }
                    ]
                },
            )
        },
    )
    case("list no grants", ["list"], responses={LIST: (200, {"data": [ROW | {"grants": []}]})})
    case(
        "list pagination",
        ["list", "-L", "3"],
        responses={
            ("GET", BASE + "?limit=3"): (200, {"data": [ROW], "pagination": {"next_cursor": "next +/"}}),
            ("GET", BASE + "?limit=2&cursor=next+%2B%2F"): (200, {"data": [MIXED | {"id": OTHER_KEY}]}),
        },
    )
    case(
        "list large page bound",
        ["list", "-L", "201", "--json", "id"],
        responses={
            ("GET", BASE + "?limit=200"): (200, {"data": [ROW], "pagination": {"next_cursor": "next"}}),
            ("GET", BASE + "?limit=200&cursor=next"): (200, {"data": []}),
        },
        json=True,
    )
    case(
        "list limit reached",
        ["list", "-L", "1"],
        responses={("GET", BASE + "?limit=1"): (200, {"data": [ROW], "pagination": {"next_cursor": "next"}})},
    )
    case("list overfetch", ["list", "-L", "1"], responses={("GET", BASE + "?limit=1"): (200, {"data": [ROW, MIXED]})})
    case(
        "list empty page cursor", ["list"], responses={LIST: (200, {"data": [], "pagination": {"next_cursor": "same"}})}
    )
    case(
        "list JSON more hint",
        ["list", "-L", "1", "--json", "*"],
        responses={("GET", BASE + "?limit=1"): (200, {"data": [ROW], "pagination": {"next_cursor": "next"}})},
        json=True,
    )
    for limit in ["0", "1001"]:
        case("list limit " + limit, ["list", "-L", limit], responses={}, exit=2, error_terms=["--limit"])
    case("view human", ["view", KEY], presentation=True)
    case("view JSON", ["view", KEY, "--json", "*"], json=True)
    case("view jq", ["view", KEY, "--json", "grants", "--jq", ".grants[].resource_id"])
    case("view mixed", ["view", KEY], responses={VIEW: (200, MIXED)}, presentation=True)
    case("view no grants", ["view", KEY], responses={VIEW: (200, ROW | {"grants": []})}, presentation=True)
    case(
        "view all VMs",
        ["view", KEY],
        responses={
            VIEW: (
                200,
                ROW | {"grants": [{"resource_type": "customer_vm", "resource_id": None, "access_level": "admin"}]},
            )
        },
        presentation=True,
    )
    case(
        "view expired",
        ["view", KEY],
        responses={VIEW: (200, ROW | {"expires_at": "2020-01-01T10:00:00Z"})},
        presentation=True,
    )
    case(
        "view revoked",
        ["view", KEY],
        responses={VIEW: (200, ROW | {"revoked_at": "2026-09-01T12:00:00Z", "revoked_reason": "owner\nrequest"})},
        presentation=True,
    )
    case(
        "view VM create unlimited",
        ["view", KEY],
        responses={VIEW: (200, ROW | {"allow_vm_create": True, "vm_create_count": 2})},
        presentation=True,
    )
    for command in ["list", "view"]:
        args = [command, *([KEY] if command == "view" else [])]
        case(command + " discovery", [*args, "--json", "?"], env={}, responses={})
        case(command + " empty JSON", [*args, "--json", ""], json=True)
        case(
            command + " unknown field",
            [*args, "--json", "token"],
            responses={},
            exit=1,
            error_terms=["Unknown JSON field"],
        )
        case(
            command + " jq without JSON",
            [*args, "--jq", ".id"],
            responses={},
            exit=2,
            error_terms=["--jq requires --json"],
        )
        case(command + " no auth", args, env={}, responses={}, exit=4)
        for status in [401, 403, 404, 422, 429, 503]:
            case(
                command + " HTTP " + str(status),
                args,
                responses={
                    LIST if command == "list" else VIEW: (status, {"detail": "fixture denied"}, {"Retry-After": "4"})
                },
                exit=4 if status == 401 else 1,
            )
    for command in ["view", "revoke"]:
        for value in ["../escape", "key-short", "key-" + "A" * 32]:
            case(
                command + " invalid " + value,
                [command, value],
                responses={},
                exit=2,
                error_terms=["Expected a token ID"],
            )
    case("revoke yes", ["revoke", KEY, "--yes"])
    case("revoke short yes", ["revoke", KEY, "-y"])
    case("revoke confirm", ["revoke", KEY], stdin="y\n")
    case("revoke decline", ["revoke", KEY], stdin="n\n", responses={}, exit=1)
    case("revoke empty default", ["revoke", KEY], stdin="\n", responses={}, exit=1)
    case("revoke EOF", ["revoke", KEY], responses={}, exit=1)
    case(
        "revoke disabled",
        ["revoke", KEY],
        env={"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "AVR_PROMPT_DISABLED": "1"},
        responses={},
        exit=1,
    )
    case("revoke no auth", ["revoke", KEY, "-y"], env={}, responses={}, exit=4)
    for status in [401, 403, 404, 422, 503]:
        case(
            "revoke HTTP " + str(status),
            ["revoke", KEY, "-y"],
            responses={DELETE: (status, {"detail": "fixture denied"})},
            exit=4 if status == 401 else 1,
        )
    case(
        "scoped view denied",
        ["view", KEY],
        env={"AVR_TOKEN": "avs_fixture", "AVR_ORG": ORG},
        responses={VIEW: (404, {"detail": "outside scope"})},
        exit=1,
    )
    for command in ["list", "view", "revoke"]:
        args = [command, *([KEY] if command != "list" else []), *(["--yes"] if command == "revoke" else [])]
        case(command + " explicit org", [*args, "--org", ORG], env={"AVR_TOKEN": harness.TOKEN, "AVR_ORG": "wrong"})
        case(
            command + " org slug",
            [*args, "--org", "AcMe"],
            responses={
                LIST: (200, {"data": [ROW]}),
                VIEW: (200, ROW),
                DELETE: (204, b""),
                ("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS}),
            },
        )
    case("view verbose", ["view", KEY, "--verbose"])
    case("plural alias", ["list", "--json", "?"], env={}, responses={})
    cases[-1]["args"][0] = "tokens"
    for status in [401, 404, 422]:
        case(
            "escaped detail " + str(status),
            ["view", KEY],
            env={"AVR_TOKEN": "avs_fixture", "AVR_ORG": ORG},
            responses={VIEW: (status, {"detail": "ok\x9b31m\u2028forged\u202e\x1b[2J"})},
            exit=4 if status == 401 else 1,
        )
    if sys.platform != "win32":
        for name in [
            "list TSV",
            "list empty",
            "list mixed grants",
            "list multiple owners",
            "list all VMs",
            "view human",
            "view mixed",
            "view revoked",
            "revoke confirm",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "token " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = item["presentation"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    for item in cases:
        if item["args"][:2] == ["token", "list"] and not item.get("tty") and "--json" not in item["args"]:
            item["tsv"] = True
    return cases


def compare(scenario, python, go):
    differences = watch.compare(scenario, python, go)
    for label, result in [("Python", python), ("Go", go)]:
        if "avs_must_never_print_fixture" in result["stdout"] + result["stderr"]:
            differences.append(label + " leaked credential")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/token-inspection-parity.json"
        )
    )
