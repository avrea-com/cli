"""VM list/show/usage parity, including terminal connection hints."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_firewall as firewall
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from urllib.parse import urlencode
import sys

ORG = inspection.ORG
VM = "cvm-019a0000000070008000000000000001"
BASE = f"/orgs/{ORG}/vms"
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "AVR_REPO": "ignored/repo"}
ROW = {
    "customer_vm_id": VM,
    "display_name": "Dev 🦫 box",
    "os_type": "linux",
    "cpu_count": 2,
    "memory_mb": 2048,
    "disk_gb": 20,
    "enable_remote_desktop": False,
    "ssh_public_keys": ["ssh-ed25519 AAAA fixture@example.test"],
    "desired_state": "RUNNING",
    "state": "PENDING",
    "state_reason": None,
    "endpoints": None,
    "stop_at": "2026-10-30T12:00:00Z",
    "created_at": "2026-10-30T04:00:00.123456Z",
    "updated_at": "2026-10-30T04:00:00Z",
    "extra": {"unknown": None},
}
ENDPOINTS = {
    "ssh": {"protocol": "ssh", "external_ip": "192.0.2.1", "external_port": 30022, "username": "runner"},
    "remote_desktop": {"protocol": "rdp", "external_ip": "192.0.2.1", "external_port": 33389, "username": "runner"},
}
USAGE_ROW = {
    "customer_vm_id": VM,
    "display_name": "Dev box",
    "os_type": "linux",
    "state": "DELETED",
    "run_count": 3,
    "runtime_seconds": 3600,
    "vcpu_seconds": 7200,
    "memory_mb_seconds": 7372800,
}
USAGE = {
    "period_start": "2026-10-01T00:00:00Z",
    "period_end": "2026-10-30T00:00:00Z",
    "total_runtime_seconds": 3600,
    "total_vcpu_seconds": 7200,
    "total_memory_mb_seconds": 7372800,
    "vms": [USAGE_ROW],
}
LIST = ("GET", BASE + "?limit=50")
SHOW = ("GET", BASE + "/" + VM)
USAGE_GET = ("GET", BASE + "/usage")
RESPONSES = {
    LIST: (200, {"data": [ROW], "pagination": {"next_cursor": "next/cursor+="}}),
    SHOW: (200, {"data": ROW}),
    USAGE_GET: (200, {"data": USAGE}),
}


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "vm inspect " + name,
                "args": ["vm", *args],
                "env": ENV.copy(),
                "responses": RESPONSES.copy(),
                **kwargs,
            }
        )

    for name, args, route in [("list", ["list"], LIST), ("show", ["show", VM], SHOW), ("usage", ["usage"], USAGE_GET)]:
        case(name + " human", args, presentation=True)
        case(name + " JSON", args + ["--json"], json=True)
        case(name + " no auth", args, env={}, responses={}, exit=4)
        case(
            name + " org slug",
            args + ["--org", "AcMe"],
            responses=RESPONSES | {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})},
            presentation=True,
        )
        case(name + " explicit org", args + ["--org", ORG], env=ENV | {"AVR_ORG": "org-other"}, presentation=True)
        case(name + " no JSON fields", args + ["--json", "*"], responses={}, exit=2, error_terms=["*"])
        case(name + " no jq", args + ["--jq", "."], responses={}, exit=2, error_terms=["--jq"])
        for status in [401, 403, 404, 409, 422, 429, 503]:
            case(
                name + " HTTP " + str(status),
                args,
                responses={route: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
    for json_output in [False, True]:
        case(
            "list empty " + str(json_output),
            ["list", *(["--json"] if json_output else [])],
            responses={LIST: (200, {"data": []})},
            json=json_output,
            presentation=not json_output,
        )
        case(
            "list quoted cursor " + str(json_output),
            ["list", *(["--json"] if json_output else [])],
            responses={LIST: (200, {"data": [], "pagination": {"next_cursor": "needs 'quoting'"}})},
            json=json_output,
            presentation=not json_output,
        )
    case(
        "list nullable cells",
        ["list"],
        responses={LIST: (200, {"data": [ROW | {"display_name": None, "desired_state": None}, {}]})},
        presentation=True,
    )
    case(
        "list state and cursor",
        ["list", "--state", "running", "--cursor", "opaque /?+", "-L", "1"],
        responses={
            ("GET", BASE + "?" + urlencode({"limit": 1, "state": "running", "cursor": "opaque /?+"})): (
                200,
                {"data": [ROW]},
            )
        },
        presentation=True,
    )
    case("list empty state", ["list", "--state", ""], presentation=True)
    case(
        "list maximum",
        ["list", "--limit", "100"],
        responses={("GET", BASE + "?limit=100"): (200, {"data": [ROW]})},
        presentation=True,
    )
    for limit in ["0", "101"]:
        case(
            "list invalid limit " + limit,
            ["list", "--limit", limit],
            responses={},
            env={},
            exit=2,
            error_terms=["--limit"],
        )
    for cursor in ["", " space", "space ", "x" * 513]:
        case(
            "list invalid cursor " + repr(cursor[:10]),
            ["list", "--cursor", cursor],
            responses={},
            exit=2,
            error_terms=["--cursor"],
        )
    for name, changes in [
        ("empty", {}),
        ("SSH ready", ROW | {"state": "RUNNING", "endpoints": {"ssh": ENDPOINTS["ssh"]}}),
        ("RDP ready Linux", ROW | {"state": "RUNNING", "enable_remote_desktop": True, "endpoints": ENDPOINTS}),
        (
            "RDP ready Windows",
            ROW | {"os_type": "windows", "state": "RUNNING", "enable_remote_desktop": True, "endpoints": ENDPOINTS},
        ),
        ("RDP pending", ROW | {"enable_remote_desktop": True}),
        (
            "VNC ready",
            ROW
            | {
                "os_type": "macos",
                "enable_remote_desktop": True,
                "endpoints": ENDPOINTS | {"remote_desktop": ENDPOINTS["remote_desktop"] | {"protocol": "vnc"}},
            },
        ),
        ("VNC pending", ROW | {"os_type": "macos", "enable_remote_desktop": True}),
        ("empty endpoints", ROW | {"endpoints": {}}),
        ("unknown endpoints", ROW | {"endpoints": {"unknown": True}}),
        ("no SSH keys", ROW | {"ssh_public_keys": []}),
        ("two SSH keys", ROW | {"ssh_public_keys": ["ssh-ed25519 AAAA a", "ssh-rsa AAAA b"]}),
        ("preload", ROW | {"precheckout_ref": "main", "preload_status": "ready", "fetch_depth": 1}),
        ("preload pending", ROW | {"precheckout_ref": "main", "preload_status": None, "fetch_depth": 0}),
        (
            "snapshot memory",
            ROW | {"state": "PAUSED", "snapshot": {"memory_included": True, "paused_at": "2026-10-30T05:00:00Z"}},
        ),
        ("snapshot disk", ROW | {"snapshot": {"memory_included": False}}),
        ("egress", ROW | {"egress_rules": firewall.RULES}),
        ("large integers JSON", ROW | {"cpu_count": 9007199254740993}),
    ]:
        is_json = name.endswith("JSON")
        case(
            "show " + name,
            ["show", VM, *(["--json"] if is_json else [])],
            responses={SHOW: (200, {"data": changes})},
            json=is_json,
            presentation=not is_json,
        )
    case("show missing argument", ["show"], responses={}, exit=2, error_terms=["VM_ID"])
    case("usage empty", ["usage"], responses={USAGE_GET: (200, {"data": USAGE | {"vms": []}})}, presentation=True)
    case("usage missing fields", ["usage"], responses={USAGE_GET: (200, {"data": {}})}, presentation=True)
    case(
        "usage nullable",
        ["usage"],
        responses={USAGE_GET: (200, {"data": USAGE | {"total_runtime_seconds": None, "vms": None}})},
        presentation=True,
    )
    case(
        "usage large JSON",
        ["usage", "--json"],
        responses={
            USAGE_GET: (200, {"data": USAGE | {"total_memory_mb_seconds": 9007199254740993, "unknown": [None]}})
        },
        json=True,
    )
    for start, end, expected_start, expected_end in [
        ("2026-10-01", "2026-10-30", "2026-10-01T00:00:00", "2026-10-30T00:00:00"),
        ("2026-10-01T12:34:56", "2026-10-30T12:34:56", "2026-10-01T12:34:56", "2026-10-30T12:34:56"),
        ("2026-1-2", "2026-2-3T1:2:3", "2026-01-02T00:00:00", "2026-02-03T01:02:03"),
    ]:
        case(
            "usage dates " + start,
            ["usage", "--start", start, "--end", end],
            responses={
                ("GET", BASE + "/usage?" + urlencode({"period_start": expected_start, "period_end": expected_end})): (
                    200,
                    {"data": USAGE},
                )
            },
            presentation=True,
        )
    for flag in ["--start", "--end"]:
        for value in ["", "yesterday", "2026-02-30", "2026-10-01T00:00:00Z", "2026-10-01T00:00:00.1"]:
            case(
                "usage invalid " + flag + repr(value),
                ["usage", flag, value],
                responses={},
                env={},
                exit=2,
                error_terms=[flag],
            )
    if sys.platform != "win32":
        for name in [
            "list human",
            "show human",
            "usage human",
            "show RDP ready Linux",
            "show RDP pending",
            "show egress",
            "show snapshot memory",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "vm inspect " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=watch.compare, default_report="build/go/vm-inspection-parity.json"
        )
    )
