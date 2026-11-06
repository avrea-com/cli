"""Exercise installation, upgrade, rollback and wheel integrity without releases."""

from functools import partial
from pathlib import Path
from scripts import go_distribution as distribution
from scripts import go_package as package
import base64
import csv
import email
import hashlib
import http.server
import io
import json
import os
import platform
import subprocess
import tarfile
import tempfile
import threading
import tomllib
import unittest
import zipfile


class DistributionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def test_wheel_records_native_scripts_and_platform(self):
        for target, tag in distribution.PLATFORMS.items():
            with self.subTest(target=target):
                source = self.root / target
                source.mkdir()
                (source / "manifest.json").write_text(json.dumps({"target": target, "version": "0.7.0-rc.1"}))
                executable = "avr.exe" if target.startswith("windows") else "avr"
                (source / executable).write_bytes(b"native-executable")
                (source / "LICENSE").write_text("license")
                (source / "THIRD-PARTY-NOTICES.txt").write_text("notices")
                wheel = distribution.wheel(source, self.root)
                self.assertEqual(wheel.name, f"avr_cli-0.7.0rc1-py3-none-{tag}.whl")
                with zipfile.ZipFile(wheel) as bundle:
                    record = next(n for n in bundle.namelist() if n.endswith("/RECORD"))
                    for name, checksum, size in csv.reader(io.StringIO(bundle.read(record).decode())):
                        if name == record:
                            continue
                        content = bundle.read(name)
                        actual = base64.urlsafe_b64encode(hashlib.sha256(content).digest()).rstrip(b"=").decode()
                        self.assertEqual(checksum, "sha256=" + actual)
                        self.assertEqual(int(size), len(content))
                    self.assertEqual(bundle.read(f"avr_cli-0.7.0rc1.data/scripts/{executable}"), b"native-executable")
                    self.assertFalse(any(n.startswith("avrea_cli/") for n in bundle.namelist()))
                    metadata = email.message_from_bytes(bundle.read("avr_cli-0.7.0rc1.dist-info/METADATA"))
                    for name in metadata.get_all("License-File", []):
                        self.assertTrue(bundle.read("avr_cli-0.7.0rc1.dist-info/licenses/" + name))

    def test_version_rejects_unpublishable_inputs(self):
        for value in ["dev", "0.7.0;bad", "../0.7.0", "0.7.0-rc.0", "01.7.0"]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                distribution.python_version(value)

    def test_channel_metadata_requires_validated_matching_artifacts(self):
        version = "0.7.0-rc.1"
        for target, tag in distribution.PLATFORMS.items():
            suffix = ".zip" if target == "windows_amd64" else ".tar.gz"
            archive = self.root / f"avr_{version}_{target}{suffix}"
            archive.write_bytes(target.encode())
            checksum = package.digest(archive)
            archive.with_name(archive.name + ".sha256").write_text(f"{checksum}  {archive.name}\n")
            report = {
                "status": "passed",
                "sha256": checksum,
                "manifest": {"version": version, "target": target, "commit": "release-commit", "dirty": False},
            }
            archive.with_name(archive.name + ".validation.json").write_text(json.dumps(report))
            wheel = self.root / f"avr_cli-0.7.0rc1-py3-none-{tag}.whl"
            wheel.write_bytes(b"wheel fixture")
            wheel.with_name(wheel.name + ".validation.json").write_text(
                json.dumps({"status": "passed", "sha256": package.digest(wheel)})
            )
        distribution.channels(self.root, version, "release-commit")
        config = tomllib.loads((self.root / "mise.toml").read_text())["tools"]["github:avrea-com/cli"]
        self.assertEqual(config["version"], version)
        for values in config["platforms"].values():
            asset = self.root / values["asset_pattern"]
            self.assertEqual(values["checksum"], "sha256:" + package.digest(asset))
        for line in (self.root / "SHA256SUMS").read_text().splitlines():
            checksum, name = line.split("  ", 1)
            self.assertEqual(checksum, package.digest(self.root / name))
        with self.assertRaisesRegex(ValueError, "clean source"):
            distribution.channels(self.root, version, "wrong-commit")
        wheel.write_bytes(b"changed after validation")
        with self.assertRaisesRegex(ValueError, "installation validation"):
            distribution.channels(self.root, version, "release-commit")

    def test_empty_sbom_is_rejected(self):
        path = self.root / "bom.json"
        path.write_text('{"components": []}')
        with self.assertRaisesRegex(ValueError, "missing packages"):
            distribution.validate_sbom(path, "0.7.0")

    def fixture_archive(self, version, content):
        target = (
            "windows_amd64" if os.name == "nt" else ("darwin_arm64" if platform.system() == "Darwin" else "linux_amd64")
        )
        stem = f"avr_{version}_{target}"
        folder = self.root / "download" / ("v" + version)
        folder.mkdir(parents=True, exist_ok=True)
        archive = folder / (stem + (".zip" if os.name == "nt" else ".tar.gz"))
        if os.name == "nt":
            with zipfile.ZipFile(archive, "w") as bundle:
                bundle.writestr(stem + "/avr.exe", content)
        else:
            with tarfile.open(archive, "w:gz") as bundle:
                item = tarfile.TarInfo(stem + "/avr")
                item.size = len(content)
                item.mode = 0o755
                bundle.addfile(item, io.BytesIO(content))
        archive.with_name(archive.name + ".sha256").write_text(f"{package.digest(archive)}  {archive.name}\n")
        return archive

    def test_install_upgrade_rollback_corruption_and_unmanaged_destination(self):
        self.fixture_archive("0.7.0-rc.1", b"first-native-binary")
        second = self.fixture_archive("0.7.0", b"second-native-binary")
        latest = self.root / "latest/download"
        latest.mkdir(parents=True)
        (latest / "avr-version.txt").write_text("0.7.0\n")
        server = http.server.ThreadingHTTPServer(
            ("127.0.0.1", 0), partial(http.server.SimpleHTTPRequestHandler, directory=str(self.root))
        )
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(thread.join)
        self.addCleanup(server.shutdown)
        destination = self.root / "install path with spaces"
        credentials = self.root / "hosts.json"
        credentials.write_bytes(b"unchanged credentials")
        env = os.environ | {
            "AVR_RELEASE_BASE_URL": f"http://127.0.0.1:{server.server_port}",
            "AVR_CONFIG_DIR": str(self.root),
        }
        if os.name == "nt":
            command = [
                os.environ.get("AVR_TEST_POWERSHELL", "pwsh"),
                "-NoProfile",
                "-ExecutionPolicy",
                "Bypass",
                "-File",
                str(package.ROOT / "packaging/install.ps1"),
                "-BinDir",
                str(destination),
            ]
            version_flag = "-Version"
            binary = destination / "avr.exe"
        else:
            command = ["sh", str(package.ROOT / "packaging/install.sh"), "--bin-dir", str(destination)]
            version_flag = "--version"
            binary = destination / "avr"

        def install(version=None, success=True):
            result = subprocess.run(
                command + ([version_flag, version] if version else []),
                env=env,
                capture_output=True,
                text=True,
                timeout=30,
                check=False,
            )
            self.assertEqual(result.returncode == 0, success, result.stdout + result.stderr)

        install("0.7.0-rc.1")
        self.assertEqual(binary.read_bytes(), b"first-native-binary")
        install()
        self.assertEqual(binary.read_bytes(), b"second-native-binary")
        install("0.7.0-rc.1")
        self.assertEqual(binary.read_bytes(), b"first-native-binary")
        marker = destination / "avr-install-method"
        pending = destination / "avr-install-method.pending"
        binary.write_bytes(b"second-native-binary")
        marker.write_text("")
        pending.write_text("native " + package.digest(binary) + "\n")
        install("0.7.0")
        self.assertFalse(pending.exists())
        self.assertEqual(marker.read_text().strip(), "native " + package.digest(binary))
        marker.unlink()
        pending.write_text("native " + package.digest(binary) + "\n")
        install("0.7.0")
        self.assertFalse(pending.exists())
        marker.write_text("native stale\n")
        install("0.7.0")
        install("0.7.0-rc.1")
        second.write_bytes(second.read_bytes() + b"corrupt")
        install("0.7.0", success=False)
        self.assertEqual(binary.read_bytes(), b"first-native-binary")
        binary.write_bytes(b"unmanaged")
        install("0.7.0-rc.1", success=False)
        self.assertEqual(binary.read_bytes(), b"unmanaged")
        self.assertEqual(credentials.read_bytes(), b"unchanged credentials")
        install("../bad", success=False)
        if os.name != "nt":
            other = self.root / "external"
            other.write_bytes(b"external")
            binary.unlink()
            binary.symlink_to(other)
            install("0.7.0-rc.1", success=False)
            self.assertEqual(other.read_bytes(), b"external")


if __name__ == "__main__":
    unittest.main()
