"""Exercise the shared WebSocket consumer independently of terminal rendering."""

from avrea_cli.config import CliConfig
from avrea_cli.metrics_stream import metrics_frames
from collections import deque
from typing import Any
from websockets.datastructures import Headers
from websockets.exceptions import ConnectionClosedError
from websockets.exceptions import InvalidStatus
from websockets.frames import Close
from websockets.http11 import Response
from websockets.sync.client import ClientConnection
import click
import json
import pytest


class Socket:
    def __init__(self, frames: list[Any]) -> None:
        self.frames = deque(frames)
        self.closed = False

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.closed = True

    def close(self):
        self.closed = True

    def recv(self, timeout=None):
        frame = self.frames.popleft()
        if isinstance(frame, BaseException):
            raise frame
        return json.dumps(frame)


def config(monkeypatch) -> CliConfig:
    monkeypatch.setenv("AVR_TOKEN", "secret-token")
    monkeypatch.setenv("AVR_HOST", "https://api.example/base")
    return CliConfig()


def test_authentication_url_frames_and_completion(monkeypatch):
    cfg = config(monkeypatch)
    sock = Socket(
        [
            TimeoutError(),
            {"signal": "status", "type": "changed"},
            {"signal": "metrics", "type": "window", "running": True, "start": 100, "end": 120},
            {"signal": "metrics", "type": "bundle", "metrics": {"cpu": {"series": []}}},
            {"signal": "metrics", "type": "complete"},
        ]
    )
    calls = []
    monkeypatch.setattr("avrea_cli.metrics_stream.connect", lambda url, **kw: calls.append((url, kw)) or sock)
    frames = list(metrics_frames(cfg, "/vms/job-1/observability/stream"))
    assert frames[0] is None
    assert [f["type"] for f in frames if f is not None] == ["window", "bundle", "complete"]
    assert calls[0][0] == "wss://api.example/base/vms/job-1/observability/stream?signals=metrics"
    assert calls[0][1]["additional_headers"]["Authorization"] == "Bearer secret-token"
    assert calls[0][1]["open_timeout"] == 10
    assert calls[0][1]["proxy"] is None
    assert calls[0][1]["ssl"] is not None
    assert sock.closed


def test_transport_retry_uses_fresh_backfill(monkeypatch):
    cfg = config(monkeypatch)
    sockets = [
        Socket([{"type": "bundle", "metrics": {"cpu": {"old": True}}}, OSError("offline")]),
        Socket([{"type": "bundle", "metrics": {}}, {"type": "complete"}]),
    ]
    pending = deque(sockets)
    delays = []
    monkeypatch.setattr("avrea_cli.metrics_stream.connect", lambda *a, **kw: pending.popleft())
    monkeypatch.setattr("avrea_cli.metrics_stream.time.sleep", delays.append)
    frames = list(metrics_frames(cfg, "/stream"))
    assert [f["metrics"] for f in frames if f and f.get("type") == "bundle"] == [{"cpu": {"old": True}}, {}]
    assert delays == [1]
    assert all(s.closed for s in sockets)


def test_retries_are_bounded(monkeypatch):
    cfg = config(monkeypatch)
    calls = []
    delays = []

    def connect(*a, **kw):
        calls.append(1)
        raise OSError("offline")

    monkeypatch.setattr("avrea_cli.metrics_stream.connect", connect)
    monkeypatch.setattr("avrea_cli.metrics_stream.time.sleep", delays.append)
    with pytest.raises(click.ClickException, match=r"exhausted \(last error: OSError: offline\)"):
        list(metrics_frames(cfg, "/stream"))
    assert len(calls) == 4
    assert delays == [1, 2, 4]


def test_retry_budget_resets_after_streaming_resumes(monkeypatch):
    cfg = config(monkeypatch)
    drop = [{"type": "bundle", "metrics": {}}, OSError("dropped")]
    pending = deque([Socket(drop) for _ in range(6)] + [Socket([{"type": "complete"}])])
    delays = []
    monkeypatch.setattr("avrea_cli.metrics_stream.connect", lambda *a, **kw: pending.popleft())
    monkeypatch.setattr("avrea_cli.metrics_stream.time.sleep", delays.append)
    frames = list(metrics_frames(cfg, "/stream"))
    assert [f["type"] for f in frames if f][-1] == "complete"
    assert delays == [1] * 6


@pytest.mark.parametrize("code", [401, 403, 404])
def test_handshake_refusal_does_not_retry(monkeypatch, code):
    cfg = config(monkeypatch)

    def connect(*a, **kw):
        raise InvalidStatus(Response(code, "refused", Headers()))

    monkeypatch.setattr("avrea_cli.metrics_stream.connect", connect)
    monkeypatch.setattr("avrea_cli.metrics_stream.time.sleep", lambda _: pytest.fail("must not retry"))
    with pytest.raises(click.ClickException, match=f"HTTP {code}"):
        list(metrics_frames(cfg, "/stream"))


def test_policy_close_is_terminal(monkeypatch):
    cfg = config(monkeypatch)
    sock = Socket([ConnectionClosedError(Close(1008, "access revoked"), None)])
    monkeypatch.setattr("avrea_cli.metrics_stream.connect", lambda *a, **kw: sock)
    with pytest.raises(click.ClickException, match="access revoked"):
        list(metrics_frames(cfg, "/stream"))
    assert sock.closed


def test_plain_ws_has_no_tls_context(monkeypatch):
    config(monkeypatch)
    monkeypatch.setenv("AVR_HOST", "http://127.0.0.1:8080")
    cfg = CliConfig()
    calls = []
    sock = Socket([{"type": "complete"}])
    monkeypatch.setattr("avrea_cli.metrics_stream.connect", lambda url, **kw: calls.append((url, kw)) or sock)
    list(metrics_frames(cfg, "/stream"))
    assert calls[0][0].startswith("ws://")
    assert calls[0][1]["ssl"] is None


def test_stream_errors_are_yielded_and_can_recover(monkeypatch, capsys):
    cfg = config(monkeypatch)
    sock = Socket(
        [
            {"signal": "metrics", "type": "error", "message": "backend unavailable"},
            {"signal": "metrics", "type": "state", "source": "polling"},
            {"signal": "metrics", "type": "bundle", "metrics": {}},
            {"signal": "metrics", "type": "complete"},
        ]
    )
    monkeypatch.setattr("avrea_cli.metrics_stream.connect", lambda *a, **kw: sock)
    frames = list(metrics_frames(cfg, "/stream"))
    assert [f["type"] for f in frames if f] == ["error", "state", "bundle", "complete"]
    # Rendering owns the error, so a cleared live frame cannot wipe it.
    assert capsys.readouterr() == ("", "")


def test_connection_error_frame_is_terminal(monkeypatch):
    cfg = config(monkeypatch)
    sock = Socket([{"type": "error", "message": "Access denied"}])
    monkeypatch.setattr("avrea_cli.metrics_stream.connect", lambda *a, **kw: sock)
    with pytest.raises(click.ClickException, match="Access denied"):
        list(metrics_frames(cfg, "/stream"))
    assert sock.closed


def test_redirect_is_refused_before_headers_can_be_forwarded(monkeypatch):
    cfg = config(monkeypatch)

    def handshake(self, *args):
        raise InvalidStatus(Response(302, "Found", Headers({"Location": "https://other.example"})))

    monkeypatch.setattr(ClientConnection, "handshake", handshake)

    def connect(*a, **kw):
        cls = kw["create_connection"]
        cls.handshake(object.__new__(cls))
        pytest.fail("redirect should have been refused")

    monkeypatch.setattr("avrea_cli.metrics_stream.connect", connect)
    with pytest.raises(click.ClickException, match="redirect"):
        list(metrics_frames(cfg, "/stream"))


def test_interrupt_closes_socket(monkeypatch):
    cfg = config(monkeypatch)
    sock = Socket([KeyboardInterrupt()])
    monkeypatch.setattr("avrea_cli.metrics_stream.connect", lambda *a, **kw: sock)
    with pytest.raises(KeyboardInterrupt):
        list(metrics_frames(cfg, "/stream"))
    assert sock.closed


def test_unrecovered_backend_error_exits_nonzero(monkeypatch):
    cfg = config(monkeypatch)
    sock = Socket(
        [
            {"signal": "metrics", "type": "error", "message": "backend failure"},
            {"signal": "metrics", "type": "complete"},
        ]
    )
    monkeypatch.setattr("avrea_cli.metrics_stream.connect", lambda *a, **kw: sock)
    with pytest.raises(click.ClickException, match="backend failure"):
        list(metrics_frames(cfg, "/stream"))
    assert sock.closed


def test_real_socket_authentication_and_completion(monkeypatch):
    from threading import Thread
    from websockets.sync.server import serve

    requests = []

    def handler(connection):
        requests.append(connection.request)
        connection.send(json.dumps({"signal": "metrics", "type": "bundle", "metrics": {"cpu": {"series": []}}}))
        connection.send(json.dumps({"signal": "metrics", "type": "complete"}))

    with serve(handler, "127.0.0.1", 0) as server:
        thread = Thread(target=server.serve_forever, daemon=True)
        thread.start()
        port = server.socket.getsockname()[1]
        monkeypatch.setenv("AVR_HOST", f"http://127.0.0.1:{port}")
        monkeypatch.setenv("AVR_TOKEN", "local-test-key")
        try:
            frames = list(metrics_frames(CliConfig(), "/vms/job-1/observability/stream"))
        finally:
            server.shutdown()
            thread.join(timeout=5)
    assert [frame["type"] for frame in frames if frame] == ["bundle", "complete"]
    assert requests[0].headers["Authorization"] == "Bearer local-test-key"
    assert requests[0].path == "/vms/job-1/observability/stream?signals=metrics"


def test_real_socket_redirect_is_not_followed(monkeypatch):
    from threading import Thread
    from websockets.sync.server import serve

    requests = []

    def redirect(connection, request):
        requests.append(request)
        return Response(302, "Found", Headers({"Location": f"ws://{request.headers['Host']}/other"}))

    with serve(lambda connection: None, "127.0.0.1", 0, process_request=redirect) as server:
        thread = Thread(target=server.serve_forever, daemon=True)
        thread.start()
        port = server.socket.getsockname()[1]
        monkeypatch.setenv("AVR_HOST", f"http://127.0.0.1:{port}")
        monkeypatch.setenv("AVR_TOKEN", "local-test-key")
        try:
            with pytest.raises(click.ClickException, match="redirect"):
                list(metrics_frames(CliConfig(), "/stream"))
        finally:
            server.shutdown()
            thread.join(timeout=5)
    assert len(requests) == 1
    assert requests[0].path == "/stream?signals=metrics"
