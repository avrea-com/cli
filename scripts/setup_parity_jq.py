"""Fetch the checksum-pinned jq oracle into build/go/oracle; never install globally."""

from pathlib import Path
import hashlib
import platform
import urllib.request

# From the official jq-1.7.1 release's sha256sum.txt.
ASSETS = {
    ("Linux", "x86_64"): ("jq-linux-amd64", "5942c9b0934e510ee61eb3e30273f1b3fe2590df93933a93d7c58b81d19c8ff5"),
    ("Darwin", "x86_64"): ("jq-macos-amd64", "4155822bbf5ea90f5c79cf254665975eb4274d426d0709770c21774de5407443"),
    ("Darwin", "arm64"): ("jq-macos-arm64", "0bbe619e663e0de2c550be2fe0d240d076799d6f8a652b70fa04aea8a8362e8a"),
    ("Windows", "AMD64"): ("jq-windows-amd64.exe", "7451fbbf37feffb9bf262bd97c54f0da558c63f0748e64152dd87b0a07b6d6ab"),
}


def main():
    asset, checksum = ASSETS[(platform.system(), platform.machine())]
    destination = Path("build/go/oracle") / ("jq.exe" if platform.system() == "Windows" else "jq")
    if destination.exists() and hashlib.sha256(destination.read_bytes()).hexdigest() == checksum:
        return
    with urllib.request.urlopen(
        f"https://github.com/jqlang/jq/releases/download/jq-1.7.1/{asset}", timeout=60
    ) as response:
        data = response.read()
    if hashlib.sha256(data).hexdigest() != checksum:
        raise SystemExit("jq checksum mismatch")
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_bytes(data)
    destination.chmod(0o755)


if __name__ == "__main__":
    main()
