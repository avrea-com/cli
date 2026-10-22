"""Mirror selection, ephemeral authentication, and native Git interoperability."""

from avrea_cli.git_mirror import clone_mirror
from avrea_cli.git_mirror import git_token
from avrea_cli.git_mirror import mirror_endpoint
from avrea_cli.git_mirror import nearest_mirror
from avrea_cli.main import cli
from datetime import UTC
from datetime import datetime
from datetime import timedelta
from http.server import BaseHTTPRequestHandler
from http.server import ThreadingHTTPServer
from pathlib import Path
from threading import Thread
from unittest.mock import AsyncMock
from urllib.parse import urlsplit
import asyncio
import base64
import click
import httpx
import json
import os
import pytest
import shlex
import subprocess

REPO_ID = "rep-0123456789abcdef0123456789abcdef"
SESSION_PATH = f"/orgs/org-default/repos/{REPO_ID}/mirror/session"
ENDPOINTS = ["https://git.fi.example.com", "https://git.us.example.com"]
TOKEN = "secret-git-jwt"


def token_response(token=TOKEN):
    return {"token": token, "expires_at": (datetime.now(UTC) + timedelta(hours=1)).isoformat()}


@pytest.fixture
def clone_api(monkeypatch):
    calls = []
    session = {"browsable": True, "full_name": "foo/faa", "endpoints": ENDPOINTS}

    def get(self, path, params=None):
        calls.append(("GET", path, params))
        if path.endswith("/repos/resolve"):
            return {"data": {"repository_id": REPO_ID, "full_name": "foo/faa"}}
        assert path == SESSION_PATH
        return session

    def post(self, path, json=None):
        calls.append(("POST", path, json))
        return token_response()

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_get", get)
    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", post)
    return session, calls


@pytest.fixture
def clone_process(monkeypatch):
    calls = []

    def run(argv, **kwargs):
        calls.append((argv, kwargs))
        return subprocess.CompletedProcess(argv, 0)

    monkeypatch.setattr("avrea_cli.git_mirror.subprocess.run", run)
    return calls


def test_clone_resolves_repo_and_races_advertised_placements(runner, monkeypatch, clone_api, clone_process):
    nearest = AsyncMock(return_value=ENDPOINTS[1])
    monkeypatch.setattr("avrea_cli.commands.repo.nearest_mirror", nearest)
    result = runner.invoke(cli, ["repo", "clone", "Foo/Faa", "my checkout", "--depth", "1", "-b", "main"])
    assert result.exit_code == 0, result.output
    _, calls = clone_api
    assert calls == [
        ("GET", "/orgs/org-default/repos/resolve", {"name": "Foo/Faa"}),
        ("GET", SESSION_PATH, None),
        ("POST", "/tokens/git", {"repos": ["foo/faa"]}),
    ]
    nearest.assert_awaited_once_with(ENDPOINTS, "foo/faa", TOKEN)
    argv, kwargs = clone_process[0]
    assert argv[-7:] == ["--branch", "main", "--depth", "1", "--", ENDPOINTS[1] + "/foo/faa.git", "my checkout"]
    assert kwargs["env"]["AVR_GIT_AUTHORIZATION"] == f"Authorization: Bearer {TOKEN}"
    assert TOKEN not in " ".join(argv)
    assert TOKEN not in result.output
    assert ENDPOINTS[1] in result.stderr


@pytest.mark.parametrize("placement", [ENDPOINTS[0], ENDPOINTS[0] + "/", "git.fi.example.com"])
def test_explicit_placement_skips_race(runner, monkeypatch, clone_api, clone_process, placement):
    nearest = AsyncMock(side_effect=AssertionError("must not race explicit placements"))
    monkeypatch.setattr("avrea_cli.commands.repo.nearest_mirror", nearest)
    result = runner.invoke(cli, ["repo", "clone", REPO_ID, "--placement", placement])
    assert result.exit_code == 0, result.output
    assert clone_process[0][0][-3:] == ["--", ENDPOINTS[0] + "/foo/faa.git", "faa"]
    assert clone_api[1][0] == ("GET", SESSION_PATH, None)
    nearest.assert_not_called()


def test_unknown_placement_fails_before_minting_token(runner, clone_api, clone_process):
    result = runner.invoke(cli, ["repo", "clone", "foo/faa", "--placement", "https://untrusted.example"])
    assert result.exit_code == 2
    assert "Available placements" in result.output
    assert ENDPOINTS[0] in result.output
    assert not any(call[0] == "POST" for call in clone_api[1])
    assert not clone_process


@pytest.mark.parametrize(
    ("session", "message"),
    [
        ({"browsable": False, "unavailable_reason": "mirror_off"}, "Git mirroring is disabled"),
        ({"browsable": False, "unavailable_reason": "not_ready"}, "No Git mirror placement is ready"),
        ({"browsable": True, "full_name": "foo/faa", "endpoints": []}, "incomplete Git mirror session"),
        ({"browsable": True, "full_name": "foo/faa", "endpoints": [None]}, "invalid Git mirror placements"),
    ],
)
def test_unavailable_mirror_does_not_mint_token(runner, clone_api, clone_process, session, message):
    clone_api[0].clear()
    clone_api[0].update(session)
    result = runner.invoke(cli, ["repo", "clone", "foo/faa"])
    assert result.exit_code == 1
    assert message in result.output
    assert not any(call[0] == "POST" for call in clone_api[1])
    assert not clone_process


@pytest.mark.parametrize("name", ["../repo", "foo/..", "foo//repo", "foo/repo?x", "https://github.com/foo/faa"])
def test_invalid_repository_rejected_locally(runner, clone_api, name):
    result = runner.invoke(cli, ["repo", "clone", name])
    assert result.exit_code == 2
    assert not clone_api[1]


@pytest.mark.parametrize(
    "url",
    [
        "http://remote.example",
        "https://user:pass@example.com",
        "https://@example.com",
        "https://example.com/path",
        "https://example.com?token=oops",
        "https://example.com/#fragment",
        "https://example.com:bad",
        "https://example.com\n",
    ],
)
def test_mirror_endpoints_must_be_secure_origins(url):
    with pytest.raises(click.ClickException, match="invalid or insecure"):
        mirror_endpoint(url)


@pytest.mark.parametrize("status", [401, 403, 503])
def test_token_errors_are_actionable(runner, monkeypatch, clone_api, clone_process, status):
    def post(self, path, json=None):
        response = httpx.Response(status, request=httpx.Request("POST", "https://api.avrea.com/tokens/git"))
        response.raise_for_status()

    monkeypatch.setattr("avrea_cli.api_client.ApiClient.public_post", post)
    result = runner.invoke(cli, ["repo", "clone", "foo/faa", "--placement", ENDPOINTS[0]])
    assert result.exit_code == (4 if status == 401 else 1)
    assert not clone_process


@pytest.mark.parametrize("response", [{}, {"token": TOKEN, "expires_at": "invalid"}, token_response("bad\ntoken")])
def test_invalid_token_does_not_leak(response):
    from unittest.mock import Mock

    client = Mock()
    client.public_post.return_value = response
    with pytest.raises(click.ClickException, match="invalid or expired") as exc:
        git_token(client, "foo/faa")
    assert TOKEN not in str(exc.value)


def test_clone_preserves_git_exit_code_and_disables_secret_tracing(runner, monkeypatch, clone_api):
    captured = {}

    def run(argv, **kwargs):
        captured.update(kwargs)
        return subprocess.CompletedProcess(argv, 128)

    monkeypatch.setenv("GIT_TRACE_REDACT", "0")
    monkeypatch.setenv("GIT_TRACE_CURL", "1")
    monkeypatch.setenv("GIT_CURL_VERBOSE", "1")
    monkeypatch.setattr("avrea_cli.git_mirror.subprocess.run", run)
    result = runner.invoke(cli, ["repo", "clone", "foo/faa", "--placement", ENDPOINTS[0]])
    assert result.exit_code == 128
    assert not any(key.startswith("GIT_TRACE") for key in captured["env"])
    assert "GIT_CURL_VERBOSE" not in captured["env"]


@pytest.mark.asyncio
async def test_race_skips_empty_mirror_and_cancels_unresponsive_one(monkeypatch):
    waiting = asyncio.Event()
    cancelled = asyncio.Event()
    visited = []

    async def respond(request):
        assert request.headers["Authorization"] == f"Bearer {TOKEN}"
        assert request.url.path == "/foo/faa.git/info/refs"
        assert request.url.params["service"] == "git-upload-pack"
        visited.append(request.url.host)
        if request.url.host == "empty.example":
            return httpx.Response(404)
        if request.url.host == "stuck.example":
            waiting.set()
            try:
                await asyncio.Future()
            finally:
                cancelled.set()
        await waiting.wait()
        return httpx.Response(200, headers={"content-type": "application/x-git-upload-pack-advertisement"})

    client = httpx.AsyncClient(transport=httpx.MockTransport(respond))
    monkeypatch.setattr("avrea_cli.git_mirror.httpx.AsyncClient", lambda **kwargs: client)
    winner = await nearest_mirror(
        ["https://empty.example", "https://stuck.example", "https://ready.example"], "foo/faa", TOKEN
    )
    assert winner == "https://ready.example"
    assert set(visited) == {"empty.example", "stuck.example", "ready.example"}
    assert cancelled.is_set()


@pytest.mark.asyncio
async def test_race_fails_when_no_mirror_can_serve_git(monkeypatch):
    async def respond(request):
        if request.url.host == "fi.example":
            return httpx.Response(200, text="load balancer error", headers={"content-type": "text/html"})
        return httpx.Response(302, headers={"location": "https://untrusted.example"})

    client = httpx.AsyncClient(transport=httpx.MockTransport(respond))
    monkeypatch.setattr("avrea_cli.git_mirror.httpx.AsyncClient", lambda **kwargs: client)
    with pytest.raises(click.ClickException, match="No mirror could serve") as exc:
        await nearest_mirror(["https://fi.example", "https://us.example"], "foo/faa", TOKEN)
    assert "HTTP 302" in str(exc.value)
    assert TOKEN not in str(exc.value)


def test_helper_returns_fresh_credentials_for_exact_repository(runner, clone_api):
    args = ["auth", "git-credential", "--url", ENDPOINTS[0] + "/foo/faa.git", "--repo", "foo/faa", "get"]
    request = "protocol=https\nhost=git.fi.example.com\npath=foo/faa.git\n\n"
    result = runner.invoke(cli, args, input=request)
    assert result.exit_code == 0, result.output
    assert f"password={TOKEN}\n" in result.stdout
    assert "password_expiry_utc=" in result.stdout
    assert clone_api[1] == [("POST", "/tokens/git", {"repos": ["foo/faa"]})]
    result = runner.invoke(cli, args, input="capability[]=authtype\n" + request)
    assert result.exit_code == 0, result.output
    assert f"credential={TOKEN}\n" in result.stdout
    assert "ephemeral=true\n" in result.stdout
    assert "password=" not in result.stdout
    assert len(clone_api[1]) == 2


@pytest.mark.parametrize(
    "credential_request",
    [
        "protocol=https\nhost=untrusted.example\npath=foo/faa.git\n\n",
        "protocol=https\nhost=git.fi.example.com\npath=other/repo.git\n\n",
        "protocol=http\nhost=git.fi.example.com\npath=foo/faa.git\n\n",
        "protocol=https\nhost=git.fi.example.com\n\n",
    ],
)
def test_helper_refuses_other_hosts_or_repositories(runner, clone_api, credential_request):
    result = runner.invoke(
        cli,
        ["auth", "git-credential", "--url", ENDPOINTS[0] + "/foo/faa.git", "--repo", "foo/faa", "get"],
        input=credential_request,
    )
    assert result.exit_code == 0
    assert result.stdout == ""
    assert clone_api[1] == []


@pytest.mark.parametrize("operation", ["store", "erase"])
def test_helper_does_not_store_or_revoke_credentials(runner, clone_api, operation):
    result = runner.invoke(
        cli,
        ["auth", "git-credential", "--url", ENDPOINTS[0] + "/foo/faa.git", "--repo", "foo/faa", operation],
        input=f"password={TOKEN}\n\n",
    )
    assert result.exit_code == 0
    assert result.stdout == ""
    assert clone_api[1] == []


@pytest.fixture
def git_server(tmp_path, monkeypatch):
    """A real smart-HTTP Git backend and a fake Avrea token/session API."""
    monkeypatch.setenv("GIT_CONFIG_NOSYSTEM", "1")
    monkeypatch.setenv("GIT_CONFIG_GLOBAL", str(tmp_path / "global-config"))
    monkeypatch.setenv("PATH", str(Path(__file__).resolve().parents[1] / ".venv/bin") + os.pathsep + os.environ["PATH"])
    source = tmp_path / "source"
    root = tmp_path / "repos"
    bare = root / "foo/faa.git"
    subprocess.run(["git", "init", "--initial-branch=main", str(source)], check=True, capture_output=True)
    (source / "README").write_text("mirror content\n")
    subprocess.run(["git", "-C", str(source), "add", "README"], check=True, capture_output=True)
    subprocess.run(
        ["git", "-C", str(source), "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "init"],
        check=True,
        capture_output=True,
    )
    subprocess.run(["git", "clone", "--bare", str(source), str(bare)], check=True, capture_output=True)
    issued = []
    authenticated = []

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def send_json(self, data):
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(data).encode())

        def do_GET(self):
            if self.path.startswith("/orgs/org-default/repos/resolve?"):
                self.send_json({"data": {"repository_id": REPO_ID, "full_name": "foo/faa"}})
            elif self.path == SESSION_PATH:
                self.send_json({"browsable": True, "full_name": "foo/faa", "endpoints": [base_url]})
            else:
                self.git_backend()

        def do_POST(self):
            if self.path == "/tokens/git":
                body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                assert body == {"repos": ["foo/faa"]}
                assert self.headers["Authorization"] == "Bearer test-token"
                token = f"git-jwt-{len(issued) + 1}"
                issued.append(token)
                self.send_json(token_response(token))
            else:
                self.git_backend()

        def git_backend(self):
            authorization = self.headers.get("Authorization", "")
            if authorization.startswith("Basic "):
                token = base64.b64decode(authorization.removeprefix("Basic ")).decode().split(":", 1)[1]
            else:
                token = authorization.removeprefix("Bearer ")
            if not issued or token != issued[-1]:
                self.send_response(401)
                self.send_header("WWW-Authenticate", 'Basic realm="mirror"')
                self.end_headers()
                return
            authenticated.append(token)
            url = urlsplit(self.path)
            env = {
                **os.environ,
                "GIT_PROJECT_ROOT": str(root),
                "GIT_HTTP_EXPORT_ALL": "1",
                "PATH_INFO": url.path,
                "QUERY_STRING": url.query,
                "REQUEST_METHOD": self.command,
                "CONTENT_TYPE": self.headers.get("Content-Type", ""),
                "REMOTE_USER": "avr",
                "HTTP_GIT_PROTOCOL": self.headers.get("Git-Protocol", ""),
            }
            data = self.rfile.read(int(self.headers.get("Content-Length", "0")))
            result = subprocess.run(["git", "http-backend"], env=env, input=data, capture_output=True, check=True)
            headers, body = result.stdout.split(b"\r\n\r\n", 1)
            header_pairs = [line.decode().split(": ", 1) for line in headers.split(b"\r\n")]
            status = next((int(v.split()[0]) for k, v in header_pairs if k == "Status"), 200)
            self.send_response(status)
            for key, value in header_pairs:
                if key != "Status":
                    self.send_header(key, value)
            self.end_headers()
            try:
                self.wfile.write(body)
            except BrokenPipeError, ConnectionResetError:
                pass  # The placement probe intentionally reads headers only.

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    base_url = f"http://127.0.0.1:{server.server_port}"
    thread = Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield base_url, issued, authenticated
    finally:
        server.shutdown()
        server.server_close()
        thread.join()


def test_real_clone_and_later_fetch_use_fresh_tokens(runner, monkeypatch, tmp_path, git_server):
    base_url, issued, authenticated = git_server
    monkeypatch.setenv("AVR_HOST", base_url)
    stored_credentials = tmp_path / "stored credentials"
    subprocess.run(
        ["git", "config", "--global", "credential.helper", shlex.join(["store", "--file", str(stored_credentials)])],
        check=True,
    )
    global_config = (tmp_path / "global-config").read_text()
    destination = tmp_path / "checkout with spaces"
    result = runner.invoke(cli, ["repo", "clone", "foo/faa", str(destination)])
    assert result.exit_code == 0, result.output
    assert (destination / "README").read_text() == "mirror content\n"
    origin = subprocess.check_output(["git", "-C", str(destination), "remote", "get-url", "origin"], text=True).strip()
    assert origin == base_url + "/foo/faa.git"
    assert issued == ["git-jwt-1"]
    config = (destination / ".git/config").read_text()
    assert "git-jwt-" not in config
    assert "test-token" not in config
    assert "extraheader" not in config.lower()
    # Changing the active CLI host must not send credentials to a different API.
    monkeypatch.setenv("AVR_HOST", "https://another-api.example.com")
    for _ in range(2):
        fetched = subprocess.run(["git", "-C", str(destination), "fetch"], capture_output=True, text=True, timeout=15)
        assert fetched.returncode == 0, fetched.stderr
    subprocess.run(
        [
            "git",
            "-C",
            str(destination),
            "-c",
            "user.name=Test",
            "-c",
            "user.email=test@example.com",
            "commit",
            "--allow-empty",
            "-m",
            "push test",
        ],
        check=True,
        capture_output=True,
    )
    pushed = subprocess.run(["git", "-C", str(destination), "push"], capture_output=True, text=True, timeout=15)
    assert pushed.returncode == 0, pushed.stderr
    assert issued == ["git-jwt-1", "git-jwt-2", "git-jwt-3", "git-jwt-4"]
    assert set(authenticated) == set(issued)
    assert (tmp_path / "global-config").read_text() == global_config
    assert not stored_credentials.exists()


def test_clone_mirror_handles_missing_git(monkeypatch):
    def missing(*args, **kwargs):
        raise FileNotFoundError

    monkeypatch.setattr("avrea_cli.git_mirror.subprocess.run", missing)
    with pytest.raises(click.ClickException, match="Git is required"):
        clone_mirror("https://api.avrea.com", ENDPOINTS[0], "foo/faa", TOKEN, None, branch=None, depth=None)
