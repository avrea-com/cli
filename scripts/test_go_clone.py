"""Exercise the Go clone and saved helper against the local smart-HTTP fixture."""

from pathlib import Path
from scripts.git_fixture import git_server  # noqa: F401 -- shared smart-HTTP fixture
import os
import pytest
import shlex
import subprocess


def test_native_clone_and_later_fetch(monkeypatch, tmp_path, git_server):  # noqa: F811 -- pytest fixture injection
    binary = os.environ.get("AVR_GO_BINARY", "")
    if not binary:
        pytest.skip("Set AVR_GO_BINARY to the native avr or avr-admin executable")
    binary = str(Path(binary).resolve())
    base_url, issued, authenticated, controls = git_server
    monkeypatch.setenv("AVR_TOKEN", "test-token")
    monkeypatch.setenv("AVR_HOST", base_url)
    monkeypatch.setenv("AVR_CONFIG_DIR", str(tmp_path / "config"))
    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.setenv("USERPROFILE", str(tmp_path))
    for key in ["HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"]:
        monkeypatch.delenv(key, raising=False)
    credentials = tmp_path / "stored credentials"
    seeded = base_url.replace("://", "://stale:stale@") + "/foo/faa.git\n"
    credentials.write_text(seeded)
    subprocess.run(
        ["git", "config", "--global", "credential.helper", shlex.join(["store", "--file", str(credentials)])],
        check=True,
    )
    global_config = (tmp_path / "global-config").read_text()
    destination = tmp_path / "checkout with spaces"
    result = subprocess.run(
        [binary, "repo", "clone", "foo/faa", str(destination)], check=False, capture_output=True, text=True, timeout=20
    )
    assert result.returncode == 0, result.stderr
    assert (destination / "README").read_text() == "mirror content\n"
    origin = subprocess.check_output(["git", "-C", str(destination), "remote", "get-url", "origin"], text=True).strip()
    assert origin == base_url + "/foo/faa.git"
    assert issued == ["git-jwt-1"]
    config = (destination / ".git/config").read_text()
    assert "git-jwt-" not in config and "test-token" not in config
    assert "extraheader" not in config.lower()
    helper = subprocess.check_output(
        ["git", "-C", str(destination), "config", "--get-all", f"credential.{origin}.helper"], text=True
    ).splitlines()[-1]
    helper_command = helper.removeprefix('!f() { case "$1" in store|erase) return 0;; esac; ').split(' "$@" || ', 1)[0]
    assert Path(shlex.split(helper_command)[1]).resolve() == Path(binary)
    monkeypatch.setenv("AVR_HOST", "https://never-contact.example.test")
    for _ in range(2):
        fetched = subprocess.run(
            ["git", "-C", str(destination), "fetch"], check=False, capture_output=True, text=True, timeout=15
        )
        assert fetched.returncode == 0, fetched.stderr
    assert issued == ["git-jwt-1", "git-jwt-2", "git-jwt-3"]
    assert set(authenticated) == set(issued)
    controls["token_status"] = 503
    fetched = subprocess.run(
        ["git", "-C", str(destination), "fetch"], check=False, capture_output=True, text=True, timeout=15
    )
    assert fetched.returncode != 0
    assert "Avrea is having trouble" in fetched.stderr
    assert credentials.read_text() == seeded
    assert (tmp_path / "global-config").read_text() == global_config
    assert "git-jwt-" not in fetched.stderr
