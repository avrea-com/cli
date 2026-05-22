"""Smoke tests for the auth login output: it should show ✓, the user's
email, and a next-step hint."""

from avrea_cli.commands.auth_cmd import _format_token
from avrea_cli.main import cli
from click.testing import CliRunner
import click
import json
import pytest


@pytest.fixture()
def runner(monkeypatch):
    monkeypatch.setenv("AVR_TOKEN", "test-token")
    monkeypatch.setenv("AVR_ORG", "org-default")
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.login", lambda *a, **kw: "ak-test123")
    monkeypatch.setattr("avrea_cli.auth.store_token", lambda *a, **kw: None)
    return CliRunner()


class TestAuthLoginOutput:
    def test_logged_in_with_email(self, runner, monkeypatch):
        monkeypatch.setattr(
            "avrea_cli.commands.auth_cmd._fetch_email",
            lambda url, key: "alice@example.com",
        )
        result = runner.invoke(cli, ["auth", "login"])
        assert result.exit_code == 0, result.output
        assert "Logged in as" in result.output
        assert "alice@example.com" in result.output
        assert "Try: avr status" in result.output

    def test_falls_back_to_generic_when_email_lookup_fails(self, runner, monkeypatch):
        """If /users/me is briefly unavailable right after login, we still
        report success — credentials are already stored."""
        monkeypatch.setattr("avrea_cli.commands.auth_cmd._fetch_email", lambda url, key: None)
        result = runner.invoke(cli, ["auth", "login"])
        assert result.exit_code == 0, result.output
        assert "Authentication complete" in result.output
        assert "Try: avr status" in result.output

    def test_click_exception_renders_login_failed_prefix(self, runner, monkeypatch):
        """auth.login raises ClickException for unsupported providers, etc.
        The wrapper should surface that under the 'Login failed:' prefix."""

        def _raise(*a, **kw):
            raise click.ClickException("Unsupported auth provider 'wat'")

        monkeypatch.setattr("avrea_cli.auth.login", _raise)
        result = runner.invoke(cli, ["auth", "login"])
        assert result.exit_code == 1
        assert "Login failed: Unsupported auth provider 'wat'" in result.output

    def test_abort_propagates_without_double_message(self, runner, monkeypatch):
        """auth.login prints its own context-specific error before raising
        click.Abort (port-bind failure, missing session, API-key creation
        error). The wrapper must NOT prepend a redundant 'Login failed:'
        line — Click's runner handles Abort cleanly."""

        def _raise(*a, **kw):
            click.echo("Error: Cannot bind to port 8765", err=True)
            raise click.Abort()

        monkeypatch.setattr("avrea_cli.auth.login", _raise)
        result = runner.invoke(cli, ["auth", "login"])
        assert result.exit_code != 0
        # Abort surfaced from auth.login's own message — not duplicated.
        assert "Cannot bind to port 8765" in result.output
        assert "Login failed:" not in result.output


class TestFormatToken:
    """``_format_token`` is the only place we mask credentials for display.
    A regression here (e.g. inverting the ``show`` branch) would silently
    print every API key on every ``avr auth status`` invocation."""

    def test_none_token_returns_placeholder(self):
        assert _format_token(None, show=False) == "(none)"
        assert _format_token(None, show=True) == "(none)"

    def test_empty_token_returns_placeholder(self):
        assert _format_token("", show=False) == "(none)"

    def test_default_masks_token(self):
        token = "sk-live-1234567890abcdefghij"
        out = _format_token(token, show=False)
        assert token not in out
        # 4-char prefix + 32 stars
        assert out.startswith("sk-l")
        assert out.endswith("*" * 32)

    def test_short_token_fully_masked(self):
        # ≤8 chars: don't leak a prefix at all
        assert _format_token("abc", show=False) == "*" * 32
        assert _format_token("abcd1234", show=False) == "*" * 32

    def test_show_returns_full_token(self):
        token = "sk-live-1234567890abcdefghij"
        assert _format_token(token, show=True) == token


class TestAuthStatusOutput:
    """End-to-end coverage for the auth status command, especially around
    --show-token and the JSON serialization of the masked field."""

    @pytest.fixture()
    def status_runner(self, monkeypatch):
        monkeypatch.setenv("AVR_TOKEN", "sk-live-1234567890abcdefghij")
        monkeypatch.setenv("AVR_ORG", "org-default")
        monkeypatch.delenv("AVR_HOST", raising=False)
        monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
        monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_get",
            lambda self, path, **kw: {
                "id": "usr-1",
                "email": "alice@example.com",
                "name": "Alice",
                "created_at": "2025-01-01T00:00:00Z",
            },
        )
        return CliRunner()

    def test_default_masks_token(self, status_runner):
        result = status_runner.invoke(cli, ["auth", "status"])
        assert result.exit_code == 0, result.output
        assert "1234567890abcdefghij" not in result.output
        assert "****" in result.output

    def test_show_token_reveals_full(self, status_runner):
        result = status_runner.invoke(cli, ["auth", "status", "--show-token"])
        assert result.exit_code == 0, result.output
        assert "sk-live-1234567890abcdefghij" in result.output

    def test_json_output_omits_token_by_default(self, status_runner):
        """Without --show-token, the token field is dropped from the schema —
        not just nulled. `--json '*'` returns the fields that exist; an
        explicit `--json token` errors with the available-fields hint, which
        is the intended discoverability path for the credential."""
        result = status_runner.invoke(cli, ["auth", "status", "--json", "*"])
        assert result.exit_code == 0, result.output
        parsed = json.loads(result.output)
        assert "sk-live-1234567890abcdefghij" not in result.output
        assert "token" not in parsed

    def test_json_uses_snake_case_keys(self, status_runner):
        """JSON consumers should see snake_case, matching the rest of --json
        output and the API's wire-name convention. ``host`` is the full URL
        (matches ``AVR_HOST``); there is no separate ``api_url`` field."""
        result = status_runner.invoke(cli, ["auth", "status", "--json", "*"])
        parsed = json.loads(result.output)
        assert "user_id" in parsed
        assert "host" in parsed
        assert parsed["host"].startswith("http")
        assert "default_org" in parsed
        assert "created_at" in parsed
        # Spaces or capitalized labels would be a regression.
        assert "User ID" not in parsed
        assert "API endpoint" not in parsed

    def test_json_show_token_includes_full(self, status_runner):
        result = status_runner.invoke(cli, ["auth", "status", "--json", "*", "--show-token"])
        assert result.exit_code == 0, result.output
        parsed = json.loads(result.output)
        assert parsed["token"] == "sk-live-1234567890abcdefghij"
