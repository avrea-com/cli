"""Validate native release triggers; manual runs can never authorize publication."""

from pathlib import Path
from scripts.go_distribution import python_version
import os


def publication_allowed(event, ref, enabled, version):
    python_version(version)
    if event == "workflow_dispatch":
        return False
    if event != "push" or ref != f"refs/tags/v{version}":
        raise ValueError("Native publication requires a tag matching internal/version/VERSION")
    return enabled == "true"


def main():
    version = Path("internal/version/VERSION").read_text(encoding="utf-8").strip()
    allowed = publication_allowed(
        os.environ["GITHUB_EVENT_NAME"],
        os.environ["GITHUB_REF"],
        os.environ.get("RELEASE_ENABLED", ""),
        version,
    )
    with Path(os.environ["GITHUB_OUTPUT"]).open("a", encoding="utf-8") as output:
        output.write(f"publish={str(allowed).lower()}\nversion={version}\n")
    print("Publication enabled for this tag." if allowed else "Artifact validation only; publication is disabled.")


if __name__ == "__main__":
    main()
