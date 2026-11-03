"""VM status notifications wake REST polling with a bounded fallback."""

from avrea_cli.vm_state_stream import vm_state_notifications
from tests.test_metrics_stream import Socket
from tests.test_metrics_stream import config


def test_changed_event_and_idle_fallback(monkeypatch):
    sock = Socket([{"signal": "metrics", "type": "bundle"}, {"signal": "status", "type": "changed"}, TimeoutError()])
    calls = []
    monkeypatch.setattr("avrea_cli.vm_state_stream.connect", lambda url, **kw: calls.append((url, kw)) or sock)
    with vm_state_notifications(config(monkeypatch)) as wait:
        assert not calls
        wait("org-1", 3)
        wait("org-1", 3)
    assert calls[0][0] == "wss://api.example/base/orgs/org-1/vms/observability/stream?signals=status"
    assert calls[0][1]["additional_headers"]["Authorization"] == "Bearer secret-token"
    assert sock.closed


def test_unavailable_stream_falls_back(monkeypatch):
    def offline(*args, **kwargs):
        raise OSError("offline")

    slept = []
    monkeypatch.setattr("avrea_cli.vm_state_stream.connect", offline)
    monkeypatch.setattr("avrea_cli.vm_state_stream.time.sleep", slept.append)
    with vm_state_notifications(config(monkeypatch)) as wait:
        wait("org-1", 3)
        wait("org-1", 2)
    assert len(slept) == 2
    assert 0 < slept[0] <= 3
    assert 0 < slept[1] <= 2
