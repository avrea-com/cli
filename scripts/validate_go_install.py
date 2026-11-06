"""Install a native wheel with pip and uv, and exercise the real native installer."""

from functools import partial
from pathlib import Path
from scripts.go_package import ROOT
from scripts.go_package import clean_env
from scripts.go_package import digest
from scripts.go_package import run
import argparse
import http.server
import json
import os
import shutil
import sys
import tempfile
import threading
import venv


def validate(archive, wheel, uv):
    report = json.loads(archive.with_name(archive.name + ".validation.json").read_text())
    version = report["manifest"]["version"]
    expected = f"avr version {version}+{report['manifest']['revision']}\n"
    checks = []
    with tempfile.TemporaryDirectory(prefix="avr install checks ") as temp:
        base = Path(temp)
        home = base / "home"
        home.mkdir()
        env = clean_env(home)
        executable = "avr.exe" if os.name == "nt" else "avr"
        alias = "avr-cli.exe" if os.name == "nt" else "avr-cli"
        virtual = base / "pip environment"
        venv.EnvBuilder(with_pip=True).create(virtual)
        scripts = virtual / ("Scripts" if os.name == "nt" else "bin")
        python = scripts / ("python.exe" if os.name == "nt" else "python")
        # Build the unchanged Python 0.6.0 package, then replace its entry points.
        # --no-deps avoids real service/dependency downloads; Python behavior is
        # covered separately by the existing parity suites.
        old = base / "old"
        run([uv, "build", "--wheel", "--out-dir", str(old)], cwd=ROOT)
        baseline = next(old.glob("*.whl"))
        run([str(python), "-m", "pip", "install", "--no-index", "--no-deps", str(baseline)])
        run([str(python), "-m", "pip", "install", "--no-index", "--no-deps", "--upgrade", str(wheel)])
        for name in [executable, alias]:
            if run([str(scripts / name), "--version"], env=env) != expected:
                raise ValueError("pip upgrade did not select native executable")
        if "USAGE" not in run([str(scripts / executable), "repo", "--help"], env=env):
            raise ValueError("Installed executable help failed")
        checks.append("pip upgrade from Python 0.6.0; avr and avr-cli run with empty PATH")
        native_hashes = {name: digest(scripts / name) for name in [executable, alias]}
        run([str(python), "-m", "pip", "install", "--no-index", "--no-deps", "--force-reinstall", str(baseline)])
        # A real reinstall replaces the native bytes with the Python entry point.
        if any(digest(scripts / name) == native_hashes[name] for name in [executable, alias]):
            raise ValueError("Rollback left a native executable in place")
        installed = run([str(python), "-c", 'from importlib.metadata import version; print(version("avr-cli"))'])
        if installed.strip() != "0.6.0":
            raise ValueError("pip rollback failed")
        checks.append("pip rollback to Python 0.6.0")
        tool_env = os.environ | {"UV_TOOL_DIR": str(base / "uv tools"), "UV_TOOL_BIN_DIR": str(base / "uv bin")}
        run([uv, "tool", "install", "--no-index", "--python", sys.executable, str(wheel)], env=tool_env)
        if run([str(base / "uv bin" / executable), "--version"], env=env) != expected:
            raise ValueError("uv tool native install failed")
        checks.append("uv tool install native wheel")
        # pipx is installed only in CI's tooling environment, never a user environment.
        pipx = shutil.which("pipx")
        if pipx:
            pipx_env = os.environ | {
                "PIPX_HOME": str(base / "pipx"),
                "PIPX_BIN_DIR": str(base / "pipx bin"),
                "PIPX_MAN_DIR": str(base / "pipx man"),
            }
            run([pipx, "install", "--python", sys.executable, "--pip-args=--no-index", str(wheel)], env=pipx_env)
            if run([str(base / "pipx bin" / executable), "--version"], env=env) != expected:
                raise ValueError("pipx native install failed")
            checks.append("pipx install native wheel")
        releases = base / "releases/download" / ("v" + version)
        releases.mkdir(parents=True)
        shutil.copyfile(archive, releases / archive.name)
        shutil.copyfile(archive.with_name(archive.name + ".sha256"), releases / (archive.name + ".sha256"))
        server = http.server.ThreadingHTTPServer(
            ("127.0.0.1", 0), partial(http.server.SimpleHTTPRequestHandler, directory=str(base / "releases"))
        )
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            native = base / "native bin"
            install_env = os.environ | {"AVR_RELEASE_BASE_URL": f"http://127.0.0.1:{server.server_port}"}
            command = (
                [
                    os.environ.get("AVR_TEST_POWERSHELL", "pwsh"),
                    "-NoProfile",
                    "-ExecutionPolicy",
                    "Bypass",
                    "-File",
                    str(ROOT / "packaging/install.ps1"),
                    "-Version",
                    version,
                    "-BinDir",
                    str(native),
                ]
                if os.name == "nt"
                else ["sh", str(ROOT / "packaging/install.sh"), "--version", version, "--bin-dir", str(native)]
            )
            run(command, env=install_env)
            if run([str(native / executable), "--version"], env=env) != expected:
                raise ValueError("Native installation failed")
            checks.append("native installer with real archive and empty-PATH execution")
        finally:
            server.shutdown()
            thread.join()
            server.server_close()
    result = {"status": "passed", "wheel": wheel.name, "sha256": digest(wheel), "checks": checks}
    wheel.with_name(wheel.name + ".validation.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", type=Path, required=True)
    parser.add_argument("--uv", default="uv")
    args = parser.parse_args()
    (archive,) = [p for p in args.directory.iterdir() if p.name.endswith((".tar.gz", ".zip"))]
    (wheel,) = args.directory.glob("*.whl")
    validate(archive.resolve(), wheel.resolve(), shutil.which(args.uv) or args.uv)


if __name__ == "__main__":
    main()
