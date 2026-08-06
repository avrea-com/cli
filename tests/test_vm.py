"""Unit tests for the `avr vm` long-running VM commands."""

from avrea_cli.main import cli
import httpx
import json
import os
import pytest
import re


class _FakeProc:
    """Minimal subprocess.Popen stand-in for _run_tunnel tests: ``exit_code``
    None means still running (until terminate()); an int means already exited."""

    def __init__(self, exit_code):
        self.returncode = exit_code
        self.terminated = False

    def poll(self):
        return self.returncode

    def wait(self, timeout=None):
        if self.returncode is None:
            self.returncode = 0
        return self.returncode

    def terminate(self):
        self.terminated = True
        if self.returncode is None:
            self.returncode = -15

    def kill(self):
        if self.returncode is None:
            self.returncode = -9


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
                "--repo",
                "owner/repo",
                "--ref",
                "main",
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
        # the optional precheckout repo/branch ride the body verbatim
        assert body["repo"] == "owner/repo"
        assert body["ref"] == "main"
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

    def test_create_surfaces_precheckout_note(self, runner, monkeypatch):
        note = "'owner/repo' is connected but not mirrored, so it will not be preloaded server-side"
        response = {"data": {"vm": SAMPLE_VM, "password": "hunter2hunter2", "precheckout_note": note}}
        store = {"return": response}
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
                "--ssh-key",
                "ssh-ed25519 AAAA test@host",
                "--repo",
                "owner/repo",
                "--ephemeral",
            ],
        )
        assert result.exit_code == 0
        assert "Note:" in result.output
        assert "not mirrored" in result.output

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

    def test_ref_rejects_non_branch(self, runner):
        result = runner.invoke(
            cli,
            [
                "vm",
                "create",
                "--name",
                "x",
                "--os",
                "linux",
                "--size",
                "2-vcpu",
                "--repo",
                "owner/repo",
                "--ref",
                "a" * 40,
                "--ephemeral",
            ],
        )
        assert result.exit_code != 0
        assert "not a branch name" in result.output

    def test_ref_without_repo_errors(self, runner, monkeypatch):
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
                "--ephemeral",
                "--ref",
                "main",
            ],
        )
        assert result.exit_code != 0
        assert "--ref requires --repo" in result.output
        assert called["n"] == 0  # never hit the API

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

    def test_ssh_key_rejects_private_key_file(self, runner, monkeypatch, tmp_path):
        # A `.pub` typo pointing at the private key must be caught locally, before
        # the private key is ever POSTed as a public key.
        called = {"n": 0}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, json=None, timeout=None: called.__setitem__("n", called["n"] + 1) or CREATE_RESPONSE,
        )
        key_file = tmp_path / "id_ed25519"
        key_file.write_text(
            "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXk=\n-----END OPENSSH PRIVATE KEY-----\n"
        )
        result = runner.invoke(
            cli,
            [
                "vm",
                "create",
                "--name",
                "d",
                "--os",
                "linux",
                "--size",
                "2-vcpu",
                "--ephemeral",
                "--ssh-key",
                f"@{key_file}",
            ],
        )
        assert result.exit_code != 0
        assert "private key" in result.output.lower()
        assert called["n"] == 0  # never uploaded

    def test_ssh_key_rejects_non_public_key(self, runner, monkeypatch):
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
                "d",
                "--os",
                "linux",
                "--size",
                "2-vcpu",
                "--ephemeral",
                "--ssh-key",
                "not a key",
            ],
        )
        assert result.exit_code != 0
        assert "not a recognized SSH public key" in result.output
        assert called["n"] == 0

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

    def test_disable_cache_maps_aliases_to_overrides(self, runner, monkeypatch):
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
                "--size",
                "2-vcpu",
                "--ephemeral",
                "--disable-cache",
                "gha,packages",
                "--disable-cache",
                "cache.nx.enabled",
            ],
        )
        assert result.exit_code == 0
        # Friendly aliases and a raw key both land as narrowing (False) overrides.
        assert store["json"]["cache_setting_overrides"] == {
            "cache.gha.enabled": False,
            "cache.packages.enabled": False,
            "cache.nx.enabled": False,
        }
        assert "Caches disabled: gha, nx, packages" in result.output

    def test_disable_cache_omitted_when_unset(self, runner, monkeypatch):
        store = {"return": CREATE_RESPONSE}
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", _capture(store))
        result = runner.invoke(
            cli, ["vm", "create", "--name", "dev", "--os", "linux", "--size", "2-vcpu", "--ephemeral"]
        )
        assert result.exit_code == 0
        # No key sent when the flag is unused, so the server keeps inherited defaults.
        assert "cache_setting_overrides" not in store["json"]

    def test_disable_cache_rejects_malformed_token(self, runner, monkeypatch):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", _capture({"return": CREATE_RESPONSE}))
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
                "--disable-cache",
                "not a cache",
            ],
        )
        assert result.exit_code != 0
        assert "is not a cache name" in result.output

    def test_invalid_size_rejected(self, runner):
        result = runner.invoke(
            cli,
            ["vm", "create", "--name", "dev", "--os", "linux", "--size", "3-vcpu", "--ephemeral"],
        )
        assert result.exit_code == 2
        assert "3-vcpu" in result.output

    def test_windows_remote_desktop_blocked(self, runner, monkeypatch):
        # Windows remote desktop is gated as "coming soon"; the CLI blocks it
        # before any API call. Remove with the vm.py block when it ships.
        called = {"n": 0}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_post",
            lambda self, path, json=None, timeout=None: called.__setitem__("n", called["n"] + 1) or CREATE_RESPONSE,
        )
        result = runner.invoke(
            cli,
            ["vm", "create", "--name", "w", "--os", "windows", "--size", "4-vcpu", "--ephemeral", "--remote-desktop"],
        )
        assert result.exit_code != 0
        assert "remote desktop" in result.output.lower()
        assert called["n"] == 0  # blocked before the API call

    def test_windows_without_remote_desktop_allowed(self, runner, monkeypatch):
        # The gate is scoped to --remote-desktop; a plain Windows VM still works.
        store = {"return": CREATE_RESPONSE}
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", _capture(store))
        result = runner.invoke(
            cli,
            ["vm", "create", "--name", "w", "--os", "windows", "--size", "4-vcpu", "--ephemeral"],
        )
        assert result.exit_code == 0
        assert store["json"]["enable_remote_desktop"] is False


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

    def test_wait_transient_429_is_retried(self, runner, monkeypatch):
        # 429 (rate limit) and 408 are transient during a poll: ride them out to
        # the deadline rather than aborting the wait the way a permanent 4xx does.
        self._post(monkeypatch)
        calls = {"n": 0}

        def _get(self, path, params=None):
            calls["n"] += 1
            if calls["n"] == 1:
                req = httpx.Request("GET", "http://x")
                raise httpx.HTTPStatusError("slow down", request=req, response=httpx.Response(429, request=req))
            return RD_SHOW_RESPONSE

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _get)
        result = runner.invoke(cli, [*self._ARGS, "--wait"])
        assert result.exit_code == 0
        assert calls["n"] >= 2  # retried past the 429 instead of aborting

    def test_wait_429_honors_retry_after(self, runner, monkeypatch):
        # A 429 with Retry-After should back off for that many seconds, not the
        # default poll cadence, so we don't hammer a rate-limiting server.
        self._post(monkeypatch)
        slept: list[float] = []
        monkeypatch.setattr("avrea_cli.vm.time.sleep", lambda s: slept.append(s))  # override _post's no-op
        calls = {"n": 0}

        def _get(self, path, params=None):
            calls["n"] += 1
            if calls["n"] == 1:
                req = httpx.Request("GET", "http://x")
                resp = httpx.Response(429, headers={"Retry-After": "7"}, request=req)
                raise httpx.HTTPStatusError("slow down", request=req, response=resp)
            return RD_SHOW_RESPONSE

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _get)
        result = runner.invoke(cli, [*self._ARGS, "--wait"])
        assert result.exit_code == 0
        assert 7 in slept  # backed off for the Retry-After value, not the default 3s

    def test_wait_429_retry_after_capped(self, runner, monkeypatch):
        # A large / hostile Retry-After is clamped to the cap, so a rate-limiting
        # (or malicious) server can't make --wait sleep arbitrarily long.
        from avrea_cli.vm import _WAIT_RETRY_AFTER_CAP

        self._post(monkeypatch)
        slept: list[float] = []
        monkeypatch.setattr("avrea_cli.vm.time.sleep", lambda s: slept.append(s))
        calls = {"n": 0}

        def _get(self, path, params=None):
            calls["n"] += 1
            if calls["n"] == 1:
                req = httpx.Request("GET", "http://x")
                resp = httpx.Response(429, headers={"Retry-After": "99999"}, request=req)
                raise httpx.HTTPStatusError("slow down", request=req, response=resp)
            return RD_SHOW_RESPONSE

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _get)
        result = runner.invoke(cli, [*self._ARGS, "--wait"])
        assert result.exit_code == 0
        assert slept == [_WAIT_RETRY_AFTER_CAP]  # clamped to the cap, not 99999

    def test_wait_transient_408_is_retried(self, runner, monkeypatch):
        # Sibling of the 429 case: 408 (Request Timeout) is transient too and must
        # be ridden out, guarding against the two codes diverging in the retry logic.
        self._post(monkeypatch)
        calls = {"n": 0}

        def _get(self, path, params=None):
            calls["n"] += 1
            if calls["n"] == 1:
                req = httpx.Request("GET", "http://x")
                raise httpx.HTTPStatusError("slow", request=req, response=httpx.Response(408, request=req))
            return RD_SHOW_RESPONSE

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _get)
        result = runner.invoke(cli, [*self._ARGS, "--wait"])
        assert result.exit_code == 0
        assert calls["n"] >= 2  # retried past the 408 instead of aborting

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

    def test_delete_wait_reports_failure_reason(self, runner, monkeypatch):
        # A VM that hits ERROR/FAILED mid-teardown must surface its reason and exit
        # nonzero, not be mislabeled "Still deleting" (which discards state_reason).
        monkeypatch.setattr("avrea_cli.vm.time.sleep", lambda *_a: None)
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_delete", lambda self, path: {"data": {"state": "DELETING"}}
        )
        failed = {"data": {**SAMPLE_VM, "state": "FAILED", "state_reason": "node lost"}}
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: failed)
        result = runner.invoke(cli, ["vm", "delete", "cvm-abc123", "--yes", "--wait"])
        assert result.exit_code == 1
        assert "FAILED" in result.output
        assert "node lost" in result.output
        assert "Still deleting" not in result.output

    def test_delete_wait_json_carries_disposition(self, runner, monkeypatch):
        # --json must distinguish failed/timeout and carry state_reason, not just
        # a bare deleted flag, so scripts don't lose the failure detail.
        monkeypatch.setattr("avrea_cli.vm.time.sleep", lambda *_a: None)
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_delete", lambda self, path: {"data": {"state": "DELETING"}}
        )
        failed = {"data": {**SAMPLE_VM, "state": "FAILED", "state_reason": "node lost"}}
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: failed)
        result = runner.invoke(cli, ["vm", "delete", "cvm-abc123", "--yes", "--wait", "--json"])
        assert result.exit_code == 1
        doc = json.loads(result.output[result.output.index("{") :])
        assert doc["deleted"] is False
        assert doc["disposition"] == "failed"
        assert doc["state_reason"] == "node lost"


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
        # Word-boundary match, not a bare `"host" in text` substring check: the
        # latter trips CodeQL's incomplete-URL-sanitization rule (false positive
        # on rendered CLI output, not URL validation).
        assert re.search(r"\bgithub\.com\b", result.output)

    def test_shows_preload_when_precheckout_configured(self, runner, monkeypatch):
        detail = {"data": {**SAMPLE_VM, "egress_rules": [], "precheckout_ref": "main", "preload_status": "preloaded"}}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: detail,
        )
        result = runner.invoke(cli, ["vm", "show", "cvm-abc123"])
        assert result.exit_code == 0
        assert "Preload" in result.output
        assert "main (preloaded)" in result.output

    def test_no_preload_row_without_precheckout(self, runner, monkeypatch):
        detail = {"data": {**SAMPLE_VM, "egress_rules": []}}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: detail,
        )
        result = runner.invoke(cli, ["vm", "show", "cvm-abc123"])
        assert result.exit_code == 0
        assert "Preload" not in result.output


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

    def test_delete_handles_null_data(self, runner, monkeypatch):
        # A 200 body with data: null must not AttributeError; it falls back to DELETING.
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_delete",
            lambda self, path, params=None: {"data": None},
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
    def test_print_emits_command_without_running(self, runner, monkeypatch):
        called = {"n": 0}
        monkeypatch.setattr(
            "avrea_cli.vm.subprocess.run",
            lambda *a, **k: called.__setitem__("n", called["n"] + 1),
        )
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: RUNNING_VM,
        )
        result = runner.invoke(cli, ["vm", "ssh", "cvm-abc123", "--print"])
        assert result.exit_code == 0
        # printed form uses accept-new since it can't reference the temp known_hosts
        assert result.output.strip() == "ssh -p 30022 -o StrictHostKeyChecking=accept-new runner@203.0.113.1"
        assert called["n"] == 0  # never ran ssh

    def test_runs_remote_command_after_destination_with_pinned_host_key(self, runner, monkeypatch):
        from pathlib import Path
        from types import SimpleNamespace

        captured = {}
        monkeypatch.setattr("avrea_cli.vm._write_known_hosts", lambda *a, **k: Path("/tmp/kh"))
        monkeypatch.setattr(
            "avrea_cli.vm.subprocess.run",
            lambda argv, **k: captured.update(argv=argv) or SimpleNamespace(returncode=0),
        )
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: RUNNING_VM,
        )
        result = runner.invoke(cli, ["vm", "ssh", "cvm-abc123", "-i", "/tmp/key", "--", "uname", "-a"])
        assert result.exit_code == 0
        # host key pinned; the remote command lands after the destination
        assert captured["argv"] == [
            "ssh",
            "-i",
            "/tmp/key",
            "-p",
            "30022",
            "-o",
            f"UserKnownHostsFile={Path('/tmp/kh')}",
            "-o",
            "StrictHostKeyChecking=yes",
            "runner@203.0.113.1",
            "uname",
            "-a",
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

    def test_warns_when_endpoint_has_no_host_key(self, runner, monkeypatch):
        # An endpoint without a host key can't be pinned, so ssh accepts on first
        # use. That fallback must be surfaced, not silent (parity with the tunnel).
        from types import SimpleNamespace

        keyless = {
            "data": {
                **SAMPLE_VM,
                "state": "RUNNING",
                "endpoints": {
                    "ssh": {
                        "protocol": "ssh",
                        "external_ip": "203.0.113.1",
                        "external_port": 30022,
                        "username": "runner",
                    }
                },
            }
        }
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: keyless)
        monkeypatch.setattr("avrea_cli.vm.subprocess.run", lambda *a, **k: SimpleNamespace(returncode=0))
        result = runner.invoke(cli, ["vm", "ssh", "cvm-abc123"])
        assert result.exit_code == 0
        assert "no SSH host key" in result.output

    def test_session_wraps_remote_command_in_tmux_with_tty(self, runner, monkeypatch):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        result = runner.invoke(cli, ["vm", "ssh", "cvm-abc123", "--session", "dev", "--print"])
        assert result.exit_code == 0
        # -t forces a tty (a remote command otherwise gets none); tmux attaches-or-creates.
        assert result.output.strip() == (
            "ssh -p 30022 -o StrictHostKeyChecking=accept-new -t runner@203.0.113.1 tmux new-session -A -s dev"
        )

    def test_session_runs_given_command_inside_the_session(self, runner, monkeypatch):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        result = runner.invoke(cli, ["vm", "ssh", "cvm-abc123", "--session", "build", "--print", "--", "make"])
        assert result.exit_code == 0
        assert result.output.strip().endswith("tmux new-session -A -s build make")

    def test_session_name_validated(self, runner, monkeypatch):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        result = runner.invoke(cli, ["vm", "ssh", "cvm-abc123", "--session", "bad name", "--print"])
        assert result.exit_code != 0
        assert "not a valid session name" in result.output

    def test_login_wraps_remote_command_for_login_shell(self, runner, monkeypatch):
        import shlex

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        result = runner.invoke(cli, ["vm", "ssh", "cvm-abc123", "--login", "--print", "--", "claude", "-p", "reply OK"])
        assert result.exit_code == 0
        # The command is one correctly-quoted `bash -lc` argument, so the ssh
        # flatten + remote login-shell re-parse reconstruct the exact args (the
        # prompt keeps its spaces) — proving the quoting survives both layers.
        tokens = shlex.split(result.output.strip())
        assert tokens[0] == "ssh"
        wrapped = tokens[-1]
        assert wrapped.startswith("bash -lc ")
        assert shlex.split(shlex.split(wrapped)[2]) == ["claude", "-p", "reply OK"]

    def test_without_login_remote_command_is_raw(self, runner, monkeypatch):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        result = runner.invoke(cli, ["vm", "ssh", "cvm-abc123", "--print", "--", "claude", "-p", "reply OK"])
        assert result.exit_code == 0
        # Default stays raw passthrough (no login shell) — Windows-safe, unchanged.
        assert "bash -lc" not in result.output
        assert result.output.strip().endswith("claude -p 'reply OK'")

    def test_session_and_login_wraps_command_in_login_shell_inside_tmux(self, runner, monkeypatch):
        import shlex

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        result = runner.invoke(
            cli,
            ["vm", "ssh", "cvm-abc123", "--session", "dev", "--login", "--print", "--", "claude", "-p", "reply OK"],
        )
        assert result.exit_code == 0
        # --login is honored alongside --session (not silently dropped): tmux
        # creates/attaches the session and runs the command through a login shell,
        # the nested quoting surviving both the ssh flatten and the re-parse.
        tokens = shlex.split(result.output.strip())
        assert tokens[-6:-1] == ["tmux", "new-session", "-A", "-s", "dev"]
        wrapped = tokens[-1]
        assert wrapped.startswith("bash -lc ")
        assert shlex.split(shlex.split(wrapped)[2]) == ["claude", "-p", "reply OK"]


class TestVmSshConfig:
    """`avr vm ssh-config` emits an ssh_config Host block and pins the host key."""

    def test_block_pins_host_key_and_writes_known_hosts(self, runner, monkeypatch, tmp_path):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        kh = tmp_path / "avr_known_hosts"
        result = runner.invoke(cli, ["vm", "ssh-config", "cvm-abc123", "--known-hosts-file", str(kh)])
        assert result.exit_code == 0
        assert "Host avr-cvm-abc123" in result.output
        assert "HostName 203.0.113.1" in result.output
        assert "Port 30022" in result.output
        assert "User runner" in result.output
        assert f"UserKnownHostsFile {kh}" in result.output
        assert "StrictHostKeyChecking yes" in result.output
        # The pinned line lands in the referenced file, keyed by [ip]:port.
        assert kh.read_text() == "[203.0.113.1]:30022 ssh-ed25519 AAAAkey\n"

    def test_custom_alias_and_identity(self, runner, monkeypatch, tmp_path):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        result = runner.invoke(
            cli,
            [
                "vm",
                "ssh-config",
                "cvm-abc123",
                "--host-alias",
                "mybox",
                "-i",
                "/tmp/key",
                "--known-hosts-file",
                str(tmp_path / "kh"),
            ],
        )
        assert result.exit_code == 0
        assert "Host mybox" in result.output
        assert "IdentityFile /tmp/key" in result.output

    def test_accept_new_and_warns_without_host_key(self, runner, monkeypatch):
        keyless = {
            "data": {
                **SAMPLE_VM,
                "state": "RUNNING",
                "endpoints": {
                    "ssh": {
                        "protocol": "ssh",
                        "external_ip": "203.0.113.1",
                        "external_port": 30022,
                        "username": "runner",
                    }
                },
            }
        }
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: keyless)
        result = runner.invoke(cli, ["vm", "ssh-config", "cvm-abc123"])
        assert result.exit_code == 0
        assert "StrictHostKeyChecking accept-new" in result.output
        assert "no SSH host key" in result.output

    def test_no_endpoint_yet_errors(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: {"data": {**SAMPLE_VM, "state": "PENDING", "endpoints": None}},
        )
        result = runner.invoke(cli, ["vm", "ssh-config", "cvm-abc123"])
        assert result.exit_code != 0
        assert "no SSH endpoint yet" in result.output

    def test_atomic_write_creates_parent_dir_0700(self, tmp_path):
        from avrea_cli.vm import _atomic_write_text

        target = tmp_path / "new_ssh_dir" / "config"
        _atomic_write_text(target, "Host x\n")
        assert target.read_text() == "Host x\n"
        if os.name == "posix":  # POSIX mode bits not enforced on Windows
            assert oct(target.parent.stat().st_mode & 0o777) == "0o700"
            assert oct(target.stat().st_mode & 0o777) == "0o600"

    def test_upsert_known_hosts_replaces_only_this_host(self, tmp_path):
        from avrea_cli.vm import _upsert_known_hosts

        kh = tmp_path / "kh"
        # No trailing newline on the last kept entry: the new pin must not merge
        # onto the same line as the preserved one.
        kh.write_text("[203.0.113.1]:30022 ssh-ed25519 OLD\n[other]:22 ssh-ed25519 KEEP")
        _upsert_known_hosts(kh, "ssh-ed25519 NEW", "203.0.113.1", 30022)
        lines = kh.read_text().splitlines()
        assert "[other]:22 ssh-ed25519 KEEP" in lines  # preserved as its own line
        assert "[203.0.113.1]:30022 ssh-ed25519 NEW" in lines  # new pin on its own line
        assert "[203.0.113.1]:30022 ssh-ed25519 OLD" not in lines
        assert not any("KEEP" in ln and "NEW" in ln for ln in lines)  # entries not merged

    def test_append_writes_block_and_preserves_config(self, runner, monkeypatch, tmp_path):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        cfg = tmp_path / "config"
        cfg.write_text("Host existing\n    HostName keep.me\n")
        result = runner.invoke(
            cli,
            [
                "vm",
                "ssh-config",
                "cvm-abc123",
                "--append",
                "--config-file",
                str(cfg),
                "--known-hosts-file",
                str(tmp_path / "kh"),
            ],
        )
        assert result.exit_code == 0
        assert "Added host avr-cvm-abc123" in result.output
        text = cfg.read_text()
        assert "Host existing" in text and "keep.me" in text  # hand-written config preserved
        assert "Host avr-cvm-abc123" in text and "203.0.113.1" in text
        assert "# >>> avrea vm avr-cvm-abc123 >>>" in text  # marker-delimited for idempotent updates
        if os.name == "posix":  # POSIX mode bits not enforced on Windows
            assert oct(cfg.stat().st_mode & 0o777) == "0o600"

    def test_append_replaces_prior_block_in_place(self, runner, monkeypatch, tmp_path):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        cfg, kh = tmp_path / "config", tmp_path / "kh"
        args = ["vm", "ssh-config", "cvm-abc123", "--append", "--config-file", str(cfg), "--known-hosts-file", str(kh)]
        assert runner.invoke(cli, args).exit_code == 0
        again = runner.invoke(cli, args)  # a restart re-run must update, not duplicate
        assert again.exit_code == 0
        assert "Updated host avr-cvm-abc123" in again.output
        assert cfg.read_text().count("Host avr-cvm-abc123") == 1

    def test_upsert_ssh_config_block_rejects_unterminated_block(self, tmp_path):
        from avrea_cli.vm import _upsert_ssh_config_block
        import click

        cfg = tmp_path / "config"
        # A begin marker whose end marker is missing (hand-edited or a partial
        # leftover). Writing would delete everything after it, so refuse instead.
        original = "Host keep\n    HostName keep.me\n\n# >>> avrea vm avr-x >>>\nHost avr-x\n    HostName 1.1.1.1\n"
        cfg.write_text(original)
        with pytest.raises(click.ClickException, match="unterminated"):
            _upsert_ssh_config_block(cfg, "avr-x", "Host avr-x\n    HostName 2.2.2.2\n")
        assert cfg.read_text() == original  # left untouched, no data loss

    def test_ssh_config_block_rejects_newline_in_endpoint_fields(self):
        from avrea_cli.vm import _ssh_config_block
        import click

        # A newline in an endpoint value would inject arbitrary ssh_config
        # directives (e.g. ProxyCommand) into the user's config.
        evil_ip = {"external_ip": "1.2.3.4\n    ProxyCommand evil", "external_port": 22, "username": "runner"}
        with pytest.raises(click.ClickException, match="embedded newline"):
            _ssh_config_block("h", evil_ip, identity_file=None, known_hosts_path=None)
        evil_user = {"external_ip": "1.2.3.4", "external_port": 22, "username": "runner\n    ProxyCommand evil"}
        with pytest.raises(click.ClickException, match="embedded newline"):
            _ssh_config_block("h", evil_user, identity_file=None, known_hosts_path=None)
        # The port is numeric; a non-integer (e.g. one carrying an injected
        # directive) is rejected rather than interpolated into the Port line.
        evil_port = {"external_ip": "1.2.3.4", "external_port": "22\n    ProxyCommand evil", "username": "runner"}
        with pytest.raises(click.ClickException, match="invalid SSH port"):
            _ssh_config_block("h", evil_port, identity_file=None, known_hosts_path=None)

    def test_ssh_config_path_arg_quotes_and_rejects_unsafe(self):
        from avrea_cli.vm import _ssh_config_path_arg
        import click

        assert _ssh_config_path_arg("/home/u/.ssh/id", "IdentityFile") == "/home/u/.ssh/id"
        # A path with a space must be double-quoted or ssh_config splits on it.
        assert _ssh_config_path_arg("/home/u/my keys/id", "IdentityFile") == '"/home/u/my keys/id"'
        # A newline would inject its own directives; ssh_config cannot escape a
        # literal double quote inside a quoted argument. Reject both.
        with pytest.raises(click.ClickException, match="embedded newline"):
            _ssh_config_path_arg("/home/u/id\n    ProxyCommand evil", "IdentityFile")
        with pytest.raises(click.ClickException, match="double quote"):
            _ssh_config_path_arg('/home/u/a "b"/id', "UserKnownHostsFile")

    def test_append_warns_on_earlier_catchall_override(self, runner, monkeypatch, tmp_path):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        cfg, kh = tmp_path / "config", tmp_path / "kh"
        # A pre-existing catch-all block sets pinning knobs that ssh applies
        # first, so they win over the block we append at the end of the file.
        cfg.write_text("Host *\n    StrictHostKeyChecking no\n    UserKnownHostsFile ~/.ssh/known_hosts\n")
        args = ["vm", "ssh-config", "cvm-abc123", "--append", "--config-file", str(cfg), "--known-hosts-file", str(kh)]
        result = runner.invoke(cli, args)
        assert result.exit_code == 0
        assert "Added host avr-cvm-abc123" in result.output
        assert "earlier 'Host *' block" in result.output
        assert "StrictHostKeyChecking" in result.output and "UserKnownHostsFile" in result.output
        # No spurious warning when no catch-all precedes the block.
        cfg.write_text("Host keep\n    HostName keep.me\n")
        again = runner.invoke(cli, args)
        assert again.exit_code == 0
        assert "earlier 'Host *' block" not in again.output

    def test_config_file_requires_append(self, runner, monkeypatch, tmp_path):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        result = runner.invoke(cli, ["vm", "ssh-config", "cvm-abc123", "--config-file", str(tmp_path / "c")])
        assert result.exit_code != 0
        assert "--config-file requires --append" in result.output


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


class TestVmDesktopTunnel:
    """`avr vm rdp` / `avr vm vnc` / `avr vm port-forward` (SSH-tunnelled)."""

    def test_rdp_print_emits_tunnel_and_client(self, runner, monkeypatch):
        popen_calls = {"n": 0}
        monkeypatch.setattr("avrea_cli.vm.sys.platform", "linux")
        monkeypatch.setattr(
            "avrea_cli.vm.subprocess.Popen",
            lambda *a, **k: popen_calls.__setitem__("n", popen_calls["n"] + 1),
        )
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RD_SHOW_RESPONSE
        )
        result = runner.invoke(cli, ["vm", "rdp", "cvm-abc123", "--print", "--local-port", "40000"])
        assert result.exit_code == 0
        # the forward targets the guest's RDP port on its own loopback
        assert "-L 127.0.0.1:40000:localhost:3389" in result.output
        assert "-p 30022" in result.output
        assert "runner@203.0.113.1" in result.output
        assert "StrictHostKeyChecking=accept-new" in result.output
        # Linux GRD needs /gfx:rfx; the client line reuses the RDP connect helper
        assert "xfreerdp" in result.output
        assert "/gfx:rfx" in result.output
        # the tunnel is SSH-secured, so the localhost RDP cert is ignored rather
        # than tripping a name-mismatch / host-changed prompt on 127.0.0.1
        assert "/cert:ignore" in result.output
        # the printed form can't embed the pinned temp known_hosts, so it notes the gap
        assert "trust-on-first-use" in result.output
        assert popen_calls["n"] == 0  # --print never opens the tunnel

    def test_rdp_opens_tunnel_with_derived_port(self, runner, monkeypatch):
        captured = {}
        monkeypatch.setattr("avrea_cli.vm.sys.platform", "linux")
        monkeypatch.setattr(
            "avrea_cli.vm._run_tunnel",
            lambda ssh_ep, **kw: captured.update(ssh_ep=ssh_ep, **kw),
        )
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RD_SHOW_RESPONSE
        )
        result = runner.invoke(cli, ["vm", "rdp", "cvm-abc123", "--local-port", "40000"])
        assert result.exit_code == 0
        assert captured["forwards"] == [(40000, 3389)]
        assert captured["ssh_ep"]["external_port"] == 30022

    def test_rdp_requires_remote_desktop(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: {"data": {**SAMPLE_VM, "state": "RUNNING"}},
        )
        result = runner.invoke(cli, ["vm", "rdp", "cvm-abc123"])
        assert result.exit_code != 0
        assert "no remote desktop" in result.output

    def test_rdp_no_ssh_endpoint_errors(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: {"data": {**RD_VM, "state": "PENDING", "endpoints": None}},
        )
        result = runner.invoke(cli, ["vm", "rdp", "cvm-abc123"])
        assert result.exit_code != 0
        assert "no SSH endpoint yet" in result.output
        assert "PENDING" in result.output

    def test_vnc_on_rdp_vm_points_to_rdp(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RD_SHOW_RESPONSE
        )
        result = runner.invoke(cli, ["vm", "vnc", "cvm-abc123"])
        assert result.exit_code != 0
        assert "speaks rdp, not vnc" in result.output
        assert "avr vm rdp cvm-abc123" in result.output

    def test_port_forward_print(self, runner, monkeypatch):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        result = runner.invoke(
            cli, ["vm", "port-forward", "cvm-abc123", "--port", "8080", "--local-port", "41000", "--print"]
        )
        assert result.exit_code == 0
        assert "-L 127.0.0.1:41000:localhost:8080" in result.output
        assert "trust-on-first-use" in result.output
        assert "runner@203.0.113.1" in result.output

    def test_port_forward_no_ssh_endpoint_errors(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: {"data": {**SAMPLE_VM, "state": "PENDING", "endpoints": None}},
        )
        result = runner.invoke(cli, ["vm", "port-forward", "cvm-abc123", "--port", "8080"])
        assert result.exit_code != 0
        assert "no SSH endpoint yet" in result.output

    def test_port_forward_multiple_ports_one_ssh_process(self, runner, monkeypatch):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        # Pin the auto-picked local port so the assertion below is deterministic
        # (a real _pick_local_port could, on some OS, hand back 5432 itself).
        monkeypatch.setattr("avrea_cli.vm._pick_local_port", lambda: 41000)
        result = runner.invoke(
            cli,
            ["vm", "port-forward", "cvm-abc123", "--port", "9000:3000", "--port", "5432", "--print"],
        )
        assert result.exit_code == 0
        # Explicit local bind honored, and every forward rides one ssh invocation.
        assert "-L 127.0.0.1:9000:localhost:3000" in result.output
        assert "-L 127.0.0.1:41000:localhost:5432" in result.output  # 5432 gets an auto-picked local port
        assert result.output.count("ssh -N") == 1

    def test_port_forward_rejects_duplicate_guest(self, runner, monkeypatch):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        result = runner.invoke(
            cli, ["vm", "port-forward", "cvm-abc123", "--port", "8080", "--port", "9000:8080", "--print"]
        )
        assert result.exit_code != 0
        assert "forwarded more than once" in result.output

    def test_port_forward_local_port_with_multiple_ports_errors(self, runner, monkeypatch):
        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", lambda self, path, params=None: RUNNING_VM)
        result = runner.invoke(
            cli, ["vm", "port-forward", "cvm-abc123", "--port", "8080", "--port", "5432", "--local-port", "41000"]
        )
        assert result.exit_code != 0
        assert "single bare --port" in result.output


class TestTunnelHelpers:
    """Pure helpers behind the tunnel commands."""

    def test_ssh_tunnel_argv_pins_host_key(self):
        from avrea_cli.vm import _ssh_tunnel_argv
        from pathlib import Path

        ep = {"external_ip": "203.0.113.1", "external_port": 30022, "username": "runner"}
        kh = Path("/tmp/kh")
        argv = _ssh_tunnel_argv(ep, forwards=[(40000, 3389)], identity_file=None, known_hosts=kh)
        # Render the expected value through Path so the assertion matches the code
        # on Windows too (where Path stringifies with backslashes).
        assert f"UserKnownHostsFile={kh}" in argv
        assert "StrictHostKeyChecking=yes" in argv
        assert "-L" in argv and "127.0.0.1:40000:localhost:3389" in argv
        assert argv[-1] == "runner@203.0.113.1"

    def test_ssh_tunnel_argv_accept_new_without_key(self):
        from avrea_cli.vm import _ssh_tunnel_argv

        ep = {"external_ip": "203.0.113.1", "external_port": 30022, "username": "runner"}
        argv = _ssh_tunnel_argv(ep, forwards=[(40000, 5900)], identity_file="/tmp/key", known_hosts=None)
        assert "StrictHostKeyChecking=accept-new" in argv
        assert "-i" in argv and "/tmp/key" in argv

    def test_known_hosts_line_brackets_nonstandard_port(self):
        from avrea_cli.vm import _known_hosts_line

        assert _known_hosts_line("ssh-ed25519 AAAA", "1.2.3.4", 30022) == "[1.2.3.4]:30022 ssh-ed25519 AAAA\n"
        assert _known_hosts_line("ssh-ed25519 AAAA", "1.2.3.4", 22) == "1.2.3.4 ssh-ed25519 AAAA\n"

    def test_parse_forward_spec_bare_and_paired(self):
        from avrea_cli.vm import _parse_forward_spec

        assert _parse_forward_spec("8080") == (None, 8080)
        assert _parse_forward_spec("9000:3000") == (9000, 3000)

    def test_parse_forward_spec_rejects_bad_values(self):
        from avrea_cli.vm import _parse_forward_spec
        import click
        import pytest

        with pytest.raises(click.UsageError):
            _parse_forward_spec("nope")
        with pytest.raises(click.UsageError):
            _parse_forward_spec("70000")  # out of range

    def test_build_forwards_auto_picks_distinct_local_ports(self, monkeypatch):
        from avrea_cli import vm as vmmod
        from itertools import count

        picks = count(41000)
        monkeypatch.setattr(vmmod, "_pick_local_port", lambda: next(picks))
        forwards = vmmod._build_forwards(("8080", "5432"), None)
        assert forwards == [(41000, 8080), (41001, 5432)]

    def test_build_forwards_rejects_duplicate_local_bind(self):
        from avrea_cli.vm import _build_forwards
        import click
        import pytest

        with pytest.raises(click.UsageError):
            _build_forwards(("40000:8080", "40000:9090"), None)

    def test_build_forwards_autopick_avoids_later_explicit_local(self, monkeypatch):
        # An auto-pick that lands on a port the user explicitly named on a later
        # spec must not be reported as a duplicate bind: explicit locals are
        # reserved before any auto-pick runs, so the auto-pick skips 9000.
        from avrea_cli import vm as vmmod
        from itertools import count

        picks = count(9000)  # first auto-pick would collide with the explicit 9000
        monkeypatch.setattr(vmmod, "_pick_local_port", lambda: next(picks))
        forwards = vmmod._build_forwards(("8080", "9000:3000"), None)
        assert forwards == [(9001, 8080), (9000, 3000)]

    def test_ssh_connect_argv_pins_host_key(self):
        from avrea_cli.vm import _ssh_connect_argv
        from pathlib import Path

        ep = {"external_ip": "203.0.113.1", "external_port": 30022, "username": "runner"}
        kh = Path("/tmp/kh")
        argv = _ssh_connect_argv(ep, identity_file="/tmp/key", known_hosts=kh)
        assert argv == [
            "ssh",
            "-i",
            "/tmp/key",
            "-p",
            "30022",
            "-o",
            f"UserKnownHostsFile={kh}",
            "-o",
            "StrictHostKeyChecking=yes",
            "runner@203.0.113.1",
        ]

    def test_ssh_connect_argv_accept_new_without_key(self):
        from avrea_cli.vm import _ssh_connect_argv

        ep = {"external_ip": "203.0.113.1", "external_port": 30022, "username": "runner"}
        argv = _ssh_connect_argv(ep, identity_file=None, known_hosts=None)
        assert argv == ["ssh", "-p", "30022", "-o", "StrictHostKeyChecking=accept-new", "runner@203.0.113.1"]

    def test_run_ssh_places_command_after_destination_and_feeds_stdin(self, monkeypatch):
        from avrea_cli.vm import run_ssh
        from pathlib import Path
        from types import SimpleNamespace

        captured = {}
        monkeypatch.setattr("avrea_cli.vm._write_known_hosts", lambda *a, **k: Path("/tmp/kh"))

        def fake_run(argv, **kwargs):
            captured["argv"] = argv
            captured["input"] = kwargs.get("input")
            return SimpleNamespace(returncode=0, stdout="ok", stderr="")

        monkeypatch.setattr("avrea_cli.vm.subprocess.run", fake_run)

        ep = {
            "external_ip": "203.0.113.1",
            "external_port": 30022,
            "username": "runner",
            "host_key": "ssh-ed25519 AAAA",
        }
        result = run_ssh(ep, ["bash", "-s"], stdin_data="echo hi")
        assert result.returncode == 0
        assert captured["input"] == "echo hi"  # secrets ride stdin, not argv
        assert captured["argv"] == [
            "ssh",
            "-p",
            "30022",
            "-o",
            f"UserKnownHostsFile={Path('/tmp/kh')}",
            "-o",
            "StrictHostKeyChecking=yes",
            "-o",
            "BatchMode=yes",
            "-o",
            "ConnectTimeout=15",
            "runner@203.0.113.1",
            "bash",
            "-s",
        ]

    def test_rdp_launch_argv_per_platform(self):
        from avrea_cli.vm import _rdp_launch_argv

        argv, detaches = _rdp_launch_argv("127.0.0.1:40000", "runner", "linux", True)
        assert argv is not None
        assert argv[0] == "xfreerdp" and "/gfx:rfx" in argv and detaches is False
        assert "/cert:ignore" in argv
        argv, detaches = _rdp_launch_argv("127.0.0.1:40000", "runner", "win32", False)
        assert argv == ["mstsc", "/v:127.0.0.1:40000"] and detaches is False
        argv, detaches = _rdp_launch_argv("127.0.0.1:40000", "runner", "darwin", False)
        assert argv is not None
        assert argv[0] == "open" and detaches is True

    def test_vnc_launch_argv_macos_only(self):
        from avrea_cli.vm import _vnc_launch_argv

        argv, detaches = _vnc_launch_argv("127.0.0.1:40000", "darwin")
        assert argv == ["open", "vnc://127.0.0.1:40000"] and detaches is True
        argv, detaches = _vnc_launch_argv("127.0.0.1:40000", "linux")
        assert argv is None

    def test_run_tunnel_propagates_ssh_failure(self, monkeypatch):
        # ssh dies with 255 after the tunnel came up -> nonzero exit, not a clean 0.
        from avrea_cli import vm as vmmod
        import click
        import pytest

        proc = _FakeProc(255)
        monkeypatch.setattr("avrea_cli.vm.subprocess.Popen", lambda *a, **k: proc)
        monkeypatch.setattr("avrea_cli.vm._wait_until_listening", lambda *a, **k: True)
        monkeypatch.setattr("avrea_cli.vm._write_known_hosts", lambda *a, **k: None)
        ep = {"external_ip": "203.0.113.1", "external_port": 30022, "username": "runner"}
        with pytest.raises(click.ClickException):
            vmmod._run_tunnel(ep, forwards=[(40000, 22)], identity_file=None, on_ready=lambda p: p.wait())

    def test_run_tunnel_clean_on_ctrl_c(self, monkeypatch):
        # Ctrl-C during the hold is an intentional teardown: no error surfaced.
        from avrea_cli import vm as vmmod

        proc = _FakeProc(None)
        monkeypatch.setattr("avrea_cli.vm.subprocess.Popen", lambda *a, **k: proc)
        monkeypatch.setattr("avrea_cli.vm._wait_until_listening", lambda *a, **k: True)
        monkeypatch.setattr("avrea_cli.vm._write_known_hosts", lambda *a, **k: None)
        ep = {"external_ip": "203.0.113.1", "external_port": 30022, "username": "runner"}

        def _interrupt(_p):
            raise KeyboardInterrupt

        vmmod._run_tunnel(ep, forwards=[(40000, 22)], identity_file=None, on_ready=_interrupt)
        assert proc.terminated  # torn down cleanly, no exception raised

    def test_run_tunnel_clean_on_client_teardown(self, monkeypatch):
        # --launch: the client exits with ssh still running; we terminate it and
        # exit clean (ssh never returned a failure code of its own).
        from avrea_cli import vm as vmmod

        proc = _FakeProc(None)
        monkeypatch.setattr("avrea_cli.vm.subprocess.Popen", lambda *a, **k: proc)
        monkeypatch.setattr("avrea_cli.vm._wait_until_listening", lambda *a, **k: True)
        monkeypatch.setattr("avrea_cli.vm._write_known_hosts", lambda *a, **k: None)
        ep = {"external_ip": "203.0.113.1", "external_port": 30022, "username": "runner"}
        vmmod._run_tunnel(ep, forwards=[(40000, 22)], identity_file=None, on_ready=lambda p: None)
        assert proc.terminated

    def test_run_tunnel_warns_when_no_host_key(self, monkeypatch):
        # When the endpoint carries no host key, the tunnel can't pin and falls back
        # to TOFU — that must be surfaced, not silent.
        from avrea_cli import vm as vmmod

        msgs: list[str] = []
        monkeypatch.setattr("avrea_cli.vm.click.echo", lambda *a, **k: msgs.append(a[0] if a else ""))
        proc = _FakeProc(None)
        monkeypatch.setattr("avrea_cli.vm.subprocess.Popen", lambda *a, **k: proc)
        monkeypatch.setattr("avrea_cli.vm._wait_until_listening", lambda *a, **k: True)
        monkeypatch.setattr("avrea_cli.vm._write_known_hosts", lambda *a, **k: None)  # no host key
        ep = {"external_ip": "203.0.113.1", "external_port": 30022, "username": "runner"}
        vmmod._run_tunnel(ep, forwards=[(40000, 22)], identity_file=None, on_ready=lambda p: None)
        assert any("no SSH host key" in m for m in msgs)


class TestVmBootstrapPlanning:
    """Argument validation and the offline `--print` plan (no VM contact)."""

    def test_ref_without_repo_errors(self, runner):
        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-1", "--ref", "main"])
        assert result.exit_code != 0
        assert "--ref requires --repo" in result.output

    @pytest.mark.parametrize("bad", ["a" * 40, "refs/tags/v1", "refs/pull/3/merge", "bad ref", "a..b"])
    def test_ref_rejects_non_branch(self, runner, bad):
        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-1", "--repo", "owner/repo", "--ref", bad])
        assert result.exit_code != 0
        assert "not a branch name" in result.output

    def test_no_steps_errors(self, runner):
        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-1"])
        assert result.exit_code != 0
        assert "Nothing to do" in result.output

    def test_env_step_keeps_secret_off_argv(self):
        from avrea_cli.vm import _env_step

        step = _env_step({"CLAUDE_CODE_OAUTH_TOKEN": "sk-ant-oat01-secret"}, redact=False)
        # The secret rides stdin, never the script (which becomes argv on the VM).
        assert "sk-ant-oat01-secret" in step.stdin
        assert "sk-ant-oat01-secret" not in step.script
        assert step.secret is True
        # Hooked into an interactive rc and a login file; a one-off remote command
        # needs `avr vm ssh --login` (or `bash -lc`) to pick these up.
        assert "$HOME/.bashrc" in step.script  # interactive non-login shells
        # The login file is what `bash -lc` / `ssh --login` actually reads, chosen
        # from the first of ~/.bash_profile, ~/.bash_login, ~/.profile that exists.
        assert '_avr_hook "$login_rc"' in step.script
        assert "$HOME/.bash_profile" in step.script

    def test_unknown_agent_rejected(self, runner):
        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-1", "--install", "gemini"])
        assert result.exit_code != 0
        assert "unknown agent" in result.output

    def test_env_missing_local_var_rejected(self, runner, monkeypatch):
        monkeypatch.delenv("DEFINITELY_UNSET_VAR", raising=False)
        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-1", "--env", "DEFINITELY_UNSET_VAR"])
        assert result.exit_code != 0
        assert "not set in the local environment" in result.output

    def test_env_bad_name_rejected(self, runner):
        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-1", "--env", "not a name=x"])
        assert result.exit_code != 0
        assert "not a valid variable name" in result.output

    def test_print_redacts_secrets_and_never_connects(self, runner, monkeypatch):
        # --print must not fetch a gh token, hit the API, or touch SSH.
        monkeypatch.setattr(
            "avrea_cli.vm._local_gh_token",
            lambda: (_ for _ in ()).throw(AssertionError("must not fetch a token in --print")),
        )
        seen = {"get": 0}
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: seen.__setitem__("get", seen["get"] + 1),
        )
        result = runner.invoke(
            cli,
            ["vm", "bootstrap", "cvm-1", "--setup-github", "--env", "MYKEY=supersecret", "--print"],
        )
        assert result.exit_code == 0, result.output
        assert seen["get"] == 0  # never connected
        assert "supersecret" not in result.output  # value rides stdin, redacted in the plan
        assert "over stdin and is not shown" in result.output
        assert "gh auth login --with-token" in result.output

    def test_build_steps_order(self):
        from avrea_cli.vm import _build_bootstrap_steps

        steps = _build_bootstrap_steps(
            env={"A": "1"},
            setup_github=True,
            gh_token="t",
            repo_url="https://example.com/r",
            repo_ref=None,
            dotfiles_url="https://example.com/d",
            agents=["claude"],
            install_avr=True,
            run_script="echo x",
            redact=False,
        )
        assert [s.name for s in steps] == [
            "env",
            "github",
            "repo",
            "dotfiles",
            "install-agents",
            "install-avr",
            "run",
        ]

    def test_env_values_ride_stdin_not_argv(self):
        from avrea_cli.vm import _build_bootstrap_steps

        steps = _build_bootstrap_steps(
            env={"TOKEN": "s3cr3t"},
            setup_github=False,
            gh_token=None,
            repo_url=None,
            repo_ref=None,
            dotfiles_url=None,
            agents=[],
            install_avr=False,
            run_script=None,
            redact=False,
        )
        (env_step,) = steps
        assert "s3cr3t" in env_step.stdin  # value on stdin
        assert "s3cr3t" not in env_step.script  # never in the script (argv)
        assert env_step.secret is True


class TestVmBootstrapRun:
    """The real run path: readiness probe, ordered SSH steps, secret handling."""

    def _patch_ssh(self, monkeypatch, calls, *, fail_step=False):
        from types import SimpleNamespace

        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: RUNNING_VM,
        )

        def fake_run_ssh(ssh_ep, command, *, identity_file=None, stdin_data=None, timeout=None, capture=True):
            calls.append({"command": command, "stdin": stdin_data, "capture": capture})
            rc = 3 if (fail_step and command != ["true"]) else 0
            return SimpleNamespace(returncode=rc, stdout="", stderr="")

        monkeypatch.setattr("avrea_cli.vm.run_ssh", fake_run_ssh)

    def test_runs_steps_in_order_over_ssh(self, runner, monkeypatch):
        calls: list[dict] = []
        self._patch_ssh(monkeypatch, calls)
        monkeypatch.setattr("avrea_cli.vm._local_gh_token", lambda: "GHTOKEN")

        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-abc123", "--setup-github", "--install", "claude"])
        assert result.exit_code == 0, result.output

        assert calls[0]["command"] == ["true"]  # SSH readiness probe first
        steps = [c for c in calls if c["command"] != ["true"]]
        assert len(steps) == 2
        assert all(c["command"][:2] == ["bash", "-lc"] for c in steps)
        assert all(c["capture"] is False for c in steps)  # steps stream live
        gh = steps[0]
        assert gh["stdin"] == "GHTOKEN"  # token rides stdin
        assert "GHTOKEN" not in " ".join(gh["command"])  # never in argv

    def test_step_failure_stops_and_names_step(self, runner, monkeypatch):
        calls: list[dict] = []
        self._patch_ssh(monkeypatch, calls, fail_step=True)

        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-abc123", "--install-avr"])
        assert result.exit_code != 0
        assert "install-avr" in result.output and "failed" in result.output
        assert len(calls) == 2  # probe + the one failing step, then stop

    def test_forward_agent_creds_ride_stdin(self, runner, monkeypatch):
        calls: list[dict] = []
        self._patch_ssh(monkeypatch, calls)
        monkeypatch.setenv("ANTHROPIC_API_KEY", "sk-ant-xyz")

        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-abc123", "--install", "claude", "--forward-agent-creds"])
        assert result.exit_code == 0, result.output
        steps = [c for c in calls if c["command"] != ["true"]]
        # the env step (carrying the forwarded key) precedes the install step
        env_call = steps[0]
        assert "sk-ant-xyz" in (env_call["stdin"] or "")
        assert "sk-ant-xyz" not in " ".join(env_call["command"])

    def test_claude_oauth_token_alone_forwarded_without_prompt(self, runner, monkeypatch):
        from types import SimpleNamespace

        calls: list[dict] = []
        self._patch_ssh(monkeypatch, calls)
        monkeypatch.delenv("ANTHROPIC_API_KEY", raising=False)
        monkeypatch.setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-oat01-only")
        monkeypatch.setattr("avrea_cli.vm._is_interactive", lambda: True)  # a token exists, so still no prompt
        ran: list = []
        monkeypatch.setattr(
            "avrea_cli.vm.subprocess.run", lambda argv, *a, **k: ran.append(argv) or SimpleNamespace(returncode=0)
        )
        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-abc123", "--install", "claude", "--forward-agent-creds"])
        assert result.exit_code == 0, result.output
        assert ran == []  # never offered setup-token
        steps = [c for c in calls if c["command"] != ["true"]]
        assert "sk-ant-oat01-only" in (steps[0]["stdin"] or "")

    def test_forward_creds_warns_on_api_key_oauth_collision(self, runner, monkeypatch):
        calls: list[dict] = []
        self._patch_ssh(monkeypatch, calls)
        monkeypatch.setenv("ANTHROPIC_API_KEY", "sk-ant-key")
        monkeypatch.setenv("CLAUDE_CODE_OAUTH_TOKEN", "sk-ant-oat01-tok")
        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-abc123", "--install", "claude", "--forward-agent-creds"])
        assert result.exit_code == 0, result.output
        assert "prefers the API key" in result.output
        env_stdin = next(c for c in calls if c["command"] != ["true"])["stdin"] or ""
        assert "sk-ant-key" in env_stdin and "sk-ant-oat01-tok" in env_stdin  # both forwarded

    def test_forward_creds_non_interactive_skips_prompt_and_warns(self, runner, monkeypatch):
        calls: list[dict] = []
        self._patch_ssh(monkeypatch, calls)
        monkeypatch.delenv("ANTHROPIC_API_KEY", raising=False)
        monkeypatch.delenv("CLAUDE_CODE_OAUTH_TOKEN", raising=False)
        monkeypatch.setattr("avrea_cli.vm._is_interactive", lambda: False)  # CI / piped stdin
        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-abc123", "--install", "claude", "--forward-agent-creds"])
        assert result.exit_code == 0, result.output
        assert "will be unauthenticated" in result.output  # warns rather than blocking on a prompt
        steps = [c for c in calls if c["command"] != ["true"]]
        assert all("CLAUDE_CODE_OAUTH_TOKEN" not in (s["stdin"] or "") for s in steps)

    def test_forward_creds_without_install_is_quiet_noop(self, runner, monkeypatch):
        # --forward-agent-creds with no --install is a no-op; the "Nothing to do"
        # error already covers it, so don't also warn about missing credentials.
        self._patch_ssh(monkeypatch, [])
        monkeypatch.delenv("ANTHROPIC_API_KEY", raising=False)
        monkeypatch.delenv("CLAUDE_CODE_OAUTH_TOKEN", raising=False)
        monkeypatch.delenv("OPENAI_API_KEY", raising=False)
        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-abc123", "--forward-agent-creds"])
        assert result.exit_code != 0
        assert "Nothing to do" in result.output
        assert "unauthenticated" not in result.output  # no misleading credential warning without --install

    def test_forward_creds_prompts_runs_setup_token_and_forwards_paste(self, runner, monkeypatch):
        from types import SimpleNamespace

        calls: list[dict] = []
        self._patch_ssh(monkeypatch, calls)
        monkeypatch.delenv("ANTHROPIC_API_KEY", raising=False)
        monkeypatch.delenv("CLAUDE_CODE_OAUTH_TOKEN", raising=False)
        monkeypatch.setattr("avrea_cli.vm._is_interactive", lambda: True)
        ran: list = []
        monkeypatch.setattr(
            "avrea_cli.vm.subprocess.run", lambda argv, *a, **k: ran.append(argv) or SimpleNamespace(returncode=0)
        )
        # confirm 'y' to run setup-token, then paste the token at the hidden prompt
        result = runner.invoke(
            cli,
            ["vm", "bootstrap", "cvm-abc123", "--install", "claude", "--forward-agent-creds"],
            input="y\nsk-ant-oat01-pasted\n",
        )
        assert result.exit_code == 0, result.output
        assert ["claude", "setup-token"] in ran  # offered and launched setup-token
        env_call = next(c for c in calls if c["command"] != ["true"])
        assert "sk-ant-oat01-pasted" in (env_call["stdin"] or "")  # pasted token rides stdin
        assert "sk-ant-oat01-pasted" not in " ".join(env_call["command"])  # never argv
        assert "sk-ant-oat01-pasted" not in result.output  # never echoed back to the terminal

    def test_unreachable_ssh_surfaces_clear_error(self, runner, monkeypatch):
        from types import SimpleNamespace

        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, params=None: RUNNING_VM,
        )
        monkeypatch.setattr("avrea_cli.vm.time.sleep", lambda *_a: None)
        # Probe never succeeds.
        monkeypatch.setattr(
            "avrea_cli.vm.run_ssh",
            lambda *a, **k: SimpleNamespace(returncode=255, stdout="", stderr=""),
        )
        result = runner.invoke(cli, ["vm", "bootstrap", "cvm-abc123", "--install-avr"])
        assert result.exit_code != 0
        assert "could not reach the VM over SSH" in result.output
