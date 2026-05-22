"""Tests for the Avrea CLI API client and configuration."""

from avrea_cli.api_client import ApiClient
from avrea_cli.config import CliConfig


def test_config_defaults(monkeypatch) -> None:
    """CliConfig should use the default production URL when no env vars are set."""
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.delenv("AVR_TOKEN", raising=False)
    monkeypatch.delenv("AVR_ORG", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    # Without this, a developer-local hosts.json would override the
    # default and the URL assertion below would fail per machine.
    monkeypatch.setattr("avrea_cli.auth.load_default_host", lambda: None)

    config = CliConfig()

    assert config.public_api_url == "https://api.avrea.com"


def test_config_from_env(monkeypatch) -> None:
    """CliConfig should use AVR_HOST when set."""
    monkeypatch.setenv("AVR_HOST", "https://api.example.com")
    monkeypatch.delenv("AVR_TOKEN", raising=False)
    monkeypatch.delenv("AVR_ORG", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)

    config = CliConfig()

    assert config.public_api_url == "https://api.example.com"


def test_config_strips_trailing_slash(monkeypatch) -> None:
    """CliConfig should strip trailing slashes from the API URL."""
    monkeypatch.setenv("AVR_HOST", "https://api.example.com/")
    monkeypatch.delenv("AVR_TOKEN", raising=False)
    monkeypatch.delenv("AVR_ORG", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)

    config = CliConfig()

    assert config.public_api_url == "https://api.example.com"


def test_api_headers_without_auth(monkeypatch) -> None:
    """API headers should include Content-Type but no auth when unauthenticated."""
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.delenv("AVR_TOKEN", raising=False)
    monkeypatch.delenv("AVR_ORG", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)

    config = CliConfig()
    headers = config.get_api_headers()

    assert headers["Content-Type"] == "application/json"
    assert "Authorization" not in headers


def test_api_headers_with_auth(monkeypatch) -> None:
    """API headers should include Bearer token when authenticated."""
    monkeypatch.setenv("AVR_TOKEN", "test-token-123")
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.delenv("AVR_ORG", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)

    config = CliConfig()
    headers = config.get_api_headers()

    assert headers["Authorization"] == "Bearer test-token-123"


def test_api_client_timeout(monkeypatch) -> None:
    """ApiClient should have a default timeout configured."""
    monkeypatch.delenv("AVR_TOKEN", raising=False)
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.delenv("AVR_ORG", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)

    config = CliConfig()
    client = ApiClient(config)

    assert client.timeout == 30.0


def _isolate_config(monkeypatch) -> None:
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.delenv("AVR_TOKEN", raising=False)
    monkeypatch.delenv("AVR_ORG", raising=False)
    monkeypatch.delenv("AVR_REPO", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_host", lambda: None)


def test_host_default_when_unset(monkeypatch) -> None:
    """No AVR_HOST, no stored default → fall back to the built-in URL."""
    _isolate_config(monkeypatch)
    monkeypatch.setattr("avrea_cli.auth.load_default_host", lambda: None)
    config = CliConfig()
    assert config.public_api_url == "https://api.avrea.com"


def test_host_uses_stored_default(monkeypatch) -> None:
    """File default_host (set on first login or via `auth switch`) wins
    over the built-in default. AVR_HOST still beats it."""
    _isolate_config(monkeypatch)
    monkeypatch.setattr("avrea_cli.auth.load_default_host", lambda: "https://api.example.com")
    config = CliConfig()
    assert config.public_api_url == "https://api.example.com"


def test_host_env_beats_stored_default(monkeypatch) -> None:
    """AVR_HOST is the explicit override even when default_host is set."""
    _isolate_config(monkeypatch)
    monkeypatch.setattr("avrea_cli.auth.load_default_host", lambda: "https://api.example.com")
    monkeypatch.setenv("AVR_HOST", "https://api.other.com")
    config = CliConfig()
    assert config.public_api_url == "https://api.other.com"


def test_token_env_overrides_stored(monkeypatch) -> None:
    """AVR_TOKEN wins over whatever's in hosts.json (CI / scripted use)."""
    _isolate_config(monkeypatch)
    monkeypatch.setenv("AVR_TOKEN", "from-env")
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: "from-disk")
    config = CliConfig()
    assert config.auth_token == "from-env"


def test_token_falls_back_to_stored(monkeypatch) -> None:
    _isolate_config(monkeypatch)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: "from-disk")
    config = CliConfig()
    assert config.auth_token == "from-disk"


def test_repo_override_from_env(monkeypatch) -> None:
    """AVR_REPO is a string the resolver consumes; CliConfig just surfaces it."""
    _isolate_config(monkeypatch)
    monkeypatch.setenv("AVR_REPO", "acme/svc")
    config = CliConfig()
    assert config.repo_override == "acme/svc"


def test_repo_override_unset_is_none(monkeypatch) -> None:
    _isolate_config(monkeypatch)
    config = CliConfig()
    assert config.repo_override is None
