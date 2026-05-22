"""httpx transport that negotiates and decompresses zstd-encoded responses.

Python 3.14 stdlib (compression.zstd); no third-party deps.
httpx >=0.28 auto-decompresses gzip/deflate but not zstd, so we transparently
decompress zstd here and strip the Content-Encoding header before httpx sees it.
"""

from compression.zstd import decompress as zstd_decompress
import httpx


def _ensure_accept_encoding(request: httpx.Request) -> None:
    """Prepend zstd to Accept-Encoding (httpx auto-fills 'gzip, deflate' for us)."""
    existing = request.headers.get("Accept-Encoding", "")
    if "zstd" in existing.lower():
        return
    request.headers["Accept-Encoding"] = f"zstd, {existing}" if existing else "zstd, gzip"


def _needs_zstd_decode(response: httpx.Response) -> bool:
    """True iff the response is zstd-encoded; raise for anything we don't recognise.

    httpx handles gzip natively (we leave Content-Encoding: gzip intact).
    For unknown encodings we raise — server bugs should surface, not hide.
    """
    encoding = response.headers.get("Content-Encoding", "").lower()
    if encoding in {"", "identity", "gzip", "deflate"}:
        return False
    if encoding != "zstd":
        raise ValueError(f"Unsupported Content-Encoding: {encoding!r}")
    return True


def _build_decoded_response(response: httpx.Response, raw: bytes, request: httpx.Request) -> httpx.Response:
    """Return a new Response with the zstd body decoded and Content-Encoding stripped.

    `request` is taken from the transport method's parameter rather than
    `response.request` because httpx assigns `response.request` *after* the
    transport returns; reading it here raises RuntimeError in real flows.
    Tests that pass `request=` into a MockTransport's Response masked this.
    """
    decoded = zstd_decompress(raw)
    # multi_items() preserves repeated headers (Set-Cookie etc.); items() would
    # comma-join them, breaking cookies whose values can legitimately contain commas.
    new_headers = httpx.Headers([(k, v) for k, v in response.headers.multi_items() if k.lower() != "content-encoding"])
    new_headers["Content-Length"] = str(len(decoded))
    return httpx.Response(
        status_code=response.status_code,
        headers=new_headers,
        content=decoded,
        request=request,
        extensions=response.extensions,
        history=response.history,
    )


class CompressingTransport(httpx.BaseTransport):
    __slots__ = ("_inner",)

    def __init__(self, inner: httpx.BaseTransport | None = None) -> None:
        self._inner = inner if inner is not None else httpx.HTTPTransport()

    def handle_request(self, request: httpx.Request) -> httpx.Response:
        _ensure_accept_encoding(request)
        response = self._inner.handle_request(request)
        if not _needs_zstd_decode(response):
            return response
        return _build_decoded_response(response, response.read(), request)

    def close(self) -> None:
        self._inner.close()
