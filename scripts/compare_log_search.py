"""Repository log-search request, JSON, pipe and styled terminal parity."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import sys

ORG, REPO = inspection.ORG, inspection.REPO
ENDPOINT = ("POST", "/logs/search")
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
ROWS = [
    {
        "id": f"log-{i}",
        "vm_id": "vm-019a0000000070008000000000000006",
        "line_number": i + 1,
        "repository_id": REPO,
        "level": level,
        "stream": stream,
        "content": content,
        "timestamp": "2026-01-02T03:04:05.123456Z",
        "score": score,
        "group_name": None,
        "step_name": "Build",
        "step_record_id": "step-fixture",
    }
    for i, (level, stream, content, score) in enumerate(
        [
            ("info", "stdout", "ready", 0.0),
            ("error", "stderr", "compile failed", 0.875),
            ("warning", "stderr", "deprecated", 0.25),
            ("debug", "stdout", "details", 0.125),
            ("diagnostic", "stdout", "diagnostic remains visible in search", 0.0),
            ("info", "stdout", "##[group]raw annotation\n\tindented continuation", 0.0),
            ("info", "stdout", "\x1b[31membedded red\x1b[0m", 0.0),
        ]
    )
]
RESPONSE = (200, {"results": ROWS, "total_estimated": 250, "has_more": True})


def scenarios():
    cases = []

    def case(name, extra=None, **kwargs):
        cases.append(
            {
                "name": "log search " + name,
                "args": ["log", "search", "--repo", REPO, *(extra or [])],
                "responses": {ENDPOINT: RESPONSE},
                "env": ENV.copy(),
                **kwargs,
            }
        )

    case("human", presentation=True)
    case("JSON", ["--json", "*"], json=True)
    case("jq", ["--json", "content,line_number", "--jq", ".[0].content"])
    case("empty fields", ["--json", ""], json=True)
    case("alias", args=["logs", "search", "--repo", REPO, "--json", "id"], json=True)
    case(
        "filters",
        [
            "--query",
            "error OR failed",
            "--stream",
            "STDERR",
            "--level",
            "ERROR",
            "--vm-id",
            "vm-example",
            "-L",
            "3",
            "--json",
            "*",
        ],
        json=True,
    )
    case("empty optional filters", ["--query", "", "--vm-id", "", "--json", "id"], json=True)
    case("empty", responses={ENDPOINT: (200, {"results": []})}, presentation=True)
    case("empty JSON", ["--json", "*"], responses={ENDPOINT: (200, {"results": []})}, json=True)
    case(
        "missing result fields",
        responses={ENDPOINT: (200, {"results": [{"content": "minimal log"}]})},
        presentation=True,
    )
    case("fallback total", responses={ENDPOINT: (200, {"results": ROWS, "has_more": False})}, presentation=True)
    case(
        "JSON control preservation",
        ["--json", "content"],
        responses={ENDPOINT: (200, {"results": [ROWS[0] | {"content": "escape\x1b]52;c;secret\x07\x00\r\n"}]})},
        json=True,
    )
    case(
        "precise integer",
        ["--json", "line_number"],
        responses={ENDPOINT: (200, {"results": [ROWS[0] | {"line_number": 9007199254740993}]})},
        json=True,
    )
    case("discovery", ["--json", "?"], env={}, responses={}, presentation=True)
    case("unknown field", ["--json", "invalid"], env={}, responses={}, exit=1)
    case("jq requires JSON", ["--jq", "."], env={}, responses={}, exit=2, error_terms=["--jq requires --json"])
    case("missing auth", env={}, responses={}, exit=4)
    case("missing repo", args=["log", "search"], responses={}, exit=1)
    for flag, value in [("--stream", "all"), ("--stream", ""), ("--level", "diagnostic"), ("--level", "")]:
        case("invalid " + flag + repr(value), [flag, value], responses={}, exit=2, error_terms=[flag])
    for limit in ["0", "-1", "100001"]:
        case(
            "server validates limit " + limit,
            ["--limit", limit],
            responses={ENDPOINT: (422, {"detail": "limit out of bounds"})},
            exit=1,
        )
    case("env repo", args=["log", "search", "--json", "id"], env=ENV | {"AVR_REPO": REPO}, json=True)
    case(
        "repo name",
        args=["log", "search", "--repo", "acme/web"],
        responses={
            ENDPOINT: RESPONSE,
            ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb"): (200, {"data": inspection.REPOSITORIES[0]}),
        },
        presentation=True,
    )
    case(
        "Git repo",
        args=["log", "search"],
        git_remote="git@github.com:acme/web.git",
        responses={
            ENDPOINT: RESPONSE,
            ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb&platform=github"): (
                200,
                {"data": inspection.REPOSITORIES[0]},
            ),
        },
        presentation=True,
    )
    for status in [401, 403, 404, 429, 503]:
        case(
            "HTTP " + str(status),
            responses={ENDPOINT: (status, {"detail": "fixture denied"})},
            exit=4 if status == 401 else 1,
        )
    if sys.platform != "win32":
        for name in ["human", "empty", "missing result fields", "Git repo"]:
            item = deepcopy(next(c for c in cases if c["name"] == "log search " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
            if name == "human":
                cases.append(
                    deepcopy(item) | {"name": "log search no color terminal", "args": ["--no-color", *item["args"]]}
                )
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=watch.compare, default_report="build/go/log-search-parity.json"
        )
    )
