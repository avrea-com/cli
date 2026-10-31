"""Run the pinned Python login oracle with deterministic loopback reverse DNS.

HTTPServer resolves its server_name during construction. Hosted macOS runners
can stall in that lookup before the callback listener is ready. Only the cosmetic
loopback name is supplied here; binding, callback HTTP, credentials, and all other
DNS lookups retain their normal behavior.
"""

from collections.abc import Callable
from unittest.mock import patch
import runpy
import socket


def loopback_fqdn(name: str, resolve: Callable[[str], str]) -> str:
    if name in {"localhost", "127.0.0.1", "::1"}:
        return "localhost"
    return resolve(name)


def main() -> None:
    resolve = socket.getfqdn
    with patch("socket.getfqdn", side_effect=lambda name="": loopback_fqdn(name, resolve)):
        runpy.run_module("avrea_cli.main", run_name="__main__")


if __name__ == "__main__":
    main()
