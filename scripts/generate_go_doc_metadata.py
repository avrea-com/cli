"""Migrate documentation metadata from the pinned public Python command tree.

Go uses this prose/type metadata with its live registry; Python is only needed
to reproduce the migration input, never by the installed native CLI.
"""

from avrea_cli.docs_gen import build_tree
from pathlib import Path
from scripts.go_help_metadata import terminal_tree
import argparse
import json
import subprocess

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    subprocess.run(
        ["git", "diff", "--exit-code", "99e74d119d83c7cef2887c2dd04a724719e6b009", "--", "avrea_cli"],
        check=True,
        stdout=subprocess.DEVNULL,
    )
    target = Path("docgen/metadata.json")
    tree = build_tree()
    tree["terminal"] = terminal_tree()
    value = json.dumps(tree, indent=2, ensure_ascii=False, default=str) + "\n"
    if args.check:
        if target.read_text() != value:
            raise SystemExit("Go documentation metadata differs from the pinned Python input")
    else:
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(value)
