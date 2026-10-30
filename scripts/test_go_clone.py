"""Exercise the Go clone and saved helper against the existing local smart-HTTP fixture."""

from pathlib import Path
from scripts import compare_clone
from tests.test_repo_clone import git_server  # noqa: F401 -- shared smart-HTTP fixture
import json
import os
import pytest
import shlex
import subprocess
import sys


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
        [binary, "repo", "clone", "foo/faa", str(destination)], capture_output=True, text=True, timeout=20
    )
    assert result.returncode == 0, result.stderr
    assert (destination / "README").read_text() == "mirror content\n"
    origin = subprocess.check_output(["git", "-C", str(destination), "remote", "get-url", "origin"], text=True).strip()
    assert origin == base_url + "/foo/faa.git"
    assert issued == ["git-jwt-1", "git-jwt-2"]
    config = (destination / ".git/config").read_text()
    assert "git-jwt-" not in config and "test-token" not in config
    assert "extraheader" not in config.lower()
    helper = subprocess.check_output(
        ["git", "-C", str(destination), "config", "--get-all", f"credential.{origin}.helper"], text=True
    ).splitlines()[-1]
    helper_command = helper.removeprefix("!f() { ").split(' "$@" || ', 1)[0]
    assert Path(shlex.split(helper_command)[1]).resolve() == Path(binary)
    monkeypatch.setenv("AVR_HOST", "https://never-contact.example.test")
    for _ in range(2):
        fetched = subprocess.run(["git", "-C", str(destination), "fetch"], capture_output=True, text=True, timeout=15)
        assert fetched.returncode == 0, fetched.stderr
    assert issued == ["git-jwt-1", "git-jwt-2", "git-jwt-3", "git-jwt-4"]
    assert set(authenticated) == set(issued)
    controls["token_status"] = 503
    fetched = subprocess.run(["git", "-C", str(destination), "fetch"], capture_output=True, text=True, timeout=15)
    assert fetched.returncode != 0
    assert "Avrea is having trouble" in fetched.stderr
    assert credentials.read_text() == seeded
    assert (tmp_path / "global-config").read_text() == global_config
    assert "git-jwt-" not in fetched.stderr


@pytest.mark.parametrize("change", [None, "executable", "repository", "trace"])
def test_clone_comparator_keeps_helper_scope_and_environment(monkeypatch, tmp_path, change):
    native = str(tmp_path / "native avr")
    python = str(tmp_path / "python avr")
    monkeypatch.setattr(sys, "argv", ["compare", "--go", native])

    def result(executable):
        command = shlex.join(
            [
                "AVR_HOST=https://api.example.test",
                executable,
                "auth",
                "git-credential",
                "--url",
                "https://git.example.test/acme/web.git",
                "--repo",
                "acme/web",
            ]
        )
        helper = (
            "credential.https://git.example.test/acme/web.git.helper=!f() { " + command + ' "$@" || { exit 1; }; }; f'
        )
        capture = {
            "args": ["clone", "--config", helper, "--", "https://git.example.test/acme/web.git", "web"],
            "env": {"GIT_TERMINAL_PROMPT": "0", "GIT_ASKPASS": "", "SSH_ASKPASS": ""},
        }
        return capture

    expected = result(python)
    actual = result(str(tmp_path / "different avr") if change == "executable" else native)
    if change == "repository":
        actual["args"][2] = actual["args"][2].replace("--repo acme/web", "--repo acme/other")
    if change == "trace":
        actual["env"]["GIT_TRACE"] = "1"

    def output(capture):
        return {
            "exit": 0,
            "hosts": None,
            "requests": [],
            "browser": None,
            "mode": None,
            "stdout": "GIT CAPTURE: " + json.dumps(capture) + "\n",
            "stderr": "",
        }

    differences = compare_clone.compare(
        {"name": "clone comparator", "args": ["repo", "clone", "acme/web"], "git_stub": 0},
        output(expected),
        output(actual),
    )
    assert bool(differences) == (change is not None)
