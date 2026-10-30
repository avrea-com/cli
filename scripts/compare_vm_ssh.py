"""VM SSH parity with synthetic endpoints and an isolated SSH executable stub."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from scripts import compare_vm_inspection as vm
from scripts import compare_vm_ssh_config as ssh_config
import json
import sys

EP = ssh_config.EP


def scenarios():
    cases = []

    def case(name, flags=None, endpoint=None, **kwargs):
        row = vm.ROW | {"state": "RUNNING", "endpoints": {"ssh": EP if endpoint is None else endpoint}}
        cases.append(
            {
                "name": "vm ssh " + name,
                "args": ["vm", "ssh", vm.VM, "--print", *(flags or [])],
                "env": vm.ENV.copy(),
                "responses": {vm.SHOW: (200, {"data": row})},
                "capture_files": [],
                **kwargs,
            }
        )

    case("print", presentation=True)
    case("print no key", endpoint=vm.ENDPOINTS["ssh"], presentation=True)
    case("IPv6", endpoint=EP | {"external_ip": "2001:db8::1"}, presentation=True)
    case("default user", endpoint=EP | {"username": ""}, presentation=True)
    case("port 22", endpoint=EP | {"external_port": 22}, presentation=True)
    for name, flags in [
        ("identity", ["-i", "~/.ssh/id_ed25519"]),
        ("long identity", ["--identity", "key with spaces"]),
        ("attached short identity", ["-ikey"]),
        ("equals identity", ["--identity=key"]),
        ("equals org", ["--org=" + vm.ORG]),
        ("remote simple", ["--", "uname", "-a"]),
        ("remote unknown flags", ["uname", "-a"]),
        ("remote unknown long", ["--unknown=value", "remote", "--other"]),
        ("login", ["--login", "--", "printf", "%s\\n", "hello world"]),
        ("login quotes", ["--login", "--", "printf", "%s", "a'b; $(id)", ""]),
        ("login no command", ["--login"]),
        ("session", ["--session", "dev"]),
        ("session command", ["--session", "dev", "--", "printf", "hello world"]),
        ("session login", ["--session", "dev", "--login", "--", "printf", "a'b"]),
        ("empty session", ["--session", ""]),
        ("max session", ["--session", "a" * 64]),
        ("option before ID", []),
        ("known option after remote", ["uname", "--session", "dev", "-a"]),
        ("unknown short cluster", ["-xyz"]),
        ("mixed known short cluster", ["-xiKEY"]),
        ("dash remote argument", ["--", "-"]),
        ("passthrough known flags", ["--", "--session", "remote", "--org", "remote-org", "-i", "remote-key"]),
    ]:
        case(name, flags, presentation=True)
    next(c for c in cases if c["name"] == "vm ssh option before ID")["args"] = [
        "vm",
        "ssh",
        "--print",
        "--identity",
        "key",
        vm.VM,
    ]
    case("explicit org", ["--org", vm.ORG], env=vm.ENV | {"AVR_ORG": "org-other"}, presentation=True)
    case(
        "org slug",
        ["--org", "AcMe"],
        responses={
            vm.SHOW: (200, {"data": vm.ROW | {"endpoints": {"ssh": EP}}}),
            ("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS}),
        },
        presentation=True,
    )
    case("no auth", env={}, responses={}, exit=4)
    for status in [401, 403, 404, 422, 429, 503]:
        case(
            "HTTP " + str(status),
            responses={vm.SHOW: (status, {"detail": "fixture denied"})},
            exit=4 if status == 401 else 1,
        )
    for row in [vm.ROW, vm.ROW | {"endpoints": {}}, {"endpoints": {"ssh": {}}}]:
        case("pending " + repr(row.get("endpoints")), responses={vm.SHOW: (200, {"data": row})}, exit=1)
    for session in ["bad.name", "bad:name", "bad name", "a" * 65, "$(id)", "bad\nname"]:
        case(
            "invalid session " + repr(session),
            ["--session", session],
            exit=2,
            error_terms=["--session", "not a valid session name"],
        )
    case(
        "session validation follows endpoint",
        ["--session", "bad.name"],
        responses={vm.SHOW: (200, {"data": vm.ROW})},
        exit=1,
    )
    case("missing ID", args=["vm", "ssh", "--print"], responses={}, exit=2, error_terms=["VM_ID"])
    for option in ["--org", "--session", "--identity", "-i"]:
        case("missing value " + option, [option], responses={}, exit=2, error_terms=[option, "requires an argument"])
    for option in ["--print=oops", "--print=false", "--login=true"]:
        case(
            "boolean assignment " + option,
            [option],
            env={},
            responses={},
            exit=2,
            error_terms=["does not take a value"],
        )
    if sys.platform != "win32":
        for exit_code in [0, 1, 42, 255]:
            case(
                "run exit " + str(exit_code),
                args=["vm", "ssh", vm.VM, "--", "uname", "-a"],
                ssh_stub=exit_code,
                ssh_stub_pins=True,
                expect_pin=True,
                exit=exit_code,
            )
        for name, flags in [
            ("run session", ["--session", "dev"]),
            ("run login", ["--login", "--", "printf", "hello world"]),
            ("run identity", ["-i", "my key"]),
            ("run remote flags", ["--", "--help", "--print"]),
            ("run unknown options", ["--unknown", "arg", "-xyz"]),
        ]:
            case(name, args=["vm", "ssh", vm.VM, *flags], ssh_stub=0, ssh_stub_pins=True, expect_pin=True)
        case(
            "run no key",
            args=["vm", "ssh", vm.VM],
            endpoint=vm.ENDPOINTS["ssh"],
            ssh_stub=0,
            ssh_stub_pins=True,
            expect_pin=False,
        )
        case(
            "run malformed key",
            args=["vm", "ssh", vm.VM],
            endpoint=EP | {"host_key": "key\nsecond line"},
            ssh_stub=0,
            ssh_stub_pins=True,
            expect_pin=False,
            exit=1,
            error_terms=["malformed SSH host key"],
        )
        for name in ["print", "login", "session command", "run exit 0", "run no key"]:
            item = deepcopy(next(c for c in cases if c["name"] == "vm ssh " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


def compare(scenario, python, go):
    differences = watch.compare(scenario, python, go)
    if scenario.get("expect_pin"):
        for label, result in [("Python", python), ("Go", go)]:
            if result.get("ssh_pin_removed") is not True:
                differences.append(label + " host-key pin was not removed")
            lines = [line for line in result["stdout"].splitlines() if line.startswith("SSH PIN: ")]
            if not lines or json.loads(lines[0][len("SSH PIN: ") :]) != {
                "text": "[192.0.2.1]:30022 " + ssh_config.KEY + "\n",
                "mode": 0o600,
            }:
                differences.append(label + " incorrect host-key pin")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/vm-ssh-parity.json")
    )
