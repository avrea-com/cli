"""Local API data for native VM process tests; no real service or SSH connection."""

from http.server import BaseHTTPRequestHandler
from typing import Any
from typing import cast
import json

TOKEN = "avr_fixture_key_never_a_real_secret"
ORG = "org-019a0000000070008000000000000001"
VM = "cvm-019a0000000070008000000000000001"
SHOW = ("GET", f"/orgs/{ORG}/vms/{VM}")
KEY = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFixtureHost fixture"
EP = {
    "protocol": "ssh",
    "external_ip": "192.0.2.1",
    "external_port": 30022,
    "username": "runner",
    "host_key": KEY,
}
ROW = {
    "customer_vm_id": VM,
    "display_name": "Dev 🦫 box",
    "os_type": "linux",
    "cpu_count": 2,
    "memory_mb": 2048,
    "disk_gb": 20,
    "enable_remote_desktop": False,
    "ssh_public_keys": ["ssh-ed25519 AAAA fixture@example.test"],
    "architecture": "x86_64",
    "state": "PENDING",
    "state_reason": None,
    "endpoints": None,
    "stop_at": "2026-10-30T12:00:00Z",
    "created_at": "2026-10-30T04:00:00.123456Z",
    "updated_at": "2026-10-30T04:00:00Z",
    "extra": {"unknown": None},
}


class Fixture(BaseHTTPRequestHandler):
    def do_GET(self):
        server = cast(Any, self.server)
        server.requests.append({"method": "GET", "path": self.path})
        if self.headers.get("Authorization") != f"Bearer {TOKEN}":
            status, payload = 401, {"detail": "Fixture credentials required"}
        else:
            status, payload = server.responses.get(("GET", self.path), (404, {"detail": "Unexpected fixture route"}))
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass
