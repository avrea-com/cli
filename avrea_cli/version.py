"""Build/version info for the avr CLI, exposed via User-Agent header.

Pattern mirrors the Go version package: Version + GitCommit + runtime info.
Version is read from installed package metadata; GitCommit is probed from
a local `.git` checkout if present, else "unknown". For installed builds
(RPM/pip wheel), the commit should be injected at build time via a
pre-generated `_build_info.py` module.
"""

from importlib import metadata
from pathlib import Path
import platform
import subprocess
import sys

# Build-time injected commit (present in RPM/wheel builds, absent in dev)
try:
    from avrea_cli import _build_info  # type: ignore[attr-defined]

    _INJECTED_COMMIT: str | None = getattr(_build_info, "GIT_COMMIT", None) or None
except ImportError:
    _INJECTED_COMMIT = None

IS_RELEASE_BUILD = _INJECTED_COMMIT is not None


def _get_version() -> str:
    try:
        return metadata.version("avr-cli")
    except metadata.PackageNotFoundError:
        return "dev"


def _get_git_commit() -> str:
    if _INJECTED_COMMIT:
        return _INJECTED_COMMIT[:8]

    # Fall back to probing the local repo (dev mode)
    try:
        repo_root = Path(__file__).resolve().parents[2]
        result = subprocess.run(
            ["git", "-C", str(repo_root), "rev-parse", "--short=8", "HEAD"],
            capture_output=True,
            text=True,
            timeout=2,
            check=False,
        )
        if result.returncode == 0:
            return result.stdout.strip() or "unknown"
    except FileNotFoundError, subprocess.TimeoutExpired:
        pass
    return "unknown"


def _get_user_agent() -> str:
    version = _get_version()
    commit = _get_git_commit()
    py = f"{sys.version_info.major}.{sys.version_info.minor}.{sys.version_info.micro}"
    os_name = platform.system().lower()  # darwin, linux, windows
    arch = platform.machine().lower()  # arm64, x86_64
    version_str = f"{version}+{commit}" if commit != "unknown" else version
    return f"avr-cli/{version_str} ({os_name} {arch}; python-{py})"


# Compute once at import time — doesn't change during a process lifetime
USER_AGENT = _get_user_agent()
