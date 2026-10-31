"""Go preview checks must not freeze or gate the existing Python release."""

from pathlib import Path
from scripts import generate_go_doc_metadata as metadata
import json
import re
import subprocess


def test_release_does_not_call_go_preview_jobs():
    root = Path(__file__).resolve().parents[1]
    workflows = root / ".github/workflows"
    release = (workflows / "release.yml").read_text(encoding="utf-8")
    ci = (workflows / "ci.yml").read_text(encoding="utf-8")
    preview = (workflows / "go-preview.yml").read_text(encoding="utf-8")
    assert "uses: ./.github/workflows/ci.yml" in release
    jobs = re.findall(r"^  ([a-z][a-z-]*):$", ci.split("jobs:\n", 1)[1], re.M)
    assert jobs and not any(name.startswith("go-") for name in jobs)
    assert "go-preview.yml" not in ci and "go-package.yml" not in ci
    triggers = preview.split("on:\n", 1)[1].split("permissions:", 1)[0]
    assert all(name + ":" in triggers for name in ["pull_request", "push", "workflow_dispatch"])
    assert "workflow_call:" not in triggers
    assert all("  " + name + ":" in preview for name in ["go-preview", "go-checks", "go-packages"])


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
    expected = set(re.findall(r"scripts[./](compare_[a-z_]+)", makefile))
    assigned = re.findall(
        r"- run: [^\n]*scripts[./](compare_[a-z_]+)[^\n]*\n        if: matrix.shard == ([0-3])",
        workflow,
    )
    assert {name for name, _ in assigned} == expected
    assert len(assigned) == len(expected)
    assert {shard for _, shard in assigned} == {"0", "1", "2", "3"}
