"""Shared test fixtures for CLI unit tests."""

from click.testing import CliRunner
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
