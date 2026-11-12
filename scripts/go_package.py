"""Build and validate native public AVR preview archives (Python stdlib only)."""

from pathlib import Path
from pathlib import PurePosixPath
import argparse
import gzip
import hashlib
import http.server
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
import zipfile

ROOT = Path(__file__).resolve().parents[1]
TARGETS = {"darwin_arm64", "linux_amd64", "linux_arm64", "windows_amd64"}


def run(args, **kwargs):
    return subprocess.check_output(args, text=True, encoding="utf-8", timeout=180, **kwargs)


def digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def clean_env(home):
    # Preserve only Windows loader essentials. No credentials, proxies, user
    # configuration, Python, jq, Git or shells are available to the executable.
    env = {k: v for k, v in os.environ.items() if k.upper() in {"SYSTEMROOT", "WINDIR"}}
    empty = home / "empty-bin"
    empty.mkdir(exist_ok=True)
    env.update(
        PATH=str(empty),
        HOME=str(home),
        USERPROFILE=str(home),
        APPDATA=str(home / "appdata"),
        LOCALAPPDATA=str(home / "localappdata"),
        XDG_CONFIG_HOME=str(home / "config"),
        AVR_CONFIG_DIR=str(home / "avr"),
        TMPDIR=str(home),
        TEMP=str(home),
        TMP=str(home),
        NO_COLOR="1",
        TERM="dumb",
        AVR_HOST="http://127.0.0.1:1",
    )
    return env


def extract(archive, destination):
    """Extract only regular, uniquely named files under one relative root."""
    seen = set()

    def write(name, data, mode):
        path = PurePosixPath(name)
        if (
            path.is_absolute()
            or len(path.parts) < 2
            or any(p in {"", ".", ".."} for p in name.split("/"))
            or "\\" in name
            or ":" in name
            or name in seen
        ):
            raise ValueError(f"Unsafe or duplicate archive member: {name}")
        seen.add(name)
        target = destination.joinpath(*path.parts)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
        target.chmod(mode & 0o777)

    if archive.name.endswith(".zip"):
        with zipfile.ZipFile(archive) as source:
            for item in source.infolist():
                mode = item.external_attr >> 16
                if not stat.S_ISREG(mode):
                    raise ValueError(f"Not a regular file: {item.filename}")
                write(item.filename, source.read(item), mode)
    else:
        with tarfile.open(archive, "r:gz") as source:
            for item in source:
                if not item.isfile():
                    raise ValueError(f"Not a regular file: {item.name}")
                stream = source.extractfile(item)
                if stream is None:
                    raise ValueError(f"Unreadable file: {item.name}")
                write(item.name, stream.read(), item.mode)
    roots = list(destination.iterdir())
    if len(roots) != 1 or not roots[0].is_dir():
        raise ValueError("Expected exactly one package directory")
    return roots[0]


def require_patched_go(version):
    match = re.fullmatch(r"go(\d+)\.(\d+)\.(\d+)", version)
    if match is None or tuple(map(int, match.groups())) < (1, 27, 4):
        raise ValueError("Distributable binaries require stable Go 1.27.4 or newer")


def validate_build_info(info, manifest):
    require_patched_go(info.get("GoVersion", ""))
    settings = {entry["Key"]: entry["Value"] for entry in info.get("Settings", [])}
    target_os, target_arch = manifest["target"].split("_", 1)
    expected = {
        "vcs.revision": manifest["commit"],
        "vcs.modified": str(manifest["dirty"]).lower(),
        "CGO_ENABLED": "0",
        "-trimpath": "true",
        "GOOS": target_os,
        "GOARCH": target_arch,
    }
    if info.get("Path") != "github.com/avrea-com/cli/cmd/avr":
        raise ValueError("Unexpected executable module path")
    for key, value in expected.items():
        if settings.get(key) != value:
            raise ValueError(f"Executable build setting {key} does not match: {settings.get(key)!r} != {value!r}")
    if manifest.get("cgo_enabled") is not False:
        raise ValueError("Package must declare a CGO-disabled build")
    return settings


def verify(archive, go="go"):
    archive = archive.resolve()
    archive.with_name(archive.name + ".validation.json").unlink(missing_ok=True)
    expected = archive.with_name(archive.name + ".sha256").read_text(encoding="utf-8").strip()
    checksum = digest(archive)
    if expected != f"{checksum}  {archive.name}":
        raise ValueError("Archive checksum mismatch")
    checks = []
    with tempfile.TemporaryDirectory(prefix="avr package check ") as temporary:
        base = Path(temporary)
        unpack = base / "unpacked"
        unpack.mkdir()
        package = extract(archive, unpack)
        manifest = json.loads((package / "manifest.json").read_text(encoding="utf-8"))
        files = {
            p.relative_to(package).as_posix(): digest(p)
            for p in package.rglob("*")
            if p.is_file() and p != package / "manifest.json"
        }
        if files != manifest["files"]:
            raise ValueError("Package file inventory or digest mismatch")
        binary_name = "avr.exe" if os.name == "nt" else "avr"
        required = {
            binary_name,
            "LICENSE",
            "README.md",
            "SECURITY.md",
            "THIRD-PARTY-NOTICES.txt",
            "completions/avr.bash",
            "completions/_avr",
            "completions/avr.fish",
            "docs/REFERENCE.md",
            "docs/INSTALL.md",
            "docs/reference.json",
        }
        if not required <= files.keys() or not any(p.startswith("man/") for p in files):
            raise ValueError("Package is missing required content")
        if not (package / "THIRD-PARTY-NOTICES.txt").read_text(encoding="utf-8").strip():
            raise ValueError("Package has empty third-party notices")
        binary = package / binary_name
        home = base / "clean home"
        home.mkdir()
        env = clean_env(home)

        def check(name, args, contains=None, extra=None, exact=None):
            started = time.perf_counter()
            output = run([str(binary), *args], cwd=home, env=env | (extra or {}))
            if contains is not None and contains not in output:
                raise ValueError(f"{name}: missing expected output {contains!r}")
            if exact is not None and output != exact:
                raise ValueError(f"{name}: output differs from packaged content")
            checks.append({"name": name, "elapsed_ms": round((time.perf_counter() - started) * 1000, 2)})
            return output

        check("version", ["--version"], exact=f"avr version {manifest['version']}+{manifest['revision']}\n")
        for command in [[], ["auth"], ["repo"], ["job"], ["run"]]:
            check("help " + " ".join(command), [*command, "--help"], contains="USAGE")
        check(
            "local config",
            ["config", "get", "org"],
            exact="org-package-check\n",
            extra={"AVR_ORG": "org-package-check"},
        )
        for shell, filename in [("bash", "avr.bash"), ("zsh", "_avr"), ("fish", "avr.fish")]:
            source = check(
                f"{shell} completion source",
                [],
                extra={"_AVR_COMPLETE": f"{shell}_source"},
            )
            if (package / "completions" / filename).read_bytes() != source.encode("utf-8"):
                raise ValueError(f"{shell} completion source: packaged bytes differ from LF source")
        build_settings = validate_build_info(json.loads(run([go, "version", "-m", "-json", str(binary)])), manifest)
        check("bundled README lookup", ["internal", "docs", "--format", "starlight", "--out", str(home / "docs")])
        if (
            not (home / "docs" / "index.md")
            .read_text(encoding="utf-8")
            .endswith((package / "README.md").read_text(encoding="utf-8"))
        ):
            raise ValueError("Bundled README was not used")

        class Fixture(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                if self.path != "/users/me" or self.headers.get("Authorization") != "Bearer package-fixture-token":
                    self.send_error(404)
                    return
                body = b'{"id":"user-package","email":"package@example.test"}'
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *_args):
                pass

        with http.server.ThreadingHTTPServer(("127.0.0.1", 0), Fixture) as server:
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            try:
                check(
                    "embedded jq against loopback fixture",
                    ["auth", "status", "--json", "email", "--jq", ".email"],
                    exact="package@example.test\n",
                    extra={"AVR_HOST": f"http://127.0.0.1:{server.server_port}", "AVR_TOKEN": "package-fixture-token"},
                )
            finally:
                server.shutdown()
                thread.join()
    report = {
        "status": "passed",
        "archive": archive.name,
        "sha256": checksum,
        "bytes": archive.stat().st_size,
        "manifest": manifest,
        "binary_build_settings": build_settings,
        "checks": checks,
        "environment": "Isolated HOME and empty PATH; HTTP test uses a loopback fixture. Network is not OS-blocked.",
    }
    report_path = archive.with_name(archive.name + ".validation.json")
    report_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(f"Validated {archive}\nReport: {report_path}")
    return report


def build(args):
    go = shutil.which(args.go)
    if go is None:
        raise ValueError(f"Go executable not found: {args.go}")
    goenv = json.loads(run([go, "env", "-json", "GOHOSTOS", "GOHOSTARCH", "GOVERSION"], cwd=ROOT))
    require_patched_go(goenv["GOVERSION"])
    target = goenv["GOHOSTOS"] + "_" + goenv["GOHOSTARCH"]
    if target not in TARGETS or (args.target and args.target != target):
        raise ValueError(f"Native build/validation required: host={target}, requested={args.target}")
    version = args.version or (ROOT / "internal" / "version" / "VERSION").read_text(encoding="utf-8").strip()
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", version):
        raise ValueError("Version must be a safe filename/ldflags identifier")
    commit = run(["git", "rev-parse", "HEAD"], cwd=ROOT).strip()
    dirty = bool(run(["git", "status", "--porcelain", "--untracked-files=normal"], cwd=ROOT).strip())
    revision = commit[:12] + (".dirty" if dirty else "")
    epoch = int(run(["git", "show", "-s", "--format=%ct", "HEAD"], cwd=ROOT).strip())
    out = Path(args.out).resolve()
    out.mkdir(parents=True, exist_ok=True)
    name = f"avr_{version}_{target}"
    with tempfile.TemporaryDirectory(prefix="avr-package-") as temporary:
        base = Path(temporary)
        package = base / name
        package.mkdir()
        binary = package / ("avr.exe" if target.startswith("windows") else "avr")
        env = os.environ | {"CGO_ENABLED": "0", "GOOS": goenv["GOHOSTOS"], "GOARCH": goenv["GOHOSTARCH"]}
        subprocess.run(
            [
                go,
                "build",
                "-trimpath",
                "-ldflags",
                f"-X main.version={version} -X main.commit={revision}",
                "-o",
                str(binary),
                "./cmd/avr",
            ],
            cwd=ROOT,
            env=env,
            check=True,
        )
        for doc_name in ["LICENSE", "README.md", "SECURITY.md", "docs/INSTALL.md"]:
            destination = package / doc_name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / doc_name, destination)
        subprocess.run(
            [
                sys.executable,
                str(ROOT / "scripts" / "go_notices.py"),
                "--go",
                go,
                "--out",
                str(package / "THIRD-PARTY-NOTICES.txt"),
            ],
            cwd=ROOT,
            check=True,
        )
        home = base / "home"
        home.mkdir()
        clean = clean_env(home)
        completions = package / "completions"
        completions.mkdir()
        for shell, filename in [("bash", "avr.bash"), ("zsh", "_avr"), ("fish", "avr.fish")]:
            (completions / filename).write_text(
                run([str(binary)], cwd=home, env=clean | {"_AVR_COMPLETE": f"{shell}_source"}),
                encoding="utf-8",
                newline="\n",
            )
        for fmt, path in [("markdown", "docs/REFERENCE.md"), ("json", "docs/reference.json"), ("man", "man")]:
            run([str(binary), "internal", "docs", "--format", fmt, "--out", str(package / path)], cwd=home, env=clean)
        manifest = {
            "schema": 1,
            "version": version,
            "revision": revision,
            "commit": commit,
            "dirty": dirty,
            "target": target,
            "go": goenv["GOVERSION"],
            "cgo_enabled": False,
            "files": {p.relative_to(package).as_posix(): digest(p) for p in sorted(package.rglob("*")) if p.is_file()},
        }
        (package / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
        archive = out / (name + (".zip" if target.startswith("windows") else ".tar.gz"))
        archive.with_name(archive.name + ".validation.json").unlink(missing_ok=True)
        members = sorted(p for p in package.rglob("*") if p.is_file())
        if target.startswith("windows"):
            with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as bundle:
                for path in members:
                    info = zipfile.ZipInfo(path.relative_to(base).as_posix(), time.gmtime(epoch)[:6])
                    info.create_system = 3
                    info.external_attr = (stat.S_IFREG | (0o755 if path == binary else 0o644)) << 16
                    info.compress_type = zipfile.ZIP_DEFLATED
                    bundle.writestr(info, path.read_bytes())
        else:
            with (
                archive.open("wb") as raw,
                gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=epoch) as compressed,
                tarfile.open(fileobj=compressed, mode="w") as bundle,
            ):
                for path in members:
                    info = bundle.gettarinfo(str(path), path.relative_to(base).as_posix())
                    info.uid = info.gid = 0
                    info.uname = info.gname = ""
                    info.mtime = epoch
                    info.mode = 0o755 if path == binary else 0o644
                    with path.open("rb") as source:
                        bundle.addfile(info, source)
    archive.with_name(archive.name + ".sha256").write_text(f"{digest(archive)}  {archive.name}\n", encoding="utf-8")
    return verify(archive, go=go)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", default="go")
    parser.add_argument("--version", help="Default: internal/version/VERSION")
    parser.add_argument("--target", choices=sorted(TARGETS), help="Assert native runner target")
    parser.add_argument("--out", default=str(ROOT / "build" / "go" / "packages"))
    parser.add_argument("--verify", type=Path, help="Verify an existing archive and its SHA-256 sidecar")
    args = parser.parse_args()
    if args.verify:
        verify(args.verify, go=args.go)
    else:
        build(args)


if __name__ == "__main__":
    main()
