"""Avrea CLI package."""

from importlib.metadata import PackageNotFoundError
from importlib.metadata import version as _pkg_version

try:
    __version__ = _pkg_version("avr-cli")
except PackageNotFoundError:
    __version__ = "0.0.0"
