"""Shared test fixtures for CLI unit tests."""

from click.testing import CliRunner
from collections.abc import Callable
from typing import Any
import httpx
import pytest


@pytest.fixture(autouse=True)
def _disable_git_repo_detect(monkeypatch):
    """Tests run from inside the avrea-core git tree; without this, every
    command path that auto-detects from `git remote get-url origin` would
    silently fire a real API call (or hit a mocked one with the wrong shape).
    Tests that want to exercise auto-detect can override the fixture with
    their own monkeypatch."""
    monkeypatch.setattr("avrea_cli.repo_context.detect_repo_from_git", lambda: None)


@pytest.fixture(autouse=True)
def _reset_repo_hint_set(monkeypatch):
    """``repo_context._HINT_EMITTED`` is process-global so the auto-detect
    hint fires once per CLI invocation. Across tests, that turns into
    order-dependence: any future test asserting on the hint string will
    pass alone but fail after a sibling test triggered the same repo.
    Reset to a fresh empty set per test."""
    monkeypatch.setattr("avrea_cli.repo_context._HINT_EMITTED", set())


@pytest.fixture(autouse=True)
def _force_tty_mode(monkeypatch):
    """CliRunner's stdout isn't a real TTY, so list commands would default to
    piped output and break every assertion that expects rendered tables.
    Force TTY mode here; tests that exercise piped output opt back in by
    re-patching `is_piped` in the command module(s) under test.

    Each command module does `from avrea_cli.display import is_piped`, so the
    name is bound at import time — patching `display.is_piped` afterwards
    doesn't affect those local bindings. Patch each consumer instead."""
    for mod in (
        "avrea_cli.commands.run",
        "avrea_cli.commands.job",
        "avrea_cli.commands.cache",
        "avrea_cli.commands.pr",
        "avrea_cli.commands.token",
    ):
        monkeypatch.setattr(f"{mod}.is_piped", lambda: False, raising=False)


@pytest.fixture
def runner(monkeypatch):
    """CliRunner with auth pre-set. Used by the bulk of the test suite —
    tests that need extra patches (slug resolution, sleep stubs, page_output
    overrides, etc.) define their own ``runner`` fixture which shadows this."""
    monkeypatch.setenv("AVR_TOKEN", "test-token")
    monkeypatch.setenv("AVR_ORG", "org-default")
    monkeypatch.delenv("AVR_HOST", raising=False)
    monkeypatch.setattr("avrea_cli.auth.load_token", lambda *, host: None)
    monkeypatch.setattr("avrea_cli.auth.load_default_org", lambda *, host: None)
    # Without this, CliConfig._resolve_host would read the developer's
    # hosts.json and tests would silently depend on local state.
    monkeypatch.setattr("avrea_cli.auth.load_default_host", lambda: None)
    return CliRunner()


class FakeApi:
    """Transport-level API double. Records each request as sent (method, URL,
    headers, body) and answers from a per-route queue, for tests that assert
    on what reaches the wire rather than on ``ApiClient`` call arguments."""

    def __init__(self) -> None:
        self.requests: list[httpx.Request] = []
        self._routes: dict[tuple[str, str], list[Callable[[], httpx.Response]]] = {}

    def reply(
        self, method: str, path: str, status: int = 200, *, json: Any = None, headers: dict[str, str] | None = None
    ) -> None:
        """Queue a response for ``method path``. Responses are served in the
        order queued; the last one repeats."""
        self._routes.setdefault((method, path), []).append(lambda: httpx.Response(status, json=json, headers=headers))

    def __call__(self, request: httpx.Request) -> httpx.Response:
        self.requests.append(request)
        queue = self._routes.get((request.method, request.url.path))
        if not queue:
            return httpx.Response(501, json={"detail": f"no fake reply for {request.method} {request.url.path}"})
        return (queue.pop(0) if len(queue) > 1 else queue[0])()

    def sent(self, method: str, path: str) -> list[httpx.Request]:
        """The recorded requests for ``method path``, in order."""
        return [r for r in self.requests if r.method == method and r.url.path == path]


@pytest.fixture
def api(monkeypatch) -> FakeApi:
    """Route every request the CLI sends through a :class:`FakeApi`."""
    fake = FakeApi()
    monkeypatch.setattr("avrea_cli.api_client.CompressingTransport", lambda: httpx.MockTransport(fake))
    return fake
