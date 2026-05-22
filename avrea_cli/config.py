"""Configuration management for Avrea CLI."""

from avrea_cli import auth
from avrea_cli.version import USER_AGENT
import os


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
            return env.rstrip("/")
        stored = auth.load_default_host()
        if stored:
            return stored.rstrip("/")
        return cls.DEFAULT_API_URL

    def get_api_headers(self) -> dict[str, str]:
        """Get headers for Avrea API requests."""
        headers = {"Content-Type": "application/json", "User-Agent": USER_AGENT}
        if self.auth_token:
            headers["Authorization"] = f"Bearer {self.auth_token}"
        return headers
