"""Collect the pinned runtime modules' license/notice files alongside native builds."""

from pathlib import Path
import argparse
import json
import subprocess
import sys


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default="go")
    parser.add_argument("--out", type=Path, default=Path("build/go/THIRD-PARTY-NOTICES.txt"))
    # Some Go distributions (e.g. Homebrew) omit GOROOT/LICENSE; local builds
    # for docs and tests do not ship, so they may skip it. Packaging may not.
    parser.add_argument("--allow-missing-go-license", action="store_true")
    args = parser.parse_args()
    manifest = json.loads(subprocess.check_output([args.go, "mod", "edit", "-json"], text=True))
    required = {entry["Path"] for entry in manifest["Require"]}
    data = subprocess.check_output([args.go, "list", "-m", "-json", "all"], text=True)
    decoder = json.JSONDecoder()
    notices = ["# AVR third-party notices\n"]
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
            notices.extend([f"\n### {file.relative_to(directory)}\n", file.read_text(encoding="utf-8")])
    goroot = Path(subprocess.check_output([args.go, "env", "GOROOT"], text=True).strip())
    go_license = goroot / "LICENSE"
    if go_license.is_file():
        notices.extend(["\n## Go standard library\n", go_license.read_text(encoding="utf-8")])
    elif args.allow_missing_go_license:
        print(f"warning: {go_license} not found; omitting Go standard library notice", file=sys.stderr)
    else:
        raise SystemExit(f"No license found for the Go standard library at {go_license}")
    destination = args.out
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text("\n".join(notices), encoding="utf-8")


if __name__ == "__main__":
    main()
