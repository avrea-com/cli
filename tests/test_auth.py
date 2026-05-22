"""Tests for credential storage and retrieval (host-keyed hosts.json)."""

from avrea_cli import auth
import json
import os
import pytest
import stat


@pytest.fixture(autouse=True)
def _isolate_hosts(tmp_path, monkeypatch):
    """Point HOSTS_FILE at a temp directory for every test."""
    hosts_file = tmp_path / "hosts.json"
    monkeypatch.setattr(auth, "HOSTS_FILE", hosts_file)


# -- store / load tokens round-trip --


def test_store_and_load_token():
    auth.store_token("key-abc", host="api.avrea.com")
    assert auth.load_token(host="api.avrea.com") == "key-abc"


def test_store_multiple_hosts_independent():
    """Each host stores its own token; they don't bleed into each other."""
    auth.store_token("key-prod", host="api.avrea.com")
    auth.store_token("key-staging", host="api.staging.example.com")

    assert auth.load_token(host="api.avrea.com") == "key-prod"
    assert auth.load_token(host="api.staging.example.com") == "key-staging"


def test_store_token_overwrites_same_host():
    auth.store_token("old", host="api.avrea.com")
    auth.store_token("new", host="api.avrea.com")

    assert auth.load_token(host="api.avrea.com") == "new"


def test_load_token_no_file_returns_none():
    """Missing hosts.json is the same as 'never logged in'."""
    assert auth.load_token(host="api.avrea.com") is None


def test_load_token_unknown_host_returns_none():
    """Asking for a host you've never logged into returns None — this is the
    signal CliConfig uses to fall through to AVR_TOKEN / show the auth hint."""
    auth.store_token("key", host="api.avrea.com")
    assert auth.load_token(host="api.other.com") is None


def test_load_token_malformed_file_returns_none(tmp_path, monkeypatch):
    """A garbled file shouldn't crash subsequent commands. Returning None
    lets the user re-auth without manually deleting the file."""
    hosts_file = tmp_path / "hosts.json"
    hosts_file.write_text("not json {")
    monkeypatch.setattr(auth, "HOSTS_FILE", hosts_file)
    assert auth.load_token(host="api.avrea.com") is None


def test_load_token_top_level_not_dict_returns_none(tmp_path, monkeypatch):
    """A JSON value that parses but isn't a dict (e.g. an array) is treated
    the same as malformed — defends against half-written files."""
    hosts_file = tmp_path / "hosts.json"
    hosts_file.write_text("[]")
    monkeypatch.setattr(auth, "HOSTS_FILE", hosts_file)
    assert auth.load_token(host="api.avrea.com") is None


def test_load_token_missing_hosts_wrapper_returns_none(tmp_path, monkeypatch):
    """A file without the ``hosts`` wrapper (e.g. someone hand-edited the
    flat pre-migration shape) reads as empty rather than crashing."""
    hosts_file = tmp_path / "hosts.json"
    hosts_file.write_text(json.dumps({"api.avrea.com": {"token": "k"}}))
    monkeypatch.setattr(auth, "HOSTS_FILE", hosts_file)
    assert auth.load_token(host="api.avrea.com") is None


def test_store_token_preserves_default_org():
    """Saving a new token must NOT clobber an existing default_org for the
    same host — common after `avr config set org` then re-auth."""
    auth.store_token("k1", host="h1")
    auth.store_default_org("org-1", host="h1")
    auth.store_token("k2", host="h1")
    assert auth.load_default_org(host="h1") == "org-1"
    assert auth.load_token(host="h1") == "k2"


# -- store / load default_org round-trip --


def test_store_and_load_default_org():
    auth.store_default_org("org-1", host="api.avrea.com")
    assert auth.load_default_org(host="api.avrea.com") == "org-1"


def test_load_default_org_no_file_returns_none():
    assert auth.load_default_org(host="api.avrea.com") is None


def test_load_default_org_unknown_host_returns_none():
    auth.store_default_org("org-1", host="api.avrea.com")
    assert auth.load_default_org(host="api.other.com") is None


# -- clear --


def test_clear_removes_host_entry():
    auth.store_token("k", host="api.avrea.com")
    auth.store_default_org("org-1", host="api.avrea.com")

    assert auth.clear(host="api.avrea.com") is True
    assert auth.load_token(host="api.avrea.com") is None
    assert auth.load_default_org(host="api.avrea.com") is None


def test_clear_only_removes_targeted_host():
    auth.store_token("k1", host="h1")
    auth.store_token("k2", host="h2")

    auth.clear(host="h1")
    assert auth.load_token(host="h1") is None
    assert auth.load_token(host="h2") == "k2"


def test_clear_no_file_returns_false():
    assert auth.clear(host="api.avrea.com") is False


def test_clear_unknown_host_returns_false():
    auth.store_token("k", host="h1")
    assert auth.clear(host="h2") is False


def test_clear_last_host_unlinks_file(tmp_path, monkeypatch):
    """When the only stored host is removed, hosts.json is unlinked so a
    fresh `auth login` starts from a clean state instead of an empty {}."""
    hosts_file = tmp_path / "hosts.json"
    monkeypatch.setattr(auth, "HOSTS_FILE", hosts_file)
    auth.store_token("k", host="h1")
    assert hosts_file.exists()
    auth.clear(host="h1")
    assert not hosts_file.exists()


# -- file permissions --


# POSIX mode bits don't translate on Windows — os.chmod only handles the
# read-only flag there, so a 0o600 chmod ends up as 0o666 in stat. The Windows
# story (ACL-based restriction via pywin32 / icacls) is a follow-up; until then
# these tests assert the POSIX guarantee on POSIX platforms only.
@pytest.mark.skipif(os.name != "posix", reason="POSIX mode bits not enforced on Windows")
def test_hosts_file_has_secure_permissions():
    """0600: protect the bearer token from group / other reads."""
    auth.store_token("key", host="api.avrea.com")
    mode = stat.S_IMODE(auth.HOSTS_FILE.stat().st_mode)
    assert mode == 0o600


@pytest.mark.skipif(os.name != "posix", reason="POSIX mode bits not enforced on Windows")
def test_store_default_org_preserves_secure_permissions():
    auth.store_token("key", host="h")
    auth.store_default_org("org", host="h")
    mode = stat.S_IMODE(auth.HOSTS_FILE.stat().st_mode)
    assert mode == 0o600


# -- on-disk shape --


def test_hosts_file_shape():
    """Pin the gh-style structure so consumers (avr-admin, third-party
    tooling) can rely on the {default_host, hosts: {host: {...}}} layout."""
    auth.store_token("key-abc", host="https://api.avrea.com")
    auth.store_default_org("org-xyz", host="https://api.avrea.com")

    payload = json.loads(auth.HOSTS_FILE.read_text())
    assert payload == {
        "default_host": "https://api.avrea.com",
        "hosts": {
            "https://api.avrea.com": {"token": "key-abc", "default_org": "org-xyz"},
        },
    }


# -- default_host --


def test_default_host_set_on_first_login():
    """First `store_token` promotes its host to default — a one-host install
    needs no `auth switch` to be usable."""
    assert auth.load_default_host() is None
    auth.store_token("k", host="https://api.avrea.com")
    assert auth.load_default_host() == "https://api.avrea.com"


def test_default_host_not_changed_by_subsequent_logins():
    """Adding a second host doesn't silently re-point the default — that
    would surprise anyone who logged in for a one-off check against staging."""
    auth.store_token("k1", host="https://api.avrea.com")
    auth.store_token("k2", host="https://api.staging.example.com")
    assert auth.load_default_host() == "https://api.avrea.com"


def test_set_default_host_requires_existing_entry():
    """Pinning a host that has no stored creds is meaningless — refuse so
    the file can't end up pointing at a nonexistent entry."""
    auth.store_token("k", host="https://api.avrea.com")
    with pytest.raises(KeyError):
        auth.set_default_host("https://nowhere.example.com")


def test_set_default_host_flips_default():
    auth.store_token("k1", host="https://api.avrea.com")
    auth.store_token("k2", host="https://api.staging.example.com")
    auth.set_default_host("https://api.staging.example.com")
    assert auth.load_default_host() == "https://api.staging.example.com"


def test_clear_promotes_remaining_host_to_default():
    """When the cleared host was the default, the next stored host becomes
    the new default. Avoids leaving the user in 'no default, but I have
    creds' limbo after `auth logout` of the primary."""
    auth.store_token("k1", host="https://api.avrea.com")
    auth.store_token("k2", host="https://api.staging.example.com")
    auth.clear(host="https://api.avrea.com")
    assert auth.load_default_host() == "https://api.staging.example.com"


def test_clear_last_host_unsets_default():
    auth.store_token("k", host="https://api.avrea.com")
    auth.clear(host="https://api.avrea.com")
    assert auth.load_default_host() is None


def test_list_hosts():
    auth.store_token("k1", host="https://a.example.com")
    auth.store_token("k2", host="https://b.example.com")
    assert set(auth.list_hosts()) == {"https://a.example.com", "https://b.example.com"}


# -- success-page render --


def test_success_html_contains_core_copy():
    """Lock the page contents we promise to show after browser auth."""
    body = auth._render_success_html("alice@example.com")
    assert "You're all set" in body  # serif headline
    assert "Your CLI session is now connected." in body
    assert "alice@example.com" in body
    assert "avr status" in body
    assert "You can close this tab" in body


def test_success_html_omits_email_block_when_unknown():
    """If /users/me lookup fails, page still renders without an email line."""
    body = auth._render_success_html(None)
    assert "You're all set" in body
    assert "@" not in body  # no email block, no leftover placeholder
    assert "avr status" in body


def test_success_html_escapes_email():
    """Email goes through html.escape so a malicious value can't break out
    (defensive — /users/me is server-controlled, but cheap to harden)."""
    body = auth._render_success_html("<script>x</script>@x")
    assert "<script>x</script>" not in body
    assert "&lt;script&gt;" in body
