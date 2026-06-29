"""Unit tests for `CliConfig` host resolution."""

from avrea_cli.config import CliConfig
import click
import pytest


@pytest.fixture(autouse=True)
def _no_stored_host(monkeypatch):
    monkeypatch.setattr("avrea_cli.auth.load_default_host", lambda: None)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.delenv("AVR_TOKEN", raising=False)
    monkeypatch.delenv("AVR_ORG", raising=False)


class TestResolveHost:
    def test_defaults_to_https_api_host(self):
        assert CliConfig().public_api_url == "https://api.avrea.com"

    def test_empty_env_host_falls_through_to_default(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "")
        assert CliConfig().public_api_url == "https://api.avrea.com"

    def test_https_host_accepted_and_trailing_slash_stripped(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "https://api.example.com/")
        assert CliConfig().public_api_url == "https://api.example.com"

    def test_stored_https_host_accepted_and_trailing_slash_stripped(self, monkeypatch):
        monkeypatch.setattr("avrea_cli.auth.load_default_host", lambda: "https://stored.example.com/")
        assert CliConfig().public_api_url == "https://stored.example.com"

    def test_http_localhost_accepted(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "http://localhost:8080")
        assert CliConfig().public_api_url == "http://localhost:8080"

    def test_http_loopback_ip_accepted(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "http://127.0.0.1:8080")
        assert CliConfig().public_api_url == "http://127.0.0.1:8080"

    def test_http_ipv6_loopback_accepted(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "http://[::1]:8080")
        assert CliConfig().public_api_url == "http://[::1]:8080"

    def test_https_scheme_is_case_insensitive(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "HTTPS://api.example.com")
        assert CliConfig().public_api_url == "HTTPS://api.example.com"

    def test_http_localhost_is_case_insensitive(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "http://LOCALHOST:8080")
        assert CliConfig().public_api_url == "http://LOCALHOST:8080"


class TestRejectInsecureHost:
    def test_scheme_only_https_rejected(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "https://")
        with pytest.raises(click.ClickException, match="complete https"):
            CliConfig()

    def test_https_without_authority_rejected(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "https:api.example.com")
        with pytest.raises(click.ClickException, match="complete https"):
            CliConfig()

    def test_schemeless_host_rejected(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "api.example.com")
        with pytest.raises(click.ClickException, match="complete https"):
            CliConfig()

    def test_whitespace_host_rejected(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", " ")
        with pytest.raises(click.ClickException, match="complete https"):
            CliConfig()

    @pytest.mark.parametrize("host", ["ftp://api.example.com", "file:///etc/hosts"])
    def test_non_http_scheme_rejected(self, monkeypatch, host):
        monkeypatch.setenv("AVR_HOST", host)
        with pytest.raises(click.ClickException, match="complete https"):
            CliConfig()

    def test_http_remote_host_rejected(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "http://api.example.com")
        with pytest.raises(click.ClickException, match="cleartext"):
            CliConfig()

    @pytest.mark.parametrize("host", ["http://127.0.0.2:8080", "http://[0:0:0:0:0:0:0:1]:8080"])
    def test_http_non_loopback_address_rejected(self, monkeypatch, host):
        monkeypatch.setenv("AVR_HOST", host)
        with pytest.raises(click.ClickException, match="cleartext"):
            CliConfig()

    def test_https_embedded_credentials_rejected(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "https://user:pass@api.example.com")
        with pytest.raises(click.ClickException, match="embedded credentials"):
            CliConfig()

    def test_loopback_with_userinfo_rejected(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "http://evil.com@localhost:8080")
        with pytest.raises(click.ClickException, match="embedded credentials"):
            CliConfig()

    def test_invalid_port_rejected(self, monkeypatch):
        monkeypatch.setenv("AVR_HOST", "https://api.example.com:notaport")
        with pytest.raises(click.ClickException, match="port"):
            CliConfig()

    @pytest.mark.parametrize("host", ["https://api.example.com\tx", "https://api.example.com\nevil.com"])
    def test_control_char_host_rejected(self, monkeypatch, host):
        monkeypatch.setenv("AVR_HOST", host)
        with pytest.raises(click.ClickException, match="control characters"):
            CliConfig()


class TestStoredHostRecovery:
    def test_insecure_stored_host_rejected_with_recovery_hint(self, monkeypatch):
        monkeypatch.setattr("avrea_cli.auth.load_default_host", lambda: "http://api.example.com")
        with pytest.raises(click.ClickException) as excinfo:
            CliConfig()
        message = excinfo.value.message
        assert "cleartext" in message
        assert "hosts.json" in message
        assert "AVR_HOST" in message
