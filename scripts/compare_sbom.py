"""SBOM history and summary parity, including opaque/malformed summary data."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import sys

ORG, REPO = inspection.ORG, inspection.REPO
SHA = "a" * 40
OTHER = "b" * 40
BASE = f"/orgs/{ORG}/repos/{REPO}/analysis/sbom"
LIST = ("GET", BASE + "?limit=50")
LATEST = ("GET", BASE + "/latest")
COMMIT = ("GET", BASE + "/commits/" + SHA)
HISTORY = ("GET", BASE + "?limit=1000")
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
ARTIFACTS = [
    {"filename": "bom.cyclonedx.json", "media_type": "application/vnd.cyclonedx+json", "raw_bytes": 2048},
    {"filename": "inventory.json", "media_type": "application/json", "raw_bytes": 1024},
]
SUMMARY = {
    "total_dependencies": 42,
    "unresolved": 3,
    "copyleft": ["gpl-package"],
    "copyleft_omitted": 0,
    "licenses": {"MIT": 30, "Apache-2.0": 9, "NOASSERTION": 3},
    "ecosystems": {"python": 40, "docker": 2},
}
SNAPSHOT = {
    "type": "sbom",
    "commit_sha": SHA,
    "branch": "main",
    "is_release": False,
    "schema_version": 1,
    "generated_at": "2026-09-01T10:00:00Z",
    "recorded_at": "2026-09-01T10:05:00Z",
    "artifacts": ARTIFACTS,
    "summary": SUMMARY,
}
ROW = {k: v for k, v in SNAPSHOT.items() if k != "summary"} | {
    "total_dependencies": 42,
    "unresolved": 3,
    "copyleft_count": 1,
}
ROWS = [
    ROW | {"is_release": True},
    ROW | {"commit_sha": OTHER, "branch": None, "total_dependencies": None, "unresolved": None, "copyleft_count": None},
]
MANY = {f"License-{i:02d}": 100 - i for i in range(25)}


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "sbom " + name,
                "args": ["sbom", *args],
                "env": ENV.copy(),
                "responses": {
                    LIST: (200, {"data": ROWS, "pagination": {"next_cursor": "next +/'page"}}),
                    LATEST: (200, SNAPSHOT),
                    COMMIT: (200, SNAPSHOT),
                },
                **kwargs,
            }
        )

    def view(name, summary=SUMMARY, extra=None, **kwargs):
        case(
            "view " + name,
            ["view", "--repo", REPO, *(extra or [])],
            responses={LATEST: (200, SNAPSHOT | {"summary": summary})},
            **kwargs,
        )

    case("list human", ["list", "--repo", REPO], presentation=True)
    case("list JSON", ["list", "--repo", REPO, "--json", "*"], json=True)
    case("list jq", ["list", "--repo", REPO, "--json", "commit_sha", "-q", ".[0].commit_sha"])
    case("list empty", ["list", "--repo", REPO], responses={LIST: (200, {"data": []})}, presentation=True)
    case(
        "list options",
        ["list", "--repo", REPO, "-L", "3", "--cursor", "next +/'page"],
        responses={("GET", BASE + "?limit=3&cursor=next+%2B%2F%27page"): (200, {"data": ROWS})},
        presentation=True,
    )
    for cursor in ["", " ", " padded", "x" * 513]:
        case(
            "list invalid cursor " + repr(cursor[:10]),
            ["list", "--repo", REPO, "--cursor", cursor],
            responses={},
            exit=2,
            error_terms=["--cursor", "1..512"],
        )
    for limit in ["0", "1001"]:
        case("list invalid limit " + limit, ["list", "--limit", limit], responses={}, exit=2, error_terms=["--limit"])
    view("human", presentation=True)
    view("JSON", extra=["--json", "*"], json=True)
    view("jq", extra=["--json", "summary", "--jq", ".summary.licenses.MIT"], json=True)
    view("empty JSON fields", extra=["--json", ""], json=True)
    view("nonobject JSON preservation", [], extra=["--json", "summary"], json=True)
    for action in ["list", "view"]:
        case(action + " discovery", [action, "--json", "?"], env={}, responses={}, presentation=True)
        case(action + " unknown field", [action, "--json", "bad_field"], env={}, responses={}, exit=1)
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
        for status in [401, 403, 404, 422, 503]:
            endpoint = LIST if action == "list" else LATEST
            case(
                action + " HTTP " + str(status),
                [action, "--repo", REPO],
                responses={endpoint: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
    case("view positional commit", ["view", SHA, "--repo", REPO], presentation=True)
    case("view commit option", ["view", "--commit", "  " + SHA.upper() + "  ", "--repo", REPO], presentation=True)
    case("view same commit twice", ["view", SHA, "--commit", SHA, "--repo", REPO], presentation=True)
    case(
        "view conflicting commits",
        ["view", SHA, "--commit", OTHER, "--repo", REPO],
        responses={},
        exit=2,
        error_terms=["Pass the commit"],
    )
    case(
        "view discovery before conflict",
        ["view", SHA, "--commit", OTHER, "--json", "?"],
        responses={},
        env={},
        presentation=True,
    )
    for commit in ["", "abc", "g" * 40, "a" * 65, "main"]:
        case(
            "view invalid commit " + repr(commit[:8]),
            ["view", "--commit", commit, "--json", "?"],
            responses={},
            env={},
            exit=2,
            error_terms=["hex commit SHA"],
        )
    case(
        "view full SHA256",
        ["view", "a" * 64, "--repo", REPO],
        responses={("GET", BASE + "/commits/" + "a" * 64): (200, SNAPSHOT | {"commit_sha": "a" * 64})},
        presentation=True,
    )
    case(
        "view prefix",
        ["view", "aaaaaaa", "--repo", REPO],
        responses={HISTORY: (200, {"data": ROWS}), COMMIT: (200, SNAPSHOT)},
        presentation=True,
    )
    case(
        "view prefix second page",
        ["view", "aaaaaaa", "--repo", REPO],
        responses={
            HISTORY: (200, {"data": [ROW | {"commit_sha": OTHER}], "pagination": {"next_cursor": "next"}}),
            ("GET", BASE + "?limit=1000&cursor=next"): (200, {"data": [ROW, ROW]}),
            COMMIT: (200, SNAPSHOT),
        },
        presentation=True,
    )
    case(
        "view no prefix match", ["view", "ccccccc", "--repo", REPO], responses={HISTORY: (200, {"data": ROWS})}, exit=1
    )
    case(
        "view ambiguous prefix",
        ["view", "aaaaaaa", "--repo", REPO],
        responses={HISTORY: (200, {"data": [ROW, ROW | {"commit_sha": "a" * 39 + "b"}]})},
        exit=1,
    )
    for name, summary in [
        ("empty summary", {}),
        ("null summary", None),
        ("nonobject summary", []),
        ("omitted summary", {"omitted": "summary too large", "total_dependencies": None}),
        ("empty copyleft", SUMMARY | {"copyleft": []}),
        (
            "negative counts",
            SUMMARY | {"total_dependencies": -1, "unresolved": -3, "licenses": {"MIT": -1, "Apache": 0}},
        ),
        (
            "malformed counts",
            SUMMARY
            | {
                "total_dependencies": True,
                "unresolved": 2.0,
                "licenses": {"valid": 4, "bool": True, "float": 4.0, "text": "5", "unknown": -1},
                "ecosystems": "bad",
            },
        ),
        ("ties", SUMMARY | {"licenses": {"zzz": 3, "aaa": 3, "bbb": 3}}),
        ("long lists", SUMMARY | {"licenses": MANY, "copyleft": [f"pkg-{i:02d}" for i in range(25)]}),
        (
            "producer omissions",
            SUMMARY
            | {
                "licenses": MANY,
                "licenses_omitted": 2,
                "ecosystems_omitted": 1,
                "copyleft": [f"pkg-{i:02d}" for i in range(25)],
                "copyleft_omitted": 5,
            },
        ),
        ("only omissions", {"licenses_omitted": 1, "ecosystems_omitted": 2, "copyleft_omitted": 1}),
        ("bad omissions", SUMMARY | {"licenses_omitted": True, "ecosystems_omitted": 2.0, "copyleft_omitted": -1}),
        (
            "escaped names",
            SUMMARY
            | {
                "licenses": {"long license\n" + ("word " * 30): 3, "ESC \x1b]52;c;bad\x07": 1},
                "ecosystems": {"a\nb\x1b": 4},
                "copyleft": ["pkg\x1b[31m", None, 3, True],
            },
        ),
    ]:
        view(name, summary, presentation=True)
    view(
        "full lists",
        SUMMARY | {"licenses": MANY, "copyleft": [f"pkg-{i:02d}" for i in range(25)], "copyleft_omitted": 3},
        extra=["--full"],
        presentation=True,
    )
    for count in [10, 11, 12]:
        view(
            "head boundary " + str(count),
            SUMMARY | {"licenses": {f"L{i}": i for i in range(count)}, "copyleft": [f"pkg-{i}" for i in range(count)]},
            presentation=True,
        )
    case(
        "view Git repo",
        ["view"],
        git_remote="git@github.com:acme/web.git",
        responses={
            LATEST: (200, SNAPSHOT),
            ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb&platform=github"): (
                200,
                {"data": inspection.REPOSITORIES[0]},
            ),
        },
        presentation=True,
    )
    if sys.platform != "win32":
        for name in [
            "list human",
            "view human",
            "view long lists",
            "view producer omissions",
            "view only omissions",
            "view escaped names",
            "view Git repo",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "sbom " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=watch.compare, default_report="build/go/sbom-inspection-parity.json"
        )
    )
