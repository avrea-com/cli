"""Token creation parity, including one-time credential delivery failures."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_job_watch as watch
from scripts import compare_token as token
import sys

POST = ("POST", token.BASE)
MINTED = token.ROW | {"token": "avs_created_fixture_credential"}
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": token.ORG}


def scenarios():
    cases = []

    def case(name, args=(), **kwargs):
        cases.append(
            {
                "name": "token create " + name,
                "args": ["token", "create", "--name", "ci-read", "--repo", token.REPO, *args],
                "env": ENV.copy(),
                "responses": {POST: (201, MINTED)},
                **kwargs,
            }
        )

    case("human", presentation=True)
    case("JSON", ["--json", "*"], json=True)
    case("projection", ["--json", "token,id"], json=True)
    case("jq credential", ["--json", "token,id", "--jq", ".token"])
    case("jq object", ["--json", "token,id", "--jq", "{token, id}"], json=True)
    for expression in [".id", "empty", "halt", "null", "false"]:
        case("filter drops " + expression, ["--json", "token,id", "--jq", expression], json=True, exit=1)
    for expression in [".[", "error(.token)", ".token, error(.token)"]:
        case("filter fails " + expression, ["--json", "token,id", "--jq", expression], json=True, exit=1)
    case("verbose", ["--verbose", "--json", "token,id"], json=True)
    case("discovery", ["--json", "?"], env={}, responses={})
    for fields in ["id", "", "grants"]:
        case(
            "no credential field " + repr(fields),
            ["--json", fields],
            responses={},
            exit=2,
            error_terms=["--json must include the token"],
        )
    case("unknown field", ["--json", "token,invalid"], responses={}, exit=1, error_terms=["Unknown JSON field"])
    case("jq requires JSON", ["--jq", ".token"], responses={}, exit=2, error_terms=["--jq requires --json"])
    for ttl in ["60", " 30M ", "8h", "7d", "00060s", "604800"]:
        case("ttl " + repr(ttl), ["--ttl", ttl, "--json", "token"], json=True)
    for ttl in ["0", "59", "604801", "8d", "9999999999999999999999999", "", "1.5h", "-1", "1h30m", "６０"]:
        case("invalid ttl " + repr(ttl), ["--ttl", ttl], responses={}, exit=2, error_terms=["--ttl"])
    case(
        "VM grants",
        ["--vm", token.VM, "--vm", token.VM + ":ADMIN", "--allow-vm-create", "--vm-create-limit", "3"],
        presentation=True,
    )
    case(
        "all options",
        [
            "--repo",
            token.REPO,
            "--repo",
            "acme/web:READ",
            "--vm",
            token.VM,
            "--allow-vm-create",
            "--vm-create-limit",
            "10",
            "--ttl",
            "7d",
            "--org",
            "AcMe",
            "--json",
            "token,id",
        ],
        responses={
            POST: (201, MINTED),
            ("GET", "/users/me/organizations"): (200, {"data": token.inspection.ORGANIZATIONS}),
            ("GET", f"/orgs/{token.ORG}/repos/resolve?name=acme%2Fweb"): (
                200,
                {"data": {"repository_id": token.REPO, "full_name": "acme/web"}},
            ),
        },
        json=True,
    )
    for flag, value in [("--repo", "acme/web:admin"), ("--repo", ":read"), ("--vm", token.VM + ":read"), ("--vm", "")]:
        case(
            "bad level " + flag + repr(value),
            [flag, value],
            responses={},
            exit=2,
            error_terms=["grants are created at"],
        )
    for limit in ["0", "11"]:
        case(
            "bad VM limit " + limit,
            ["--allow-vm-create", "--vm-create-limit", limit],
            responses={},
            exit=2,
            error_terms=["--vm-create-limit"],
        )
    case(
        "limit needs allowance",
        ["--vm-create-limit", "2"],
        responses={},
        exit=2,
        error_terms=["requires --allow-vm-create"],
    )
    case(
        "grant bound",
        [arg for _ in range(100) for arg in ("--repo", token.REPO)],
        responses={},
        exit=2,
        error_terms=["at most 100 grants", "101"],
    )
    case("no auth", env={}, responses={}, exit=4)
    for name, args in [
        ("VM creation only", ["--allow-vm-create"]),
        ("VM grant only", ["--vm", token.VM]),
        ("no grants", []),
    ]:
        case(
            name,
            args=args,
            **({"responses": {}, "exit": 2, "error_terms": ["Pass at least one"]} if not args else {}),
        )
        cases[-1]["args"] = ["token", "create", "--name", "agent", *args]
    case("missing name", args=["token", "create", "--repo", token.REPO], responses={}, exit=2, error_terms=["--name"])
    cases[-1]["args"] = ["token", "create", "--repo", token.REPO]
    for status in [401, 403, 404, 409, 422, 429, 500, 502, 503, 504]:
        case(
            "HTTP " + str(status),
            responses={POST: (status, {"detail": "fixture denied"}, {"Retry-After": "30"})},
            exit=4 if status == 401 else 1,
        )
    case("404 VM capability", ["--allow-vm-create"], responses={POST: (404, {"detail": "unavailable"})}, exit=1)
    case("grant refusal", responses={POST: (403, {"detail": "grants[0] unavailable"})}, exit=1)
    case(
        "grant validation",
        responses={POST: (422, {"detail": [{"loc": ["body", "grants", 0, "access_level"], "msg": "invalid"}]})},
        exit=1,
    )
    case("503 no detail", responses={POST: (503, None)}, exit=1)
    case("503 no retry header", responses={POST: (503, {"detail": "unavailable"})}, exit=1)
    case(
        "scoped forbidden",
        env={"AVR_TOKEN": "avs_fixture", "AVR_ORG": token.ORG},
        responses={POST: (403, {"detail": "caller is scoped"})},
        exit=1,
    )
    case("malformed JSON", responses={POST: (201, b"{")}, exit=1)
    without = {k: v for k, v in MINTED.items() if k != "token"}
    for name, value in [
        ("missing", without),
        ("null", MINTED | {"token": None}),
        ("empty", MINTED | {"token": ""}),
        ("wrong type", MINTED | {"token": {"value": "avs_nested_secret"}}),
        ("list", [MINTED]),
    ]:
        case("success without credential " + name, responses={POST: (201, value)}, exit=1)
    case("malformed summary", responses={POST: (201, MINTED | {"created_at": {"invalid": "timestamp"}})}, exit=1)
    if sys.platform != "win32":
        for name in ["human", "VM grants"]:
            item = deepcopy(next(c for c in cases if c["name"] == "token create " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


def compare(scenario, python, go):
    if scenario.get("json") and scenario.get("exit", 0):
        differences = watch.compare(scenario | {"exit": 0}, python | {"exit": 0}, go | {"exit": 0})
        if python["exit"] != scenario["exit"] or go["exit"] != scenario["exit"]:
            differences.append(f"expected exit {scenario['exit']}")
    else:
        differences = watch.compare(scenario, python, go)
    for label, result in [("Python", python), ("Go", go)]:
        if MINTED["token"] in result["stderr"] or "avs_nested_secret" in result["stderr"]:
            differences.append(label + " credential on stderr")
        if len([r for r in result["requests"] if r["method"] == "POST"]) > 1:
            differences.append(label + " repeated mint request")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/token-create-parity.json"
        )
    )
