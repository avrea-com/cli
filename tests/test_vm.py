"""Unit tests for the `avr vm` long-running VM commands."""

from avrea_cli.main import cli
import json

SAMPLE_VM = {
    "customer_vm_id": "cvm-abc123",
    "display_name": "dev box",
    "os_type": "linux",
    "cpu_count": 2,
    "memory_mb": 2048,
    "disk_gb": 20,
    "enable_remote_desktop": False,
    "ssh_public_keys": ["ssh-ed25519 AAAA test@host"],
    "desired_state": "RUNNING",
    "state": "PENDING",
    "state_reason": None,
    "endpoints": None,
    "stop_at": "2026-06-15T12:00:00Z",
    "created_at": "2026-06-15T04:00:00Z",
    "updated_at": "2026-06-15T04:00:00Z",
}

CREATE_RESPONSE = {"data": {"vm": SAMPLE_VM, "password": "hunter2hunter2"}}


def _capture(store):
    """Return a stub that records (path, json, params) and returns ``value``."""

    def _stub(self, path, json=None, params=None, timeout=None):
        store["path"] = path
        store["json"] = json
        store["params"] = params
        return store["return"]

    return _stub


class TestVmCreate:
    def test_requires_ephemeral_acknowledgement(self, runner, monkeypatch):
        called = {"n": 0}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, json=None, timeout=None: called.__setitem__("n", called["n"] + 1) or CREATE_RESPONSE,
        )
        result = runner.invoke(
            cli,
            [
                "vm",
                "create",
                "--name",
                "dev",
                "--os",
                "linux",
                "--size",
                "2-vcpu",
            ],
        )
        assert result.exit_code == 2
        assert "ephemeral" in result.output.lower()
        assert called["n"] == 0  # never hit the API

    def test_create_success(self, runner, monkeypatch):
        store = {"return": CREATE_RESPONSE}
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", _capture(store))
        result = runner.invoke(
            cli,
            [
                "vm",
                "create",
                "--name",
                "dev box",
                "--os",
                "linux",
                "--size",
                "2-vcpu",
                "--ttl",
                "8h",
                "--ssh-key",
                "ssh-ed25519 AAAA test@host",
                "--ephemeral",
            ],
        )
        assert result.exit_code == 0
        assert store["path"] == "/orgs/org-default/vms"
        body = store["json"]
        assert body["ephemeral"] is True
        assert body["os_type"] == "linux"
        assert body["size"] == "2-vcpu"
        assert body["ttl_seconds"] == 8 * 3600
        assert body["ssh_public_keys"] == ["ssh-ed25519 AAAA test@host"]
        # os_version is omitted so the server resolves the OS default
        assert "os_version" not in body
        # the server derives cpu/memory/disk from the size tier; the CLI must not send them
        assert "cpu_count" not in body
        assert "image_series_name" not in body
        # one-time password and the poll hint are both surfaced
        assert "hunter2hunter2" in result.output
        assert "vm show cvm-abc123" in result.output

    def test_create_json_output(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, json=None, timeout=None: CREATE_RESPONSE,
        )
        result = runner.invoke(
            cli,
            [
                "vm",
                "create",
                "--name",
                "dev",
                "--os",
                "linux",
                "--size",
                "2-vcpu",
                "--ephemeral",
                "--json",
            ],
        )
        assert result.exit_code == 0
        payload = json.loads(result.output)
        assert payload["password"] == "hunter2hunter2"
        assert payload["vm"]["customer_vm_id"] == "cvm-abc123"

    def test_ttl_out_of_range_rejected(self, runner):
        result = runner.invoke(
            cli,
            [
                "vm",
                "create",
                "--name",
                "dev",
                "--os",
                "linux",
                "--size",
                "2-vcpu",
                "--ttl",
                "30d",
                "--ephemeral",
            ],
        )
        assert result.exit_code == 2
        assert "TTL must be between" in result.output

    def test_ssh_key_from_file(self, runner, monkeypatch, tmp_path):
        store = {"return": CREATE_RESPONSE}
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", _capture(store))
        key_file = tmp_path / "id.pub"
        key_file.write_text("ssh-ed25519 AAAAfromfile user@host\n")
        result = runner.invoke(
            cli,
            [
                "vm",
                "create",
                "--name",
                "dev",
                "--os",
                "linux",
                "--size",
                "2-vcpu",
                "--ephemeral",
                "--ssh-key",
                f"@{key_file}",
            ],
        )
        assert result.exit_code == 0
        assert store["json"]["ssh_public_keys"] == ["ssh-ed25519 AAAAfromfile user@host"]

    def test_os_version_and_size_forwarded(self, runner, monkeypatch):
        store = {"return": CREATE_RESPONSE}
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", _capture(store))
        result = runner.invoke(
            cli,
            [
                "vm",
                "create",
                "--name",
                "dev",
                "--os",
                "linux",
                "--os-version",
                "ubuntu-22.04",
                "--size",
                "8-vcpu",
                "--ephemeral",
            ],
        )
        assert result.exit_code == 0
        assert store["json"]["os_version"] == "ubuntu-22.04"
        assert store["json"]["size"] == "8-vcpu"

    def test_invalid_size_rejected(self, runner):
        result = runner.invoke(
            cli,
            ["vm", "create", "--name", "dev", "--os", "linux", "--size", "3-vcpu", "--ephemeral"],
        )
        assert result.exit_code == 2
        assert "3-vcpu" in result.output


class TestVmList:
    def test_table_output(self, runner, monkeypatch):
        listing = {"data": [SAMPLE_VM], "pagination": {"next_cursor": "CURSOR123"}}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: listing,
        )
        result = runner.invoke(cli, ["vm", "list"])
        assert result.exit_code == 0
        assert "cvm-abc123" in result.output
        assert "dev box" in result.output
        # next page cursor advertised
        assert "CURSOR123" in result.output

    def test_state_filter_passed(self, runner, monkeypatch):
        store = {"return": {"data": [], "pagination": {}}}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: store.__setitem__("params", params) or store["return"],
        )
        result = runner.invoke(cli, ["vm", "list", "--state", "RUNNING", "--limit", "10"])
        assert result.exit_code == 0
        assert store["params"]["state"] == "RUNNING"
        assert store["params"]["limit"] == 10

    def test_json_output(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: {"data": [SAMPLE_VM], "pagination": {}},
        )
        result = runner.invoke(cli, ["vm", "list", "--json"])
        assert result.exit_code == 0
        assert json.loads(result.output)[0]["customer_vm_id"] == "cvm-abc123"


class TestVmShow:
    def test_shows_endpoints_and_egress(self, runner, monkeypatch):
        detail = {
            "data": {
                **SAMPLE_VM,
                "state": "RUNNING",
                "endpoints": {
                    "ssh": {
                        "protocol": "ssh",
                        "external_ip": "203.0.113.1",
                        "external_port": 30022,
                        "username": "runner",
                        "host_key": "ssh-ed25519 AAAAkey",
                    },
                    "remote_desktop": None,
                },
                "egress_rules": [
                    {
                        "position": 0,
                        "action": "allow",
                        "cidr": None,
                        "fqdn": "github.com",
                        "protocol": "tcp",
                        "port_start": 443,
                        "port_end": 443,
                        "is_default": False,
                    }
                ],
            }
        }
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: detail,
        )
        result = runner.invoke(cli, ["vm", "show", "cvm-abc123"])
        assert result.exit_code == 0
        assert "runner@203.0.113.1 -p 30022" in result.output
        assert "github.com" in result.output


class TestVmUpdate:
    def test_noop_rejected(self, runner):
        result = runner.invoke(cli, ["vm", "update", "cvm-abc123"])
        assert result.exit_code == 2
        assert "Nothing to update" in result.output

    def test_update_name_and_ttl(self, runner, monkeypatch):
        store = {"return": CREATE_RESPONSE}
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_patch", _capture(store))
        result = runner.invoke(cli, ["vm", "update", "cvm-abc123", "--name", "renamed", "--ttl", "2h"])
        assert result.exit_code == 0
        assert store["path"] == "/orgs/org-default/vms/cvm-abc123"
        assert store["json"]["display_name"] == "renamed"
        assert store["json"]["ttl_seconds"] == 2 * 3600
        assert store["json"]["rotate_password"] is False

    def test_rotate_password(self, runner, monkeypatch):
        store = {"return": CREATE_RESPONSE}
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_patch", _capture(store))
        result = runner.invoke(cli, ["vm", "update", "cvm-abc123", "--rotate-password"])
        assert result.exit_code == 0
        assert store["json"]["rotate_password"] is True
        assert "hunter2hunter2" in result.output


class TestVmPower:
    def test_start_sets_desired_running(self, runner, monkeypatch):
        store = {"return": CREATE_RESPONSE}
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_patch", _capture(store))
        result = runner.invoke(cli, ["vm", "start", "cvm-abc123"])
        assert result.exit_code == 0
        assert store["json"] == {"desired_state": "RUNNING"}
        assert "hunter2hunter2" in result.output

    def test_stop_sets_desired_stopped(self, runner, monkeypatch):
        store = {"return": {"data": {"vm": {**SAMPLE_VM, "state": "STOPPING"}, "password": None}}}
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_patch", _capture(store))
        result = runner.invoke(cli, ["vm", "stop", "cvm-abc123"])
        assert result.exit_code == 0
        assert store["json"] == {"desired_state": "STOPPED"}


class TestVmDelete:
    def test_aborts_without_confirmation(self, runner, monkeypatch):
        called = {"n": 0}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_delete",
            lambda self, path, params=None: called.__setitem__("n", called["n"] + 1) or None,
        )
        result = runner.invoke(cli, ["vm", "delete", "cvm-abc123"], input="n\n")
        assert result.exit_code != 0
        assert called["n"] == 0

    def test_delete_with_yes(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_delete",
            lambda self, path, params=None: {"data": {"customer_vm_id": "cvm-abc123", "state": "DELETING"}},
        )
        result = runner.invoke(cli, ["vm", "delete", "cvm-abc123", "--yes"])
        assert result.exit_code == 0
        assert "DELETING" in result.output


class TestVmUsage:
    def test_usage_table_and_totals(self, runner, monkeypatch):
        report = {
            "data": {
                "period_start": "2026-05-16T00:00:00Z",
                "period_end": "2026-06-15T00:00:00Z",
                "vms": [
                    {
                        "customer_vm_id": "cvm-abc123",
                        "display_name": "dev box",
                        "os_type": "linux",
                        "state": "RUNNING",
                        "run_count": 3,
                        "runtime_seconds": 3600,
                        "vcpu_seconds": 7200,
                        "memory_mb_seconds": 7372800,
                    }
                ],
                "total_runtime_seconds": 3600,
                "total_vcpu_seconds": 7200,
                "total_memory_mb_seconds": 7372800,
            }
        }
        store = {"return": report}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: store.__setitem__("params", params) or store["return"],
        )
        result = runner.invoke(cli, ["vm", "usage", "--start", "2026-05-16"])
        assert result.exit_code == 0
        assert "cvm-abc123" in result.output
        assert "Total runtime (s)" in result.output
        assert store["params"]["period_start"].startswith("2026-05-16")


RUNNING_VM = {
    "data": {
        **SAMPLE_VM,
        "state": "RUNNING",
        "endpoints": {
            "ssh": {
                "protocol": "ssh",
                "external_ip": "203.0.113.1",
                "external_port": 30022,
                "username": "runner",
                "host_key": "ssh-ed25519 AAAAkey",
            },
            "remote_desktop": None,
        },
    }
}


class TestVmSsh:
    def test_print_emits_command_without_exec(self, runner, monkeypatch):
        called = {"n": 0}
        monkeypatch.setattr("avrea_cli.vm.os.execvp", lambda *a: called.__setitem__("n", called["n"] + 1))
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: RUNNING_VM,
        )
        result = runner.invoke(cli, ["vm", "ssh", "cvm-abc123", "--print"])
        assert result.exit_code == 0
        assert result.output.strip() == "ssh -p 30022 runner@203.0.113.1"
        assert called["n"] == 0  # never exec'd

    def test_exec_builds_argv_with_identity_and_passthrough(self, runner, monkeypatch):
        captured = {}
        monkeypatch.setattr(
            "avrea_cli.vm.os.execvp",
            lambda file, argv: captured.update(file=file, argv=argv),
        )
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: RUNNING_VM,
        )
        result = runner.invoke(
            cli,
            ["vm", "ssh", "cvm-abc123", "-i", "/tmp/key", "--", "-L", "8080:localhost:80"],
        )
        assert result.exit_code == 0
        assert captured["file"] == "ssh"
        # generated options first, passthrough next, destination last
        assert captured["argv"] == [
            "ssh",
            "-i",
            "/tmp/key",
            "-p",
            "30022",
            "-L",
            "8080:localhost:80",
            "runner@203.0.113.1",
        ]

    def test_no_endpoint_yet_errors(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: {"data": {**SAMPLE_VM, "state": "PENDING", "endpoints": None}},
        )
        result = runner.invoke(cli, ["vm", "ssh", "cvm-abc123"])
        assert result.exit_code != 0
        assert "no SSH endpoint yet" in result.output
        assert "PENDING" in result.output
