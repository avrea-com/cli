"""Public VM contract changes: forks, ports, capabilities, and metrics."""

from avrea_cli.main import cli
from typing import Any
import json
import pytest

VM_ID = "cvm-0123456789abcdef0123456789abcdef"
FORK_ID = "cvm-1123456789abcdef0123456789abcdef"
BASE = "/orgs/org-default/vms"
VM: dict[str, Any] = {
    "customer_vm_id": VM_ID,
    "display_name": "dev #1?",
    "os_type": "linux",
    "architecture": "x86_64",
    "state": "RUNNING",
    "memory_mb": 2048,
    "cpu_count": 2,
    "disk_gb": 20,
    "endpoints": {"ssh": {"username": "runner", "external_ip": "127.0.0.1", "external_port": 2222}},
}


@pytest.mark.parametrize("command", ["start", "stop"])
def test_retired_commands_are_unknown(runner, command):
    result = runner.invoke(cli, ["vm", command, VM_ID])
    assert result.exit_code == 2
    assert "No such command" in result.output


def test_create_generated_name_arch_and_ports(runner, api):
    api.reply("POST", BASE, 202, json={"data": {"vm": VM}})
    result = runner.invoke(
        cli,
        [
            "vm",
            "create",
            "--os",
            "linux",
            "--size",
            "2-vcpu",
            "--arch",
            "arm64",
            "--idempotency-key",
            "create-1",
            "--https-port",
            "8080",
            "--https-port",
            "3000",
        ],
    )
    assert result.exit_code == 0, result.output
    body = json.loads(api.requests[0].content)
    assert body["architecture"] == "arm64"
    assert body["idempotency_key"] == "create-1"
    assert body["https_ports"] == [8080, 3000]
    assert "display_name" not in body
    assert "ephemeral" not in body


@pytest.mark.parametrize(
    "args",
    [
        ["--https-port", "0"],
        ["--https-port", "65536"],
        ["--https-port", "22"],
        ["--https-port", "3389"],
        ["--https-port", "5900"],
        ["--https-port", "3000"] * 2,
        sum((["--https-port", str(port)] for port in range(3000, 3011)), []),
    ],
)
def test_invalid_https_ports_do_not_call_api(runner, api, args):
    result = runner.invoke(cli, ["vm", "create", "--os", "linux", "--size", "2-vcpu", *args])
    assert result.exit_code == 2, result.output
    assert api.requests == []


@pytest.mark.parametrize(
    ("args", "ports"), [(["--https-port", "3000", "--https-port", "8080"], [3000, 8080]), (["--clear-https-ports"], [])]
)
def test_update_replaces_ports(runner, api, args, ports):
    api.reply("PATCH", f"{BASE}/{VM_ID}", json={"data": {"vm": VM}})
    result = runner.invoke(cli, ["vm", "update", VM_ID, *args])
    assert result.exit_code == 0, result.output
    assert json.loads(api.requests[0].content)["https_ports"] == ports


def test_update_rejects_clear_and_ports(runner, api):
    result = runner.invoke(cli, ["vm", "update", VM_ID, "--clear-https-ports", "--https-port", "3000"])
    assert result.exit_code == 2
    assert api.requests == []


def test_names_are_encoded_in_request_paths(runner, api):
    name = "dev #1?"
    api.reply("GET", f"{BASE}/{name}", json={"data": VM})
    result = runner.invoke(cli, ["vm", "show", name])
    assert result.exit_code == 0, result.output
    assert api.requests[0].url.raw_path.endswith(b"dev%20%231%3F")


def test_list_filters(runner, api):
    api.reply("GET", BASE, json={"data": []})
    result = runner.invoke(cli, ["vm", "list", "--scope", "mine", "--no-sandbox", "--state", "PAUSED"])
    assert result.exit_code == 0, result.output
    assert dict(api.requests[0].url.params)["scope"] == "mine"
    assert dict(api.requests[0].url.params)["sandbox"] == "false"


def test_fork_waits_for_fork_not_source(runner, api):
    fork = {**VM, "customer_vm_id": FORK_ID}
    api.reply("POST", f"{BASE}/{VM_ID}/fork", 202, json={"data": {**fork, "state": "PENDING"}})
    api.reply("GET", f"{BASE}/{FORK_ID}", json={"data": fork})
    result = runner.invoke(cli, ["vm", "fork", VM_ID, "--name", "experiment", "--ttl", "1h", "--wait", "--json"])
    assert result.exit_code == 0, result.output
    assert json.loads(api.requests[0].content) == {"display_name": "experiment", "ttl_seconds": 3600}
    assert json.loads(result.stdout)["customer_vm_id"] == FORK_ID
    assert [r.url.path for r in api.requests] == [f"{BASE}/{VM_ID}/fork", f"{BASE}/{FORK_ID}"]


@pytest.mark.parametrize(("state", "exit_code"), [("PENDING", 1), ("FAILED", 1)])
def test_fork_unsuccessful_wait(runner, api, state, exit_code):
    fork = {**VM, "customer_vm_id": FORK_ID, "state": state}
    api.reply("POST", f"{BASE}/{VM_ID}/fork", 202, json={"data": fork})
    api.reply("GET", f"{BASE}/{FORK_ID}", json={"data": fork})
    result = runner.invoke(cli, ["vm", "fork", VM_ID, "--wait", "--wait-timeout", "0", "--json"])
    assert result.exit_code == exit_code, result.output
    assert json.loads(result.stdout)["state"] == state


@pytest.mark.parametrize("state", ["starting", None])
def test_fork_wait_tolerates_unnormalised_state(runner, api, state):
    booting = {**VM, "customer_vm_id": FORK_ID, "state": state}
    api.reply("POST", f"{BASE}/{VM_ID}/fork", 202, json={"data": booting})
    api.reply("GET", f"{BASE}/{FORK_ID}", json={"data": booting})
    result = runner.invoke(cli, ["vm", "fork", VM_ID, "--wait", "--wait-timeout", "0"])
    assert result.exit_code == 1
    assert "error state" not in result.output


def test_fork_refusal(runner, api):
    api.reply("POST", f"{BASE}/{VM_ID}/fork", 409, json={"detail": "Source must be PAUSED"})
    result = runner.invoke(cli, ["vm", "fork", VM_ID])
    assert result.exit_code != 0
    assert "PAUSED" in result.output


def test_fork_interruption(runner, api, monkeypatch):
    api.reply("POST", f"{BASE}/{VM_ID}/fork", 202, json={"data": {**VM, "customer_vm_id": FORK_ID}})
    monkeypatch.setattr("avrea_cli.vm._wait_for_vm", lambda *a, **kw: (_ for _ in ()).throw(KeyboardInterrupt()))
    result = runner.invoke(cli, ["vm", "fork", VM_ID, "--wait"])
    assert result.exit_code != 0


def test_rotation_displays_urls(runner, api):
    vm = {
        **VM,
        "https_ports": [3000, 8080],
        "endpoints": {
            **VM["endpoints"],
            "https": [
                {"port": 3000, "url": "https://new-3000.example"},
                {"port": 8080, "url": "https://new-8080.example"},
            ],
        },
    }
    api.reply("POST", f"{BASE}/{VM_ID}/https-label-rotations", json={"data": vm})
    result = runner.invoke(cli, ["vm", "rotate-https-label", VM_ID])
    assert result.exit_code == 0, result.output
    assert "https://new-3000.example" in result.output
    assert "https://new-8080.example" in result.output


def test_endpoint_without_url_still_renders(runner, api):
    vm = {
        **VM,
        "endpoints": {**VM["endpoints"], "https": [{"port": 3000}, {"port": 8080, "url": "https://ok.example"}]},
    }
    api.reply("GET", f"{BASE}/{VM_ID}", json={"data": vm})
    result = runner.invoke(cli, ["vm", "show", VM_ID])
    assert result.exit_code == 0, result.output
    assert "8080: https://ok.example" in result.output
    assert "3000:" not in result.output


def test_capabilities_tolerate_partial_records(runner, api):
    data = {"os_types": [{"architectures": [{"size_specs": [{"size": "2-vcpu"}]}, {}]}]}
    api.reply("GET", f"{BASE}/capabilities", json={"data": data})
    result = runner.invoke(cli, ["vm", "capabilities"])
    assert result.exit_code == 0, result.output
    assert "2-vcpu" in result.output


@pytest.mark.parametrize("as_json", [True, False])
def test_capabilities(runner, api, as_json):
    data = {
        "max_active_vcpus": 32,
        "active_vcpus": 4,
        "max_active_per_user": 2,
        "active_count": 1,
        "os_types": [
            {
                "os_type": "linux",
                "max_active": None,
                "active_count": 1,
                "architectures": [
                    {
                        "architecture": "arm64",
                        "versions": ["ubuntu-26.04"],
                        "default_version": "ubuntu-26.04",
                        "size_specs": [{"size": "2-vcpu", "cpu_count": 2, "memory_mb": 2048, "disk_gb": 20}],
                    }
                ],
            }
        ],
    }
    api.reply("GET", f"{BASE}/capabilities", json={"data": data})
    result = runner.invoke(cli, ["vm", "capabilities", *(["--json"] if as_json else [])])
    assert result.exit_code == 0, result.output
    if as_json:
        assert json.loads(result.stdout) == data
    else:
        assert "arm64" in result.output
        assert "ubuntu-26.04" in result.output
        assert "linux: no per-OS limit" in result.output
        assert "None" not in result.output


def test_capabilities_null_caps_are_readable(runner, api):
    data = {"max_active_vcpus": None, "active_vcpus": None, "os_types": [{"os_type": "macos", "max_active": 2}]}
    api.reply("GET", f"{BASE}/capabilities", json={"data": data})
    result = runner.invoke(cli, ["vm", "capabilities"])
    assert result.exit_code == 0, result.output
    assert "unlimited" in result.output
    assert "macos: active n/a, limit 2" in result.output
    assert "None" not in result.output


def test_egress_apply_result(runner, api):
    api.reply("PATCH", f"{BASE}/{VM_ID}", json={"data": {"vm": VM, "egress_rules_applied": False}})
    result = runner.invoke(cli, ["vm", "update", VM_ID, "--egress-rules", "[]"])
    assert result.exit_code == 0, result.output
    assert "unconfirmed" in result.output


@pytest.mark.parametrize("applied,expected", [(True, "served by the running VM now"), (False, "next start or resume")])
def test_https_ports_apply_result(runner, api, applied, expected):
    api.reply("PATCH", f"{BASE}/{VM_ID}", json={"data": {"vm": VM, "https_ports_applied": applied}})
    result = runner.invoke(cli, ["vm", "update", VM_ID, "--https-port", "3000"])
    assert result.exit_code == 0, result.output
    assert "HTTPS ports: " in result.output
    assert expected in result.output


def test_metrics_single_shot_window_and_sources(runner, api):
    api.reply("GET", f"{BASE}/{VM_ID}", json={"data": VM})
    data = {"running": False, "start": 100, "end": 200, "metrics": {"cpu": {"series": []}, "load": {"series": []}}}
    api.reply("GET", f"{BASE}/{VM_ID}/metrics", json={"data": data})
    result = runner.invoke(
        cli,
        ["vm", "metrics", VM_ID, "--source", "cpu", "--source", "network", "--start", "100", "--end", "200", "--json"],
    )
    assert result.exit_code == 0, result.output
    assert json.loads(result.stdout) == {"cpu": {"series": []}}
    assert api.requests[-1].url.params.get_list("source") == ["cpu", "network"]
    assert api.requests[-1].url.params["start"] == "100"


def test_metrics_stream_resolves_name_and_replaces_bundle(runner, api, monkeypatch):
    api.reply("GET", f"{BASE}/dev", json={"data": VM})
    paths = []

    def frames(config, path):
        paths.append(path)
        yield {"type": "window", "start": 100, "end": 120, "running": True}
        yield {"type": "bundle", "metrics": {"cpu": {"series": []}, "memory": {"unit": "bytes", "series": []}}}
        yield {"type": "bundle", "metrics": {"cpu": {"unit": "ratio", "series": []}}}
        yield {"type": "complete"}

    monkeypatch.setattr("avrea_cli.vm.metrics_frames", frames)
    result = runner.invoke(cli, ["vm", "metrics", "dev", "--watch", "--json", "--source", "cpu"])
    assert result.exit_code == 0, result.output
    assert paths == [f"{BASE}/{VM_ID}/observability/stream"]
    assert [json.loads(line) for line in result.stdout.splitlines()] == [
        {"cpu": {"series": []}},
        {"cpu": {"unit": "ratio", "series": []}},
    ]
    assert len(api.requests) == 1


def test_metrics_stream_scales_memory(runner, api, monkeypatch):
    api.reply("GET", f"{BASE}/{VM_ID}", json={"data": VM})
    monkeypatch.setattr(
        "avrea_cli.vm.metrics_frames",
        lambda *a: iter(
            [
                {
                    "type": "bundle",
                    "metrics": {
                        "memory": {
                            "unit": "bytes",
                            "series": [{"labels": {"state": "used"}, "values": [[100, 1024**3]]}],
                        }
                    },
                },
                {"type": "complete"},
            ]
        ),
    )
    result = runner.invoke(cli, ["vm", "metrics", VM_ID, "--watch", "--source", "memory"])
    assert result.exit_code == 0, result.output
    assert "1.0 GB / 2.0 GB" in result.output


def test_metrics_empty_completion(runner, api, monkeypatch):
    api.reply("GET", f"{BASE}/{VM_ID}", json={"data": VM})
    monkeypatch.setattr("avrea_cli.vm.metrics_frames", lambda *a: iter([{"type": "complete"}]))
    result = runner.invoke(cli, ["vm", "metrics", VM_ID, "--watch"])
    assert result.exit_code == 0
    assert "No metrics" in result.output


def test_metrics_watch_draws_before_first_bundle(runner, api, monkeypatch):
    api.reply("GET", f"{BASE}/{VM_ID}", json={"data": VM})
    frames = [None, {"signal": "metrics", "type": "error", "message": "backend unavailable"}, {"type": "complete"}]
    monkeypatch.setattr("avrea_cli.vm.metrics_frames", lambda *a: iter(frames))
    result = runner.invoke(cli, ["vm", "metrics", VM_ID, "--watch"])
    assert result.exit_code == 0, result.output
    first, *_ = result.output.split("Metrics: backend unavailable")
    assert VM_ID in first
    assert "Waiting for metrics..." in first


def test_metrics_watch_interrupt(runner, api, monkeypatch):
    api.reply("GET", f"{BASE}/{VM_ID}", json={"data": VM})

    def frames(*args):
        yield None
        raise KeyboardInterrupt

    monkeypatch.setattr("avrea_cli.vm.metrics_frames", frames)
    result = runner.invoke(cli, ["vm", "metrics", VM_ID, "--watch"])
    assert result.exit_code == 130


def test_metrics_watch_rejects_explicit_window(runner, api):
    result = runner.invoke(cli, ["vm", "metrics", VM_ID, "--watch", "--end", "200"])
    assert result.exit_code == 2
    assert api.requests == []


def test_ssh_config_named_vm_uses_canonical_alias(runner, api):
    api.reply("GET", f"{BASE}/dev #1?", json={"data": VM})
    result = runner.invoke(cli, ["vm", "ssh-config", "dev #1?"])
    assert result.exit_code == 0, result.output
    assert f"Host avr-{VM_ID}" in result.stdout
    assert len(api.requests) == 1
