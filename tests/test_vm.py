"""Unit tests for the `avr vm` long-running VM commands."""

from avrea_cli.main import cli
import httpx
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

RD_VM = {**SAMPLE_VM, "enable_remote_desktop": True}
RD_CREATE_RESPONSE = {"data": {"vm": RD_VM, "password": "hunter2hunter2"}}
RD_ENDPOINTS = {
    "ssh": {"protocol": "ssh", "external_ip": "203.0.113.1", "external_port": 30022, "username": "runner"},
    "remote_desktop": {"protocol": "rdp", "external_ip": "203.0.113.1", "external_port": 33389, "username": "runner"},
}
RD_SHOW_RESPONSE = {"data": {**RD_VM, "state": "RUNNING", "endpoints": RD_ENDPOINTS}}
RD_ROTATE_RESPONSE = {
    "data": {"vm": {**RD_VM, "state": "RUNNING", "endpoints": RD_ENDPOINTS}, "password": "hunter2hunter2"}
}


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
        assert "memory_mb" not in body
        assert "disk_gb" not in body
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


class TestVmCreateWait:
    """`avr vm create --wait` polls until the VM is connectable, then prints a
    fully baked (real endpoints + password) connect command."""

    def _post(self, monkeypatch):
        monkeypatch.setattr("avrea_cli.vm.sys.platform", "linux")
        monkeypatch.setattr("avrea_cli.vm.time.sleep", lambda *_a: None)
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, json=None, timeout=None: RD_CREATE_RESPONSE,
        )

    _ARGS = ["vm", "create", "--name", "dev", "--os", "linux", "--size", "2-vcpu", "--remote-desktop", "--ephemeral"]

    def test_wait_success_bakes_full_paste_ready_command(self, runner, monkeypatch):
        self._post(monkeypatch)
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RD_SHOW_RESPONSE
        )
        result = runner.invoke(cli, [*self._ARGS, "--wait"])
        assert result.exit_code == 0
        # Real endpoint AND password, no placeholder = paste-and-go.
        assert (
            "xfreerdp /v:203.0.113.1:33389 /u:runner /p:hunter2hunter2 /gfx:rfx +clipboard /cert:tofu" in result.output
        )
        assert "IP:PORT" not in result.output
        assert "hunter2hunter2" in result.output

    def test_wait_polls_past_pending(self, runner, monkeypatch):
        self._post(monkeypatch)
        calls = {"n": 0}
        pending = {"data": {**RD_VM, "state": "PENDING", "endpoints": None}}

        def _get(self, path, params=None):
            calls["n"] += 1
            return RD_SHOW_RESPONSE if calls["n"] >= 2 else pending

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _get)
        result = runner.invoke(cli, [*self._ARGS, "--wait"])
        assert result.exit_code == 0
        assert calls["n"] >= 2  # kept polling past the first PENDING
        assert (
            "xfreerdp /v:203.0.113.1:33389 /u:runner /p:hunter2hunter2 /gfx:rfx +clipboard /cert:tofu" in result.output
        )

    def test_wait_timeout_still_surfaces_password(self, runner, monkeypatch):
        self._post(monkeypatch)
        pending = {"data": {**RD_VM, "state": "PENDING", "endpoints": None}}
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: pending)
        result = runner.invoke(cli, [*self._ARGS, "--wait", "--wait-timeout", "0"])
        assert result.exit_code == 1  # timeout exits nonzero so scripts can detect it
        assert "hunter2hunter2" in result.output  # password is never lost on timeout
        assert "IP:PORT" in result.output  # falls back to the placeholder command
        assert "avr vm show cvm-abc123" in result.output  # re-run hint

    def test_wait_recovers_from_transient_error(self, runner, monkeypatch):
        # A connection-level blip mid-poll must be retried, not crash the wait.
        self._post(monkeypatch)
        calls = {"n": 0}

        def _get(self, path, params=None):
            calls["n"] += 1
            if calls["n"] == 1:
                raise httpx.ConnectError("connection reset")
            return RD_SHOW_RESPONSE

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _get)
        result = runner.invoke(cli, [*self._ARGS, "--wait"])
        assert result.exit_code == 0
        assert calls["n"] >= 2  # retried after the transient error
        assert (
            "xfreerdp /v:203.0.113.1:33389 /u:runner /p:hunter2hunter2 /gfx:rfx +clipboard /cert:tofu" in result.output
        )

    def test_wait_transient_5xx_times_out_cleanly(self, runner, monkeypatch):
        # Repeated 5xx from the show endpoint must not abort early or traceback.
        self._post(monkeypatch)

        def _get(self, path, params=None):
            req = httpx.Request("GET", "http://x")
            raise httpx.HTTPStatusError("boom", request=req, response=httpx.Response(500, request=req))

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _get)
        result = runner.invoke(cli, [*self._ARGS, "--wait", "--wait-timeout", "0"])
        assert result.exit_code == 1  # timed out, exits nonzero
        assert not isinstance(result.exception, httpx.HTTPError)  # clean exit, no traceback
        assert "hunter2hunter2" in result.output  # password still surfaced

    def test_wait_permanent_4xx_surfaces_immediately(self, runner, monkeypatch):
        # A 4xx (auth / bad request) is permanent: surface it at once rather than
        # retrying to the timeout and hiding the actionable error.
        self._post(monkeypatch)
        calls = {"n": 0}

        def _get(self, path, params=None):
            calls["n"] += 1
            req = httpx.Request("GET", "http://x")
            raise httpx.HTTPStatusError("forbidden", request=req, response=httpx.Response(403, request=req))

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _get)
        result = runner.invoke(cli, [*self._ARGS, "--wait"])  # full default timeout
        assert result.exit_code != 0
        assert calls["n"] == 1  # surfaced on the first poll, no retry loop

    def test_wait_json_emits_single_final_document(self, runner, monkeypatch):
        self._post(monkeypatch)
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RD_SHOW_RESPONSE
        )
        result = runner.invoke(cli, [*self._ARGS, "--wait", "--json"])
        assert result.exit_code == 0
        # Progress is on stderr; the stdout payload is exactly one JSON document
        # (CliRunner concatenates both streams into .output).
        doc = json.loads(result.output[result.output.index("{") :])
        assert doc["password"] == "hunter2hunter2"
        assert doc["state"] == "RUNNING"
        assert doc["endpoints"]["remote_desktop"]["external_port"] == 33389  # waited for real endpoints

    def test_wait_failure_exits_nonzero_with_reason(self, runner, monkeypatch):
        self._post(monkeypatch)
        failed = {"data": {**RD_VM, "state": "ERROR", "state_reason": "node exploded", "endpoints": None}}
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: failed)
        result = runner.invoke(cli, [*self._ARGS, "--wait"])
        assert result.exit_code == 1
        assert "node exploded" in result.output
        assert "hunter2hunter2" in result.output  # password surfaced before the failure


class TestVmLifecycleWait:
    """--wait on start / stop / delete."""

    def test_start_wait_prints_full_connect(self, runner, monkeypatch):
        monkeypatch.setattr("avrea_cli.vm.sys.platform", "linux")
        monkeypatch.setattr("avrea_cli.vm.time.sleep", lambda *_a: None)
        patch_resp = {"data": {"vm": {**RD_VM, "state": "PENDING", "endpoints": None}, "password": "hunter2hunter2"}}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_patch",
            lambda self, path, json=None, params=None, timeout=None: patch_resp,
        )
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RD_SHOW_RESPONSE
        )
        result = runner.invoke(cli, ["vm", "start", "cvm-abc123", "--wait"])
        assert result.exit_code == 0
        # Same paste-ready payoff as create: real endpoint + the fresh password.
        assert (
            "xfreerdp /v:203.0.113.1:33389 /u:runner /p:hunter2hunter2 /gfx:rfx +clipboard /cert:tofu" in result.output
        )

    def test_stop_wait_blocks_until_stopped(self, runner, monkeypatch):
        monkeypatch.setattr("avrea_cli.vm.time.sleep", lambda *_a: None)
        patch_resp = {"data": {"vm": {**RD_VM, "state": "STOPPING", "endpoints": None}}}
        stopped = {"data": {**RD_VM, "state": "STOPPED", "desired_state": "STOPPED", "endpoints": None}}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_patch",
            lambda self, path, json=None, params=None, timeout=None: patch_resp,
        )
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: stopped)
        result = runner.invoke(cli, ["vm", "stop", "cvm-abc123", "--wait"])
        assert result.exit_code == 0
        assert "is now STOPPED" in result.output
        assert "IP:PORT" not in result.output  # concise confirmation, no connect-line noise
        assert "Re-run once ready" not in result.output  # target reached, no timeout note

    def test_stop_wait_timeout_does_not_claim_stopped(self, runner, monkeypatch):
        # On a timed-out stop, we must not print "is now STOPPED" (it is not).
        monkeypatch.setattr("avrea_cli.vm.time.sleep", lambda *_a: None)
        patch_resp = {"data": {"vm": {**RD_VM, "state": "STOPPING", "endpoints": None}}}
        stopping = {"data": {**RD_VM, "state": "STOPPING", "endpoints": None}}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_patch",
            lambda self, path, json=None, params=None, timeout=None: patch_resp,
        )
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: stopping)
        result = runner.invoke(cli, ["vm", "stop", "cvm-abc123", "--wait", "--wait-timeout", "0"])
        assert result.exit_code == 1
        assert "is now STOPPED" not in result.output  # never claim success on timeout
        assert "Not STOPPED yet" in result.output  # the timeout message instead

    def test_delete_wait_until_gone(self, runner, monkeypatch):
        monkeypatch.setattr("avrea_cli.vm.time.sleep", lambda *_a: None)
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_delete", lambda self, path: {"data": {"state": "DELETING"}}
        )

        def _gone(self, path, params=None):
            req = httpx.Request("GET", "http://x")
            raise httpx.HTTPStatusError("not found", request=req, response=httpx.Response(404, request=req))

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _gone)
        result = runner.invoke(cli, ["vm", "delete", "cvm-abc123", "--yes", "--wait"])
        assert result.exit_code == 0
        assert "deleted" in result.output.lower()


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


class TestVmConnectLine:
    """The ready-to-paste `Connect` line printed under `Remote desktop  yes`."""

    def _create(self, runner, monkeypatch, platform, response=RD_CREATE_RESPONSE):
        monkeypatch.setattr("avrea_cli.vm.sys.platform", platform)
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, json=None, timeout=None: response,
        )
        return runner.invoke(
            cli,
            ["vm", "create", "--name", "dev", "--os", "linux", "--size", "2-vcpu", "--remote-desktop", "--ephemeral"],
        )

    def _show(self, runner, monkeypatch, platform, response=RD_SHOW_RESPONSE):
        monkeypatch.setattr("avrea_cli.vm.sys.platform", platform)
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: response)
        return runner.invoke(cli, ["vm", "show", "cvm-abc123"])

    def test_create_linux_bakes_password_placeholder_and_rfx(self, runner, monkeypatch):
        result = self._create(runner, monkeypatch, "linux")
        assert result.exit_code == 0
        # Password baked in, IP:PORT placeholder, RemoteFX forced, cert auto-accept on the baked line.
        assert "xfreerdp /v:IP:PORT /u:USER /p:hunter2hunter2 /gfx:rfx +clipboard /cert:tofu" in result.output
        assert "appears in `avr vm show cvm-abc123`" in result.output

    def test_create_darwin_uri_quoted_and_scriptable_alt(self, runner, monkeypatch):
        result = self._create(runner, monkeypatch, "darwin")
        assert result.exit_code == 0
        # URI is quoted and the space stays percent-encoded as full%20address.
        assert 'open "rdp://full%20address=s:IP:PORT&username=s:USER"' in result.output
        # A scriptable FreeRDP line carries the password since the URI cannot.
        assert "sdl3-freerdp /v:IP:PORT /u:USER /p:hunter2hunter2 /gfx:rfx +clipboard" in result.output

    def test_create_windows_cmdkey_then_mstsc(self, runner, monkeypatch):
        result = self._create(runner, monkeypatch, "win32")
        assert result.exit_code == 0
        assert "cmdkey /generic:TERMSRV/IP /user:USER /pass:hunter2hunter2" in result.output
        assert "mstsc /v:IP:PORT" in result.output
        # The cleanup line must be pasteable on its own, with the note on a separate line.
        assert any(line.strip() == "cmdkey /delete:TERMSRV/IP" for line in result.output.splitlines())

    def test_create_without_remote_desktop_has_no_connect(self, runner, monkeypatch):
        result = self._create(runner, monkeypatch, "linux", response=CREATE_RESPONSE)
        assert result.exit_code == 0
        assert "Connect" not in result.output

    def test_show_real_endpoint_omits_password(self, runner, monkeypatch):
        result = self._show(runner, monkeypatch, "linux")
        assert result.exit_code == 0
        assert "xfreerdp /v:203.0.113.1:33389 /u:runner /gfx:rfx +clipboard" in result.output
        # Password is not retrievable at show time, and the cert flag rides only the baked line.
        assert "hunter2hunter2" not in result.output
        assert "/p:" not in result.output
        assert "/cert:tofu" not in result.output

    def test_windows_cmdkey_target_is_host_only(self, runner, monkeypatch):
        # Real endpoint + rotated password: the TERMSRV target must carry the host, never the port.
        monkeypatch.setattr("avrea_cli.vm.sys.platform", "win32")
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_patch",
            lambda self, path, json=None, params=None, timeout=None: RD_ROTATE_RESPONSE,
        )
        result = runner.invoke(cli, ["vm", "update", "cvm-abc123", "--rotate-password"])
        assert result.exit_code == 0
        assert "cmdkey /generic:TERMSRV/203.0.113.1 /user:runner /pass:hunter2hunter2" in result.output
        assert "mstsc /v:203.0.113.1:33389" in result.output
        assert "TERMSRV/203.0.113.1:33389" not in result.output  # port must not leak into the target

    def test_macos_guest_vnc_has_no_connect_line(self, runner, monkeypatch):
        vnc = {
            "data": {
                **RD_VM,
                "os_type": "macos",
                "state": "RUNNING",
                "endpoints": {
                    "remote_desktop": {
                        "protocol": "vnc",
                        "external_ip": "203.0.113.1",
                        "external_port": 35900,
                        "username": "runner",
                    }
                },
            }
        }
        result = self._show(runner, monkeypatch, "linux", response=vnc)
        assert result.exit_code == 0
        assert "Connect" not in result.output

    def test_show_darwin_offers_scriptable_freerdp_without_password(self, runner, monkeypatch):
        # Finding 3: the FreeRDP alternative (with /gfx:rfx) must appear at show time too,
        # not only when a password is baked in, or macOS users hit the black-screen trap.
        result = self._show(runner, monkeypatch, "darwin")
        assert result.exit_code == 0
        assert 'open "rdp://full%20address=s:203.0.113.1:33389&username=s:runner"' in result.output
        assert "sdl3-freerdp /v:203.0.113.1:33389 /u:runner /gfx:rfx +clipboard" in result.output
        assert "/p:" not in result.output  # no password at show time
        assert "hunter2hunter2" not in result.output

    def test_windows_guest_freerdp_omits_rfx(self, runner, monkeypatch):
        # A Windows guest is not GNOME Remote Desktop, so /gfx:rfx (a GRD-only paint
        # workaround) must not be forced; let FreeRDP negotiate normally.
        win_guest = {
            "data": {
                **RD_VM,
                "os_type": "windows",
                "state": "RUNNING",
                "endpoints": {
                    "remote_desktop": {
                        "protocol": "rdp",
                        "external_ip": "203.0.113.1",
                        "external_port": 33389,
                        "username": "Administrator",
                    }
                },
            }
        }
        result = self._show(runner, monkeypatch, "linux", response=win_guest)
        assert result.exit_code == 0
        assert "xfreerdp /v:203.0.113.1:33389 /u:Administrator +clipboard" in result.output
        assert "/gfx:rfx" not in result.output

    def test_running_without_rd_endpoint_keeps_placeholder_hint(self):
        # Finding 2: a bare IP:PORT placeholder always carries its "appears in show" hint,
        # even for a RUNNING VM whose remote_desktop endpoint is (defensively) absent.
        from avrea_cli.vm import _connect_block

        vm = {**RD_VM, "os_type": "linux", "state": "RUNNING", "endpoints": {"ssh": {"external_ip": "x"}}}
        lines = _connect_block(vm, None)
        assert any("IP:PORT" in ln for ln in lines)
        assert any("appears in `avr vm show" in ln for ln in lines)

    def test_posix_password_quoting(self):
        from avrea_cli.vm import _rdp_connect_lines

        assert "/p:'a b'" in _rdp_connect_lines("IP:PORT", "runner", "a b", "linux", True)[0]
        assert "/p:abc123" in _rdp_connect_lines("IP:PORT", "runner", "abc123", "linux", True)[0]
