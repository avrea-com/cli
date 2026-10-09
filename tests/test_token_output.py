"""Token credential delivery, diagnostics and scripted output."""

from avrea_cli.display import escape_control_characters
from avrea_cli.display import print_piped_row
from avrea_cli.main import cli
from click.testing import CliRunner
from tests.conftest import FakeApi
from tests.test_token import _minted
from typing import Any
from uuid import UUID
import csv
import errno
import httpx
import io
import json
import os
import pytest
import subprocess
import sys

ORG = "org-019a0000000070008000000000000001"
REPO = "rep-019a0000000070008000000000000002"
OTHER_REPO = "rep-019a0000000070008000000000000003"
TOKENS = f"/orgs/{ORG}/access-tokens"


def _revoke_command(record: dict[str, Any]) -> str:
    return f"`avr token revoke {record['id']} --org {ORG}`"


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


@pytest.mark.parametrize(
    "failure", ["missing", "invalid", "timeout", "runtime", "permission", "executable", "interrupt"]
)
def test_filter_failure_preserves_created_credential(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any], failure: str
) -> None:
    commands: list[list[str]] = []

    def run_jq(command: list[str], **_kwargs: Any) -> subprocess.CompletedProcess[str]:
        commands.append(command)
        if failure == "missing":
            raise FileNotFoundError
        if failure == "timeout":
            raise subprocess.TimeoutExpired(command, timeout=10)
        if failure == "permission":
            raise PermissionError("cannot execute jq")
        if failure == "executable":
            raise OSError(errno.ENOEXEC, "invalid executable")
        if failure == "interrupt":
            raise KeyboardInterrupt
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
    assert _revoke_command(minted_record) in result.stderr
    # Only a message that is fixed text is repeated; jq's own may quote its input.
    assert ("not found on PATH" in result.stderr) == (failure == "missing")
    assert "parse error" not in result.stderr
    assert "compile error" not in result.stderr
    assert commands == [["jq", "-r", ".token"]]
    assert len(api.sent("POST", TOKENS)) == 1


def test_successful_filter_preserves_the_requested_projection(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any]
) -> None:
    monkeypatch.setattr(
        "avrea_cli.json_output.subprocess.run",
        lambda command, **_kwargs: subprocess.CompletedProcess(
            command, 0, stdout=minted_record["token"] + "\n", stderr=""
        ),
    )
    api.reply("POST", TOKENS, 201, json=minted_record)
    result = runner.invoke(
        cli, ["token", "create", "--name", "ci", "--repo", REPO, "--json", "token,id", "--jq", ".token"]
    )
    assert result.exit_code == 0, result.output
    assert result.stdout == minted_record["token"] + "\n"
    assert minted_record["token"] not in result.stderr
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
    assert result.stderr.splitlines()[0] == f"Error: The scoped token was rejected (HTTP 401): {detail}"
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
    assert "grants[1] names an object you cannot reach at that level" in result.stderr
    assert "counts from 0 after resources are resolved and duplicates removed" in result.stderr
    assert "the --repo values first, then the --vm values, in the order given" in result.stderr


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


@pytest.mark.parametrize("filtered", ["", "key-only\n", '{"id":"key-only"}\n'])
def test_filter_cannot_discard_a_created_credential(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any], filtered: str
) -> None:
    monkeypatch.setattr(
        "avrea_cli.json_output.subprocess.run",
        lambda command, **_kwargs: subprocess.CompletedProcess(command, 0, stdout=filtered, stderr=""),
    )
    api.reply("POST", TOKENS, 201, json=minted_record)
    result = runner.invoke(
        cli, ["token", "create", "--name", "ci", "--repo", REPO, "--json", "token,id", "--jq", ".id"]
    )
    assert result.exit_code == 1
    assert json.loads(result.stdout) == {"token": minted_record["token"], "id": minted_record["id"]}
    assert "The output filter must retain the one-time credential." in result.stderr
    assert _revoke_command(minted_record) in result.stderr
    assert minted_record["token"] not in result.stderr
    assert len(api.sent("POST", TOKENS)) == 1


def test_human_render_failure_keeps_the_exports_and_revoke_hint(
    runner: CliRunner, api: FakeApi, minted_record: dict[str, Any]
) -> None:
    api.reply("POST", TOKENS, 201, json={**minted_record, "created_at": {"invalid": "timestamp"}})
    result = runner.invoke(cli, ["token", "create", "--name", "ci", "--repo", REPO])
    assert result.exit_code == 1
    assert f"export AVR_TOKEN={minted_record['token']}" in result.stdout
    assert f"export AVR_ORG={ORG}" in result.stdout
    assert "The export lines on stdout are complete and valid" in result.stderr
    assert _revoke_command(minted_record) in result.stderr
    assert minted_record["token"] not in result.stderr


@pytest.mark.parametrize("json_output", [True, False])
def test_buffered_output_failure_reports_revoke_without_claiming_recovery(
    minted_record: dict[str, Any], json_output: bool
) -> None:
    args = ["token", "create", "--name", "ci", "--repo", REPO]
    if json_output:
        args += ["--json", "token,id"]
    code = (
        "from avrea_cli.api_client import ApiClient\n"
        "from avrea_cli.main import cli\n"
        f"ApiClient.public_post = lambda *args, **kwargs: {minted_record!r}\n"
        f"cli(prog_name='avr', args={args!r})\n"
    )
    env = {**os.environ, "AVR_TOKEN": "avk_test", "AVR_ORG": ORG, "AVR_HOST": "https://api.avrea.com"}
    env.pop("PYTHONUNBUFFERED", None)
    reader, writer = os.pipe()
    os.close(reader)
    try:
        with subprocess.Popen([sys.executable, "-c", code], stdout=writer, stderr=subprocess.PIPE, env=env) as child:
            _, stderr = child.communicate(timeout=10)
    finally:
        os.close(writer)
    assert child.returncode != 0
    errors = stderr.decode()
    assert _revoke_command(minted_record) in errors
    assert "stdout contains" not in errors
    assert "export lines" not in errors
    assert minted_record["token"] not in errors


@pytest.mark.parametrize("json_output", [True, False])
@pytest.mark.parametrize("body", ["list", "no_token", "null_token", "empty_token", "non_string_token"])
def test_success_without_a_credential_is_reported_not_rendered(
    runner: CliRunner, api: FakeApi, minted_record: dict[str, Any], body: str, json_output: bool
) -> None:
    record = {key: value for key, value in minted_record.items() if key != "token"}
    reply: Any = {
        "list": [minted_record],
        "no_token": record,
        "null_token": {**record, "token": None},
        "empty_token": {**record, "token": ""},
        "non_string_token": {**record, "token": {"value": "avs_nested_secret"}},
    }[body]
    api.reply("POST", TOKENS, 201, json=reply)
    args = ["token", "create", "--name", "ci", "--repo", REPO, *(["--json", "token,id"] if json_output else [])]
    result = runner.invoke(cli, args)
    assert result.exit_code == 1
    assert isinstance(result.exception, SystemExit)
    assert result.stdout == ""
    assert "without a credential" in result.stderr
    assert "avs_nested_secret" not in result.output
    assert minted_record["token"] not in result.output
    if body == "list":
        assert "Check `avr token list` before retrying." in result.stderr
    else:
        assert _revoke_command(minted_record) in result.stderr


@pytest.mark.parametrize(
    ("status", "body"),
    [(500, {"detail": "Internal Server Error"}), (502, None), (503, None), (504, None), (504, {"detail": "timeout"})],
)
def test_server_error_on_create_warns_that_a_token_may_exist(
    runner: CliRunner, api: FakeApi, minted_record: dict[str, Any], status: int, body: dict[str, str] | None
) -> None:
    api.reply("POST", TOKENS, status, json=body, headers={"Retry-After": "30"})
    result = runner.invoke(cli, ["token", "create", "--name", "ci", "--repo", REPO])
    assert result.exit_code == 1
    assert f"HTTP {status}" in result.stderr
    assert "  Hint: A token may have been created; check `avr token list` before retrying." in result.stderr
    assert "No token was created" not in result.stderr
    assert len(api.sent("POST", TOKENS)) == 1


@pytest.mark.parametrize("failure", [httpx.ReadTimeout, httpx.ConnectError, KeyboardInterrupt, ValueError])
def test_unknown_mint_outcome_warns_before_retry(
    runner: CliRunner, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any], failure: type[Exception]
) -> None:
    calls = []

    def fail_post(_client: Any, path: str, **_kwargs: Any) -> None:
        calls.append(path)
        raise failure("untrusted transport detail")

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", fail_post)
    result = runner.invoke(cli, ["token", "create", "--name", "ci", "--repo", REPO])
    assert result.exit_code != 0
    assert "may have been created" in result.stderr
    assert "avr token list" in result.stderr
    assert "before retrying" in result.stderr
    assert "untrusted transport detail" not in result.output
    assert calls == [TOKENS]


@pytest.mark.parametrize("command", ["view", "revoke"])
@pytest.mark.parametrize(
    "bad_id", [".", "..", "../../../users/me/api-keys/current", "key-/../", "key-ABC", "key-short"]
)
def test_bad_token_id_sends_no_request(
    runner: CliRunner, api: FakeApi, minted_record: dict[str, Any], command: str, bad_id: str
) -> None:
    result = runner.invoke(cli, ["token", command, bad_id, *(["--yes"] if command == "revoke" else [])])
    assert result.exit_code == 2
    assert "token ID" in result.stderr
    assert api.requests == []


@pytest.mark.parametrize("piped", [True, False])
def test_token_names_cannot_forge_rows_or_terminal_controls(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any], piped: bool
) -> None:
    name = "safe\tforged\nrow\r\x1b[31m\x85next\u2028line"
    record = {key: value for key, value in minted_record.items() if key != "token"}
    api.reply("GET", TOKENS, json={"data": [{**record, "name": name}], "pagination": {}})
    monkeypatch.setattr("avrea_cli.commands.token.is_piped", lambda: piped)
    result = runner.invoke(cli, ["token", "list"], color=True)
    assert result.exit_code == 0, result.output
    assert "safe\\tforged\\nrow\\r\\x1b[31m\\x85next\\u2028line" in result.stdout
    assert "\x1b[31m" not in result.stdout
    if piped:
        assert len(result.stdout.splitlines()) == 2
        assert len(result.stdout.splitlines()[1].split("\t")) == 6
    structured = runner.invoke(cli, ["token", "list", "--json", "name"])
    assert json.loads(structured.stdout) == [{"name": name}]


def test_shared_piped_rows_escape_controls(capsys: pytest.CaptureFixture[str]) -> None:
    print_piped_row([0, None, "a\tb\nc\x1b"])
    assert capsys.readouterr().out == "0\t\ta\\tb\\nc\\x1b\n"


@pytest.mark.parametrize(
    ("char", "escaped"),
    [
        ("\x00", "\\x00"),
        ("\x1f", "\\x1f"),
        ("\x7f", "\\x7f"),
        ("\x85", "\\x85"),
        ("\x9b", "\\x9b"),
        ("\x9f", "\\x9f"),
        ("\u2028", "\\u2028"),
        ("\u2029", "\\u2029"),
        ("\u202a", "\\u202a"),
        ("\u202e", "\\u202e"),
        ("\u2066", "\\u2066"),
        ("\u2069", "\\u2069"),
    ],
)
def test_line_breaking_and_reordering_characters_are_escaped(char: str, escaped: str) -> None:
    text = escape_control_characters(f"a{char}b")
    assert text == f"a{escaped}b"
    assert text.splitlines() == [text]


@pytest.mark.parametrize(
    "text", ["plain", "C:\\temp\\new", "a\\tb", "caf\u00e9 \u65e5\u672c \u00a0\u2027\u202f\u2065\u206a", " ~"]
)
def test_ordinary_text_is_left_alone(text: str) -> None:
    assert escape_control_characters(text) == text


SERVER_TEXT = "ok\x9b31m\u2028forged\u202e\x1b[2J"
SERVER_TEXT_ESCAPED = "ok\\x9b31m\\u2028forged\\u202e\\x1b[2J"


@pytest.mark.parametrize("command", ["view", "create"])
def test_token_details_escape_server_text(
    runner: CliRunner, api: FakeApi, minted_record: dict[str, Any], command: str
) -> None:
    record = {**minted_record, "name": SERVER_TEXT, "revoked_at": "2026-10-08T06:00:00Z", "revoked_reason": SERVER_TEXT}
    if command == "create":
        api.reply("POST", TOKENS, 201, json=record)
        result = runner.invoke(cli, ["token", "create", "--name", "ci", "--repo", REPO], color=True)
    else:
        del record["token"]
        api.reply("GET", f"{TOKENS}/{record['id']}", json=record)
        result = runner.invoke(cli, ["token", "view", record["id"]], color=True)
    assert result.exit_code == 0, result.output
    assert result.stdout.count(SERVER_TEXT_ESCAPED) == 2
    for raw in ("\x9b", "\u2028", "\u202e", "\x1b"):
        assert raw not in result.stdout


@pytest.mark.parametrize("status", [401, 404, 422])
def test_error_details_escape_server_text(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any], status: int
) -> None:
    monkeypatch.setenv("AVR_TOKEN", minted_record["token"])
    api.reply("GET", f"{TOKENS}/{minted_record['id']}", status, json={"detail": SERVER_TEXT})
    result = runner.invoke(cli, ["token", "view", minted_record["id"]], color=True)
    assert result.exit_code != 0
    assert SERVER_TEXT_ESCAPED in result.stderr
    for raw in ("\x9b", "\u2028", "\u202e", "\x1b"):
        assert raw not in result.stderr


def test_scoped_token_does_not_borrow_the_login_organization(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any]
) -> None:
    monkeypatch.setenv("AVR_TOKEN", minted_record["token"])
    monkeypatch.delenv("AVR_ORG")
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda **_kwargs: ORG)
    result = runner.invoke(cli, ["cache", "list", "--repo", REPO])
    assert result.exit_code == 1
    assert "AVR_ORG" in result.stderr
    assert api.requests == []
    status = runner.invoke(cli, ["auth", "status", "--json", "default_org,credential_type"])
    assert json.loads(status.stdout) == {"default_org": None, "credential_type": "scoped_token"}
    explicit = f"/orgs/{ORG}/repos/{REPO}/cache/entries"
    api.reply("GET", explicit, json={"data": [], "total": 0})
    assert runner.invoke(cli, ["cache", "list", "--repo", REPO, "--org", ORG]).exit_code == 0


@pytest.mark.parametrize("organization", ["option", "environment", "none"])
def test_scoped_run_url_refusal_explains_the_supported_reference(
    runner: CliRunner,
    api: FakeApi,
    monkeypatch: pytest.MonkeyPatch,
    minted_record: dict[str, Any],
    organization: str,
) -> None:
    monkeypatch.setenv("AVR_TOKEN", minted_record["token"])
    if organization != "environment":
        monkeypatch.delenv("AVR_ORG")
    result = runner.invoke(
        cli,
        [
            "run",
            "view",
            "https://console.avrea.com/org/acme/runs/run-019a0000000070008000000000000009",
            *(["--org", ORG] if organization == "option" else []),
        ],
    )
    assert result.exit_code == 1
    assert result.stderr == "Error: A scoped token cannot resolve a run URL's organization. Pass the run ID instead.\n"
    assert api.requests == []


def test_auth_status_states_scoped_liveness_is_unchecked(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any]
) -> None:
    monkeypatch.setenv("AVR_TOKEN", minted_record["token"])
    result = runner.invoke(cli, ["auth", "status"])
    assert result.exit_code == 0
    assert "It is not checked here: an expired or revoked one fails on first use." in result.stdout
    assert api.requests == []


@pytest.mark.parametrize("listed_name", ["Acme/API", "aCme/Api"])
def test_scoped_repository_match_folds_the_server_name(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any], listed_name: str
) -> None:
    monkeypatch.setenv("AVR_TOKEN", minted_record["token"])
    api.reply(
        "GET",
        f"/orgs/{ORG}/repos",
        json={"data": [{"full_name": listed_name, "repository_id": REPO}], "pagination": {}},
    )
    api.reply("GET", f"/orgs/{ORG}/repos/{REPO}/cache/entries", json={"data": [], "total": 0})
    result = runner.invoke(cli, ["cache", "list", "--repo", "acme/api"])
    assert result.exit_code == 0, result.output
    assert len(api.requests) == 2


def test_scoped_repository_lookup_stops_a_repeated_cursor(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any]
) -> None:
    monkeypatch.setenv("AVR_TOKEN", minted_record["token"])
    path = f"/orgs/{ORG}/repos"
    page = {"data": [{"full_name": "acme/other", "repository_id": OTHER_REPO}], "pagination": {"next_cursor": "same"}}
    api.reply("GET", path, json=page)
    api.reply("GET", path, json=page)
    api.reply("GET", path, 500, json={"detail": "unexpected third request"})
    result = runner.invoke(cli, ["cache", "list", "--repo", "acme/api"])
    assert result.exit_code == 1
    assert len(api.requests) == 2
    assert "cursor" in result.stderr


@pytest.mark.parametrize("empty", [[], None])
def test_scoped_repository_lookup_stops_an_empty_page_with_a_cursor(
    runner: CliRunner,
    api: FakeApi,
    monkeypatch: pytest.MonkeyPatch,
    minted_record: dict[str, Any],
    empty: list[Any] | None,
) -> None:
    monkeypatch.setenv("AVR_TOKEN", minted_record["token"])
    path = f"/orgs/{ORG}/repos"
    for number in range(3):
        api.reply("GET", path, json={"data": empty, "pagination": {"next_cursor": f"fresh-{number}"}})
    result = runner.invoke(cli, ["cache", "list", "--repo", "acme/api"])
    assert result.exit_code == 1
    assert len(api.requests) == 1
    assert "cursor that does not advance" in result.stderr


def test_scoped_repository_lookup_skips_malformed_rows(
    runner: CliRunner, api: FakeApi, monkeypatch: pytest.MonkeyPatch, minted_record: dict[str, Any]
) -> None:
    monkeypatch.setenv("AVR_TOKEN", minted_record["token"])
    rows = [
        None,
        "bad row",
        {"full_name": "acme/api"},
        {"full_name": "acme/api", "repository_id": ""},
        {"full_name": "acme/api", "repository_id": 123},
        {"full_name": "Acme/API", "repository_id": REPO},
    ]
    api.reply("GET", f"/orgs/{ORG}/repos", json={"data": rows, "pagination": {}})
    api.reply("GET", f"/orgs/{ORG}/repos/{REPO}/cache/entries", json={"data": [], "total": 0})
    result = runner.invoke(cli, ["cache", "list", "--repo", "acme/api"])
    assert result.exit_code == 0, result.output
    assert len(api.requests) == 2
