"""`avr vm create --from-spawn-script`: an agent sandbox per vendor work order.

The vendor controller runs this as its spawn script with the work order in
the environment, and reads only the exit code: 0 created, 1 retry, 2 refused.
"""

from avrea_cli.main import cli
from avrea_cli.spawn_script import exit_code_for_status
from pathlib import Path
from tests.conftest import FakeApi
from typing import Any
import httpx
import json
import pytest

VMS_PATH = "/orgs/org-default/vms"
CREATED = {"data": {"vm": {"customer_vm_id": "cvm-0123456789abcdef0123456789abcdef"}, "password": None}}
WORK_ORDER_JWT = "eyJhbGciOiJFUzI1NiJ9.work-order.sig"
ACCOUNT_EMAIL = "someone@example.com"


@pytest.fixture
def claude_code_env(monkeypatch, tmp_path: Path) -> Path:
    order = tmp_path / "work-order"
    order.write_text(WORK_ORDER_JWT + "\n")
    monkeypatch.setenv("CLAUDE_RUNNER_WORK_ORDER_FILE", str(order))
    monkeypatch.setenv("CLAUDE_RUNNER_ORDER_ID", "ord_123")
    monkeypatch.setenv("CLAUDE_RUNNER_SESSION_ID", "sess_1")
    monkeypatch.setenv("CLAUDE_RUNNER_ACCOUNT_EMAIL", ACCOUNT_EMAIL)
    return order


def _spawn(runner, provider: str, *args: str) -> Any:
    return runner.invoke(cli, ["vm", "create", "--from-spawn-script", provider, *args])


def _sent_body(api: FakeApi) -> dict[str, Any]:
    (request,) = api.sent("POST", VMS_PATH)
    return json.loads(request.content)


@pytest.mark.usefixtures("claude_code_env")
def test_claude_code_creates_a_sandbox_from_the_work_order(runner, api: FakeApi) -> None:
    api.reply("POST", VMS_PATH, 202, json=CREATED)

    result = _spawn(runner, "claude-code", "--os-version", "ubuntu-24.04")

    assert result.exit_code == 0, result.output
    assert result.stdout.strip() == "cvm-0123456789abcdef0123456789abcdef"
    body = _sent_body(api)
    assert body["idempotency_key"] == "ord_123"
    assert body["bootstrap"] == {"provider": "claude_code", "credential": WORK_ORDER_JWT}
    assert body["os_type"] == "linux"
    assert body["size"] == "4-vcpu"
    assert body["os_version"] == "ubuntu-24.04"
    assert body["ephemeral"] is True
    assert "repo" not in body
    assert "display_name" not in body
    assert WORK_ORDER_JWT not in result.output
    assert ACCOUNT_EMAIL not in result.output
    assert ACCOUNT_EMAIL not in json.dumps(body)


def test_a_missing_work_order_is_refused(runner, api: FakeApi) -> None:
    result = _spawn(runner, "claude-code")

    assert result.exit_code == 2
    assert api.requests == []


def test_cursor_is_not_offered(runner, api: FakeApi, monkeypatch) -> None:
    # Cursor's controller documents no retry for a failed spawn, so a quota
    # refusal would orphan its claimed worker; it waits for a queued create.
    monkeypatch.setenv("CURSOR_AGENT_WORKER_ID", "wkr-7")
    monkeypatch.setenv("CURSOR_POOL", "build-pool")
    monkeypatch.setenv("CURSOR_AUTH_TOKEN", "per-claim-token")

    result = _spawn(runner, "cursor")

    assert result.exit_code == 2
    assert "Invalid value for '--from-spawn-script'" in result.output
    assert api.requests == []


@pytest.mark.usefixtures("claude_code_env")
@pytest.mark.parametrize(
    ("status", "expected"),
    [
        (202, 0),
        (200, 0),
        (408, 1),
        (409, 1),
        (429, 1),
        (500, 1),
        (503, 1),
        (400, 2),
        (401, 2),
        (403, 2),
        (404, 2),
        (422, 2),
    ],
)
def test_the_api_answer_becomes_the_vendor_exit_code(runner, api: FakeApi, status: int, expected: int) -> None:
    api.reply("POST", VMS_PATH, status, json=CREATED if status < 300 else {"detail": "nope"})

    result = _spawn(runner, "claude-code")

    assert result.exit_code == expected, result.output
    assert WORK_ORDER_JWT not in result.output


@pytest.mark.usefixtures("claude_code_env")
@pytest.mark.parametrize(
    "args",
    [
        ["--name", "box"],
        ["--os", "windows"],
        ["--remote-desktop"],
        ["--wait"],
        ["--json"],
        ["--repo", "owner/repo"],
        ["--ref", "main"],
        ["--fetch-depth", "1"],
    ],
)
def test_options_a_sandbox_cannot_take_are_refused(runner, api: FakeApi, args: list[str]) -> None:
    result = _spawn(runner, "claude-code", *args)

    assert result.exit_code == 2
    assert api.requests == []


@pytest.mark.usefixtures("claude_code_env")
def test_a_transport_failure_has_the_vendor_retry(runner, monkeypatch) -> None:
    def refuse(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("connection refused", request=request)

    monkeypatch.setattr("avrea_cli.api_client.CompressingTransport", lambda: httpx.MockTransport(refuse))

    result = _spawn(runner, "claude-code")

    assert result.exit_code == 1, result.output
    assert "ConnectError" in result.output
    assert WORK_ORDER_JWT not in result.output


@pytest.mark.usefixtures("claude_code_env")
@pytest.mark.parametrize(("org_args", "default_org"), [(["--org", "acme"], "org-default"), ([], "acme"), ([], None)])
def test_an_organization_without_its_id_blocks_the_session(
    runner, api: FakeApi, monkeypatch, org_args: list[str], default_org: str | None
) -> None:
    # A slug or a missing organization needs a lookup no exit code fits, and a
    # retry cannot fix the hook's configuration, so the session waits for an
    # owner before any request is sent.
    if default_org is None:
        monkeypatch.delenv("AVR_ORG")
    else:
        monkeypatch.setenv("AVR_ORG", default_org)

    result = _spawn(runner, "claude-code", *org_args)

    assert result.exit_code == 2, result.output
    assert "organization ID (org-...)" in result.output
    assert api.requests == []


@pytest.mark.usefixtures("claude_code_env")
def test_an_organization_id_is_used_as_given(runner, api: FakeApi) -> None:
    api.reply("POST", "/orgs/org-acme/vms", 202, json=CREATED)

    result = _spawn(runner, "claude-code", "--org", "org-acme")

    assert result.exit_code == 0, result.output
    assert [request.url.path for request in api.requests] == ["/orgs/org-acme/vms"]


def test_a_plain_create_still_needs_its_shape(runner, api: FakeApi) -> None:
    result = runner.invoke(cli, ["vm", "create", "--os", "linux", "--size", "2-vcpu", "--ephemeral"])

    assert result.exit_code == 2
    assert "Missing option '--name'" in result.output
    assert api.requests == []


@pytest.mark.parametrize(
    ("status", "expected"),
    [(201, 0), (299, 0), (400, 2), (407, 2), (408, 1), (409, 1), (418, 2), (429, 1), (499, 2), (500, 1), (599, 1)],
)
def test_exit_code_boundaries(status: int, expected: int) -> None:
    assert exit_code_for_status(status) == expected
