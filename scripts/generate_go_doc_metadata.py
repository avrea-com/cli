"""Generate Go documentation metadata from the pinned Python snapshot.

Baseline upgrades are explicit: update BASELINE in compare_auth, regenerate the
metadata and command inventory, then run the parity matrix. Working-tree Python
edits do not change this migration input or freeze Python development.
"""

from pathlib import Path
from scripts.compare_auth import BASELINE
import argparse
import io
import os
import subprocess
import sys
import tarfile
import tempfile


def generate():
    root = Path(__file__).resolve().parents[1]
    archive = subprocess.check_output(["git", "archive", BASELINE, "avrea_cli"], cwd=root)
    with tempfile.TemporaryDirectory(prefix="avr-doc-oracle-") as directory:
        with tarfile.open(fileobj=io.BytesIO(archive)) as source:
            source.extractall(directory, filter="data")
        code = (
            "import json; from avrea_cli.docs_gen import build_tree; "
            "from scripts.go_help_metadata import terminal_tree; "
            "tree=build_tree(); tree['terminal']=terminal_tree(); "
            "print(json.dumps(tree, indent=2, ensure_ascii=False, default=str))"
        )
        return subprocess.check_output(
            [sys.executable, "-c", code],
            cwd=directory,
            env=os.environ | {"PYTHONPATH": os.pathsep.join([directory, str(root)]), "PYTHONIOENCODING": "utf-8"},
            text=True,
            encoding="utf-8",
        )


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    target = Path("docgen/metadata.json")
    value = generate()
    if args.check:
        if target.read_text(encoding="utf-8") != value:
            raise SystemExit("Go documentation metadata differs from the pinned Python input")
    else:
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(value, encoding="utf-8", newline="\n")
