"""Unit tests for repo_context — resolve_repo and git remote parsing."""

from avrea_cli.config import CliConfig
from avrea_cli.helpers import EXIT_AUTH_REQUIRED
from avrea_cli.repo_context import detect_repo_from_git
from avrea_cli.repo_context import parse_remote_url
from avrea_cli.repo_context import resolve_repo
from avrea_cli.repo_context import resolve_repo_or_detect
from avrea_cli.repo_context import resolve_repos_or_detect
from unittest.mock import MagicMock
import click
import httpx
import pytest


@pytest.fixture()
def config(monkeypatch):
    monkeypatch.setenv("AVR_TOKEN", "test-token")
    monkeypatch.setenv("AVR_ORG", "org-default")
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    return CliConfig()


def _http_404() -> httpx.HTTPStatusError:
    request = httpx.Request("GET", "https://api.example.com/x")
    response = httpx.Response(404, request=request, json={"detail": "not found"})
    return httpx.HTTPStatusError("not found", request=request, response=response)


def _http_404_with_detail(detail: dict) -> httpx.HTTPStatusError:
    """404 carrying a structured detail body (as the resolve endpoint emits)."""
    request = httpx.Request("GET", "https://api.example.com/x")
    response = httpx.Response(404, request=request, json={"detail": detail})
    return httpx.HTTPStatusError("not found", request=request, response=response)


class TestResolveRepo:
    """Pin the wire-level contract: APIResponse(data=...) envelope unwrap."""

    def test_repo_id_passthrough(self, config):
        """rep-xxx IDs are returned as-is without API calls."""
        client = MagicMock()
        result = resolve_repo(client, config, "org-1", "rep-abc123")
        assert result == "rep-abc123"
        client.public_get.assert_not_called()

    def test_unwraps_data_envelope(self, config):
        """The /repos/resolve endpoint returns {data: {...}} — the unwrap regression
        was that we read result['repository_id'] directly, which crashed at runtime."""
        client = MagicMock()
        client.public_get.return_value = {
            "data": {
                "repository_id": "rep-from-resolve",
                "full_name": "acme/cool",
                "platform": "github",
            }
        }
        result = resolve_repo(client, config, "org-1", "acme/cool")
        assert result == "rep-from-resolve"
        client.public_get.assert_called_once_with("/orgs/org-1/repos/resolve", params={"name": "acme/cool"})

    def test_invalid_format_aborts(self, config):
        """Without a slash, the resolver bails before any API call."""
        client = MagicMock()
        with pytest.raises(click.Abort):
            resolve_repo(client, config, "org-1", "no-slash-here")
        client.public_get.assert_not_called()

    def test_not_found_aborts(self, config):
        """404 → single round-trip; resolver renders the structured 404 body
        the server now provides instead of doing a client-side list-and-filter."""
        client = MagicMock()
        client.public_get.side_effect = _http_404_with_detail(
            {
                "message": "not found",
                "nearby_full_names": [],
                "other_orgs": [],
            }
        )
        with pytest.raises(click.Abort):
            resolve_repo(client, config, "org-1", "acme/missing")
        # Resolver must NOT make a second list-and-filter call now that
        # the 404 body is self-describing.
        client.public_get.assert_called_once()

    def test_404_renders_nearby_and_other_orgs(self, config, capsys):
        """The structured 404 body's hints reach stderr verbatim — no second
        round-trip, no client-side typo logic."""
        client = MagicMock()
        client.public_get.side_effect = _http_404_with_detail(
            {
                "message": "not found",
                "nearby_full_names": ["acme/foobar", "acme/foobaz"],
                "other_orgs": [{"organization_id": "org-2", "slug": "secondary", "name": "Secondary"}],
            }
        )
        with pytest.raises(click.Abort):
            resolve_repo(client, config, "org-1", "acme/fooba")
        err = capsys.readouterr().err
        assert "acme/foobar" in err and "acme/foobaz" in err
        assert "secondary" in err


class TestParseRemoteUrl:
    """Cover the git remote URL formats the Avrea CLI needs to support."""

    @pytest.mark.parametrize(
        "url",
        [
            # Three syntactic shapes — SCP-like, https, ssh — exercise each
            # branch of the parser. The git+ prefix and trailing-.git toggles
            # are mechanical regex variations; not worth a row each.
            "git@github.com:acme/cool.git",
            "https://github.com/acme/cool.git",
            "ssh://git@github.com/acme/cool",
        ],
    )
    def test_known_formats(self, url):
        assert parse_remote_url(url) == "acme/cool"

    @pytest.mark.parametrize(
        "url",
        [
            "",
            "not a url",
            "https://github.com/acme",  # missing repo
            "https://github.com/acme/cool/extra",  # too many segments
            "ftp://github.com/acme/cool",  # unsupported scheme
        ],
    )
    def test_rejects_invalid(self, url):
        assert parse_remote_url(url) is None


class TestDetectRepoFromGit:
    """The git invocation is mocked — we only assert on the URL → owner/repo step."""

    def test_returns_none_when_git_fails(self, monkeypatch):
        result_obj = MagicMock(returncode=1, stdout="")
        monkeypatch.setattr("subprocess.run", lambda *a, **kw: result_obj)
        assert detect_repo_from_git() is None

    def test_parses_origin_remote(self, monkeypatch):
        result_obj = MagicMock(returncode=0, stdout="git@github.com:acme/cool.git\n")
        monkeypatch.setattr("subprocess.run", lambda *a, **kw: result_obj)
        assert detect_repo_from_git() == "acme/cool"


class TestResolveRepoOrDetect:
    """Single-repo helper used by cache/log/workflow_run/settings/status."""

    def test_explicit_flag_skips_git(self, config, monkeypatch):
        client = MagicMock()
        client.public_get.return_value = {"data": {"repository_id": "rep-from-flag"}}

        # Even if git would resolve, an explicit flag wins.
        def _git_should_not_run() -> str | None:
            raise AssertionError("detect_repo_from_git should not run when --repo is passed")

        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", _git_should_not_run)
        result = resolve_repo_or_detect(client, config, "org-1", "acme/explicit")
        assert result == "rep-from-flag"

    def test_falls_back_to_git_remote(self, config, monkeypatch):
        client = MagicMock()
        client.public_get.return_value = {"data": {"repository_id": "rep-from-git"}}
        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/cool")
        result = resolve_repo_or_detect(client, config, "org-1", None)
        assert result == "rep-from-git"

    def test_returns_none_when_optional_and_no_git(self, config, monkeypatch):
        """Optional callers (status, settings) get None — they decide what to do."""
        client = MagicMock()
        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: None)
        result = resolve_repo_or_detect(client, config, "org-1", None)
        assert result is None
        client.public_get.assert_not_called()

    def test_required_raises_when_no_git_and_no_flag(self, config, monkeypatch):
        client = MagicMock()
        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: None)
        with pytest.raises(click.ClickException, match="Could not determine repository"):
            resolve_repo_or_detect(client, config, "org-1", None, required=True)

    def test_soft_detect_falls_back_to_none_when_repo_not_in_org(self, config, monkeypatch, capsys):
        """`avr status` / `avr settings list` UX: if you're inside a git
        checkout the org doesn't track, the auto-detect should silently fall
        back to org-wide rather than aborting the command."""
        client = MagicMock()
        # Resolve endpoint 404s, list returns no matches → _RepoNotInOrgError.
        client.public_get.side_effect = [
            _http_404(),  # /orgs/{org}/repos/resolve
            {"data": [{"repository_id": "rep-other", "full_name": "acme/other"}]},
        ]
        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/foreign")
        result = resolve_repo_or_detect(client, config, "org-1", None)
        assert result is None
        err = capsys.readouterr().err
        assert "isn't in this org" in err
        assert "acme/foreign" in err

    def test_soft_detect_propagates_auth_failure_instead_of_falling_back(self, config, monkeypatch, capsys):
        """A non-404 status (auth/transport/server error) on the resolve
        endpoint must NOT be silently converted to "repo not in org" — that
        would route the user into degraded org-wide queries with broken
        auth and no diagnostic. A 401 here earns the same auth hint and exit 4
        as every other API call, so scripts can trigger `avr auth login`."""
        client = MagicMock()
        request = httpx.Request("GET", "https://api.example.com/x")
        response = httpx.Response(401, request=request, json={"detail": "unauthorized"})
        client.public_get.side_effect = httpx.HTTPStatusError("unauthorized", request=request, response=response)
        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/foreign")
        with pytest.raises(SystemExit) as exc_info:
            resolve_repo_or_detect(client, config, "org-1", None)
        assert exc_info.value.code == EXIT_AUTH_REQUIRED
        err = capsys.readouterr().err
        assert "avr auth login" in err
        assert "isn't in this org" not in err

    def test_server_error_reports_status_not_httpx_internals(self, config, monkeypatch, capsys):
        """Repo resolution used to render httpx's own exception string, which
        carries a two-line message and an MDN link. The user gets the CLI's
        error vocabulary instead, like every other failing API call."""
        client = MagicMock()
        request = httpx.Request("GET", "https://api.example.com/x")
        response = httpx.Response(503, request=request, json={"detail": "upstream down"})
        client.public_get.side_effect = httpx.HTTPStatusError("unavailable", request=request, response=response)
        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/foreign")
        with pytest.raises(SystemExit) as exc_info:
            resolve_repo_or_detect(client, config, "org-1", None)
        assert exc_info.value.code == 1
        err = capsys.readouterr().err
        assert "Error: Avrea is having trouble (HTTP 503)" in err
        assert "  Detail: upstream down" in err
        assert "developer.mozilla.org" not in err


class TestResolveReposOrDetect:
    """Multi-repo helper used by run/job/workflow list."""

    def test_returns_resolved_explicit_list(self, config, monkeypatch):
        client = MagicMock()
        client.public_get.return_value = {"data": {"repository_id": "rep-x"}}
        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "should/not-be-used")
        result = resolve_repos_or_detect(client, config, "org-1", ("rep-a", "rep-b"))
        assert result == ["rep-a", "rep-b"]  # rep-xxx forms pass through

    def test_auto_detects_when_empty_and_in_git(self, config, monkeypatch):
        client = MagicMock()
        client.public_get.return_value = {"data": {"repository_id": "rep-auto"}}
        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: "acme/cool")
        result = resolve_repos_or_detect(client, config, "org-1", ())
        assert result == ["rep-auto"]

    def test_returns_empty_when_no_repos_and_not_in_git(self, config, monkeypatch):
        """Outside a git tree, the list-style commands list everything."""
        client = MagicMock()
        monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: None)
        result = resolve_repos_or_detect(client, config, "org-1", ())
        assert result == []
        client.public_get.assert_not_called()
