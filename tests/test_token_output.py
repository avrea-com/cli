"""Token credential delivery, diagnostics and scripted output."""

from avrea_cli.main import cli
from click.testing import CliRunner
from tests.conftest import FakeApi
from tests.test_token import _minted
from typing import Any
from uuid import UUID
import csv
import io
import json
import pytest
import subprocess

ORG = "org-019a0000000070008000000000000001"
REPO = "rep-019a0000000070008000000000000002"
OTHER_REPO = "rep-019a0000000070008000000000000003"
TOKENS = f"/orgs/{ORG}/access-tokens"


@pytest.fixture
def minted_record(monkeypatch: pytest.MonkeyPatch) -> dict[str, Any]:
    monkeypatch.setenv("AVR_ORG", ORG)
    return _minted(
        id=f"key-{UUID('019a0000-0000-7000-8000-000000000005').hex}",
        organization_id=ORG,
        user_id="usr-019a0000000070008000000000000006",
        created_at="2026-10-08T05:00:00Z",
        expires_at="2026-10-08T13:00:00Z",
        grants=[{"resource_type": "repository", "resource_id": REPO, "access_level": "read"}],
    )


@pytest.mark.parametrize("failure", ["missing", "invalid", "timeout", "runtime"])
def test_filter_failure_preserves_created_credential(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any], failure: str
) -> None:
    def run_jq(command: list[str], **_kwargs: Any) -> subprocess.CompletedProcess[str]:
        assert command[0] == "jq"
        if failure == "missing":
            raise FileNotFoundError
        if failure == "timeout":
            raise subprocess.TimeoutExpired(command, timeout=10)
        if failure == "runtime":
            return subprocess.CompletedProcess(command, 5, stdout="", stderr=f"parse error: {minted_record['token']}")
        return subprocess.CompletedProcess(command, 3, stdout="", stderr="compile error")

    monkeypatch.setattr("avrea_cli.json_output.subprocess.run", run_jq)
    api.reply("POST", TOKENS, 201, json=minted_record)
    result = runner.invoke(
        cli, ["token", "create", "--name", "ci", "--repo", REPO, "--json", "token,id", "--jq", ".token"]
    )
    assert result.exit_code == 1
    assert minted_record["token"] in result.stdout
    assert json.loads(result.stdout) == {"token": minted_record["token"], "id": minted_record["id"]}
    assert result.stdout.count(minted_record["token"]) == 1
    assert minted_record["token"] not in result.stderr
    assert "token was created" in result.stderr.lower()
    assert "unfiltered" in result.stderr.lower()
    assert len(api.sent("POST", TOKENS)) == 1


def test_successful_filter_preserves_the_requested_projection(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any]
) -> None:
    monkeypatch.setattr(
        "avrea_cli.json_output.subprocess.run",
        lambda command, **_kwargs: subprocess.CompletedProcess(
            command, 0, stdout=minted_record["id"] + "\n", stderr=""
        ),
    )
    api.reply("POST", TOKENS, 201, json=minted_record)
    result = runner.invoke(
        cli, ["token", "create", "--name", "ci", "--repo", REPO, "--json", "token,id", "--jq", ".id"]
    )
    assert result.exit_code == 0, result.output
    assert result.stdout == minted_record["id"] + "\n"
    assert minted_record["token"] not in result.output
    assert len(api.sent("POST", TOKENS)) == 1


@pytest.mark.parametrize(
    "detail",
    [
        "Scoped tokens are disabled for this organization",
        "Scoped token owner or organization is no longer active",
        "Invalid scoped token",
    ],
)
def test_rejection_preserves_the_reason_without_claiming_expiry(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any], detail: str
) -> None:
    monkeypatch.setenv("AVR_TOKEN", minted_record["token"])
    api.reply("GET", f"/orgs/{ORG}/repos/{REPO}/cache/entries", 401, json={"detail": detail})
    result = runner.invoke(cli, ["cache", "list", "--repo", REPO])
    assert result.exit_code == 4
    assert detail in result.stderr
    assert "the token has expired or been revoked" not in result.stderr
    assert result.stderr.count("Error:") == 1
    assert minted_record["token"] not in result.output


def test_grant_refusal_identifies_the_deduplicated_request(
    runner: CliRunner, api: FakeApi, minted_record: dict[str, Any]
) -> None:
    api.reply("POST", TOKENS, 403, json={"detail": "grants[1] names an object you cannot reach at that level"})
    result = runner.invoke(
        cli, ["token", "create", "--name", "ci", "--repo", REPO, "--repo", REPO, "--repo", OTHER_REPO]
    )
    assert result.exit_code == 1
    (request,) = api.sent("POST", TOKENS)
    assert [g["resource_id"] for g in json.loads(request.content)["grants"]] == [REPO, OTHER_REPO]
    assert OTHER_REPO in result.stderr or "duplicate" in result.stderr.lower()


@pytest.mark.parametrize("owners", [1, 2])
def test_piped_list_has_stable_columns_and_raw_timestamps(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any], owners: int
) -> None:
    records = [{k: v for k, v in minted_record.items() if k != "token"}]
    records[0]["last_used_at"] = "2026-10-08T05:00:00Z"
    if owners == 2:
        records.append(
            {
                **records[0],
                "id": f"key-{UUID('019a0000-0000-7000-8000-000000000007').hex}",
                "user_id": "usr-019a0000000070008000000000000008",
                "last_used_at": None,
            }
        )
    monkeypatch.setattr("avrea_cli.commands.token.is_piped", lambda: True, raising=False)
    api.reply("GET", TOKENS, json={"data": records, "pagination": {"next_cursor": None}})
    result = runner.invoke(cli, ["token", "list"])
    assert result.exit_code == 0, result.output
    reader = csv.DictReader(io.StringIO(result.stdout), delimiter="\t")
    assert reader.fieldnames == ["id", "name", "expires_at", "last_used_at", "user_id", "grants"]
    rows = list(reader)
    assert len(rows) == owners
    for row, record in zip(rows, records, strict=True):
        assert row["id"] == record["id"]
        assert row["user_id"] == record["user_id"]
        assert row["expires_at"] == record["expires_at"]
        assert row["last_used_at"] == (record["last_used_at"] or "")
        assert row["grants"] == "1 repo"
    assert minted_record["token"] not in result.output
