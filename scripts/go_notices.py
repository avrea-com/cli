"""Collect the pinned runtime modules' license/notice files alongside preview builds."""

from pathlib import Path
import argparse
import json
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default="go")
    args = parser.parse_args()
    manifest = json.loads(subprocess.check_output([args.go, "mod", "edit", "-json"], text=True))
    required = {entry["Path"] for entry in manifest["Require"]}
    data = subprocess.check_output([args.go, "list", "-m", "-json", "all"], text=True)
    decoder = json.JSONDecoder()
    notices = ["# Go preview third-party notices\n"]
    while data.strip():
        module, end = decoder.raw_decode(data.lstrip())
        data = data.lstrip()[end:]
        if module["Path"] not in required:
            continue
        if "Dir" not in module:
            module = json.loads(
                subprocess.check_output(
                    [args.go, "mod", "download", "-json", module["Path"] + "@" + module["Version"]], text=True
                )
            )
        directory = Path(module["Dir"])
        files = [
            p
            for p in directory.rglob("*")
            if p.is_file() and p.name.upper().startswith(("LICENSE", "COPYING", "NOTICE"))
        ]
        if not files:
            raise SystemExit(f"No license found for {module['Path']}")
        notices.append(f"\n## {module['Path']} {module['Version']}\n")
        for file in sorted(files):
            notices.extend([f"\n### {file.relative_to(directory)}\n", file.read_text()])
    goroot = Path(subprocess.check_output([args.go, "env", "GOROOT"], text=True).strip())
    notices.extend(["\n## Go standard library\n", (goroot / "LICENSE").read_text()])
    destination = Path("build/go/THIRD-PARTY-NOTICES.txt")
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text("\n".join(notices))


if __name__ == "__main__":
    main()
