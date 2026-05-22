"""Unit tests for cross-cutting CLI conventions:
- Exit code 4 for auth-required failures
- AVR_DEBUG=api as an env-driven equivalent of --verbose
- AVR_PROMPT_DISABLED to refuse interactive prompts
- AVR_PAGER / PAGER for paging long output
"""

from avrea_cli.display import page_output
from avrea_cli.helpers import EXIT_AUTH_REQUIRED
from avrea_cli.main import cli
from click.testing import CliRunner
from unittest.mock import MagicMock
from unittest.mock import patch
import httpx
import os
import pytest


@pytest.fixture()
def runner(monkeypatch):
    monkeypatch.setenv("AVR_TOKEN", "tok")
    monkeypatch.setenv("AVR_ORG", "org-default")
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.delenv("NO_COLOR", raising=False)
    monkeypatch.delenv("AVR_DEBUG", raising=False)
    monkeypatch.delenv("AVR_PROMPT_DISABLED", raising=False)
    monkeypatch.delenv("AVR_PAGER", raising=False)
    monkeypatch.delenv("PAGER", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    return CliRunner()


# ----------------------------------------------------------------------------
# Exit code 4: auth required
# ----------------------------------------------------------------------------


class TestExitCodeAuthRequired:
    """Auth-required failures must exit with code 4 so scripts can branch on
    `$? -eq 4` and trigger `avr auth login`. Generic failures stay at 1."""

    def test_no_token_exits_4(self, runner, monkeypatch):
        monkeypatch.delenv("AVR_TOKEN", raising=False)
        # `health` is intentionally unauthenticated; pick an auth-gated command.
        result = runner.invoke(cli, ["auth", "status"])
        assert result.exit_code == EXIT_AUTH_REQUIRED == 4
        assert "avr auth login" in result.output

    def test_401_response_exits_4(self, runner, monkeypatch):
        def raise_401(self, path, **kw):
            req = httpx.Request("GET", "https://api.example/users/me")
            raise httpx.HTTPStatusError(
                "401",
                request=req,
                response=httpx.Response(401, request=req),
            )

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", raise_401)
        result = runner.invoke(cli, ["auth", "status"])
        assert result.exit_code == 4
        assert "avr auth login" in result.output

    def test_generic_http_failure_still_exits_1(self, runner, monkeypatch):
        def raise_500(self, path, **kw):
            req = httpx.Request("GET", "https://api.example/orgs/org-1/workflow-runs")
            raise httpx.HTTPStatusError(
                "500",
                request=req,
                response=httpx.Response(500, request=req, json={"detail": "boom"}),
            )

        monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", raise_500)
        result = runner.invoke(cli, ["run", "list"])
        assert result.exit_code == 1
        assert "boom" in result.output


# ----------------------------------------------------------------------------
# AVR_DEBUG=api
# ----------------------------------------------------------------------------


def _ok_health_response() -> MagicMock:
    resp = MagicMock(status_code=200)
    resp.json.return_value = {"status": "ok"}
    resp.request.url = "https://api.example/health"
    return resp


class TestAvrDebug:
    def test_avr_debug_api_enables_verbose(self, runner, monkeypatch):
        monkeypatch.setenv("AVR_DEBUG", "api")
        with patch("avrea_cli.api_client.httpx.Client.get", return_value=_ok_health_response()):
            result = runner.invoke(cli, ["health"])
        assert result.exit_code == 0
        # Same wire-format as --verbose
        assert "GET" in result.output
        assert "https://api.example/health" in result.output

    def test_avr_debug_unrelated_value_does_not_enable(self, runner, monkeypatch):
        # AVR_DEBUG with values we don't recognize today (e.g. cache, oauth)
        # must not flip on api-level logging.
        monkeypatch.setenv("AVR_DEBUG", "cache,oauth")
        with patch("avrea_cli.api_client.httpx.Client.get", return_value=_ok_health_response()):
            result = runner.invoke(cli, ["health"])
        assert result.exit_code == 0
        assert "https://api.example/health" not in result.output

    def test_avr_debug_with_extra_categories(self, runner, monkeypatch):
        # Comma-separated; presence of `api` enables logging.
        monkeypatch.setenv("AVR_DEBUG", "cache,api,oauth")
        with patch("avrea_cli.api_client.httpx.Client.get", return_value=_ok_health_response()):
            result = runner.invoke(cli, ["health"])
        assert result.exit_code == 0
        assert "GET" in result.output

    def test_avr_debug_typo_emits_warning(self, runner, monkeypatch):
        """``AVR_DEBUG=apii`` (typo) must surface a warning to stderr — silent
        drop would leave users wondering why their debug logging isn't on."""
        monkeypatch.setenv("AVR_DEBUG", "apii")
        with patch("avrea_cli.api_client.httpx.Client.get", return_value=_ok_health_response()):
            result = runner.invoke(cli, ["health"])
        assert result.exit_code == 0
        assert "Warning: AVR_DEBUG" in result.output
        assert "apii" in result.output
        # The warning lists what *is* known so users can correct the typo.
        assert "Known: api" in result.output

    def test_avr_debug_warning_lists_multiple_unknowns(self, runner, monkeypatch):
        monkeypatch.setenv("AVR_DEBUG", "foo,bar,api")
        with patch("avrea_cli.api_client.httpx.Client.get", return_value=_ok_health_response()):
            result = runner.invoke(cli, ["health"])
        assert "Warning: AVR_DEBUG ignored unknown categories: bar, foo" in result.output

    def test_avr_debug_known_only_no_warning(self, runner, monkeypatch):
        monkeypatch.setenv("AVR_DEBUG", "api")
        with patch("avrea_cli.api_client.httpx.Client.get", return_value=_ok_health_response()):
            result = runner.invoke(cli, ["health"])
        assert "Warning: AVR_DEBUG" not in result.output


# ----------------------------------------------------------------------------
# AVR_PROMPT_DISABLED
# ----------------------------------------------------------------------------


class TestAvrPromptDisabled:
    def test_cache_delete_refuses_to_prompt_when_disabled(self, runner, monkeypatch):
        monkeypatch.setenv("AVR_PROMPT_DISABLED", "1")
        # No public_get/post mock needed — the guard runs before any network call.
        result = runner.invoke(cli, ["cache", "delete", "--repo", "rep-foo", "--type", "gha", "--key", "node_modules"])
        assert result.exit_code != 0
        assert "AVR_PROMPT_DISABLED" in result.output
        assert "--yes" in result.output

    def test_yes_flag_bypasses_guard(self, runner, monkeypatch):
        """With --yes the prompt is skipped entirely, so the guard never fires."""
        monkeypatch.setenv("AVR_PROMPT_DISABLED", "1")
        called: list[dict] = []
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_delete",
            lambda self, path, params=None: (called.append({"path": path, "params": params}), {"deleted_count": 0})[1],
        )
        result = runner.invoke(
            cli,
            [
                "cache",
                "delete",
                "--repo",
                "rep-foo",
                "--type",
                "gha",
                "--key",
                "node_modules",
                "--yes",
            ],
        )
        assert result.exit_code == 0, result.output
        assert called and called[0]["path"].endswith("/cache/entries")

    def test_cache_purge_all_refuses_to_prompt_when_disabled(self, runner, monkeypatch):
        """--all is the destructive blast-radius path; the prompt guard must
        protect it the same way --key does."""
        monkeypatch.setenv("AVR_PROMPT_DISABLED", "1")
        result = runner.invoke(cli, ["cache", "delete", "--repo", "rep-foo", "--all"])
        assert result.exit_code != 0
        assert "AVR_PROMPT_DISABLED" in result.output
        assert "--yes" in result.output

    def test_cache_purge_all_with_yes_succeeds_under_disabled(self, runner, monkeypatch):
        monkeypatch.setenv("AVR_PROMPT_DISABLED", "1")
        called: list[str] = []
        monkeypatch.setattr(
            "avrea_cli.api_client.ApiClient.public_delete",
            lambda self, path, params=None: (called.append(path), {"deleted_count": 7})[1],
        )
        result = runner.invoke(cli, ["cache", "delete", "--repo", "rep-foo", "--all", "--yes"])
        assert result.exit_code == 0, result.output
        assert called and called[0].endswith("/cache")
        assert "Purged 7" in result.output


# ----------------------------------------------------------------------------
# Pager (AVR_PAGER / PAGER)
# ----------------------------------------------------------------------------


class TestPagerEnv:
    def test_avr_pager_empty_disables_paging(self, runner, monkeypatch):
        monkeypatch.setenv("AVR_PAGER", "")
        echoed: list[str] = []
        monkeypatch.setattr("click.echo_via_pager", lambda content, **kw: pytest.fail("pager should not be invoked"))
        # Monkey-patch is_piped to False so we exercise the TTY branch.
        monkeypatch.setattr("avrea_cli.display.is_piped", lambda: False)
        # Capture click.echo into echoed list.
        monkeypatch.setattr("click.echo", lambda *a, **kw: echoed.append(a[0] if a else ""))
        page_output("hello")
        assert echoed == ["hello"]

    def test_pager_empty_disables_paging(self, runner, monkeypatch):
        monkeypatch.setenv("PAGER", "")
        # AVR_PAGER unset; PAGER='' should still disable.
        echoed: list[str] = []
        monkeypatch.setattr("click.echo_via_pager", lambda content, **kw: pytest.fail("pager should not be invoked"))
        monkeypatch.setattr("avrea_cli.display.is_piped", lambda: False)
        monkeypatch.setattr("click.echo", lambda *a, **kw: echoed.append(a[0] if a else ""))
        page_output("hello")
        assert echoed == ["hello"]

    def test_avr_pager_overrides_pager(self, runner, monkeypatch):
        """When both are set, AVR_PAGER wins. We verify the click pager call
        sees AVR_PAGER as PAGER for click's resolution, then restores PAGER."""
        monkeypatch.setenv("PAGER", "less")
        monkeypatch.setenv("AVR_PAGER", "more -R")
        observed_pager: list[str | None] = []

        def fake_pager(content, **kw):
            observed_pager.append(os.environ.get("PAGER"))

        monkeypatch.setattr("click.echo_via_pager", fake_pager)
        monkeypatch.setattr("avrea_cli.display.is_piped", lambda: False)

        page_output("body")
        assert observed_pager == ["more -R"]
        # PAGER restored to its original value
        assert os.environ.get("PAGER") == "less"

    def test_piped_output_skips_pager(self, runner, monkeypatch):
        """When the user pipes us, page_output should plain-echo even if
        PAGER is set — never page into a non-terminal sink."""
        monkeypatch.setenv("PAGER", "less")
        monkeypatch.setattr("click.echo_via_pager", lambda content, **kw: pytest.fail("pager should not be invoked"))
        monkeypatch.setattr("avrea_cli.display.is_piped", lambda: True)
        echoed: list[str] = []
        monkeypatch.setattr("click.echo", lambda *a, **kw: echoed.append(a[0] if a else ""))

        page_output("body")
        assert echoed == ["body"]
