"""`avr vm` commands take the VM's display name as well as its cvm- ID."""

from avrea_cli.main import cli
from tests.test_vm import CREATE_RESPONSE
from tests.test_vm import SAMPLE_VM
from tests.test_vm import _capture
import pytest

DETAIL = {"data": {**SAMPLE_VM, "egress_rules": []}}
DELETED = {"data": {"customer_vm_id": "cvm-abc123", "state": "DELETING"}}


def _get_recorder(store, response):
    def _get(self, path, params=None, timeout=None):
        store["path"] = path
        return response

    return _get


@pytest.mark.parametrize(
    ("ref", "segment"),
    [
        ("Dev Box", "Dev%20Box"),
        ("dev/box", "dev%2Fbox"),
        ("cvm-abc123", "cvm-abc123"),
    ],
)
def test_show_sends_the_name_or_id_as_one_path_segment(runner, monkeypatch, ref: str, segment: str) -> None:
    store: dict = {}
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", _get_recorder(store, DETAIL))

    result = runner.invoke(cli, ["vm", "show", ref])

    assert result.exit_code == 0, result.output
    assert store["path"] == f"/orgs/org-default/vms/{segment}"


def test_delete_by_name(runner, monkeypatch) -> None:
    store: dict = {}

    def _delete(self, path, params=None):
        store["path"] = path
        return DELETED

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_delete", _delete)

    result = runner.invoke(cli, ["vm", "delete", "Dev Box", "--yes"])

    assert result.exit_code == 0, result.output
    assert store["path"] == "/orgs/org-default/vms/Dev%20Box"


def test_create_without_a_name_leaves_naming_to_the_server(runner, monkeypatch) -> None:
    store: dict = {"return": CREATE_RESPONSE}
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", _capture(store))

    result = runner.invoke(cli, ["vm", "create", "--os", "linux", "--size", "2-vcpu", "--ephemeral"])

    assert result.exit_code == 0, result.output
    assert "display_name" not in store["json"]
    # The follow-up hint names the VM the way a person will type it.
    assert "avr vm show 'dev box'" in result.output
    assert "vm show cvm-abc123" not in result.output


def test_create_with_a_name_sends_it(runner, monkeypatch) -> None:
    store: dict = {"return": CREATE_RESPONSE}
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", _capture(store))

    result = runner.invoke(
        cli, ["vm", "create", "--name", "Dev Box", "--os", "linux", "--size", "2-vcpu", "--ephemeral"]
    )

    assert result.exit_code == 0, result.output
    assert store["json"]["display_name"] == "Dev Box"


@pytest.mark.parametrize("command", ["show", "ssh", "pause", "resume", "delete", "update", "start", "stop"])
def test_usage_names_the_argument_vm(runner, command: str) -> None:
    result = runner.invoke(cli, ["vm", command, "--help"])

    assert result.exit_code == 0, result.output
    assert "[OPTIONS] VM" in result.output
    assert "VM_ID" not in result.output
