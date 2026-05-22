"""Tests for the zstd-aware httpx transport used by ApiClient."""

from avrea_cli.transport import CompressingTransport
from compression.zstd import compress as zstd_compress
import gzip
import httpx
import pytest


def _stub_transport(
    body: bytes,
    headers: dict[str, str],
    status: int = 200,
) -> httpx.MockTransport:
    """Mock transport that *does not* pre-populate `request` on the Response.

    Real `HTTPTransport` returns a Response without `.request` set; httpx assigns
    it later in `_send_single_request`. Passing `request=request` here would mask
    bugs that read `response.request` inside the transport.
    """

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(status_code=status, headers=headers, content=body)

    return httpx.MockTransport(handler)


def test_adds_accept_encoding_when_absent():
    seen: dict[str, str] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["accept-encoding"] = request.headers.get("Accept-Encoding", "")
        return httpx.Response(200, content=b'{"ok":true}', headers={"Content-Type": "application/json"})

    inner = httpx.MockTransport(handler)
    transport = CompressingTransport(inner=inner)
    with httpx.Client(transport=transport) as client:
        client.get("https://example.test/")

    assert "zstd" in seen["accept-encoding"]
    assert "gzip" in seen["accept-encoding"]


def test_prepends_zstd_to_user_set_encoding():
    """A caller's explicit Accept-Encoding gets zstd prepended, not replaced."""
    seen: dict[str, str] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["accept-encoding"] = request.headers.get("Accept-Encoding", "")
        return httpx.Response(200, content=b'{"ok":true}', headers={"Content-Type": "application/json"})

    inner = httpx.MockTransport(handler)
    transport = CompressingTransport(inner=inner)
    with httpx.Client(transport=transport) as client:
        client.get("https://example.test/", headers={"Accept-Encoding": "br"})

    assert "zstd" in seen["accept-encoding"]
    assert "br" in seen["accept-encoding"]


def test_does_not_duplicate_zstd():
    seen: dict[str, str] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["accept-encoding"] = request.headers.get("Accept-Encoding", "")
        return httpx.Response(200, content=b'{"ok":true}', headers={"Content-Type": "application/json"})

    inner = httpx.MockTransport(handler)
    transport = CompressingTransport(inner=inner)
    with httpx.Client(transport=transport) as client:
        client.get("https://example.test/", headers={"Accept-Encoding": "zstd, gzip"})

    assert seen["accept-encoding"].count("zstd") == 1


def test_decompresses_zstd_response():
    payload = b'{"items": [1, 2, 3, 4, 5]}'
    compressed = zstd_compress(payload)
    inner = _stub_transport(
        compressed,
        {"Content-Encoding": "zstd", "Content-Type": "application/json"},
    )
    transport = CompressingTransport(inner=inner)
    with httpx.Client(transport=transport) as client:
        response = client.get("https://example.test/")

    assert response.status_code == 200
    assert response.content == payload
    assert "content-encoding" not in {k.lower(): v for k, v in response.headers.items()}
    assert int(response.headers["Content-Length"]) == len(payload)


def test_passes_through_gzip_for_httpx_to_handle():
    payload = b'{"items": [1, 2, 3]}'
    compressed = gzip.compress(payload)
    inner = _stub_transport(
        compressed,
        {"Content-Encoding": "gzip", "Content-Type": "application/json"},
    )
    transport = CompressingTransport(inner=inner)
    with httpx.Client(transport=transport) as client:
        response = client.get("https://example.test/")

    assert response.status_code == 200
    assert response.content == payload  # httpx auto-decompresses gzip


def test_decoded_response_has_request_set():
    """Regression: response.request must be readable after our transport returns.
    Real HTTPTransport doesn't pre-populate it, so we must pass it through ourselves
    rather than reading response.request inside the transport (which raises)."""
    payload = b'{"ok": true}'
    compressed = zstd_compress(payload)

    def handler(request: httpx.Request) -> httpx.Response:
        # Deliberately do NOT pass request= — match real HTTPTransport behaviour
        return httpx.Response(
            200,
            headers={"Content-Encoding": "zstd", "Content-Type": "application/json"},
            content=compressed,
        )

    inner = httpx.MockTransport(handler)
    transport = CompressingTransport(inner=inner)
    with httpx.Client(transport=transport) as client:
        response = client.get("https://example.test/")

    assert response.content == payload
    # Request was set (no RuntimeError); url comes from the original request
    assert str(response.request.url) == "https://example.test/"


def test_decompress_preserves_duplicate_headers():
    """Set-Cookie can repeat — collapsing via comma-join would corrupt cookies."""
    payload = b'{"ok": true}'
    compressed = zstd_compress(payload)

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            headers=[
                ("Content-Encoding", "zstd"),
                ("Content-Type", "application/json"),
                ("Set-Cookie", "session=abc; Path=/"),
                ("Set-Cookie", "csrf=xyz; Path=/"),
            ],
            content=compressed,
            request=request,
        )

    inner = httpx.MockTransport(handler)
    transport = CompressingTransport(inner=inner)
    with httpx.Client(transport=transport) as client:
        response = client.get("https://example.test/")

    set_cookies = [v for k, v in response.headers.multi_items() if k.lower() == "set-cookie"]
    assert set_cookies == ["session=abc; Path=/", "csrf=xyz; Path=/"]


def test_passes_through_identity():
    payload = b'{"ok": true}'
    inner = _stub_transport(payload, {"Content-Type": "application/json"})
    transport = CompressingTransport(inner=inner)
    with httpx.Client(transport=transport) as client:
        response = client.get("https://example.test/")

    assert response.content == payload


def test_unknown_encoding_raises():
    inner = _stub_transport(
        b"\x00" * 16,
        {"Content-Encoding": "br", "Content-Type": "application/json"},
    )
    transport = CompressingTransport(inner=inner)
    with httpx.Client(transport=transport) as client:
        with pytest.raises(ValueError, match="Unsupported Content-Encoding"):
            client.get("https://example.test/")
