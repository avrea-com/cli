"""Tunnel command parity with local endpoints; auto-picked ports are normalized by role."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from scripts import compare_vm_inspection as vm
from scripts import compare_vm_ssh_config as ssh
import re
import sys

ROW = vm.ROW | {"state": "RUNNING", "enable_remote_desktop": True, "endpoints": {"ssh": ssh.EP}}


def scenarios():
    cases = []

    def case(name, command="port-forward", flags=None, **kwargs):
        default = ["--port", "48080:8080"] if command == "port-forward" else ["--local-port", "43389"]
        row = ROW | ({"os_type": "macos"} if command == "vnc" else {})
        cases.append(
            {
                "name": "vm tunnel " + name,
                "args": ["vm", command, vm.VM, "--print", *(default if flags is None else flags)],
                "env": vm.ENV.copy(),
                "responses": {vm.SHOW: (200, {"data": row})},
                "capture_files": [],
                **kwargs,
            }
        )

    for command in ["port-forward", "rdp", "vnc"]:
        case(command + " print", command, presentation=True)
        case(command + " no auth", command, env={}, responses={}, exit=4)
        defaults = ["--port", "48080:8080"] if command == "port-forward" else ["--local-port", "43389"]
        case(command + " identity", command, [*defaults, "-i", "key with spaces"], presentation=True)
        case(
            command + " explicit org",
            command,
            [*defaults, "--org", vm.ORG],
            env=vm.ENV | {"AVR_ORG": "org-other"},
            presentation=True,
        )
        row = ROW | ({"os_type": "macos"} if command == "vnc" else {})
        case(
            command + " slug",
            command,
            [*defaults, "--org", "AcMe"],
            responses={
                vm.SHOW: (200, {"data": row}),
                ("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS}),
            },
            presentation=True,
        )
        for status in [401, 403, 404, 422, 429, 503]:
            case(
                command + " HTTP " + str(status),
                command,
                responses={vm.SHOW: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
        for value in ["0", "-1", "65536", "bad"]:
            case(
                command + " invalid local " + value,
                command,
                [*defaults, "--local-port", value],
                env={},
                responses={},
                exit=2,
                error_terms=["--local-port"],
            )
        case(command + " no endpoint", command, responses={vm.SHOW: (200, {"data": row | {"endpoints": None}})}, exit=1)
        case(
            command + " missing ID",
            command,
            args=["vm", command, *defaults],
            responses={},
            exit=2,
            error_terms=["VM_ID"],
        )
        case(command + " no JSON", command, [*defaults, "--json"], responses={}, exit=2, error_terms=["--json"])
        case(
            command + " auto port",
            command,
            ["--port", "8080"] if command == "port-forward" else [],
            auto_ports=True,
            presentation=True,
        )
        if sys.platform != "win32":
            for status in [0, 42]:
                case(
                    command + " fails before listening " + str(status),
                    command,
                    args=["vm", command, vm.VM, *defaults],
                    ssh_stub=status,
                    ssh_stub_pins=True,
                    exit=1,
                    error_terms=["SSH tunnel did not come up"],
                )
    for name, flags in [
        ("multi explicit", ["--port", "48080:8080", "--port", "45432:5432"]),
        ("local override", ["--port", "8080", "--local-port", "48080"]),
        ("signed port", ["--port", "+48080:+8080"]),
        ("underscores", ["--port", "48_080:8_080"]),
        ("whitespace", ["--port", " 48080 : 8080 "]),
        ("boundary ports", ["--port", "1:65535", "--port", "65535:1"]),
    ]:
        case(name, flags=flags, presentation=True)
    case("multi auto", flags=["--port", "8080", "--port", "5432"], auto_ports=True, presentation=True)
    case(
        "mixed auto",
        flags=["--port", "8080", "--port", "48080:3000", "--port", "5432"],
        auto_ports=True,
        explicit_ports=[48080],
        presentation=True,
    )
    for value in [
        "",
        "bad",
        "0",
        "65536",
        "-1",
        "9999999999999999999999",
        ":8080",
        "8080:",
        "a:8080",
        "8080:bad",
        "1:2:3",
    ]:
        case(
            "invalid port " + repr(value), flags=["--port", value], env={}, responses={}, exit=2, error_terms=["--port"]
        )
    for name, flags, terms in [
        (
            "duplicate guest",
            ["--port", "48080:8080", "--port", "49080:8080"],
            ["guest 8080 is forwarded more than once"],
        ),
        ("duplicate local", ["--port", "48080:8080", "--port", "48080:3000"], ["local 48080 is bound more than once"]),
        (
            "override explicit",
            ["--port", "48080:8080", "--local-port", "49080"],
            ["--local-port applies to a single bare --port"],
        ),
        (
            "override multi",
            ["--port", "8080", "--port", "3000", "--local-port", "49080"],
            ["--local-port applies to a single bare --port"],
        ),
        ("missing port", [], ["--port"]),
    ]:
        case(name, flags=flags, env={}, responses={}, exit=2, error_terms=terms)
    for command in ["rdp", "vnc"]:
        for flags in [["--launch"], ["--launch", "--no-launch"], ["--no-launch", "--launch"]]:
            case(
                command + " print launch flags " + repr(flags),
                command,
                ["--local-port", "43389", *flags],
                presentation=True,
            )
        case(
            command + " desktop disabled",
            command,
            responses={vm.SHOW: (200, {"data": ROW | {"enable_remote_desktop": False}})},
            exit=1,
        )
        case(
            command + " wrong protocol",
            command,
            responses={vm.SHOW: (200, {"data": ROW | {"os_type": "macos" if command == "rdp" else "linux"}})},
            exit=1,
        )
    for guest in ["linux", "windows"]:
        case("rdp " + guest, "rdp", responses={vm.SHOW: (200, {"data": ROW | {"os_type": guest}})}, presentation=True)
    case(
        "rdp live endpoint",
        "rdp",
        responses={
            vm.SHOW: (
                200,
                {
                    "data": ROW
                    | {
                        "endpoints": {
                            "ssh": ssh.EP,
                            "remote_desktop": vm.ENDPOINTS["remote_desktop"] | {"username": "desktop"},
                        }
                    }
                },
            )
        },
        presentation=True,
    )
    case(
        "vnc live endpoint",
        "vnc",
        responses={
            vm.SHOW: (
                200,
                {
                    "data": ROW
                    | {
                        "endpoints": {
                            "ssh": ssh.EP,
                            "remote_desktop": vm.ENDPOINTS["remote_desktop"] | {"protocol": "vnc"},
                        }
                    }
                },
            )
        },
        presentation=True,
    )
    case(
        "live protocol wins over OS",
        "rdp",
        responses={
            vm.SHOW: (
                200,
                {
                    "data": ROW
                    | {
                        "os_type": "macos",
                        "endpoints": {"ssh": ssh.EP, "remote_desktop": vm.ENDPOINTS["remote_desktop"]},
                    }
                },
            )
        },
        presentation=True,
    )
    if sys.platform != "win32":
        for name in ["port-forward print", "rdp print", "vnc print", "multi explicit", "rdp fails before listening 42"]:
            item = deepcopy(next(c for c in cases if c["name"] == "vm tunnel " + name))
            item.pop("error_terms", None)
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


def compare(scenario, python, go):
    python, go = deepcopy(python), deepcopy(go)
    differences = []
    if scenario.get("auto_ports"):
        for label, result in [("Python", python), ("Go", go)]:
            pairs = re.findall(r"127\.0\.0\.1:(\d+):localhost:(\d+)", result["stdout"])
            locals_ = [int(local) for local, _ in pairs]
            if not locals_ or len(set(locals_)) != len(locals_) or any(not 1 <= p <= 65535 for p in locals_):
                differences.append(label + " invalid auto-selected ports")
            for index, local in enumerate(locals_):
                if local not in scenario.get("explicit_ports", []):
                    for stream in ["stdout", "stderr"]:
                        result[stream] = re.sub(
                            r"127\.0\.0\.1:" + str(local) + r"(?!\d)",
                            "127.0.0.1:<auto" + str(index) + ">",
                            result[stream],
                        )
    if scenario.get("ssh_stub_pins"):
        for label, result in [("Python", python), ("Go", go)]:
            if result["ssh_pin_removed"] is not True:
                differences.append(label + " failed tunnel leaked its host-key pin")
    return differences + watch.compare(scenario, python, go)


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/vm-tunnel-parity.json")
    )
