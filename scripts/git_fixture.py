"""Local smart-HTTP Git server used by native clone integration tests."""

from datetime import UTC
from datetime import datetime
from datetime import timedelta
from http.server import BaseHTTPRequestHandler
from http.server import ThreadingHTTPServer
from threading import Thread
from typing import TypedDict
from urllib.parse import urlsplit
import base64
import json
import os
import pytest
import subprocess

REPO_ID = "rep-0123456789abcdef0123456789abcdef"
SESSION_PATH = "/repos/foo/faa/mirror/session"
TOKEN = "secret-git-jwt"


class GitServerControls(TypedDict):
    redirect: str | None
    token_status: int
    git_status: int
    endpoints: list[str] | None
    unauthorized_requests: int


def token_response(token=TOKEN):
    return {"token": token, "expires_at": (datetime.now(UTC) + timedelta(hours=1)).isoformat()}


@pytest.fixture
def git_server(tmp_path, monkeypatch):
    """A real smart-HTTP Git backend and a fake Avrea token/session API."""
    for name in ("GIT_ASKPASS", "SSH_ASKPASS"):
        monkeypatch.setenv(name, "")
    for name in ("DISPLAY", "WAYLAND_DISPLAY"):
        monkeypatch.delenv(name, raising=False)
    monkeypatch.setenv("GIT_TERMINAL_PROMPT", "0")
    monkeypatch.setenv("GIT_CONFIG_NOSYSTEM", "1")
    monkeypatch.setenv("GIT_CONFIG_GLOBAL", str(tmp_path / "global-config"))
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
    controls: GitServerControls = {
        "redirect": None,
        "token_status": 200,
        "git_status": 200,
        "endpoints": None,
        "unauthorized_requests": 0,
    }

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def send_json(self, data):
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(data).encode())

        def do_GET(self):
            if self.path in {SESSION_PATH, f"/repos/{REPO_ID}/mirror/session"}:
                self.send_json(
                    {"browsable": True, "full_name": "foo/faa", "endpoints": controls["endpoints"] or [base_url]}
                )
            else:
                self.git_backend()

        def do_POST(self):
            if self.path == "/tokens/git":
                if controls["token_status"] != 200:
                    self.send_response(controls["token_status"])
                    self.end_headers()
                    return
                body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                assert body == {"repos": ["foo/faa"]}
                assert self.headers["Authorization"] == "Bearer test-token"
                token = f"git-jwt-{len(issued) + 1}"
                issued.append(token)
                self.send_json(token_response(token))
            else:
                self.git_backend()

        def git_backend(self):
            if controls["redirect"]:
                self.send_response(302)
                self.send_header("Location", controls["redirect"])
                self.end_headers()
                return
            authorization = self.headers.get("Authorization", "")
            if authorization.startswith("Basic "):
                token = base64.b64decode(authorization.removeprefix("Basic ")).decode().split(":", 1)[1]
            else:
                token = authorization.removeprefix("Bearer ")
            if not issued or token != issued[-1]:
                controls["unauthorized_requests"] += 1
                self.send_response(401)
                self.send_header("WWW-Authenticate", 'Basic realm="mirror"')
                self.end_headers()
                return
            authenticated.append(token)
            if controls["git_status"] != 200:
                self.send_response(controls["git_status"])
                if controls["git_status"] == 401:
                    self.send_header("WWW-Authenticate", 'Basic realm="mirror"')
                self.end_headers()
                return
            url = urlsplit(self.path)
            if "git-receive-pack" in self.path:
                self.send_response(403)
                self.end_headers()
                self.wfile.write(b"repository is a read-only mirror of an upstream source")
                return
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
        yield base_url, issued, authenticated, controls
    finally:
        server.shutdown()
        server.server_close()
        thread.join()
