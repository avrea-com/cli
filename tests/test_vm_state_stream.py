"""VM status notifications wake REST polling with a bounded fallback."""

from avrea_cli.vm_state_stream import vm_state_notifications
from tests.test_metrics_stream import Socket
from tests.test_metrics_stream import config


def test_changed_event_and_idle_fallback(monkeypatch):
    sock = Socket([{"signal": "metrics", "type": "bundle"}, {"signal": "status", "type": "changed"}, TimeoutError()])
    calls = []
    monkeypatch.setattr("avrea_cli.vm_state_stream.connect", lambda url, **kw: calls.append((url, kw)) or sock)
    monkeypatch.setattr("avrea_cli.vm_state_stream.time.sleep", lambda _: None)
    with vm_state_notifications(config(monkeypatch), "org-1", "cvm-1") as wait:
        assert not calls
        wait(3)
        wait(3)
    assert calls[0][0] == "wss://api.example/base/orgs/org-1/vms/cvm-1/observability/stream?signals=status"
    assert calls[0][1]["additional_headers"]["Authorization"] == "Bearer secret-token"
    assert calls[0][1]["proxy"] is None
    assert calls[0][1]["ssl"] is not None
    assert sock.closed


def test_change_burst_is_floored(monkeypatch):
    sock = Socket([{"type": "changed"}, {"type": "changed"}])
    clock = iter([0.0, 0.0, 0.1, 5.0, 5.0, 5.05])
    slept = []
    monkeypatch.setattr("avrea_cli.vm_state_stream.connect", lambda url, **kw: sock)
    monkeypatch.setattr("avrea_cli.vm_state_stream.time.monotonic", lambda: next(clock))
    monkeypatch.setattr("avrea_cli.vm_state_stream.time.sleep", slept.append)
    with vm_state_notifications(config(monkeypatch), "org-1", "cvm-1") as wait:
        wait(3)
        wait(3)
    assert [round(s, 2) for s in slept] == [0.9, 0.95]


def test_connect_timeout_marks_stream_unavailable(monkeypatch):
    calls = []

    def hang(*args, **kwargs):
        calls.append(1)
        raise TimeoutError("handshake")

    slept = []
    monkeypatch.setattr("avrea_cli.vm_state_stream.connect", hang)
    monkeypatch.setattr("avrea_cli.vm_state_stream.time.sleep", slept.append)
    with vm_state_notifications(config(monkeypatch), "org-1", "cvm-1") as wait:
        wait(3)
        wait(3)
    assert len(calls) == 1
    assert len(slept) == 2


def test_unavailable_stream_falls_back(monkeypatch):
    def offline(*args, **kwargs):
        raise OSError("offline")

    slept = []
    monkeypatch.setattr("avrea_cli.vm_state_stream.connect", offline)
    monkeypatch.setattr("avrea_cli.vm_state_stream.time.sleep", slept.append)
    with vm_state_notifications(config(monkeypatch), "org-1", "cvm-1") as wait:
        wait(3)
        wait(2)
    assert len(slept) == 2
    assert 0 < slept[0] <= 3
    assert 0 < slept[1] <= 2
