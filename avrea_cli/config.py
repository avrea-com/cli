"""Configuration management for Avrea CLI."""

from avrea_cli import auth
from avrea_cli.version import USER_AGENT
from urllib.parse import urlsplit
import click
import os

# http:// is tolerated only for these loopback hosts, where the bearer key
# never leaves the machine. Every other host must be https:// — otherwise the
# Authorization header travels in cleartext.
_LOOPBACK_HOSTS = frozenset({"localhost", "127.0.0.1", "::1"})


def _require_secure_host(host: str) -> str:
    """Reject a resolved host that can't safely carry the API key.

    A complete ``https://`` URL is required; ``http://`` is allowed only for
    loopback. Embedded credentials (``user:pass@``) are refused so the key is
    never paired with an inline secret that could leak through a logged URL.
    Each rejection reason carries a distinct message so callers (and tests) can
    tell them apart.
    """
    # ``urlsplit`` silently strips tab/newline/CR (WHATWG) and never validates
    # the port, so a control-char or bad-port host would pass the checks below
    # and only blow up as ``httpx.InvalidURL`` at request time. That is not an
    # ``httpx.HTTPError``, so the client's handlers miss it and the user gets a
    # raw traceback. Reject both here, before the request is ever built.
    if any(ord(c) < 0x20 or ord(c) == 0x7F for c in host):
        raise click.ClickException(
            f"Refusing to use host {host!r}: control characters are not allowed in the API host."
        )
    parts = urlsplit(host)
    try:
        _ = parts.port
    except ValueError:
        raise click.ClickException(f"Refusing to use host {host!r}: the port is not a valid number.") from None
    hostname = parts.hostname
    if parts.username or parts.password:
        raise click.ClickException(
            f"Refusing to use host {host!r}: remove the embedded credentials (user:pass@) from the API host."
        )
    if parts.scheme == "https" and hostname:
        return host
    if parts.scheme == "http" and hostname in _LOOPBACK_HOSTS:
        return host
    if parts.scheme == "http" and hostname:
        raise click.ClickException(
            f"Refusing to use host {host!r}: a plain-http remote host would "
            f"send the API key in cleartext. Use https:// (http:// is allowed "
            f"only for localhost)."
        )
    raise click.ClickException(
        f"Refusing to use host {host!r}: expected a complete https:// URL such as https://api.avrea.com."
    )


class CliConfig:
    """CLI configuration sourced from env vars and ``hosts.json``.

    Env vars:
        AVR_HOST    Full URL of the Avrea API. Highest precedence; if unset
                    the CLI falls through to the file's ``default_host``,
                    then to ``https://api.avrea.com``.
        AVR_TOKEN   API key. Overrides whatever's stored for the resolved host.
        AVR_ORG     Default organization ID. Overrides the stored default.
        AVR_REPO    Repository (org/name or rep-xxx). Consumed by command-
                    level resolvers in ``repo_context``.
    """

    DEFAULT_API_URL = "https://api.avrea.com"

    def __init__(self):
        self.public_api_url = self._resolve_host()

        self.auth_token = os.getenv("AVR_TOKEN") or auth.load_token(host=self.public_api_url)
        self.default_org = os.getenv("AVR_ORG") or auth.load_default_org(host=self.public_api_url)
        # AVR_REPO is read here so a single source-of-truth lives on the
        # config; ``repo_context`` reads ``config.repo_override`` instead of
        # touching os.environ directly, which keeps tests deterministic.
        self.repo_override = os.getenv("AVR_REPO") or None

    @classmethod
    def _resolve_host(cls) -> str:
        """``AVR_HOST`` → file ``default_host`` → built-in default."""
        env = os.getenv("AVR_HOST")
        if env:
            return _require_secure_host(env.rstrip("/"))
        stored = auth.load_default_host()
        if stored:
            try:
                return _require_secure_host(stored.rstrip("/"))
            except click.ClickException as exc:
                # The bad host is the stored default in hosts.json, which is
                # resolved before any subcommand runs, so the plain message
                # would brick recovery commands too. Point at the escape hatch.
                raise click.ClickException(
                    f"{exc.message} This is your stored default host in "
                    f"hosts.json: override it by setting AVR_HOST to a valid "
                    f"https:// URL, or edit hosts.json directly."
                ) from exc
        return cls.DEFAULT_API_URL

    def get_api_headers(self) -> dict[str, str]:
        """Get headers for Avrea API requests."""
        headers = {"Content-Type": "application/json", "User-Agent": USER_AGENT}
        if self.auth_token:
            headers["Authorization"] = f"Bearer {self.auth_token}"
        return headers
