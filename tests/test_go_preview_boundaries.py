"""The Python oracle and Go parity inventory remain independently maintained."""

from pathlib import Path
from scripts import generate_go_doc_metadata as metadata
from scripts.validate_parity_reports import inventory
import json
import re
import subprocess


def test_metadata_reads_pinned_tree_despite_working_python_changes(tmp_path, monkeypatch):
    def git(*args):
        return subprocess.check_output(["git", *args], cwd=tmp_path, text=True).strip()

    package = tmp_path / "avrea_cli"
    package.mkdir()
    (package / "__init__.py").write_text("")
    (package / "docs_gen.py").write_text("def build_tree(): return {'source': 'pinned'}")
    scripts = tmp_path / "scripts"
    scripts.mkdir()
    (scripts / "go_help_metadata.py").write_text("def terminal_tree(): return {}")
    git("init", "-q")
    git("add", "avrea_cli")
    git(
        "-c",
        "user.name=Fixture",
        "-c",
        "user.email=fixture@example.test",
        "-c",
        "commit.gpgsign=false",
        "commit",
        "-qm",
        "fixture",
    )
    monkeypatch.setattr(metadata, "BASELINE", git("rev-parse", "HEAD"))
    monkeypatch.setattr(metadata, "__file__", str(scripts / "generate_go_doc_metadata.py"))
    (package / "docs_gen.py").write_text("raise RuntimeError('working tree must not be imported')")
    assert json.loads(metadata.generate()) == {"source": "pinned", "terminal": {}}


def test_every_parity_script_has_one_ci_shard():
    root = Path(__file__).resolve().parents[1]
    workflow = (root / ".github/workflows/go-preview.yml").read_text(encoding="utf-8")
    makefile = (root / "Makefile").read_text(encoding="utf-8")
    suites = inventory()
    expected = set(suites)
    assert set(re.findall(r"scripts[./](compare_[a-z_]+)", makefile)) == expected
    assigned = re.findall(
        r"- run: [^\n]*scripts[./](compare_[a-z_]+)[^\n]*\n        if: matrix.shard == ([0-3])",
        workflow,
    )
    assert {name for name, _ in assigned} == expected
    assert len(assigned) == len(expected)
    assert {shard for _, shard in assigned} == {"0", "1", "2", "3"}
    assert all(suites[name]["shard"] == int(shard) for name, shard in assigned)
    assert "continue-on-error:" not in workflow
    gate = "run: python scripts/validate_parity_reports.py --binary build/go/avr.exe --shard ${{ matrix.shard }}"
    assert gate in workflow
