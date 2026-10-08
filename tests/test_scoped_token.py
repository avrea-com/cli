"""Unit tests for using a scoped token (``avs_...``) as the CLI credential."""

from avrea_cli.config import CliConfig
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import get_org_slug
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import is_scoped_token
from avrea_cli.main import cli
from unittest.mock import MagicMock
import click
import httpx
import json
import pytest

SCOPED = "avs_0123456789abcdefghijklmnopqrstuv"
API_KEY = "avk_0123456789abcdefghijklmnopqrstuv"

ENTRIES = "/orgs/org-default/repos/rep-123/cache/entries"
CACHE_LIST = ["cache", "list", "--repo", "rep-123"]


@pytest.fixture
def scoped(runner, monkeypatch):
    """The shared runner, holding a scoped token instead of an API key."""
    monkeypatch.setenv("AVR_TOKEN", SCOPED)
    return runner


@pytest.mark.parametrize(
    ("credential", "expected"),
    [
        (SCOPED, True),
        ("avs_", True),
        (API_KEY, False),
        ("test-token", False),
        ("xavs_abc", False),
        ("avsabc", False),
        ("AVS_abc", False),
        ("", False),
        (None, False),
    ],
)
def test_is_scoped_token(credential, expected):
    assert is_scoped_token(credential) is expected


class TestCredentialIsSentUnchanged:
    def test_bearer_header_carries_the_scoped_token(self, scoped, api):
        api.reply("GET", ENTRIES, json={"data": [], "total": 0})
        result = scoped.invoke(cli, CACHE_LIST)
        assert result.exit_code == 0, result.output
        (request,) = api.requests
        assert request.headers["Authorization"] == f"Bearer {SCOPED}"


class TestOrganizationRule:
    """A scoped token is bound to one organization and cannot read the
    membership list, so only an ``org-...`` ID resolves."""

    def _assert_refused(self, result, api):
        assert result.exit_code == 1
        assert "scoped token is bound to one organization" in result.stderr
        assert "--org org-" in result.stderr
        assert "AVR_ORG" in result.stderr
        assert api.requests == []

    def test_slug_option_is_refused_without_a_lookup(self, scoped, api):
        self._assert_refused(scoped.invoke(cli, [*CACHE_LIST, "--org", "acme"]), api)

    def test_slug_in_avr_org_is_refused_without_a_lookup(self, scoped, api, monkeypatch):
        monkeypatch.setenv("AVR_ORG", "acme")
        self._assert_refused(scoped.invoke(cli, CACHE_LIST), api)

    def test_missing_organization_is_refused_without_a_lookup(self, scoped, api, monkeypatch):
        monkeypatch.delenv("AVR_ORG")
        self._assert_refused(scoped.invoke(cli, CACHE_LIST), api)

    def test_id_option_is_used_as_given(self, scoped, api, monkeypatch):
        monkeypatch.delenv("AVR_ORG")
        api.reply("GET", "/orgs/org-other/repos/rep-123/cache/entries", json={"data": [], "total": 0})
        result = scoped.invoke(cli, [*CACHE_LIST, "--org", "org-other"])
        assert result.exit_code == 0, result.output
        assert [r.url.path for r in api.requests] == ["/orgs/org-other/repos/rep-123/cache/entries"]

    def test_id_in_avr_org_is_used_as_given(self, scoped, api):
        api.reply("GET", ENTRIES, json={"data": [], "total": 0})
        result = scoped.invoke(cli, CACHE_LIST)
        assert result.exit_code == 0, result.output
        assert [r.url.path for r in api.requests] == [ENTRIES]

    def test_id_option_wins_over_a_slug_in_avr_org(self, scoped, api, monkeypatch):
        monkeypatch.setenv("AVR_ORG", "acme")
        api.reply("GET", "/orgs/org-other/repos/rep-123/cache/entries", json={"data": [], "total": 0})
        result = scoped.invoke(cli, [*CACHE_LIST, "--org", "org-other"])
        assert result.exit_code == 0, result.output

    @pytest.mark.parametrize(("option", "default"), [("acme", None), (None, "acme"), (None, None)])
    def test_get_org_id_never_calls_the_membership_lookup(self, option, default):
        config = MagicMock(spec=CliConfig)
        config.auth_token = SCOPED
        config.default_org = default
        client = MagicMock()
        with pytest.raises(click.Abort):
            get_org_id(config, option, client=client)
        client.public_get.assert_not_called()

    def test_get_org_id_refuses_a_slug_without_a_client_too(self):
        config = MagicMock(spec=CliConfig)
        config.auth_token = SCOPED
        config.default_org = None
        with pytest.raises(click.Abort):
            get_org_id(config, "acme")

    def test_api_key_still_resolves_a_slug(self, runner, api):
        api.reply("GET", "/users/me/organizations", json={"data": [{"organization_id": "org-acme", "slug": "acme"}]})
        api.reply("GET", "/orgs/org-acme/repos/rep-123/cache/entries", json={"data": [], "total": 0})
        result = runner.invoke(cli, [*CACHE_LIST, "--org", "acme"])
        assert result.exit_code == 0, result.output
        assert len(api.sent("GET", "/users/me/organizations")) == 1

    def test_slug_for_console_urls_skips_the_lookup(self):
        client = MagicMock()
        client.config.auth_token = SCOPED
        assert get_org_slug(client, "org-default") == "org-default"
        client.public_get.assert_not_called()


class TestAuthStatus:
    def test_reports_a_scoped_token_and_its_organization(self, scoped, api):
        result = scoped.invoke(cli, ["auth", "status"])
        assert result.exit_code == 0, result.output
        assert "scoped token" in result.stdout
        assert "org-default" in result.stdout
        assert ["Host", "https://api.avrea.com"] in [line.split() for line in result.stdout.splitlines()]
        assert "avr auth login" not in result.output
        # `/users/me` answers 404 to a scoped token, so it is never asked.
        assert api.requests == []

    def test_token_is_masked_unless_asked_for(self, scoped, api):
        masked = scoped.invoke(cli, ["auth", "status"])
        assert SCOPED not in masked.output
        assert "avs_****" in masked.stdout
        shown = scoped.invoke(cli, ["auth", "status", "--show-token"])
        assert shown.exit_code == 0, shown.output
        assert SCOPED in shown.stdout

    def test_says_how_to_set_a_missing_organization(self, scoped, api, monkeypatch):
        monkeypatch.delenv("AVR_ORG")
        result = scoped.invoke(cli, ["auth", "status"])
        assert result.exit_code == 0, result.output
        assert "not set" in result.stdout
        assert "AVR_ORG" in result.stdout
        assert api.requests == []

    def test_flags_a_slug_as_unusable(self, scoped, api, monkeypatch):
        monkeypatch.setenv("AVR_ORG", "acme")
        result = scoped.invoke(cli, ["auth", "status"])
        assert result.exit_code == 0, result.output
        assert "acme" in result.stdout
        assert "org-..." in result.stdout
        assert api.requests == []

    def test_json(self, scoped, api):
        result = scoped.invoke(cli, ["auth", "status", "--json", "*"])
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == {
            "created_at": None,
            "credential_type": "scoped_token",
            "default_org": "org-default",
            "email": None,
            "host": "https://api.avrea.com",
            "name": None,
            "user_id": None,
        }
        assert api.requests == []

    def test_json_show_token(self, scoped, api):
        result = scoped.invoke(cli, ["auth", "status", "--json", "credential_type,token", "--show-token"])
        assert result.exit_code == 0, result.output
        assert json.loads(result.stdout) == {"credential_type": "scoped_token", "token": SCOPED}

    def test_api_key_still_reads_the_user(self, runner, api):
        api.reply("GET", "/users/me", json={"id": "usr-1", "email": "alice@example.com", "name": "Alice"})
        result = runner.invoke(cli, ["auth", "status"])
        assert result.exit_code == 0, result.output
        assert "alice@example.com" in result.stdout
        assert "scoped token" not in result.stdout
        parsed = json.loads(runner.invoke(cli, ["auth", "status", "--json", "credential_type,email"]).stdout)
        assert parsed == {"credential_type": "api_key", "email": "alice@example.com"}


class TestConfigStatus:
    @pytest.mark.parametrize("args", [["config"], ["config", "list"]])
    def test_reports_a_scoped_token_without_any_lookup(self, scoped, api, args):
        result = scoped.invoke(cli, args)
        assert result.exit_code == 0, result.output
        assert "scoped token" in result.stdout
        assert "AVR_TOKEN" in result.stdout
        assert "org-default" in result.stdout
        assert "/users/me failed" not in result.stdout
        assert SCOPED not in result.output
        assert api.requests == []

    def test_api_key_still_reads_the_user(self, runner, api):
        api.reply("GET", "/users/me", json={"id": "usr-1", "email": "alice@example.com"})
        api.reply("GET", "/users/me/organizations", json={"data": [{"organization_id": "org-default", "slug": "acme"}]})
        result = runner.invoke(cli, ["config"])
        assert result.exit_code == 0, result.output
        assert "authenticated as alice@example.com" in result.stdout
        assert "scoped token" not in result.stdout


class TestScopeHint:
    """A 404 or 403 answered to a scoped token usually means "outside the
    token's scope", which the bare status does not say."""

    HINT = "scoped token"

    def _refused(self, runner, api, status):
        api.reply("GET", ENTRIES, status, json={"detail": "Not found" if status == 404 else "Refused"})
        return runner.invoke(cli, CACHE_LIST)

    @pytest.mark.parametrize("status", [404, 403])
    def test_hint_follows_a_refusal_of_a_scoped_token(self, scoped, api, status):
        result = self._refused(scoped, api, status)
        assert result.exit_code == 1
        assert f"HTTP {status}" in result.stderr
        hints = [line for line in result.stderr.splitlines() if self.HINT in line]
        assert len(hints) == 1
        assert "repositories and VMs" in hints[0]
        # Listing organizations is not open to a scoped token, so it is not suggested.
        assert "avr org list" not in result.stderr
        assert SCOPED not in result.output

    @pytest.mark.parametrize("status", [404, 403])
    def test_no_hint_for_an_api_key(self, runner, api, status):
        result = self._refused(runner, api, status)
        assert result.exit_code == 1
        assert f"HTTP {status}" in result.stderr
        assert self.HINT not in result.stderr
        assert ("avr org list" in result.stderr) is (status == 403)

    @pytest.mark.parametrize("status", [400, 409, 422, 429, 500, 503])
    def test_no_hint_for_other_statuses(self, scoped, api, status):
        result = self._refused(scoped, api, status)
        assert result.exit_code == 1
        assert self.HINT not in result.stderr

    NEW_TOKEN_HINT = (
        "  Hint: ask a member of the organization to check the API's reason above. If a replacement is needed, "
        "create one with `avr token create` and set it as AVR_TOKEN.\n"
    )
    LOGIN_HINT = (
        "To get started with Avrea CLI, please run:  avr auth login\n"
        "Alternatively, set the AVR_TOKEN environment variable to an Avrea API token.\n"
    )

    def test_revoked_scoped_token_gets_one_error_and_a_hint_that_fits(self, scoped, api):
        """Its holder is usually a script or an agent: logging in is not how it
        gets a new token, so the login hint is replaced."""
        api.reply("GET", "/orgs/org-default/access-tokens/key-1", 401, json={"detail": "Scoped token has been revoked"})
        result = scoped.invoke(cli, ["token", "view", "key-1"])
        assert result.exit_code == 4
        assert result.stderr == (
            "Error: The scoped token was rejected (HTTP 401): Scoped token has been revoked\n" + self.NEW_TOKEN_HINT
        )
        assert result.stdout == ""

    def test_expired_scoped_token(self, scoped, api):
        api.reply("GET", ENTRIES, 401, json={"detail": "Scoped token has expired"})
        result = scoped.invoke(cli, CACHE_LIST)
        assert result.exit_code == 4
        assert result.stderr == (
            "Error: The scoped token was rejected (HTTP 401): Scoped token has expired\n" + self.NEW_TOKEN_HINT
        )

    def test_rejected_scoped_token_without_a_reason(self, scoped, api):
        api.reply("GET", ENTRIES, 401)
        result = scoped.invoke(cli, CACHE_LIST)
        assert result.exit_code == 4
        assert result.stderr == "Error: The scoped token was rejected (HTTP 401).\n" + self.NEW_TOKEN_HINT

    def test_rejected_scoped_token_names_a_non_default_api_in_the_same_line(self, scoped, api, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "https://avrea.example.com")
        api.reply("GET", ENTRIES, 401, json={"detail": "Scoped token has been revoked"})
        result = scoped.invoke(cli, CACHE_LIST)
        assert result.exit_code == 4
        assert result.stderr == (
            "Error: https://avrea.example.com rejected the scoped token (HTTP 401): Scoped token has been revoked\n"
            + self.NEW_TOKEN_HINT
        )

    def test_rejected_api_key_output_is_unchanged(self, runner, api):
        api.reply("GET", ENTRIES, 401, json={"detail": "Invalid API key"})
        result = runner.invoke(cli, CACHE_LIST)
        assert result.exit_code == 4
        assert result.stderr == self.LOGIN_HINT

    def test_rejected_api_key_output_is_unchanged_on_a_non_default_api(self, runner, api, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "https://avrea.example.com")
        api.reply("GET", ENTRIES, 401, json={"detail": "Invalid API key"})
        result = runner.invoke(cli, CACHE_LIST)
        assert result.exit_code == 4
        assert result.stderr == (
            "Error: https://avrea.example.com rejected your credentials (HTTP 401).\n" + self.LOGIN_HINT
        )

    def test_rejected_stored_login_output_is_unchanged(self, runner, api, monkeypatch):
        monkeypatch.delenv("AVR_TOKEN")
        monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: API_KEY)
        api.reply("GET", ENTRIES, 401, json={"detail": "Invalid API key"})
        result = runner.invoke(cli, CACHE_LIST)
        assert result.exit_code == 4
        assert result.stderr == self.LOGIN_HINT
        assert api.requests[0].headers["Authorization"] == f"Bearer {API_KEY}"

    @pytest.mark.parametrize(
        ("credential", "expected"),
        [(SCOPED, True), (API_KEY, False), (None, False)],
    )
    def test_hint_is_keyed_on_the_credential_the_request_carried(self, capsys, credential, expected):
        headers = {} if credential is None else {"Authorization": f"Bearer {credential}"}
        request = httpx.Request("POST", "https://api.avrea.com/orgs/org-1/vms", headers=headers)
        response = httpx.Response(403, request=request, json={"detail": "Refused"})
        with pytest.raises(SystemExit) as excinfo:
            handle_http_error(httpx.HTTPStatusError("err", request=request, response=response), "create VM")
        assert excinfo.value.code == 1
        assert (self.HINT in capsys.readouterr().err) is expected

    def test_hint_on_a_command_that_passes_its_own_404_hint(self, scoped, api):
        api.reply("GET", "/orgs/org-default/vms/cvm-9", 404, json={"detail": "Not found"})
        result = scoped.invoke(cli, ["vm", "show", "cvm-9"])
        assert result.exit_code == 1
        assert "avr vm list" in result.stderr
        assert len([line for line in result.stderr.splitlines() if self.HINT in line]) == 1
