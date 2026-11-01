"""VM create and spawn-script comparisons using local API and credential fixtures."""

from copy import deepcopy
from scripts import compare_audit_health as audit
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_vm_inspection as vm
from scripts import compare_vm_lifecycle as lifecycle
from scripts import compare_vm_update as update
import json
import sys

POST = ("POST", vm.BASE)
FLAGS = ["--name", "devbox", "--os", "linux", "--size", "4-vcpu", "--ephemeral"]
DATA = {"vm": vm.ROW, "password": lifecycle.PASSWORD, "extra": {"large": 9007199254740993}}
SPAWN_ENV = {
    "CLAUDE_RUNNER_ORDER_ID": "fixture-order",
    "CLAUDE_RUNNER_WORK_ORDER_FILE": "work-order",
    "CLAUDE_RUNNER_ENVIRONMENT_KEY": "fixture-environment-key-must-not-be-sent",
}
CREDENTIAL = b"  fixture-per-session-credential\n"


def scenarios():
    cases = []

    def case(name, flags=None, **kwargs):
        cases.append(
            {
                "name": "vm create " + name,
                "args": ["vm", "create", *FLAGS, *(flags or [])],
                "env": vm.ENV.copy(),
                "responses": {POST: (200, {"data": DATA}), vm.SHOW: (200, {"data": lifecycle.RUNNING})},
                "capture_files": [],
                **kwargs,
            }
        )

    case("human", presentation=True)
    case("JSON", ["--json"], json=True)
    case("no auth", env={}, responses={}, exit=4)
    case("explicit org", ["--org", vm.ORG], env=vm.ENV | {"AVR_ORG": "org-other"}, presentation=True)
    case(
        "org slug",
        ["--org", "AcMe"],
        responses={
            POST: (200, {"data": DATA}),
            ("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS}),
        },
        presentation=True,
    )
    for status in [401, 403, 404, 408, 409, 422, 429, 503]:
        case(
            "HTTP " + str(status),
            responses={POST: (status, {"detail": "fixture denied"})},
            exit=4 if status == 401 else 1,
        )
    for name, flags in [
        ("empty name", ["--name", ""]),
        ("repeated name", ["--name", "new"]),
        ("desktop", ["--remote-desktop"]),
        ("desktop off wins", ["--remote-desktop", "--no-remote-desktop"]),
        ("desktop on wins", ["--no-remote-desktop", "--remote-desktop"]),
        ("TTL", ["--ttl", "7d"]),
        ("SSH literal", ["--ssh-key", update.KEY]),
        ("SSH repeated", ["--ssh-key", update.KEY, "--ssh-key", update.KEY]),
        ("egress", ["--egress-rules", json.dumps(update.RULES)]),
        ("empty egress", ["--egress-rules", "[]"]),
        ("repository", ["--repo", "owner/repo"]),
        ("empty repository", ["--repo", ""]),
        ("branch", ["--repo", "owner/repo", "--ref", " feature/topic "]),
        ("empty branch", ["--ref", ""]),
        ("all history", ["--repo", "owner/repo", "--fetch-depth", "0"]),
        ("history", ["--repo", "owner/repo", "--fetch-depth", "100"]),
        ("cache aliases", ["--disable-cache", " GHA, packages ", "--disable-cache", "go-build"]),
        ("cache raw and duplicates", ["--disable-cache", "gha,cache.gha.enabled, GHA"]),
        ("empty cache", ["--disable-cache", " , "]),
        ("server validates caches", ["--disable-cache", "new-cache"]),
        ("server validates OS pairing", ["--os", "macos", "--size", "1-vcpu", "--os-version", "windows-2025"]),
    ]:
        case(name, flags, presentation=True)
    for os_name in ["linux", "macos", "windows"]:
        case("OS " + os_name, ["--os", os_name], presentation=True)
    for version in ["ubuntu-22.04", "ubuntu-24.04", "ubuntu-26.04", "macos-26", "windows-2025"]:
        case("version " + version, ["--os-version", version], presentation=True)
    for size in ["1-vcpu", "2-vcpu", "4-vcpu", "8-vcpu", "16-vcpu", "32-vcpu"]:
        case("size " + size, ["--size", size], presentation=True)
    for flag, value in [
        ("--os", "Linux"),
        ("--os-version", "invalid"),
        ("--size", "3-vcpu"),
        ("--from-spawn-script", "cursor"),
    ]:
        case("invalid choice " + flag, [flag, value], env={}, responses={}, exit=2, error_terms=[flag])
    for flag in ["--name", "--os", "--size"]:
        flags = FLAGS.copy()
        i = flags.index(flag)
        del flags[i : i + 2]
        case("missing " + flag, args=["vm", "create", *flags], env={}, responses={}, exit=2, error_terms=[flag])
    case(
        "missing ephemeral",
        args=["vm", "create", *FLAGS[:-1]],
        env={},
        responses={},
        exit=2,
        error_terms=["--ephemeral"],
    )
    for name, flags, terms in [
        ("ref without repo", ["--ref", "main"], ["--ref requires --repo"]),
        ("depth without repo", ["--fetch-depth", "0"], ["--fetch-depth requires --repo"]),
        ("negative depth", ["--fetch-depth", "-1"], ["--fetch-depth"]),
        ("bad TTL", ["--ttl", "4m"], ["--ttl"]),
        ("bad cache", ["--disable-cache", "cache.gha.other"], ["--disable-cache", "not a cache name"]),
        ("cache injection", ["--disable-cache", "gha;id"], ["--disable-cache", "not a cache name"]),
        ("private key", ["--ssh-key", update.PRIVATE], ["private key", ".pub"]),
        ("bad egress", ["--egress-rules", "{}"], ["JSON array"]),
    ]:
        case(name, flags, env={}, responses={}, exit=2, error_terms=terms)
    for ref in [
        " ",
        "refs/heads/main",
        "refs/tags/v1",
        "refs/pull/1/head",
        "a" * 40,
        "a..b",
        "-main",
        "a;id",
        "a/",
        "a.lock.",
        "a" * 256,
    ]:
        case(
            "bad branch " + repr(ref[:20]),
            ["--repo", "owner/repo", "--ref", ref],
            env={},
            responses={},
            exit=2,
            error_terms=["not a branch name"],
        )
    case(
        "input files",
        ["--ssh-key", "@key.pub", "--egress-rules", "@rules.json"],
        fixture_files={
            "key.pub": update.KEY.encode(),
            "rules.json": json.dumps(update.RULES).encode(),
        },
        presentation=True,
    )
    note_data = DATA | {"precheckout_note": "Mirror warming; checkout may take longer."}
    for as_json in [False, True]:
        case(
            "notes " + str(as_json),
            ["--disable-cache", "turbo,gha,packages", *(["--json"] if as_json else [])],
            responses={POST: (200, {"data": note_data})},
            json=as_json,
            presentation=not as_json,
        )
        for outcome, row in [
            ("ready", lifecycle.RUNNING),
            ("pending", vm.ROW),
            ("failed", lifecycle.FAILED),
            ("SSH not ready", lifecycle.RUNNING | {"endpoints": {}}),
            ("desktop not ready", lifecycle.RUNNING | {"endpoints": {"ssh": vm.ENDPOINTS["ssh"]}}),
        ]:
            case(
                "wait " + outcome + " " + str(as_json),
                ["--wait", "--wait-timeout", "0", *(["--json"] if as_json else [])],
                responses={POST: (200, {"data": note_data}), vm.SHOW: (200, {"data": row})},
                exit=0 if outcome == "ready" else 1,
                json=as_json,
                presentation=not as_json,
            )
    case("wait negative", ["--wait", "--wait-timeout", "-1"], presentation=True)
    for status in [403, 404, 408, 429, 503]:
        case(
            "wait HTTP " + str(status),
            ["--wait", "--wait-timeout", "0"],
            responses={POST: (200, {"data": DATA}), vm.SHOW: (status, {"detail": "fixture poll failed"})},
            exit=1,
        )
    case(
        "wait progress",
        ["--wait", "--wait-timeout", "10"],
        responses={
            POST: (200, {"data": DATA}),
            vm.SHOW: [(200, {"data": vm.ROW}), (200, {"data": lifecycle.RUNNING})],
        },
        presentation=True,
    )
    case(
        "wait retry",
        ["--wait", "--wait-timeout", "10"],
        responses={
            POST: (200, {"data": DATA}),
            vm.SHOW: [(429, {"detail": "slow"}, {"Retry-After": "0"}), (200, {"data": lifecycle.RUNNING})],
        },
        presentation=True,
    )
    for password in [None, "", "fixture pass'word"]:
        case(
            "password " + repr(password),
            responses={POST: (200, {"data": DATA | {"vm": lifecycle.RUNNING, "password": password}})},
            presentation=True,
        )
    case("no jq", ["--json", "--jq", "."], responses={}, exit=2, error_terms=["--jq"])
    case("extra argument", ["extra"], responses={}, exit=2, error_terms=["Got unexpected extra argument (extra)"])

    def spawn(name, flags=None, **kwargs):
        case(
            "spawn " + name,
            args=["vm", "create", "--from-spawn-script", "claude-code", *(flags or [])],
            **{"env": vm.ENV | SPAWN_ENV, "fixture_files": {"work-order": CREDENTIAL}, **kwargs},
        )

    spawn("human", presentation=True)
    spawn("no password returned", responses={POST: (200, {"data": {"vm": vm.ROW}})}, presentation=True)
    spawn("explicit org", ["--org", vm.ORG], env=vm.ENV | SPAWN_ENV | {"AVR_ORG": ""}, presentation=True)
    spawn("explicit linux", ["--os", "linux", "--size", "8-vcpu", "--ephemeral"], presentation=True)
    spawn(
        "all allowed",
        [
            "--os-version",
            "ubuntu-24.04",
            "--ttl",
            "7d",
            "--ssh-key",
            update.KEY,
            "--egress-rules",
            "[]",
            "--disable-cache",
            "gha",
            "--wait-timeout",
            "-1",
        ],
        presentation=True,
    )
    spawn("trimmed order", env=vm.ENV | SPAWN_ENV | {"CLAUDE_RUNNER_ORDER_ID": "  fixture-order  "}, presentation=True)
    spawn("verbose", ["--verbose"], presentation=True)
    for status in [400, 401, 403, 404, 408, 409, 422, 429, 500, 503]:
        spawn(
            "HTTP " + str(status),
            responses={POST: (status, {"detail": "fixture refused"})},
            exit=1 if status in [408, 409, 429, 500, 503] else 2,
        )
    for flags, terms in [
        (["--name", ""], ["--name is derived"]),
        (["--os", "windows"], ["Agent sandboxes run linux"]),
        (["--remote-desktop"], ["no remote desktop"]),
        (["--wait"], ["drop --wait and --json"]),
        (["--json"], ["drop --wait and --json"]),
        (["--repo", ""], ["drop --repo"]),
        (["--ref", ""], ["drop --ref"]),
        (["--fetch-depth", "0"], ["drop --fetch-depth"]),
    ]:
        spawn("invalid flags " + flags[0], flags, responses={}, exit=2, error_terms=terms)
    for org in ["", "acme"]:
        spawn(
            "invalid org " + repr(org),
            env=vm.ENV | SPAWN_ENV | {"AVR_ORG": org},
            responses={},
            exit=2,
            error_terms=["needs an organization ID"],
        )
    for key in ["CLAUDE_RUNNER_ORDER_ID", "CLAUDE_RUNNER_WORK_ORDER_FILE"]:
        spawn(
            "missing " + key, env=vm.ENV | SPAWN_ENV | {key: ""}, responses={}, exit=2, error_terms=[key, "is not set"]
        )
    spawn("missing file", fixture_files={}, responses={}, exit=2, error_terms=["cannot read the work order file"])
    spawn(
        "empty file",
        fixture_files={"work-order": b" \n"},
        responses={},
        exit=2,
        error_terms=["work order file is empty"],
    )
    spawn("no auth", env=SPAWN_ENV | {"AVR_ORG": vm.ORG}, responses={}, exit=4)
    spawn(
        "private key before order read",
        ["--ssh-key", update.PRIVATE],
        fixture_files={},
        responses={},
        exit=2,
        error_terms=["private key", ".pub"],
    )
    if sys.platform != "win32":
        for name in [
            "human",
            "notes False",
            "wait ready False",
            "wait pending False",
            'password "fixture pass\'word"',
            "spawn human",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "vm create " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=audit.compare, default_report="build/go/vm-create-parity.json"
        )
    )
