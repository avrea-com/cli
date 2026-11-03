"""Authenticated observability frames shared by VM and job metrics watchers."""

__lazy_modules__ = ["websockets.sync.client", "websockets.exceptions"]

from avrea_cli.config import CliConfig
from collections.abc import Iterator
from typing import Any
from urllib.parse import urlsplit
from urllib.parse import urlunsplit
from websockets.exceptions import ConnectionClosed
from websockets.exceptions import InvalidHandshake
from websockets.exceptions import InvalidStatus
from websockets.sync.client import ClientConnection
from websockets.sync.client import connect
import certifi
import click
import json
import ssl
import time

_MAX_CONSECUTIVE_FAILURES = 4


def _connection_without_redirects() -> type[ClientConnection]:
    # Newer websockets releases follow redirects during connect(). Refuse them
    # inside the handshake, before the client can reuse authentication headers.
    class Connection(ClientConnection):
        def handshake(
            self, additional_headers: Any = None, user_agent_header: str | None = None, timeout: float | None = None
        ) -> None:
            try:
                super().handshake(additional_headers, user_agent_header, timeout)
            except InvalidStatus as exc:
                if 300 <= exc.response.status_code < 400:
                    raise click.ClickException("Refusing metrics stream redirect.") from None
                raise

    return Connection


def _ssl_context(scheme: str) -> ssl.SSLContext | None:
    # Trust the same CA bundle as the REST client, which verifies with certifi.
    return ssl.create_default_context(cafile=certifi.where()) if scheme == "wss" else None


def metrics_frames(config: CliConfig, path: str) -> Iterator[dict[str, Any] | None]:
    """Yield frames, or None on an idle tick so callers can refresh their logs.

    Every bundle replaces the preceding snapshot, including after reconnection.
    The synchronous WebSocket handshake refuses redirects, keeping authorization
    on the configured host. Completion belongs to the signal, not the socket.
    """
    base = urlsplit(config.public_api_url)
    scheme = "wss" if base.scheme == "https" else "ws"
    url = urlunsplit((scheme, base.netloc, base.path.rstrip("/") + path, "signals=metrics", ""))
    last_error: str | None = None
    failures = 0
    cause = ""
    while True:
        try:
            with connect(
                url,
                additional_headers=config.get_api_headers(),
                open_timeout=10,
                close_timeout=5,
                max_size=16 * 1024 * 1024,
                # The REST client ignores proxy environment variables; match it.
                proxy=None,
                ssl=_ssl_context(scheme),
                create_connection=_connection_without_redirects(),
            ) as connection:
                while True:
                    try:
                        raw = connection.recv(timeout=1)
                    except TimeoutError:
                        yield None
                        continue
                    try:
                        frame = json.loads(raw)
                    except (ValueError, UnicodeError) as exc:
                        raise click.ClickException("Invalid JSON in metrics stream.") from exc
                    if not isinstance(frame, dict):
                        raise click.ClickException("Invalid metrics stream frame.")
                    if frame.get("signal") not in (None, "metrics"):
                        continue
                    if frame.get("type") == "error":
                        message = str(frame.get("message") or "Metrics stream error")
                        if frame.get("signal") is None:
                            raise click.ClickException(message)
                        last_error = message
                    if frame.get("type") == "bundle":
                        last_error = None
                        failures = 0  # The budget covers consecutive failures, not a whole watch.
                    if frame.get("type") == "complete":
                        if last_error is not None:
                            raise click.ClickException(last_error)
                        connection.close()
                        yield frame
                        return
                    yield frame
        except InvalidStatus as exc:
            code = exc.response.status_code
            if code != 429 and code < 500:
                raise click.ClickException(f"Metrics stream refused: HTTP {code}.") from None
            cause = f"HTTP {code}"
        except ConnectionClosed as exc:
            if exc.rcvd is not None and exc.rcvd.code in (1008, 4001, 4003, 4401, 4403):
                raise click.ClickException(f"Metrics stream refused: {exc.rcvd.reason or 'access denied'}.") from None
            cause = f"{type(exc).__name__}: {exc}"
        except (OSError, InvalidHandshake) as exc:
            # TimeoutError is an OSError. Keep the cause so TLS, DNS or proxy
            # trouble is diagnosable once the retry budget runs out.
            cause = f"{type(exc).__name__}: {exc}"
        failures += 1
        if failures == _MAX_CONSECUTIVE_FAILURES:
            raise click.ClickException(
                f"Metrics stream disconnected; reconnect attempts exhausted (last error: {cause})."
            )
        delay = 2 ** (failures - 1)
        click.echo(f"Metrics stream disconnected; reconnecting in {delay}s...", err=True)
        time.sleep(delay)
