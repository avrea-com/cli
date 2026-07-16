"""SSO routing for login: `--email` sends SSO-enforced domains to their IdP,
and an OAuth attempt the control plane bounces explains how to retry."""

from avrea_cli import auth
from avrea_cli.main import cli
from click.testing import CliRunner
import click
import httpx
import pytest


def _stub_post(payload):
    def _post(url, **kwargs):
        return httpx.Response(200, json=payload, request=httpx.Request("POST", url))

    return _post


class TestSsoDiscovery:
    def test_returns_login_path_for_enforced_domain(self, monkeypatch):
        monkeypatch.setattr(
            auth.httpx,
            "post",
            _stub_post({"method": "saml", "saml_login_path": "/saml/acme/login"}),
        )
        path = auth.discover_sso_login_path("https://api.example.com", "alice@acme.com")
        assert path == "/saml/acme/login"

    def test_returns_none_when_domain_uses_oauth(self, monkeypatch):
        monkeypatch.setattr(auth.httpx, "post", _stub_post({"method": "oauth"}))
        assert auth.discover_sso_login_path("https://api.example.com", "alice@gmail.com") is None

    def test_email_travels_in_body_not_url(self, monkeypatch):
        """The endpoint takes the email in the POST body specifically to keep
        it out of access logs — don't undo that from this side."""
        seen = {}

        def _post(url, **kwargs):
            seen["url"] = url
            seen["json"] = kwargs.get("json")
            return httpx.Response(200, json={"method": "oauth"}, request=httpx.Request("POST", url))

        monkeypatch.setattr(auth.httpx, "post", _post)
        auth.discover_sso_login_path("https://api.example.com", "alice@acme.com")

        assert seen["json"] == {"email": "alice@acme.com"}
        assert "alice@acme.com" not in seen["url"]

    def test_saml_without_a_path_is_an_error(self, monkeypatch):
        monkeypatch.setattr(auth.httpx, "post", _stub_post({"method": "saml"}))
        with pytest.raises(click.ClickException, match="no login path"):
            auth.discover_sso_login_path("https://api.example.com", "alice@acme.com")

    def test_unreachable_api_is_an_error(self, monkeypatch):
        def _post(url, **kwargs):
            raise httpx.ConnectError("connection refused")

        monkeypatch.setattr(auth.httpx, "post", _post)
        with pytest.raises(click.ClickException, match="check for SSO"):
            auth.discover_sso_login_path("https://api.example.com", "alice@acme.com")

    @pytest.mark.parametrize("payload", [[], None, "saml"], ids=["list", "null", "string"])
    def test_non_object_json_is_an_error_not_a_crash(self, monkeypatch, payload):
        """Valid JSON of the wrong shape (a proxy returning null, say) must
        surface as the same clean message, not an AttributeError."""
        monkeypatch.setattr(auth.httpx, "post", _stub_post(payload))
        with pytest.raises(click.ClickException, match="Unexpected SSO discovery response"):
            auth.discover_sso_login_path("https://api.example.com", "alice@acme.com")


class TestAuthUrl:
    def test_saml_path_wins_over_provider(self):
        url = auth._auth_url(
            "https://api.example.com",
            "http://localhost:8765/callback",
            provider="github",
            saml_login_path="/saml/acme/login",
        )
        assert url == "https://api.example.com/saml/acme/login?next=http%3A%2F%2Flocalhost%3A8765%2Fcallback"

    def test_github_and_google_keep_their_oauth_urls(self):
        github = auth._auth_url(
            "https://api.example.com", "http://localhost:8765/callback", provider="github", saml_login_path=None
        )
        google = auth._auth_url(
            "https://api.example.com", "http://localhost:8765/callback", provider="google", saml_login_path=None
        )
        assert github == "https://api.example.com/oauth/github/login?next=http://localhost:8765/callback"
        assert google == "https://api.example.com/oauth/google/login?final_redirect=http://localhost:8765/callback"

    def test_unsupported_provider_raises(self):
        with pytest.raises(click.ClickException, match="Unsupported auth provider"):
            auth._auth_url(
                "https://api.example.com", "http://localhost:8765/callback", provider="wat", saml_login_path=None
            )


class TestSsoRequiredBounce:
    """An SSO-enforced domain that tries OAuth lands back with sso_required and
    no session. The generic 'no session received' is a dead end, so point at the
    flag that actually works."""

    @pytest.fixture()
    def bounced_server(self, monkeypatch):
        class _FakeServer:
            timeout = None

            def __init__(self, address, handler):
                pass

            def handle_request(self):
                auth.OAuthCallbackHandler.sso_required_slug = "acme"

            def server_close(self):
                pass

        monkeypatch.setattr(auth, "HTTPServer", _FakeServer)
        monkeypatch.setattr(auth.webbrowser, "open", lambda url: True)

    def test_names_the_org_and_the_retry(self, bounced_server, capsys):
        with pytest.raises(click.Abort):
            auth.login("https://api.example.com", provider="github")

        err = capsys.readouterr().err
        assert "acme requires single sign-on" in err
        assert "avr auth login --email" in err
        assert "no session received" not in err


class TestLoginRoutesByDiscovery:
    """Whole flow through login(): what discovery says decides where the
    browser actually goes, and the SAML landing is exchanged for a key like
    any other."""

    @pytest.fixture()
    def flow(self, monkeypatch):
        opened: list[str] = []

        def _install(discovery):
            def _post(url, **kwargs):
                if url.endswith("/sso/discover"):
                    payload = discovery
                elif url.endswith("/users/me/api-keys"):
                    payload = {"api_key": "ak-saml123"}
                else:  # best-effort logout of the short-lived session
                    payload = {}
                return httpx.Response(200, json=payload, request=httpx.Request("POST", url))

            class _FakeServer:
                timeout = None

                def __init__(self, address, handler):
                    pass

                def handle_request(self):
                    auth.OAuthCallbackHandler.session_cookie = "ses-landed"
                    auth.OAuthCallbackHandler.csrf_token = "csrf-landed"

                def server_close(self):
                    pass

            monkeypatch.setattr(auth.httpx, "post", _post)
            monkeypatch.setattr(auth, "HTTPServer", _FakeServer)
            monkeypatch.setattr(auth.webbrowser, "open", lambda url: bool(opened.append(url)) or True)
            return opened

        return _install

    def test_enforced_domain_opens_the_idp_and_returns_a_key(self, flow):
        opened = flow({"method": "saml", "saml_login_path": "/saml/acme/login"})

        api_key = auth.login("https://api.example.com", provider="github", email="alice@acme.com")

        assert api_key == "ak-saml123"
        assert opened == ["https://api.example.com/saml/acme/login?next=http%3A%2F%2Flocalhost%3A8765%2Fcallback"]

    def test_oauth_domain_falls_back_to_the_provider(self, flow):
        """--email is only a routing hint: a domain without SSO still goes
        through the OAuth provider."""
        opened = flow({"method": "oauth"})

        api_key = auth.login("https://api.example.com", provider="github", email="alice@gmail.com")

        assert api_key == "ak-saml123"
        assert opened == ["https://api.example.com/oauth/github/login?next=http://localhost:8765/callback"]


class TestLoginCommandWiring:
    def test_email_flag_reaches_auth_login(self, monkeypatch):
        seen = {}

        def _login(public_api_url, *, provider, email=None):
            seen["provider"] = provider
            seen["email"] = email
            return "ak-test123"

        monkeypatch.setenv("AVR_TOKEN", "test-token")
        monkeypatch.delenv("AVR_HOST", raising=False)
        monkeypatch.setattr("avrea_cli.auth.login", _login)
        monkeypatch.setattr("avrea_cli.auth.store_token", lambda *a, **kw: None)
        monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: "org-1")
        monkeypatch.setattr("avrea_cli.commands.auth_cmd._fetch_email", lambda url, key: "alice@acme.com")

        result = CliRunner().invoke(cli, ["auth", "login", "--email", "alice@acme.com"])

        assert result.exit_code == 0, result.output
        assert seen == {"provider": "github", "email": "alice@acme.com"}
