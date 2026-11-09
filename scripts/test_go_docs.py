"""Verify docs find a bundled README beside the native executable."""

from pathlib import Path
import os
import pytest
import shutil
import subprocess


def test_docs_check_includes_untracked_pages(tmp_path):
    if shutil.which("make") is None:
        pytest.skip("make is not installed")
    makefile = Path(__file__).resolve().parents[1] / "Makefile"
    subprocess.run(["git", "init", "-q", str(tmp_path)], check=True)
    command = ["make", "-f", str(makefile), "-o", "go-docs", "docs-check"]

    def check(expected):
        result = subprocess.run(command, cwd=tmp_path, capture_output=True, text=True, check=False)
        assert (result.returncode == 0) == expected, result.stdout + result.stderr

    (tmp_path / "unrelated.txt").write_text("unrelated")
    check(True)
    page = tmp_path / "docs/starlight/new.md"
    page.parent.mkdir(parents=True)
    page.write_text("new generated page")
    check(False)
    subprocess.run(["git", "add", "docs"], cwd=tmp_path, check=True)
    check(False)
    subprocess.run(
        [
            "git",
            "-c",
            "user.name=Fixture",
            "-c",
            "user.email=fixture@example.test",
            "-c",
            "commit.gpgsign=false",
            "commit",
            "-qm",
            "fixture",
        ],
        cwd=tmp_path,
        check=True,
    )
    check(True)
    page.write_text("changed generated page")
    check(False)


def test_docs_default_bundled_readme(tmp_path):
    value = os.environ.get("AVR_GO_BINARY")
    if not value:
        pytest.skip("Set AVR_GO_BINARY to the native avr executable")
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


def test_pre_commit_accepts_staged_docs_and_rejects_stale_output(tmp_path):
    make = shutil.which("make")
    bash = shutil.which("bash")
    if os.name == "nt" or make is None or bash is None:
        pytest.skip("requires POSIX make and bash")
    assert make is not None and bash is not None
    root = Path(__file__).resolve().parents[1]
    subprocess.run(["git", "init", "-q", str(tmp_path)], check=True)
    # Exercise the real hook and check recipes with a deterministic generator.
    # No Go build is needed to test index-versus-HEAD semantics.
    (tmp_path / "Makefile").write_text(
        (root / "Makefile").read_text() + "\ngo-build:\n\t@:\ngo-docs:\n\t@cp source.txt README.md\n"
    )
    source = tmp_path / "source.txt"
    readme = tmp_path / "README.md"
    source.write_text("current docs\n")
    readme.write_text(source.read_text())
    subprocess.run(["git", "add", "README.md"], cwd=tmp_path, check=True)

    def hook():
        return subprocess.run(
            [bash, str(root / ".githooks/pre-commit")],
            cwd=tmp_path,
            capture_output=True,
            text=True,
            check=False,
        )

    result = hook()
    assert result.returncode == 0, result.stderr
    # CI must still reject changes relative to HEAD, including staged docs.
    assert (
        subprocess.run(
            [make, "docs-check"],
            cwd=tmp_path,
            capture_output=True,
            check=False,
        ).returncode
        != 0
    )
    source.write_text("regenerated docs\n")
    assert hook().returncode != 0
    assert readme.read_text() == source.read_text()
    subprocess.run(["git", "add", "README.md"], cwd=tmp_path, check=True)
    assert hook().returncode == 0
    page = tmp_path / "docs/starlight/new.md"
    page.parent.mkdir(parents=True)
    page.write_text("new page\n")
    assert hook().returncode != 0
    subprocess.run(["git", "add", str(page)], cwd=tmp_path, check=True)
    assert hook().returncode == 0
    page.unlink()
    assert hook().returncode != 0
