"""XDG-compliant paths for avr-cli.

Resolution order for the config dir:
    1. ``AVR_CONFIG_DIR`` — explicit override
    2. ``XDG_CONFIG_HOME/avrea`` — Linux/XDG convention (also used on macOS
       unless ``AVR_MACOS_NATIVE_PATHS=1`` flips to ``~/Library/...``)
    3. ``~/.config/avrea`` — final fallback
"""

from dataclasses import dataclass
from pathlib import Path
from platformdirs import PlatformDirs
from platformdirs.unix import Unix
import os
import sys

APP_NAME = "avrea"


def _resolve_config_dir() -> Path:
    """Pick the directory where ``hosts.json`` lives."""
    override = os.environ.get("AVR_CONFIG_DIR")
    if override:
        return Path(override).expanduser()
    if sys.platform == "darwin" and os.environ.get("AVR_MACOS_NATIVE_PATHS") != "1":
        # Linux-style ~/.config/avrea on macOS by default — matches dev-server
        # tooling and avoids splitting credentials across two paths when
        # working between macOS and Linux containers.
        return Path(Unix(APP_NAME, ensure_exists=False).user_config_dir)
    return Path(PlatformDirs(APP_NAME, ensure_exists=False).user_config_dir)


@dataclass(frozen=True)
class AvrPaths:
    """Resolved paths for the avr-cli application."""

    config_dir: Path

    @property
    def hosts_file(self) -> Path:
        """Per-host credentials store, keyed by hostname."""
        return self.config_dir / "hosts.json"


PATHS = AvrPaths(config_dir=_resolve_config_dir())
