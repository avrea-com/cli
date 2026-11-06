"""Verify docs find a bundled README beside the native executable."""

from pathlib import Path
import os
import pytest
import shutil
import subprocess


def test_docs_default_bundled_readme(tmp_path):
    value = os.environ.get("AVR_GO_BINARY")
    if not value:
        pytest.skip("Set AVR_GO_BINARY to the public or admin executable")
    assert value is not None
    original = Path(value).resolve()
    package = tmp_path / "package"
    package.mkdir()
    binary = package / ("avr.exe" if os.name == "nt" else "avr")
    shutil.copy2(original, binary)
    readme = "# Bundled CLI\n\nThis overview ships with the package.\n"
    (package / "README.md").write_text(readme)
    env = {key: value for key, value in os.environ.items() if not key.startswith("AVR_")}
    env.update(AVR_CONFIG_DIR=str(tmp_path / "config"), AVR_HOST="http://127.0.0.1:1")
    result = subprocess.run(
        [str(binary), "internal", "docs", "--format", "starlight", "--out", "out"],
        cwd=tmp_path,
        env=env,
        capture_output=True,
        text=True,
        timeout=30,
        check=False,
    )
    assert result.returncode == 0, result.stderr
    assert "This overview ships with the package." in (tmp_path / "out/index.md").read_text()
