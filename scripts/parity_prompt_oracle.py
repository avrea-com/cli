"""Run hidden-prompt fixtures through Python's standard piped-input fallback.

Windows getpass reads the console even when stdin is a pipe. The fixture has no
interactive console, so select the same standard-library fallback used on Unix.
The pinned command still performs prompting, validation, and credential handling.
This adapter is only selected for explicit hidden-prompt parity scenarios.
"""

from unittest.mock import patch
import getpass
import runpy


def main() -> None:
    with patch("getpass.getpass", getpass.fallback_getpass):
        runpy.run_module("avrea_cli.main", run_name="__main__")


if __name__ == "__main__":
    main()
