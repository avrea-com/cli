"""Read independent Python documentation/help metadata for differential tests.

The pinned Python baseline is only an oracle, never an input to the native build.
No generated metadata is stored in the source tree.
"""

from functools import lru_cache
from pathlib import Path
from scripts.compare_auth import BASELINE
import io
import os
import subprocess
import sys
import tarfile
import tempfile


@lru_cache
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
