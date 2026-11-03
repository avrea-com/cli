"""Per-VM status notifications used to wake status polling."""

from avrea_cli.config import CliConfig
from avrea_cli.metrics_stream import _connection_without_redirects
from avrea_cli.metrics_stream import _ssl_context
from collections.abc import Callable
from collections.abc import Iterator
from contextlib import ExitStack
from contextlib import contextmanager
from urllib.parse import quote
from urllib.parse import urlsplit
from urllib.parse import urlunsplit
from websockets.exceptions import WebSocketException
from websockets.sync.client import connect
import click
import json
import time

# Floor between wakes, so bursts of change frames cannot turn into a GET storm.
_MIN_WAKE_INTERVAL = 1.0


@contextmanager
def vm_state_notifications(config: CliConfig, org_id: str, vm_id: str) -> Iterator[Callable[[float], None]]:
    """Wait for a status change of one VM or a bounded fallback interval.

    Connect lazily so an already-ready VM needs no socket. A failed stream
    falls back to timed polling for the rest of this wait.
    """
    connection = None
    unavailable = False

    with ExitStack() as stack:

        def wait(timeout: float) -> None:
            nonlocal connection, unavailable
            started = time.monotonic()
            deadline = started + timeout
            if not unavailable:
                try:
                    if connection is None:
                        base = urlsplit(config.public_api_url)
                        path = f"/orgs/{quote(org_id, safe='')}/vms/{quote(vm_id, safe='')}/observability/stream"
                        scheme = "wss" if base.scheme == "https" else "ws"
                        url = urlunsplit((scheme, base.netloc, base.path.rstrip("/") + path, "signals=status", ""))
                        connection = stack.enter_context(
                            connect(
                                url,
                                additional_headers=config.get_api_headers(),
                                open_timeout=timeout,
                                close_timeout=0,
                                proxy=None,
                                ssl=_ssl_context(scheme),
                                create_connection=_connection_without_redirects(),
                            )
                        )
                    while (remaining := deadline - time.monotonic()) > 0:
                        frame = json.loads(connection.recv(timeout=remaining))
                        if isinstance(frame, dict):
                            if frame.get("type") == "changed" and frame.get("signal") in (None, "status"):
                                floor = min(started + _MIN_WAKE_INTERVAL, deadline) - time.monotonic()
                                if floor > 0:
                                    time.sleep(floor)
                                return
                            if frame.get("type") == "error":
                                unavailable = True
                                break
                except TimeoutError:
                    # Only an idle recv is a fallback tick; a connect or
                    # handshake timeout means the stream is unusable.
                    if connection is not None:
                        return
                    unavailable = True
                except OSError, WebSocketException, ValueError, click.ClickException:
                    unavailable = True
            remaining = deadline - time.monotonic()
            if remaining > 0:
                time.sleep(remaining)

        yield wait
