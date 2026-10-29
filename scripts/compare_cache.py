"""Cache inspection, browser output and confirmed mutation parity."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import sys

ORG, REPO = inspection.ORG, inspection.REPO
BASE = f"/orgs/{ORG}/repos/{REPO}/cache"
LIST = ("GET", BASE + "/entries?limit=100&order=created_at.desc")
USAGE = ("GET", BASE)
DELETE = ("DELETE", BASE)
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
ROWS = [
    {
        "cache_type": "gha",
        "key": "node_modules",
        "ref": "refs/heads/main",
        "size_bytes": 1234567,
        "created_at": "2026-01-02T03:04:05.678901Z",
        "last_accessed_at": None,
        "version": "v1",
        "hit_count": 42,
    },
    {"cache_type": "bazel", "key": None, "ref": None, "size_bytes": 0, "created_at": None},
    {"cache_type": "turbo", "key": "雪 café", "ref": "", "size_bytes": 1024**3, "created_at": "2026-01-03T00:00:00Z"},
]
DATA = {
    "total_size_bytes": 12 * 1024**3,
    "quota_bytes": 10 * 1024**3,
    "over_quota": True,
    "by_type": [
        {"cache_type": "gha", "size_bytes": 11 * 1024**3, "entry_count": 7},
        {"cache_type": "bazel", "size_bytes": 1024**3, "entry_count": 1},
    ],
}
ORGS = {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})}


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "cache " + name,
                "args": ["cache", *args],
                "env": ENV.copy(),
                "responses": {LIST: (200, {"data": ROWS, "total": 25}), USAGE: (200, {"data": DATA})},
                **kwargs,
            }
        )

    case("list pipe", ["list", "--repo", REPO], presentation=True)
    case("list JSON", ["list", "--repo", REPO, "--json", "*"], json=True)
    case("list jq", ["list", "--repo", REPO, "--json", "key,hit_count", "-q", ".[0].key"])
    case("list empty JSON fields", ["list", "--repo", REPO, "--json", ""], json=True)
    case("list empty", ["list", "--repo", REPO], responses={LIST: (200, {"data": [], "total": 0})}, presentation=True)
    case(
        "list filters",
        [
            "list",
            "--repo",
            REPO,
            "--type",
            "GHA",
            "--key",
            "node modules",
            "--ref",
            "refs/heads/main",
            "-L",
            "3",
            "--offset",
            "3",
            "--order",
            "created_at.asc",
        ],
        responses={
            (
                "GET",
                BASE
                + "/entries?limit=3&order=created_at.asc&cache_type=gha&key=node+modules"
                + "&ref=refs%2Fheads%2Fmain&offset=3",
            ): (200, {"data": ROWS, "total": 25})
        },
        presentation=True,
    )
    case(
        "list exact last page",
        ["list", "--repo", REPO, "--offset", "22"],
        responses={
            ("GET", BASE + "/entries?limit=100&order=created_at.desc&offset=22"): (200, {"data": ROWS, "total": 25})
        },
        presentation=True,
    )
    case(
        "list controls JSON",
        ["list", "--repo", REPO, "--json", "*"],
        responses={LIST: (200, {"data": [ROWS[0] | {"key": "a\tb\nc\x1b[31m"}], "total": 1})},
        json=True,
    )
    case(
        "list controls pipe",
        ["list", "--repo", REPO],
        responses={LIST: (200, {"data": [ROWS[0] | {"key": "a\tb\nc\x1b[31m"}], "total": 1})},
        presentation=True,
    )
    case("list web piped", ["list", "--repo", REPO, "--web"], responses=ORGS, presentation=True)
    case(
        "list web conflict",
        ["list", "--repo", REPO, "--web", "--json", "*"],
        responses={},
        exit=2,
        error_terms=["--web", "--json"],
    )
    for flag, value in [("--limit", "0"), ("--limit", "1001"), ("--offset", "-1"), ("--order", "CREATED_AT.DESC")]:
        case(
            "list invalid " + flag + value,
            ["list", "--repo", REPO, flag, value],
            responses={},
            exit=2,
            error_terms=[flag],
        )
    for action in ["list", "usage"]:
        case(action + " discovery", [action, "--json", "?"], env={}, responses={}, presentation=True)
        case(action + " unknown fields", [action, "--json", "no_field"], env={}, responses={}, exit=1)
        case(
            action + " jq requires JSON",
            [action, "--jq", "."],
            env={},
            responses={},
            exit=2,
            error_terms=["--jq requires --json"],
        )
        case(action + " missing repo", [action], responses={}, exit=1)
        case(action + " missing auth", [action, "--repo", REPO], env={}, responses={}, exit=4)
        case(action + " environment repo", [action, "--json", "*"], env=ENV | {"AVR_REPO": REPO}, json=True)
        for status in [401, 403, 404, 503]:
            endpoint = LIST if action == "list" else USAGE
            case(
                action + " HTTP " + str(status),
                [action, "--repo", REPO],
                responses={endpoint: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
    case("usage human", ["usage", "--repo", REPO], presentation=True)
    case("usage JSON", ["usage", "--repo", REPO, "--json", "*"], json=True)
    case("usage jq", ["usage", "--repo", REPO, "--json", "by_type", "-q", ".by_type | length"], json=True)
    case(
        "usage zero quota",
        ["usage", "--repo", REPO],
        responses={USAGE: (200, {"data": DATA | {"quota_bytes": 0, "over_quota": False}})},
        presentation=True,
    )
    case(
        "usage no types",
        ["usage", "--repo", REPO],
        responses={
            USAGE: (200, {"data": {"total_size_bytes": 0, "quota_bytes": 0, "over_quota": False, "by_type": []}})
        },
        presentation=True,
    )
    for count in [0, 1, 5]:
        case(
            "delete all count " + str(count),
            ["delete", "--repo", REPO, "--all", "--yes"],
            responses={DELETE: (200, {"deleted_count": count})},
            presentation=True,
        )
        case(
            "delete key count " + str(count),
            ["delete", "--repo", REPO, "--type", "GHA", "--key", "node modules", "--ref", "refs/heads/main", "-y"],
            responses={
                ("DELETE", BASE + "/entries?cache_type=GHA&key=node+modules&ref=refs%2Fheads%2Fmain"): (
                    200,
                    {"deleted_count": count},
                )
            },
            presentation=True,
        )
    case("delete no body", ["delete", "--repo", REPO, "--all", "-y"], responses={DELETE: (204, b"")}, presentation=True)
    case(
        "delete all confirm",
        ["delete", "--repo", REPO, "--all"],
        responses={DELETE: (200, {"deleted_count": 3})},
        stdin="y\n",
        presentation=True,
    )
    case(
        "delete key confirm",
        ["delete", "--repo", REPO, "--type", "gha", "--key", "node_modules"],
        responses={("DELETE", BASE + "/entries?cache_type=gha&key=node_modules"): (200, {"deleted_count": 1})},
        stdin="yes\n",
        presentation=True,
    )
    case("delete decline", ["delete", "--repo", REPO, "--all"], responses={}, stdin="n\n", exit=1, presentation=True)
    case("delete EOF", ["delete", "--repo", REPO, "--all"], responses={}, exit=1, presentation=True)
    for args, name in [
        ([], "no selector"),
        (["--all", "--key", "x"], "both selectors"),
        (["--key", "x"], "missing type"),
    ]:
        case("delete " + name, ["delete", "--repo", REPO, *args], responses={}, exit=1)
    case("delete missing auth before selectors", ["delete"], responses={}, env={}, exit=4)
    for args, name in [(["--all"], "purge"), (["--type", "gha", "--key", "x"], "key")]:
        case(
            "delete disabled " + name,
            ["delete", "--repo", REPO, *args],
            responses={},
            env=ENV | {"AVR_PROMPT_DISABLED": "1"},
            exit=1,
        )
    case(
        "delete denied",
        ["delete", "--repo", REPO, "--all", "-y"],
        responses={DELETE: (403, {"detail": "fixture denied"})},
        exit=1,
    )
    case(
        "delete repo label",
        ["delete", "--repo", "acme/web", "--all"],
        responses={
            ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb"): (200, {"data": inspection.REPOSITORIES[0]}),
            DELETE: (200, {"deleted_count": 2}),
        },
        stdin="y\n",
        presentation=True,
    )
    case(
        "delete Git label",
        ["delete", "--all"],
        git_remote="git@github.com:acme/web.git",
        responses={
            ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb&platform=github"): (
                200,
                {"data": inspection.REPOSITORIES[0]},
            ),
            DELETE: (200, {"deleted_count": 2}),
        },
        stdin="y\n",
        presentation=True,
    )
    case(
        "delete environment label",
        ["delete", "--all"],
        env=ENV | {"AVR_REPO": "acme/web"},
        responses={
            ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb"): (200, {"data": inspection.REPOSITORIES[0]}),
            DELETE: (200, {"deleted_count": 2}),
        },
        stdin="y\n",
        presentation=True,
    )
    if sys.platform != "win32":
        for name in [
            "list pipe",
            "list empty",
            "list filters",
            "list exact last page",
            "usage human",
            "usage zero quota",
            "delete all confirm",
            "delete key confirm",
            "delete decline",
            "delete Git label",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "cache " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
        item = deepcopy(next(c for c in cases if c["name"] == "cache list web piped"))
        item["name"] = "cache list web terminal"
        item["tty"] = item["merge_terminal"] = item["browser_capture"] = True
        item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
        cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=watch.compare, default_report="build/go/cache-parity.json")
    )
