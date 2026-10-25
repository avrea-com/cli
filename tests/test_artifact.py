"""Artifact commands use repository-scoped, authenticated APIs and atomic files."""

from avrea_cli.main import cli
from click.testing import CliRunner
from pathlib import Path
from typing import Any
import hashlib
import httpx
import json
import pytest

ART = "art-" + "1" * 32
REPO = "rep-" + "2" * 32
BASE = f"/orgs/org-default/repos/{REPO}/artifacts"
DATA = b"downloaded artifact"
ROW = {
    "id": ART,
    "name": "output",
    "file_name": "artifact.zip",
    "state": "complete",
    "size_bytes": len(DATA),
    "digest_sha256": hashlib.sha256(DATA).hexdigest(),
}


def test_list_selector_and_cursor(runner: CliRunner, api: Any) -> None:
    api.reply("GET", BASE, json={"data": [ROW], "pagination": {"next_cursor": "cursor2"}})
    result = runner.invoke(cli, ["artifact", "list", REPO, "--run-id", "123", "--limit", "2", "--json", "id,name"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.stdout) == [{"id": ART, "name": "output"}]
    request = api.sent("GET", BASE)[0]
    assert request.url.params["run_id"] == "eq.123"
    assert request.url.params["limit"] == "2"
    assert "--cursor cursor2" in result.stderr
    assert request.headers["authorization"] == "Bearer test-token"


def test_get_and_confirmed_delete(runner: CliRunner, api: Any) -> None:
    api.reply("GET", f"{BASE}/{ART}", json={"data": ROW})
    api.reply("DELETE", f"{BASE}/{ART}", status=204)
    result = runner.invoke(cli, ["artifact", "get", ART, "--repo", REPO, "--json", "id,state"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.stdout) == {"id": ART, "state": "complete"}
    result = runner.invoke(cli, ["artifact", "delete", ART, "--repo", REPO, "--yes", "--json", "*"])
    assert result.exit_code == 0, result.output
    assert json.loads(result.stdout) == {"deleted": True, "id": ART}
    assert len(api.sent("DELETE", f"{BASE}/{ART}")) == 1


@pytest.mark.parametrize("bad", [False, True])
def test_download_verifies_before_replacing_existing_file(
    runner: CliRunner, monkeypatch: Any, tmp_path: Path, bad: bool
) -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("/download"):
            return httpx.Response(200, content=b"bad" if bad else DATA)
        return httpx.Response(200, json={"data": ROW})

    monkeypatch.setattr("avrea_cli.api_client.CompressingTransport", lambda: httpx.MockTransport(respond))
    target = tmp_path / "artifact.zip"
    target.write_bytes(b"previous")
    result = runner.invoke(cli, ["artifact", "download", ART, "--repo", REPO, "--path", str(target), "--json", "*"])
    assert result.exit_code == int(bad), result.output
    assert target.read_bytes() == (b"previous" if bad else DATA)
    assert not list(tmp_path.glob(".avr-artifact-*"))
    assert all(request.headers["authorization"] == "Bearer test-token" for request in requests)
    if not bad:
        assert json.loads(result.stdout)["path"] == str(target)


def test_download_does_not_follow_redirects(runner: CliRunner, monkeypatch: Any, tmp_path: Path) -> None:
    requests: list[httpx.Request] = []

    def respond(request: httpx.Request) -> httpx.Response:
        requests.append(request)
        if request.url.path.endswith("/download"):
            return httpx.Response(302, headers={"Location": "https://different.example/secret"})
        return httpx.Response(200, json={"data": ROW})

    monkeypatch.setattr("avrea_cli.api_client.CompressingTransport", lambda: httpx.MockTransport(respond))
    result = runner.invoke(cli, ["artifact", "download", ART, "--repo", REPO, "--path", str(tmp_path)])
    assert result.exit_code != 0
    assert all(request.url.host != "different.example" for request in requests)
    assert not list(tmp_path.iterdir())
