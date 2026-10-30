"""Paired executable auth checks. Uses synthetic credentials and loopback HTTP only.

Run with Python 3.14 and the Python CLI dependencies installed:
  python scripts/compare_auth.py --go build/go/avr
Raw captures, requests, local effects and differences go into the JSON report.
"""

from __future__ import annotations

from compression import zstd
from pathlib import Path
from typing import Any
from typing import cast
from urllib.parse import parse_qsl
from urllib.parse import urlencode
from urllib.parse import urlsplit
from urllib.parse import urlunsplit
import argparse
import base64
import copy
import decimal
import gzip
import hashlib
import http.server
import io
import json
import os
import re
import signal
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
import urllib.request

BASELINE = "99e74d119d83c7cef2887c2dd04a724719e6b009"
TOKEN = "avr_fixture_key_never_a_real_secret"
USER = {
    "id": "usr-fixture",
    "email": "fixture@example.test",
    "name": "Fixture User",
    "created_at": "2026-01-01T00:00:00Z",
}


def canonical_path(path):
    parts = urlsplit(path)
    query = urlencode(sorted(parse_qsl(parts.query, keep_blank_values=True), key=lambda item: item[0]))
    return urlunsplit(parts._replace(query=query))


class Fixture(http.server.BaseHTTPRequestHandler):
    server: Any

    def log_message(self, *_):
        pass

    def do_GET(self):
        self.respond()

    def do_POST(self):
        self.respond()

    def do_PUT(self):
        self.respond()

    def do_DELETE(self):
        self.respond()

    def do_PATCH(self):
        self.respond()

    def respond(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        self.server.requests.append(
            {
                "method": self.command,
                "path": self.path,
                "authorization": self.headers.get("Authorization"),
                "cookie": self.headers.get("Cookie"),
                "csrf": self.headers.get("X-CSRF-Token"),
                "body": (
                    {"base64": base64.b64encode(body).decode(), "content_type": self.headers.get("Content-Type")}
                    if self.headers.get("Content-Type") == "application/xml"
                    else dict(parse_qsl(body.decode(), keep_blank_values=True))
                    if self.headers.get("Content-Type") == "application/x-www-form-urlencoded"
                    else json.loads(body)
                    if body
                    else None
                ),
            }
        )
        key = (self.command, canonical_path(self.path))
        if key in self.server.responses:
            response = self.server.responses[key]
        else:
            # Explicit opt-in for fixtures with moving time-window queries.
            # The full query remains in requests for semantic validation.
            response = getattr(self.server, "dynamic_responses", {}).get(
                (self.command, urlsplit(self.path).path), (200, USER)
            )
        if isinstance(response, list):
            response = response.pop(0) if len(response) > 1 else response[0]
        status, payload, *extra = response
        if getattr(self.server, "expand_host", False) and not isinstance(payload, bytes):
            payload = json.loads(json.dumps(payload).replace("FIXTURE_HOST", self.server.fixture_host))
        self.send_response(status)
        if not extra or not any(k.lower() == "content-type" for k in extra[0]):
            self.send_header(
                "Content-Type", "application/octet-stream" if isinstance(payload, bytes) else "application/json"
            )
        for key, value in (extra[0] if extra else {}).items():
            self.send_header(key, value)
        data = payload if isinstance(payload, bytes) else json.dumps(payload).encode()
        if self.server.encoding:
            self.send_header("Content-Encoding", self.server.encoding)
            data = gzip.compress(data) if self.server.encoding == "gzip" else zstd.compress(data)
        self.end_headers()
        self.wfile.write(data)


def run(executable, scenario, root, host):
    with tempfile.TemporaryDirectory(prefix="avr-parity-") as directory:
        home = Path(directory)
        cfg = home / "config"
        cfg.mkdir()
        for name in scenario.get("fixture_dirs", []):
            (home / name).mkdir(parents=True, exist_ok=True)
        for name, data in scenario.get("fixture_files", {}).items():
            target = home / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
            if os.name != "nt":
                target.chmod(0o644)
        hosts = copy.deepcopy(scenario.get("hosts"))
        if hosts is not None:
            hosts = json.loads(json.dumps(hosts).replace("FIXTURE_HOST", host))
            (cfg / "hosts.json").write_text(json.dumps(hosts))
        env = {
            k: v
            for k, v in os.environ.items()
            if not k.startswith("AVR_") and k.lower() not in {"http_proxy", "https_proxy", "all_proxy"}
        }
        env.update(
            HOME=directory,
            USERPROFILE=directory,
            XDG_CONFIG_HOME=directory,
            APPDATA=directory,
            LOCALAPPDATA=directory,
            AVR_CONFIG_DIR=str(cfg),
            AVR_HOST=host,
            NO_COLOR="1",
            TERM="dumb",
            PYTHONPATH=str(root),
            PYTHONIOENCODING="utf-8",
            TZ="UTC",
            NO_PROXY="localhost,127.0.0.1,::1",
        )
        env.update(scenario.get("env", {}))
        env = {k: v for k, v in env.items() if v is not None}
        if scenario.get("expand_env"):
            env = {k: v.replace("FIXTURE_HOST", host).replace("FIXTURE_HOME", directory) for k, v in env.items()}
        if "ssh_stub" in scenario and sys.platform != "win32":
            stub = home / "bin"
            stub.mkdir()
            script = (
                "#!"
                + sys.executable
                + "\nimport json,sys\nprint('SSH ARGS: ' + json.dumps(sys.argv[1:]))\nsys.exit("
                + str(scenario["ssh_stub"])
                + ")\n"
            )
            if scenario.get("ssh_stub_pins"):
                script = (
                    "#!"
                    + sys.executable
                    + "\nimport json,os,sys\nfrom pathlib import Path\n"
                    + "args=sys.argv[1:]; pin=None\n"
                    + "for i,arg in enumerate(args):\n"
                    + " if arg.startswith('UserKnownHostsFile='):\n"
                    + "  path=Path(arg.split('=',1)[1]); Path('.ssh-stub-pin').write_text(str(path))\n"
                    + "  pin={'text':path.read_text(),'mode':path.stat().st_mode & 0o777}\n"
                    + "  args[i]='UserKnownHostsFile=<known_hosts>'\n"
                    + "print('SSH ARGS: ' + json.dumps(args))\n"
                    + "print('SSH PIN: ' + json.dumps(pin))\n"
                    + "sys.exit("
                    + str(scenario["ssh_stub"])
                    + ")\n"
                )
            for tool in ("ssh", "sshpass"):
                executable_stub = stub / tool
                executable_stub.write_text(script)
                executable_stub.chmod(0o700)
            env["PATH"] = str(stub) + os.pathsep + env["PATH"]
        if "git_stub" in scenario and sys.platform != "win32":
            stub = home / "bin"
            stub.mkdir(exist_ok=True)
            script = (
                "#!"
                + sys.executable
                + "\nimport json,os,sys\n"
                + "keys=['GIT_TERMINAL_PROMPT','GIT_ASKPASS','SSH_ASKPASS']\n"
                + "keys += [k for k in os.environ if k.startswith('GIT_TRACE') or k=='GIT_CURL_VERBOSE']\n"
                + "capture={'args':sys.argv[1:], 'env':{k:os.environ.get(k) for k in keys}}\n"
                + "print('GIT CAPTURE: ' + json.dumps(capture))\n"
                + "sys.exit("
                + str(scenario["git_stub"])
                + ")\n"
            )
            executable_stub = stub / "git"
            executable_stub.write_text(script)
            executable_stub.chmod(0o700)
            env["PATH"] = str(stub) + os.pathsep + env["PATH"]
        if "bootstrap_stub" in scenario and sys.platform != "win32":
            from scripts.bootstrap_fixture import make_stubs  # noqa: PLC0415 - optional fixture for module callers

            env["PATH"] = make_stubs(home, scenario["bootstrap_stub"])
        args = [a.replace("FIXTURE_HOST", host).replace("FIXTURE_HOME", directory) for a in scenario["args"]]
        if remote := scenario.get("git_remote"):
            subprocess.run(["git", "init", "-q", directory], env=env, check=True, capture_output=True, timeout=5)
            subprocess.run(
                ["git", "-C", directory, "remote", "add", "origin", remote],
                env=env,
                check=True,
                capture_output=True,
                timeout=5,
            )
        callback_errors = []
        callback_worker = None
        browser_target = home / "browser-target"
        if scenario.get("login") or scenario.get("browser_capture"):
            browser = home / "browser.py"
            browser.write_text(
                "import pathlib,sys; pathlib.Path(" + repr(str(browser_target)) + ").write_text(sys.argv[1])"
            )
            env["AVR_BROWSER"] = '"' + sys.executable + '" "' + str(browser) + '" %s'
            env["BROWSER"] = env["AVR_BROWSER"]

        if scenario.get("login"):

            def callback():
                deadline = time.monotonic() + 15
                while not browser_target.exists() and time.monotonic() < deadline:
                    time.sleep(0.01)
                if not browser_target.exists():
                    callback_errors.append("browser was not invoked")
                    return
                try:
                    request = urllib.request.Request(
                        "http://localhost:8765/callback?"
                        + scenario.get("callback", "session=session-fixture&csrf_token=csrf-fixture")
                    )
                    with urllib.request.urlopen(request, timeout=10) as response:
                        response.read()
                except Exception as exc:
                    callback_errors.append(str(exc))

            callback_worker = threading.Thread(target=callback, daemon=True)
            callback_worker.start()
        if scenario.get("tty"):
            import pty  # noqa: PLC0415 - unavailable on Windows

            master, slave = pty.openpty()
            try:
                process = subprocess.Popen(
                    executable + args,
                    env=env,
                    cwd=directory,
                    stdin=subprocess.PIPE,
                    stdout=slave,
                    stderr=slave if scenario.get("merge_terminal") else subprocess.PIPE,
                    text=True,
                )
                os.close(slave)
                slave = None
                chunks = []

                def drain_terminal():
                    while True:
                        try:
                            chunk = os.read(master, 65536)
                        except OSError:
                            break
                        if not chunk:
                            break
                        chunks.append(chunk)

                # Drain while the child is alive: PTYs can discard unread
                # output on close, and a full PTY buffer blocks the child.
                reader = threading.Thread(target=drain_terminal, daemon=True)
                reader.start()
                try:
                    _, stderr = process.communicate(input=scenario.get("stdin", ""), timeout=20)
                finally:
                    if process.poll() is None:
                        process.kill()
                        process.communicate()
                    reader.join(timeout=5)
                if reader.is_alive():
                    raise RuntimeError("terminal reader did not finish")
                result = subprocess.CompletedProcess(
                    process.args, process.returncode, b"".join(chunks).decode(), stderr or ""
                )
            finally:
                os.close(master)
                if slave is not None:
                    os.close(slave)
        elif (
            scenario.get("interrupt_after_event")
            or scenario.get("interrupt_after_text")
            or scenario.get("close_pipe_after_event")
        ):
            process = subprocess.Popen(
                executable + args,
                env=env,
                cwd=directory,
                stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )
            assert process.stdout is not None
            watchdog = threading.Timer(15, process.kill)
            watchdog.start()
            try:
                first = process.stdout.readline()
                if target := scenario.get("interrupt_after_text"):
                    while target not in first:
                        line = process.stdout.readline()
                        if not line:
                            raise RuntimeError("process exited before interruption marker")
                        first += line
                if not first:
                    raise RuntimeError("watch exited without its first event")
                if scenario.get("close_pipe_after_event"):
                    process.stdout.close()
                    process.stdout = None
                else:
                    process.send_signal(signal.SIGINT)
                rest, stderr = process.communicate(timeout=5)
                rest = rest or ""
                result = subprocess.CompletedProcess(process.args, process.returncode, first + rest, stderr)
            finally:
                watchdog.cancel()
                if process.poll() is None:
                    process.kill()
                    process.communicate()
        else:
            result = subprocess.run(
                executable + args,
                env=env,
                cwd=directory,
                input=scenario.get("stdin", ""),
                capture_output=True,
                text=True,
                timeout=20,
            )
        if callback_worker is not None:
            callback_worker.join(timeout=16)
            if callback_errors:
                raise RuntimeError(callback_errors)
        path = cfg / "hosts.json"
        captured = {}
        for name in scenario.get("capture_files", []):
            target = home / name
            candidates = sorted(target.rglob("*")) if target.is_dir() else [target]
            for item in candidates:
                if item.is_file():
                    data = item.read_bytes()
                    if scenario.get("normalize_file_home"):
                        data = data.replace(directory.encode(), b"<home>")
                    captured[item.relative_to(home).as_posix()] = {
                        **(
                            {"text": data.decode("utf-8", errors="replace")}
                            if scenario.get("capture_text_files")
                            else {}
                        ),
                        "size": len(data),
                        "sha256": hashlib.sha256(data).hexdigest(),
                        "mode": item.stat().st_mode & 0o777 if os.name != "nt" else None,
                    }
        stdout, stderr = result.stdout, result.stderr
        if "capture_files" in scenario:
            # JSON strings escape Windows separators; normalize that representation too.
            escaped = json.dumps(directory)[1:-1]
            stdout = stdout.replace(escaped, "<home>").replace(directory, "<home>")
            stderr = stderr.replace(escaped, "<home>").replace(directory, "<home>")
        pin_marker = home / ".ssh-stub-pin"
        pin_removed = not Path(pin_marker.read_text()).exists() if pin_marker.exists() else None
        bootstrap = {}
        if "bootstrap_stub" in scenario:
            from scripts.bootstrap_fixture import read_records  # noqa: PLC0415 - optional fixture for module callers

            bootstrap["bootstrap"] = read_records(home)
        return {
            **bootstrap,
            **({"ssh_pin_removed": pin_removed} if scenario.get("ssh_stub_pins") else {}),
            **({"files": captured} if "capture_files" in scenario else {}),
            "exit": result.returncode,
            "browser": browser_target.read_text() if browser_target.exists() else None,
            "stdout": stdout.replace(str(cfg), "<config>"),
            "stderr": stderr.replace(str(cfg), "<config>"),
            "hosts": json.loads(path.read_text()) if path.exists() else None,
            "mode": (path.stat().st_mode & 0o777) if path.exists() and os.name != "nt" else None,
        }


def compare(scenario, python, go):
    differences = []
    expected = scenario.get("exit", 0)
    if python["exit"] != expected or go["exit"] != expected:
        differences.append(f"expected exit {expected}")

    def visible(value):
        return " ".join(re.sub(r"\x1b\[[0-9;]*m", "", value).split())

    def typed(value):
        if isinstance(value, dict):
            return {k: typed(v) for k, v in value.items()}
        if isinstance(value, list):
            return [typed(v) for v in value]
        return (type(value).__name__, value)

    for key in ("exit", "hosts", "requests", "browser"):
        if typed(python[key]) != typed(go[key]):
            differences.append(key)

    if scenario.get("json") and python["exit"] == go["exit"] == 0:
        if typed(json.loads(python["stdout"], parse_float=decimal.Decimal)) != typed(
            json.loads(go["stdout"], parse_float=decimal.Decimal)
        ):
            differences.append("JSON values")
    elif scenario["args"][:2] == ["auth", "git-credential"]:
        if python["stdout"] != go["stdout"]:
            differences.append("Git protocol stdout")
    else:
        # Whitespace and styling are presentation; retain every other visible word.
        if visible(python["stdout"]) != visible(go["stdout"]):
            differences.append("stdout")
    if "error_terms" in scenario:
        for term in scenario["error_terms"]:
            if term not in python["stderr"] or term not in go["stderr"]:
                differences.append("stderr missing: " + term)
    elif visible(python["stderr"]) != visible(go["stderr"]):
        differences.append("stderr")
    if scenario.get("writes") and go["mode"] not in (None, 0o600):
        differences.append("credential file permissions")
    return differences


def scenarios():
    stored = {
        "default_host": "FIXTURE_HOST",
        "hosts": {
            "FIXTURE_HOST": {"token": TOKEN, "default_org": "org-fixture"},
            "https://other.example.test": {"token": "other-fixture", "custom": 42},
        },
    }
    cases = [
        {"name": "missing credentials", "args": ["auth", "status"], "exit": 4},
        {"name": "field discovery", "args": ["auth", "status", "--json", "?"]},
        {"name": "status JSON", "args": ["auth", "status", "--json", "*"], "hosts": stored, "json": True},
        {"name": "status human", "args": ["auth", "status"], "hosts": stored},
        {
            "name": "show token",
            "args": ["auth", "status", "--show-token", "--json", "token"],
            "hosts": stored,
            "json": True,
        },
        {"name": "jq raw string", "args": ["auth", "status", "--json", "email", "-q", ".email"], "hosts": stored},
        {
            "name": "scoped local status",
            "args": ["auth", "status", "--json", "*"],
            "hosts": stored,
            "env": {"AVR_TOKEN": "avs_fixture"},
            "json": True,
        },
        {"name": "list hosts", "args": ["auth", "switch"], "hosts": stored},
        {
            "name": "switch hosts",
            "args": ["auth", "switch", "https://other.example.test/"],
            "hosts": stored,
            "writes": True,
        },
        {"name": "logout", "args": ["auth", "logout"], "hosts": stored, "writes": True},
        {"name": "logout empty", "args": ["logout"]},
    ]
    for args, name, code, terms in [
        (["auth", "status", "--json", "typo"], "unknown JSON field", 1, ["Unknown JSON field(s)", "typo", "Available"]),
        (["auth", "status", "--json"], "bare JSON argument", 2, ["--json"]),
        (["auth", "status", "--jq", ".email"], "jq requires JSON", 2, ["--jq requires --json"]),
        (["auth", "status", "--bad-flag"], "unknown flag", 2, ["--bad-flag"]),
        (["auth", "status", "extra"], "extra argument", 2, ["extra"]),
        (["auth", "login", "--provider", "bad"], "invalid provider", 2, ["--provider", "github", "google"]),
        (
            ["auth", "switch", "https://missing.example.test"],
            "unknown host",
            1,
            ["no stored credentials", "Available hosts"],
        ),
        (["auth", "status", "--json", "token"], "token requires show-token", 1, ["Unknown JSON field(s): token"]),
        (["auth", "status", "--json", "email", "--jq", "["], "invalid jq expression", 1, ["jq error"]),
        (["auth", "status", "--json", "email", "--jq", 'error("fixture")'], "jq runtime error", 1, ["jq error"]),
    ]:
        cases.append({"name": name, "args": args, "hosts": stored, "exit": code, "error_terms": terms})
    cases.extend(
        [
            {
                "name": "scoped human status",
                "args": ["auth", "status"],
                "hosts": stored,
                "env": {"AVR_TOKEN": "avs_fixture"},
            },
            {
                "name": "environment overrides",
                "args": ["auth", "status", "--show-token", "--json", "*"],
                "hosts": stored,
                "env": {"AVR_TOKEN": "avs_explicit", "AVR_ORG": "org-explicit"},
                "json": True,
            },
            {
                "name": "stored host fallback",
                "args": ["auth", "status", "--json", "*"],
                "hosts": stored,
                "env": {"AVR_HOST": None},
                "json": True,
            },
            {
                "name": "empty environment fallback",
                "args": ["auth", "status", "--json", "*"],
                "hosts": stored,
                "env": {"AVR_TOKEN": "", "AVR_ORG": ""},
                "json": True,
            },
            {
                "name": "partial user data",
                "args": ["auth", "status", "--json", "*"],
                "hosts": stored,
                "responses": {("GET", "/users/me"): (200, {"email": None})},
                "json": True,
            },
            {"name": "empty store switch", "args": ["auth", "switch"], "exit": 1},
            {
                "name": "logout despite revocation failure",
                "args": ["auth", "logout"],
                "hosts": stored,
                "responses": {("DELETE", "/users/me/api-keys/current"): (500, {"detail": "fixture failure"})},
                "writes": True,
            },
            {
                "name": "logout last host",
                "args": ["logout"],
                "hosts": {"default_host": "FIXTURE_HOST", "hosts": {"FIXTURE_HOST": {"token": TOKEN}}},
                "writes": True,
            },
            {
                "name": "jq multiple results",
                "args": ["auth", "status", "--json", "email,name", "-q", ".email, .name"],
                "hosts": stored,
            },
            {"name": "jq no results", "args": ["auth", "status", "--json", "email", "-q", "empty"], "hosts": stored},
        ]
    )
    for status in [401, 403, 404, 409, 422, 429, 500]:
        cases.append(
            {
                "name": f"HTTP {status}",
                "args": ["auth", "status"],
                "hosts": stored,
                "responses": {("GET", "/users/me"): (status, {"detail": "fixture denial"})},
                "exit": 4 if status == 401 else 1,
            }
        )
    login_responses = {
        ("POST", "/sso/discover"): (200, {"method": "saml", "saml_login_path": "/sso/acme/login"}),
        ("POST", "/users/me/api-keys"): (200, {"api_key": TOKEN}),
        ("POST", "/users/me/logout"): (200, {}),
        ("GET", "/users/me/organizations"): (200, {"data": [{"organization_id": "org-fixture", "slug": "fixture"}]}),
    }
    for name, args in [
        ("github login", ["auth", "login"]),
        ("google login alias", ["login", "--provider", "Google"]),
        ("SSO login", ["auth", "login", "--email", "fixture@example.test"]),
    ]:
        cases.append({"name": name, "args": args, "login": True, "responses": login_responses, "writes": True})
    cases.append(
        {
            "name": "login preserves org",
            "args": ["auth", "login"],
            "login": True,
            "responses": login_responses,
            "hosts": stored,
            "writes": True,
        }
    )
    multi_orgs = dict(login_responses)
    multi_orgs[("GET", "/users/me/organizations")] = (
        200,
        {"data": [{"organization_id": "org-one"}, {"organization_id": "org-two"}]},
    )
    cases.append(
        {
            "name": "login multiple orgs",
            "args": ["auth", "login"],
            "login": True,
            "responses": multi_orgs,
            "writes": True,
        }
    )
    failed_exchange = dict(login_responses)
    failed_exchange[("POST", "/users/me/api-keys")] = (403, {"detail": "never print credential-fixture"})
    cases.append(
        {
            "name": "login exchange denied",
            "args": ["auth", "login"],
            "login": True,
            "responses": failed_exchange,
            "exit": 1,
            "error_terms": ["HTTP 403", "creating an API key"],
        }
    )
    cases.append(
        {
            "name": "SSO required bounce",
            "args": ["auth", "login"],
            "login": True,
            "responses": login_responses,
            "callback": "sso_required=acme",
            "exit": 1,
            "error_terms": ["requires single sign-on", "avr auth login --email"],
        }
    )
    helper = ["auth", "git-credential", "--url", "https://mirror.test/owner/repo.git", "--repo", "owner/repo", "get"]
    protocol = "protocol=https\nhost=mirror.test\npath=owner/repo.git\n"
    token_responses = {
        ("POST", "/tokens/git"): (200, {"token": "git.fixture.token", "expires_at": "2099-01-01T00:00:00Z"})
    }
    cases.extend(
        [
            {
                "name": "Git basic protocol",
                "args": helper,
                "stdin": protocol + "\n",
                "hosts": stored,
                "responses": token_responses,
            },
            {
                "name": "Git bearer protocol",
                "args": helper,
                "stdin": protocol + "capability[]=authtype\n\n",
                "hosts": stored,
                "responses": token_responses,
            },
            {
                "name": "Git URL mismatch",
                "args": helper,
                "stdin": protocol.replace("owner/repo.git", "other/repo.git") + "\n",
                "hosts": stored,
            },
            {"name": "Git no auth", "args": helper, "stdin": protocol + "\n", "exit": 4},
            {
                "name": "Git invalid token",
                "args": helper,
                "stdin": protocol + "\n",
                "hosts": stored,
                "responses": {
                    ("POST", "/tokens/git"): (200, {"token": "secret\nfixture", "expires_at": "2099-01-01T00:00:00Z"})
                },
                "exit": 1,
            },
            {"name": "Git store is no-op", "args": helper[:-1] + ["store"]},
        ]
    )
    cases.extend(
        [
            {"name": "verbose status", "args": ["auth", "status", "--verbose"], "hosts": stored},
            {"name": "debug env status", "args": ["auth", "status"], "hosts": stored, "env": {"AVR_DEBUG": "api"}},
            {"name": "no-color flags", "args": ["auth", "status", "--no-color", "--no-links"], "hosts": stored},
        ]
    )
    if os.name != "nt":
        cases.extend(
            [
                {"name": "TTY status", "args": ["auth", "status"], "hosts": stored, "tty": True},
                {"name": "TTY host selection", "args": ["auth", "switch"], "hosts": stored, "tty": True},
            ]
        )
    cases.extend(
        [
            {
                "name": "gzip status",
                "args": ["auth", "status", "--json", "*"],
                "hosts": stored,
                "encoding": "gzip",
                "json": True,
            },
            {
                "name": "zstd status",
                "args": ["auth", "status", "--json", "*"],
                "hosts": stored,
                "encoding": "zstd",
                "json": True,
            },
            {
                "name": "field validation detail",
                "args": ["auth", "status"],
                "hosts": stored,
                "exit": 1,
                "responses": {
                    ("GET", "/users/me"): (
                        422,
                        {"detail": [{"loc": ["body", "grants", 0, "scope"], "msg": "invalid scope"}]},
                    )
                },
            },
            {"name": "jq halt", "args": ["auth", "status", "--json", "email", "-q", ".email, halt"], "hosts": stored},
            {
                "name": "missing login session",
                "args": ["auth", "login"],
                "login": True,
                "responses": login_responses,
                "callback": "",
                "exit": 1,
                "error_terms": ["Authentication failed", "no session received"],
            },
            {
                "name": "SSO discovery failure",
                "args": ["auth", "login", "--email", "fixture@example.test"],
                "responses": {("POST", "/sso/discover"): (503, {})},
                "exit": 1,
                "error_terms": ["SSO"],
            },
        ]
    )
    for status in [401, 403, 404]:
        cases.append(
            {
                "name": f"Git scoped HTTP {status}",
                "args": helper,
                "stdin": protocol + "\n",
                "env": {"AVR_TOKEN": "avs_fixture"},
                "exit": 4 if status == 401 else 1,
                "responses": {("POST", "/tokens/git"): (status, {"detail": "outside fixture scope"})},
            }
        )
    return cases


def main(
    case_factory=scenarios, compare_results=compare, default_report="build/go/auth-parity.json", prepare_oracle=None
):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", required=True)
    parser.add_argument("--report", default=default_report)
    parser.add_argument("--jq-dir", default="build/go/oracle")
    args = parser.parse_args()
    jq_dir = Path(args.jq_dir).resolve()
    os.environ["PATH"] = str(jq_dir) + os.pathsep + os.environ["PATH"]
    jq_version = subprocess.check_output(["jq", "--version"], text=True).strip()
    if jq_version not in {"jq-1.7.1", "jq-1.7.1-apple"}:
        raise SystemExit("Use the pinned oracle: python scripts/setup_parity_jq.py")
    root = Path(__file__).resolve().parents[1]
    # Pin the Python oracle independently of local edits to Python source.
    archive = subprocess.check_output(["git", "archive", BASELINE, "avrea_cli"], cwd=root)
    report = {
        "python_baseline": BASELINE,
        "python_version": sys.version,
        "go_binary_sha256": hashlib.sha256(Path(args.go).read_bytes()).hexdigest(),
        "go_version": subprocess.check_output([str(Path(args.go).resolve()), "--version"], text=True).strip(),
        "jq_oracle": jq_version,
        "scenarios": [],
    }
    with tempfile.TemporaryDirectory(prefix="avr-oracle-") as oracle:
        with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
            tar.extractall(oracle, filter="data")
        if prepare_oracle is not None:
            prepare_oracle(Path(oracle))
        server = cast(Any, http.server.ThreadingHTTPServer(("127.0.0.1", 0), Fixture))
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        host = f"http://127.0.0.1:{server.server_port}"
        try:
            for scenario in case_factory():
                results = []
                for executable in ([sys.executable, "-m", "avrea_cli.main"], [str(Path(args.go).resolve())]):
                    server.requests = []
                    server.expand_host = scenario.get("expand_host", False)
                    server.fixture_host = host
                    server.responses = copy.deepcopy(
                        {
                            (method, canonical_path(path)): response
                            for (method, path), response in scenario.get("responses", {}).items()
                        }
                    )
                    server.dynamic_responses = copy.deepcopy(scenario.get("dynamic_responses", {}))
                    server.encoding = scenario.get("encoding")
                    result = run(executable, scenario, Path(oracle), host)
                    result["requests"] = copy.deepcopy(server.requests)
                    results.append(result)
                differences = compare_results(scenario, *results)
                report["scenarios"].append(
                    {"name": scenario["name"], "python": results[0], "go": results[1], "differences": differences}
                )
                print(("FAIL" if differences else "PASS"), scenario["name"], ", ".join(differences))
        finally:
            server.shutdown()
            server.server_close()
            worker.join()
    target = Path(args.report)
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(json.dumps(report, indent=2) + "\n")
    return int(any(s["differences"] for s in report["scenarios"]))


if __name__ == "__main__":
    sys.exit(main())
