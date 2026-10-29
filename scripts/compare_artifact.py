"""Artifact parity, including on-disk integrity and failed replacement behavior."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import hashlib
import os
import sys

ORG, REPO = inspection.ORG, inspection.REPO
ART = "art-019a0000000070008000000000000001"
BASE = f"/orgs/{ORG}/repos/{REPO}/artifacts"
GET = ("GET", BASE + "/" + ART)
DOWNLOAD = ("GET", GET[1] + "/download")
DELETE = ("DELETE", GET[1])
CONTENT = b"PK\x03\x04fixture archive bytes\x00\xff\n"
ROW = {
    "name": "build-output",
    "id": ART,
    "repository_id": REPO,
    "workflow_run_id": inspection.RUN,
    "run_kind": "github",
    "run_attempt": 2,
    "size_bytes": len(CONTENT),
    "digest_sha256": hashlib.sha256(CONTENT).hexdigest(),
    "file_name": "build.zip",
    "file_count": 3,
    "archived": True,
    "state": "complete",
    "created_at": "2026-01-01T12:00:00Z",
    "completed_at": "2026-01-01T12:01:00Z",
    "expires_at": None,
}
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
LIST = ("GET", BASE + "?limit=50")


def file_record(data, mode=0o600):
    return {"size": len(data), "sha256": hashlib.sha256(data).hexdigest(), "mode": mode if os.name != "nt" else None}


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "artifact " + name,
                "args": ["artifact", *args],
                "responses": {GET: (200, {"data": ROW}), LIST: (200, {"data": [ROW]})},
                "env": ENV.copy(),
                **kwargs,
            }
        )

    case("list human", ["list", REPO], presentation=True)
    case("list JSON", ["list", REPO, "--json", "*"], json=True)
    case("list jq", ["list", REPO, "--json", "id,name", "-q", ".[].name"])
    case("list empty", ["list", REPO], responses={LIST: (200, {"data": []})}, presentation=True)
    case(
        "list null and missing",
        ["list", REPO],
        responses={LIST: (200, {"data": [{"id": ART, "name": "雪 café", "state": "complete"}]})},
        presentation=True,
    )
    case(
        "list filters",
        ["list", REPO, "--limit", "1000", "--run-id", "12345", "--cursor", "next +/="],
        responses={("GET", BASE + "?limit=1000&run_id=eq.12345&cursor=next+%2B%2F%3D"): (200, {"data": [ROW]})},
        presentation=True,
    )
    for reference in ["run-019a0000000070008000000000000001", "awr-019a0000000070008000000000000001"]:
        case(
            "list run " + reference[:3],
            ["list", REPO, "--run-id", reference, "--json", "id"],
            responses={("GET", BASE + "?limit=50&run_id=eq." + reference): (200, {"data": [ROW]})},
            json=True,
        )
    for mode in ["human", "JSON"]:
        case(
            "list cursor " + mode,
            ["list", REPO, *(["--json", "*"] if mode == "JSON" else [])],
            responses={LIST: (200, {"data": [ROW], "pagination": {"next_cursor": "next-page"}})},
            json=mode == "JSON",
            presentation=mode == "human",
        )
    case("get human", ["get", ART, "--repo", REPO], presentation=True)
    case("get JSON", ["get", ART, "--repo", REPO, "--json", "*"], json=True)
    case("get jq", ["get", ART, "--repo", REPO, "--json", "id,archived", "--jq", ".archived"], json=True)
    case(
        "get unknown raw members",
        ["get", ART, "--repo", REPO],
        presentation=True,
        responses={
            GET: (200, {"data": ROW | {"extra": {"z": True, "a": [None, 2.0, "it's fine"]}, "large": 9007199254740993}})
        },
    )
    case("get empty", ["get", ART, "--repo", REPO], responses={GET: (200, {"data": {}})}, presentation=True)
    case("get empty JSON flag", ["get", ART, "--repo", REPO, "--json", ""], presentation=True)
    case("get whitespace JSON flag", ["get", ART, "--repo", REPO, "--json", " "], json=True)
    case("get environment repo", ["get", ART], env=ENV | {"AVR_REPO": REPO}, presentation=True)
    resolve = {("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb"): (200, {"data": inspection.REPOSITORIES[0]})}
    case(
        "get repo name",
        ["get", ART, "--repo", "acme/web"],
        responses=resolve | {GET: (200, {"data": ROW})},
        presentation=True,
    )
    case(
        "get Git repo",
        ["get", ART],
        git_remote="git@github.com:acme/web.git",
        responses={
            ("GET", f"/orgs/{ORG}/repos/resolve?name=acme%2Fweb&platform=github"): (
                200,
                {"data": inspection.REPOSITORIES[0]},
            ),
            GET: (200, {"data": ROW}),
        },
        presentation=True,
    )
    case("get missing repo", ["get", ART], responses={}, exit=1)
    case("get missing auth", ["get", ART, "--repo", REPO], responses={}, env={}, exit=4)
    for value in ["bad", ART.upper(), ART[:-1]]:
        case(
            "get invalid ID " + value,
            ["get", value, "--repo", REPO],
            responses={},
            exit=2,
            error_terms=["Expected an Avrea art- ID"],
        )
    for value in ["0", "1001"]:
        case(
            "list invalid limit " + value,
            ["list", REPO, "--limit", value],
            responses={},
            exit=2,
            error_terms=["--limit"],
        )
    for action in ["list", "get", "download", "delete"]:
        base_args = [action, REPO] if action == "list" else [action, ART, "--repo", REPO]
        case(action + " field discovery", [*base_args, "--json", "?"], responses={}, env={}, presentation=True)
        case(action + " unknown field", [*base_args, "--json", "not_a_field"], responses={}, env={}, exit=1)
        case(
            action + " jq without JSON",
            [*base_args, "--jq", "."],
            responses={},
            env={},
            exit=2,
            error_terms=["--jq requires --json"],
        )
    for status in [401, 403, 404, 409, 422, 429, 503]:
        case(
            "get HTTP " + str(status),
            ["get", ART, "--repo", REPO],
            responses={GET: (status, {"detail": "fixture denied"})},
            exit=4 if status == 401 else 1,
        )
    case("delete yes human", ["delete", ART, "--repo", REPO, "-y"], responses={DELETE: (204, b"")}, presentation=True)
    case(
        "delete yes JSON",
        ["delete", ART, "--repo", REPO, "--yes", "--json", "*"],
        responses={DELETE: (200, {})},
        json=True,
    )
    case(
        "delete confirm", ["delete", ART, "--repo", REPO], responses={DELETE: (200, {})}, stdin="y\n", presentation=True
    )
    case("delete decline", ["delete", ART, "--repo", REPO], responses={}, stdin="n\n", exit=1, presentation=True)
    case("delete EOF", ["delete", ART, "--repo", REPO], responses={}, exit=1, presentation=True)
    case(
        "delete disabled prompt",
        ["delete", ART, "--repo", REPO],
        responses={},
        env=ENV | {"AVR_PROMPT_DISABLED": "1"},
        exit=1,
    )
    case(
        "delete denied",
        ["delete", ART, "--repo", REPO, "-y"],
        responses={DELETE: (403, {"detail": "fixture denied"})},
        exit=1,
    )

    def download(name, row=None, body=CONTENT, path="downloads", expected=None, **kwargs):
        args = ["download", ART, "--repo", REPO, "--path", path]
        if kwargs.pop("json", False):
            args += ["--json", "*"]
            kwargs["json"] = True
        case(
            "download " + name,
            args,
            responses={GET: (200, {"data": ROW if row is None else row}), DOWNLOAD: (200, body)},
            fixture_dirs=["downloads"],
            capture_files=["downloads"],
            expected_files={"downloads/build.zip": file_record(CONTENT)} if expected is None else expected,
            **kwargs,
        )

    download("human", presentation=True)
    download("JSON", json=True)
    download("default path", expected={"build.zip": file_record(CONTENT)}, json=True)
    cases[-1]["args"] = ["artifact", "download", ART, "--repo", REPO, "--json", "*"]
    cases[-1]["capture_files"] = ["."]
    download("empty path", path="", expected={"build.zip": file_record(CONTENT)}, json=True)
    cases[-1]["capture_files"] = ["."]
    download(
        "nested filename",
        path="downloads/nested/custom.zip",
        expected={"downloads/nested/custom.zip": file_record(CONTENT)},
        json=True,
    )
    download("tilde", path="~/downloads", json=True)
    download("absolute", path="FIXTURE_HOME/downloads", json=True)
    download("replace", fixture_files={"downloads/build.zip": b"old"}, json=True)
    download(
        "empty",
        row=ROW | {"size_bytes": 0, "digest_sha256": hashlib.sha256(b"").hexdigest()},
        body=b"",
        expected={"downloads/build.zip": file_record(b"")},
        json=True,
    )
    download("gzip", encoding="gzip", expected={}, exit=1, error_terms=["connection or stream interrupted"])
    download("zstd", encoding="zstd", expected={}, exit=1, error_terms=["connection or stream interrupted"])
    for name, change, body, term in [
        ("short", {}, CONTENT[:-1], "verification failed"),
        ("oversized", {}, CONTENT + b"extra", "exceeds its recorded size"),
        ("wrong digest", {"digest_sha256": "0" * 64}, CONTENT, "verification failed"),
        ("incomplete", {"state": "pending"}, CONTENT, "invalid integrity metadata"),
        ("negative size", {"size_bytes": -1}, CONTENT, "invalid integrity metadata"),
        ("too large", {"size_bytes": 10 * 1024**3 + 1}, CONTENT, "invalid integrity metadata"),
        ("float size", {"size_bytes": 2.5}, CONTENT, "invalid integrity metadata"),
        ("string size", {"size_bytes": "3"}, CONTENT, "invalid integrity metadata"),
        ("bad digest", {"digest_sha256": "BAD"}, CONTENT, "invalid integrity metadata"),
        *[
            (f"unsafe filename {i}", {"file_name": value}, CONTENT, "invalid filename")
            for i, value in enumerate(["../bad", "a\\b", ".", "", "a\nb", "a\x00b"])
        ],
    ]:
        download(
            name,
            row=ROW | change,
            body=body,
            fixture_files={"downloads/build.zip": b"old"},
            expected={"downloads/build.zip": file_record(b"old", 0o644)},
            exit=1,
            error_terms=[term],
        )
    for status in [302, 401, 403, 404, 503]:
        download("HTTP " + str(status), expected={}, exit=4 if status == 401 else 1)
        cases[-1]["responses"][DOWNLOAD] = (status, {"detail": "fixture denied"}, {"Location": "/must-not-follow"})
    if sys.platform != "win32":
        for name in [
            "list human",
            "list empty",
            "get human",
            "get unknown raw members",
            "get Git repo",
            "delete confirm",
            "delete decline",
            "download human",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "artifact " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] = ENV | {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


def compare(scenario, python, go):
    differences = watch.compare(scenario, python, go)
    if "expected_files" in scenario:
        for name, result in [("Python", python), ("Go", go)]:
            if result.get("files") != scenario["expected_files"]:
                differences.append(name + " file effects")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/artifact-parity.json")
    )
