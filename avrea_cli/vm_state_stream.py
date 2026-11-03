"""Organization VM change notifications used to wake status polling."""

from avrea_cli.config import CliConfig
from avrea_cli.metrics_stream import _connection_without_redirects
from collections.abc import Callable
from collections.abc import Iterator
from contextlib import contextmanager
from urllib.parse import quote
from urllib.parse import urlsplit
from urllib.parse import urlunsplit
from websockets.exceptions import WebSocketException
from websockets.sync.client import connect
import click
import json
import time


@contextmanager
def vm_state_notifications(config: CliConfig) -> Iterator[Callable[[str, float], None]]:
    """Wait for a status change or a bounded fallback interval.

    Connect lazily so an already-ready VM needs no socket. A failed stream
    falls back to timed polling for the rest of this wait.
    """
    connection = None
    unavailable = False

    def wait(org_id: str, timeout: float) -> None:
        nonlocal connection, unavailable
        deadline = time.monotonic() + timeout
        if not unavailable:
            try:
                if connection is None:
                    base = urlsplit(config.public_api_url)
                    path = f"/orgs/{quote(org_id, safe='')}/vms/observability/stream"
                    url = urlunsplit(
                        (
                            "wss" if base.scheme == "https" else "ws",
                            base.netloc,
                            base.path.rstrip("/") + path,
                            "signals=status",
                            "",
                        )
                    )
                    connection = connect(
                        url,
                        additional_headers=config.get_api_headers(),
                        open_timeout=timeout,
                        close_timeout=0,
                        create_connection=_connection_without_redirects(),
                    )
                while (remaining := deadline - time.monotonic()) > 0:
                    frame = json.loads(connection.recv(timeout=remaining))
                    if isinstance(frame, dict):
                        if frame.get("type") == "changed" and frame.get("signal") in (None, "status"):
                            return
                        if frame.get("type") == "error":
                            unavailable = True
                            break
            except TimeoutError:
                return
            except OSError, WebSocketException, ValueError, click.ClickException:
                unavailable = True
        remaining = deadline - time.monotonic()
        if remaining > 0:
            time.sleep(remaining)

    try:
        yield wait
    finally:
        if connection is not None:
            connection.close()
