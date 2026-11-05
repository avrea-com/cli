"""Python 0.6.0 VM additions: capabilities, forks, HTTPS ports, and name lookup."""

from copy import deepcopy
from scripts import compare_audit_health as audit
from scripts import compare_auth as harness
from scripts import compare_vm_create as create
from scripts import compare_vm_inspection as vm
from scripts import compare_vm_lifecycle as lifecycle
from scripts import compare_vm_update as update
from urllib.parse import urlencode
import sys

FORK = "cvm-019a0000000070008000000000000002"
ROW = lifecycle.RUNNING | {
    "architecture": "arm64",
    "repository_full_name": "acme/web",
    "repository_ref": "new-ref",
    "precheckout_ref": "old-ref",
    "preload_status": "ready",
    "access_level": "admin",
    "started_at": "2026-11-05T12:00:00Z",
    "stopped_at": None,
    "https_ports": [3000, 8443],
    "endpoints": vm.ENDPOINTS
    | {
        "https": [
            {"port": 3000, "url": "https://fixture.example.test"},
            {"port": 8443, "url": "https://second.example.test"},
            {"port": 9000},
        ]
    },
}
CAP = {
    "max_active_vcpus": 64,
    "active_vcpus": 6,
    "max_active_per_user": None,
    "active_count": None,
    "os_types": [
        {
            "os_type": "linux",
            "max_active": None,
            "active_count": 2,
            "architectures": [
                {
                    "architecture": "x86_64",
                    "versions": ["ubuntu-24.04", "ubuntu-26.04"],
                    "default_version": "ubuntu-26.04",
                    "size_specs": [{"size": "2-vcpu", "cpu_count": 2, "memory_mb": 8192, "disk_gb": 40}],
                },
                {
                    "architecture": "arm64",
                    "versions": ["ubuntu-26.04"],
                    "default_version": "ubuntu-26.04",
                    "size_specs": [{"size": "4-vcpu", "cpu_count": 4, "memory_mb": 16384, "disk_gb": 80}],
                },
            ],
        },
        {"os_type": "windows", "max_active": 2, "active_count": None, "architectures": []},
    ],
    "unknown": {"large": 9007199254740993},
}


def scenarios():
    cases = []

    def add(name, args, responses=None, **kwargs):
        cases.append(
            {
                "name": "vm release " + name,
                "args": ["vm", *args],
                "env": vm.ENV.copy(),
                "responses": responses or {},
                **kwargs,
            }
        )

    routes = {
        "capabilities": ("GET", vm.BASE + "/capabilities"),
        "fork": ("POST", vm.BASE + "/" + vm.VM + "/fork"),
        "rotate-https-label": ("POST", vm.BASE + "/" + vm.VM + "/https-label-rotations"),
    }
    for command, route in routes.items():
        args = [command] + ([] if command == "capabilities" else [vm.VM])
        row = CAP if command == "capabilities" else ROW | {"customer_vm_id": FORK} if command == "fork" else ROW
        for as_json in (False, True):
            add(
                command + " " + str(as_json),
                args + (["--json"] if as_json else []),
                {route: (200, {"data": row})},
                json=as_json,
                presentation=not as_json,
            )
        add(command + " no auth", args, env={}, exit=4)
        for status in [401, 403, 404, 409, 422, 429, 503]:
            add(
                command + " HTTP " + str(status),
                args,
                {route: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
    for data in [
        {},
        CAP | {"os_types": []},
        CAP | {"max_active_vcpus": None, "active_vcpus": None, "max_active_per_user": 3, "active_count": 0},
    ]:
        add(
            "capability limits " + str(len(cases)),
            ["capabilities"],
            {routes["capabilities"]: (200, {"data": data})},
            presentation=True,
        )
    fork_get = ("GET", vm.BASE + "/" + FORK)
    for flags in [["--name", "copied", "--ttl", "7d"], ["--name", ""], ["--ttl", "300"]]:
        add(
            "fork options " + str(flags),
            ["fork", vm.VM, *flags],
            {routes["fork"]: (200, {"data": ROW | {"customer_vm_id": FORK}})},
            presentation=True,
        )
    for state in ["RUNNING", "PENDING", "FAILED"]:
        for as_json in [False, True]:
            add(
                "fork wait " + state + str(as_json),
                ["fork", vm.VM, "--wait", "--wait-timeout", "0", *(["--json"] if as_json else [])],
                {
                    routes["fork"]: (200, {"data": ROW | {"customer_vm_id": FORK, "state": "PENDING"}}),
                    fork_get: (200, {"data": ROW | {"customer_vm_id": FORK, "state": state}}),
                },
                exit=0 if state == "RUNNING" else 1,
                json=as_json,
                presentation=not as_json,
            )
    for flags, term in [(["--ttl", "1s"], "TTL must"), (["--wait-timeout", "-1"], "--wait-timeout")]:
        add("fork invalid " + str(flags), ["fork", vm.VM, *flags], exit=2, error_terms=[term])
    for flags, query in [
        ([], {"scope": "all"}),
        (["--scope", "mine"], {"scope": "mine"}),
        (["--sandbox"], {"scope": "all", "sandbox": "true"}),
        (["--no-sandbox"], {"scope": "all", "sandbox": "false"}),
        (["--sandbox", "--no-sandbox"], {"scope": "all", "sandbox": "false"}),
        (["--no-sandbox", "--sandbox", "--state", "paused"], {"scope": "all", "sandbox": "true", "state": "PAUSED"}),
    ]:
        add(
            "list selection " + str(flags),
            ["list", *flags],
            {("GET", vm.BASE + "?" + urlencode({"limit": 50} | query)): (200, {"data": [ROW]})},
            presentation=True,
        )
    for flags in [["--scope", "other"], ["--state", "STOPPED"], ["--state", "unknown"]]:
        add("list invalid " + str(flags), ["list", *flags], exit=2, error_terms=[flags[0]])
    for command in ["show", "fork", "rotate-https-label", "update", "pause", "resume", "delete"]:
        name = "Dev Box"
        suffix = {
            "fork": "/fork",
            "rotate-https-label": "/https-label-rotations",
            "pause": "/pause",
            "resume": "/resume",
        }.get(command, "")
        method = {"show": "GET", "update": "PATCH", "delete": "DELETE"}.get(command, "POST")
        payload = {"vm": ROW, "password": None} if command == "update" else ROW
        flags = ["--name", "renamed"] if command == "update" else ["--yes"] if command == "delete" else []
        add(
            command + " by name",
            [command, name, *flags, "--json"],
            {(method, vm.BASE + "/Dev%20Box" + suffix): (200, {"data": payload})},
            json=True,
        )
    for command, base_args, route, payload in [
        ("create", ["create", "--os", "linux", "--size", "2-vcpu"], create.POST, {"vm": ROW, "password": None}),
        ("update", ["update", vm.VM], update.PATCH, {"vm": ROW, "password": None}),
    ]:
        for flags in [
            ["--https-port", "3000"],
            ["--https-port", "8443", "--https-port", "3000"],
            ["--https-port", " 3000 "],
        ]:
            add(
                command + " ports " + str(flags),
                [*base_args, *flags],
                {route: (200, {"data": payload})},
                presentation=True,
            )
        for values, term in [
            (["0"], "--https-port"),
            (["65536"], "--https-port"),
            (["no"], "--https-port"),
            (["3000,8443"], "--https-port"),
            (["3000", "3000"], "unique"),
            (["22"], "reserved"),
            (["3389"], "reserved"),
            (["5900"], "reserved"),
            ([str(3000 + i) for i in range(11)], "At most ten"),
        ]:
            flags = [part for value in values for part in ("--https-port", value)]
            add(command + " invalid ports " + str(values), [*base_args, *flags], exit=2, error_terms=[term])
    for flags in [
        ["--arch", "arm64"],
        ["--arch", "future-architecture"],
        ["--idempotency-key", "custom-key"],
        ["--idempotency-key", ""],
        ["--ephemeral"],
    ]:
        add(
            "create extra " + str(flags),
            ["create", "--os", "linux", "--size", "4-vcpu", *flags],
            {create.POST: (200, {"data": {"vm": ROW, "password": None}})},
            presentation=True,
        )
    add(
        "clear ports",
        ["update", vm.VM, "--clear-https-ports"],
        {update.PATCH: (200, {"data": {"vm": ROW}})},
        presentation=True,
    )
    add(
        "conflicting ports",
        ["update", vm.VM, "--clear-https-ports", "--https-port", "3000"],
        exit=2,
        error_terms=["cannot be combined"],
    )
    for applied in [True, False]:
        add(
            "applied notes " + str(applied),
            ["update", vm.VM, "--https-port", "3000"],
            {
                update.PATCH: (
                    200,
                    {"data": {"vm": ROW, "egress_rules_applied": applied, "https_ports_applied": applied}},
                )
            },
            presentation=True,
        )
    for flags in [["--arch", "arm64"], ["--idempotency-key", "key"]]:
        add(
            "spawn incompatible " + str(flags),
            ["create", "--from-spawn-script", "claude-code", *flags],
            exit=2,
            error_terms=[flags[0]],
        )
    add(
        "spawn ports and architecture",
        ["create", "--from-spawn-script", "claude-code", "--arch", "x86_64", "--https-port", "3000"],
        {create.POST: (200, {"data": {"vm": ROW, "password": None}})},
        env=vm.ENV | create.SPAWN_ENV,
        fixture_files={"work-order": create.CREDENTIAL},
        presentation=True,
    )
    if sys.platform != "win32":
        for original in [case for case in cases if case.get("presentation")][:5]:
            cases.append(
                deepcopy(original)
                | {
                    "name": original["name"] + " terminal",
                    "tty": True,
                    "merge_terminal": True,
                    "env": original["env"] | {"NO_COLOR": "", "TERM": "xterm-256color"},
                }
            )
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=audit.compare, default_report="build/go/vm-release-parity.json"
        )
    )
