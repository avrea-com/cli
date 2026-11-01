"""Verify docs find a bundled README beside the native executable."""

from pathlib import Path
from scripts import compare_auth as harness
import io
import os
import pytest
import shutil
import subprocess
import sys
import tarfile


def test_docs_default_bundled_readme_matches_python(tmp_path):
    value = os.environ.get("AVR_GO_BINARY")
    if not value:
        pytest.skip("Set AVR_GO_BINARY to the public or admin executable")
    assert value is not None
    original = Path(value).resolve()
    package = tmp_path / "package"
    package.mkdir()
    binary = package / ("avr.exe" if os.name == "nt" else "avr")
    shutil.copy2(original, binary)
    oracle = tmp_path / "oracle"
    oracle.mkdir()
    root = Path(__file__).resolve().parents[1]
    archive = subprocess.check_output(["git", "archive", harness.BASELINE, "avrea_cli"], cwd=root)
    with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
        tar.extractall(oracle, filter="data")
    readme = "# Bundled CLI\n\nThis overview ships with the package.\n"
    (package / "README.md").write_text(readme)
    (oracle / "README.md").write_text(readme)
    scenario = {
        "args": ["internal", "docs", "--format", "starlight", "--out", "out"],
        "env": {},
        "capture_files": ["out/index.md"],
        "capture_text_files": True,
        "normalize_file_newlines": True,
    }
    python = harness.run([sys.executable, "-m", "avrea_cli.main"], scenario, oracle, "http://127.0.0.1:1")
    native = harness.run([str(binary)], scenario, oracle, "http://127.0.0.1:1")
    assert python["exit"] == native["exit"] == 0
    assert python["stdout"] == native["stdout"]
    assert python["files"] == native["files"]
    assert "This overview ships with the package." in native["files"]["out/index.md"]["text"]
