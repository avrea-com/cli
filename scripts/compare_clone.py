"""Native Git clone invocation and mirror-discovery Python/Go comparisons."""

from copy import deepcopy
from pathlib import Path
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import json
import os
import shlex
import sys

ENV = {"AVR_TOKEN": harness.TOKEN}
SESSION = ("GET", "/repos/acme/web/mirror/session")
TOKEN = ("POST", "/tokens/git")
PROBE = ("GET", "/acme/web.git/info/refs?service=git-upload-pack")
JWT = "git.synthetic.fixture"
ROW = {
    "browsable": True,
    "full_name": "acme/web",
    "endpoints": ["https://git.fi.example.test", "https://git.de.example.test"],
}
TOKEN_ROW = {"token": JWT, "expires_at": "2099-01-01T00:00:00Z"}
DEFAULT = {SESSION: (200, ROW), TOKEN: (200, TOKEN_ROW)}


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "clone " + name,
                "args": ["repo", "clone", *args],
                "env": ENV.copy(),
                "responses": DEFAULT.copy(),
                **kwargs,
            }
        )

    for status in [401, 403, 404, 409, 422, 429, 503]:
        case(
            "session HTTP " + str(status),
            ["acme/web"],
            responses={SESSION: (status, {"detail": "fixture denied"})},
            exit=4 if status == 401 else 1,
        )
        case(
            "token HTTP " + str(status),
            ["acme/web"],
            responses=DEFAULT | {TOKEN: (status, {"detail": "fixture denied"})},
            exit=4 if status == 401 else 1,
        )
    case("no auth", ["acme/web"], env={}, responses={}, exit=4)
    for repo in ["rep-invalid", "a/..", "a/./b", "a//b", "a/b?query", "a/b/c", "https://github.com/a/b", "/repo"]:
        case("invalid name " + repo, [repo], responses={}, exit=2, error_terms=["REPOSITORY"])
    case("missing repository", [], responses={}, exit=2, error_terms=["REPOSITORY"])
    case("extra argument", ["a/b", "one", "two"], responses={}, exit=2, error_terms=["unexpected extra argument"])
    for option, args in [
        ("depth zero", ["--depth", "0"]),
        ("depth negative", ["--depth", "-1"]),
        ("depth text", ["--depth", "one"]),
        ("org", ["--org", inspection.ORG]),
    ]:
        case("invalid " + option, ["acme/web", *args], responses={}, exit=2, error_terms=["--" + option.split()[0]])
    case(
        "directory file",
        ["acme/web", "FIXTURE_HOME/existing"],
        fixture_files={"existing": b"fixture"},
        capture_files=[],
        responses={},
        exit=2,
        error_terms=["DIRECTORY", "is a file"],
    )
    for value in [{}, {"browsable": "yes"}, {"browsable": None}]:
        case("invalid session " + repr(value), ["acme/web"], responses={SESSION: (200, value)}, exit=1)
    for reason in ["mirror_off", "not_ready", "future_reason", None]:
        case(
            "unavailable " + repr(reason),
            ["acme/web"],
            responses={SESSION: (200, {"browsable": False, "unavailable_reason": reason})},
            exit=1,
        )
    case(
        "unavailable ID",
        [inspection.REPO],
        responses={
            ("GET", f"/repos/{inspection.REPO}/mirror/session"): (
                200,
                {"browsable": False, "unavailable_reason": "mirror_off"},
            )
        },
        exit=1,
    )
    for fields in [{"full_name": None}, {"endpoints": []}, {"endpoints": None}, {"endpoints": [None]}]:
        case("incomplete " + repr(fields), ["acme/web"], responses={SESSION: (200, ROW | fields)}, exit=1)
    for ep in [
        "http://remote.test",
        "https://user:secret@remote.test",
        "https://git.test/path",
        "https://git.test?",
        "https://git.test#",
        "https://git.test:bad",
        "https://git.test\n",
        "https://a=b",
        "https://xn--a",
        "https://[v1.x]",
    ]:
        case("unsafe endpoint " + repr(ep), ["acme/web"], responses={SESSION: (200, ROW | {"endpoints": [ep]})}, exit=1)
    for name in ["../web", "acme/web?secret"]:
        case(
            "unsafe session name " + name,
            ["acme/web"],
            responses={SESSION: (200, ROW | {"full_name": name})},
            exit=2,
            error_terms=["REPOSITORY"],
        )
    for placement in ["unknown.test", "https://untrusted.test", ""]:
        case(
            "unknown placement " + repr(placement),
            ["acme/web", "--placement", placement],
            exit=2,
            error_terms=["--placement", "Available placements"],
        )
    for value in [
        {},
        {"token": "private\nvalue", "expires_at": "2099-01-01T00:00:00Z"},
        {"token": JWT, "expires_at": "2020-01-01T00:00:00Z"},
        {"token": JWT, "expires_at": "2099-01-01T00:00:00"},
        {"token": JWT, "expires_at": "invalid"},
    ]:
        case("invalid token " + repr(value), ["acme/web"], responses=DEFAULT | {TOKEN: (200, value)}, exit=1)
    for status in [200, 302, 401, 403, 404, 503]:
        case(
            "probe HTTP " + str(status),
            ["acme/web"],
            expand_host=True,
            responses={
                SESSION: (200, ROW | {"endpoints": ["FIXTURE_HOST"]}),
                TOKEN: (200, TOKEN_ROW),
                PROBE: (
                    status,
                    b"fixture",
                    {"Content-Type": "text/html", "Location": "https://never-contact.example.test"},
                ),
            },
            exit=1,
        )
    if sys.platform != "win32":
        for placement in ["git.fi.example.test", "https://git.fi.example.test/", "https://git.fi.example.test"]:
            case("explicit " + placement, ["acme/web", "--placement", placement], git_stub=0, presentation=True)
        case(
            "options",
            [
                "acme/web",
                "checkout with spaces",
                "--placement",
                "git.fi.example.test",
                "-b",
                "topic name",
                "--depth",
                "3",
            ],
            git_stub=0,
            presentation=True,
        )
        case("empty branch", ["acme/web", "--placement", "git.fi.example.test", "--branch", ""], git_stub=0)
        case("Git failure", ["acme/web", "--placement", "git.fi.example.test"], git_stub=128, exit=128)
        case(
            "Git traces removed",
            ["acme/web", "--placement", "git.fi.example.test"],
            git_stub=0,
            env=ENV | {"GIT_TRACE": "1", "GIT_TRACE_CURL": "1", "GIT_CURL_VERBOSE": "1", "GIT_TRACE_REDACT": "0"},
        )
        case(
            "scoped no org",
            ["acme/web", "--placement", "git.fi.example.test"],
            git_stub=0,
            env={"AVR_TOKEN": "avs_fixture"},
        )
        case(
            "ID canonical name",
            [inspection.REPO, "--placement", "git.fi.example.test"],
            git_stub=0,
            responses={("GET", f"/repos/{inspection.REPO}/mirror/session"): (200, ROW)},
        )
        case(
            "suffix removed",
            ["rep-acme/web.git", "--placement", "git.fi.example.test"],
            git_stub=0,
            responses={("GET", "/repos/rep-acme/web/mirror/session"): (200, ROW)},
        )
        case(
            "duplicate endpoints",
            ["acme/web", "--placement", "git.fi.example.test"],
            git_stub=0,
            responses={
                SESSION: (
                    200,
                    ROW | {"endpoints": ["https://git.fi.example.test", "https://git.fi.example.test" + "/"]},
                )
            },
        )
        case(
            "nearest",
            ["acme/web"],
            git_stub=0,
            expand_host=True,
            responses={
                SESSION: (200, ROW | {"endpoints": ["FIXTURE_HOST"]}),
                TOKEN: (200, TOKEN_ROW),
                PROBE: (200, b"fixture", {"Content-Type": "application/x-git-upload-pack-advertisement"}),
            },
            presentation=True,
        )
        for name in ["options", "nearest", "unavailable 'mirror_off'"]:
            item = deepcopy(next(c for c in cases if c["name"] == "clone " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    for host, valid in [("xn--a", False), ("xn--bcher-kva.example", True)]:
        cases.append(
            {
                "name": "clone helper IDNA " + host,
                "args": [
                    "auth",
                    "git-credential",
                    "--url",
                    f"https://{host}/acme/web.git",
                    "--repo",
                    "acme/web",
                    "get",
                ],
                "env": ENV.copy(),
                "stdin": f"protocol=https\nhost={host}\npath=acme/web.git\n\n",
                "responses": {TOKEN: (200, TOKEN_ROW)} if valid else {},
                "exit": 0 if valid else 1,
            }
        )
    cases.extend(deepcopy(c) for c in harness.scenarios() if c["name"].startswith("Git "))
    return cases


def compare(scenario, python, go):
    python, go = deepcopy(python), deepcopy(go)
    differences = []
    if "git_stub" in scenario:
        for side, result in [("python", python), ("go", go)]:
            lines = result["stdout"].splitlines()
            for i, line in enumerate(lines):
                if not line.startswith("GIT CAPTURE: "):
                    continue
                captured = json.loads(line.removeprefix("GIT CAPTURE: "))
                if captured["env"] != {"GIT_TERMINAL_PROMPT": "0", "GIT_ASKPASS": "", "SSH_ASKPASS": ""}:
                    differences.append(side + " Git environment")
                for j, arg in enumerate(captured["args"]):
                    if ".helper=!f() { " not in arg:
                        continue
                    prefix, helper = arg.split(".helper=!f() { ", 1)
                    command, suffix = helper.split(' "$@" || ', 1)
                    parts = shlex.split(command)
                    if len(parts) != 8 or parts[2:4] != ["auth", "git-credential"] or not os.path.isabs(parts[1]):
                        differences.append(side + " native helper arguments")
                    if side == "go":
                        expected = Path(sys.argv[sys.argv.index("--go") + 1]).resolve()
                        if Path(parts[1]).resolve() != expected:
                            differences.append("helper does not use the current Go executable")
                    parts[1] = "<native-cli>"
                    captured["args"][j] = prefix + ".helper=" + json.dumps(parts) + " " + suffix
                lines[i] = "GIT CAPTURE: " + json.dumps(captured)
            result["stdout"] = "\n".join(lines) + ("\n" if lines else "")
    # Presentation is compared on the normalized helper command, not shell quoting
    # or the intentional Python-versus-native executable path difference.
    return differences + watch.compare(scenario, python, go)


if __name__ == "__main__":
    sys.exit(harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/clone-parity.json"))
