"""Integrity checks for the package validator; actual binary smoke is in go_package."""

from pathlib import Path
from scripts import go_package as package
from unittest import mock
import hashlib
import io
import json
import os
import stat
import tarfile
import tempfile
import unittest
import zipfile


class PackageTests(unittest.TestCase):
    def test_binary_settings_must_match_manifest(self):
        manifest = {"target": "linux_amd64", "commit": "current", "dirty": False, "cgo_enabled": False}
        settings = {
            "GOOS": "linux",
            "GOARCH": "amd64",
            "vcs.revision": "current",
            "vcs.modified": "false",
            "CGO_ENABLED": "0",
            "-trimpath": "true",
        }

        def info(values):
            return {
                "Path": "github.com/avrea-com/cli/cmd/avr",
                "Settings": [{"Key": k, "Value": v} for k, v in values.items()],
            }

        package.validate_build_info(info(settings), manifest)
        for key, wrong in {
            "GOOS": "darwin",
            "GOARCH": "arm64",
            "vcs.revision": "old",
            "vcs.modified": "true",
            "CGO_ENABLED": "1",
            "-trimpath": "false",
        }.items():
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "build setting"):
                package.validate_build_info(info(settings | {key: wrong}), manifest)

    def test_crlf_completion_rejected_despite_matching_manifest(self):
        content = dict.fromkeys(
            [
                "avr.exe" if os.name == "nt" else "avr",
                "LICENSE",
                "README.md",
                "THIRD-PARTY-NOTICES.txt",
                "completions/avr.bash",
                "completions/_avr",
                "completions/avr.fish",
                "docs/REFERENCE.md",
                "docs/reference.json",
                "man/avr.1",
            ],
            b"fixture\n",
        )
        content["completions/avr.bash"] = b"fixture\r\n"
        manifest = {
            "version": "test",
            "revision": "abc",
            "files": {n: hashlib.sha256(b).hexdigest() for n, b in content.items()},
        }
        content["manifest.json"] = json.dumps(manifest).encode()
        archive = self.checksum(self.tar([("package/" + n, b, tarfile.REGTYPE) for n, b in content.items()]))
        responses = ["avr version test+abc\n", *["USAGE\n"] * 5, "org-package-check\n", "fixture\n"]
        with (
            mock.patch.object(package, "run", side_effect=responses),
            self.assertRaisesRegex(ValueError, "packaged bytes"),
        ):
            package.verify(archive)

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.destination = self.root / "extracted"
        self.destination.mkdir()

    def tar(self, entries):
        archive = self.root / "package.tar.gz"
        with tarfile.open(archive, "w:gz") as bundle:
            for name, content, kind in entries:
                item = tarfile.TarInfo(name)
                item.type = kind
                item.mode = 0o755
                item.size = len(content)
                if kind == tarfile.SYMTYPE:
                    item.linkname = "/tmp/outside-package"
                bundle.addfile(item, io.BytesIO(content))
        return archive

    def checksum(self, archive):
        archive.with_name(archive.name + ".sha256").write_text(f"{package.digest(archive)}  {archive.name}\n")
        return archive

    def test_corruption_rejected_and_old_success_removed(self):
        archive = self.checksum(self.tar([("package/avr", b"original", tarfile.REGTYPE)]))
        report = archive.with_name(archive.name + ".validation.json")
        report.write_text('{"status":"passed"}')
        archive.write_bytes(archive.read_bytes() + b"corruption")
        with self.assertRaisesRegex(ValueError, "checksum mismatch"):
            package.verify(archive)
        self.assertFalse(report.exists())

    def test_manifest_mismatch_rejected_before_execution(self):
        manifest = json.dumps({"files": {"avr": "incorrect"}}).encode()
        archive = self.checksum(
            self.tar(
                [
                    ("package/avr", b"not an executable", tarfile.REGTYPE),
                    ("package/manifest.json", manifest, tarfile.REGTYPE),
                ]
            )
        )
        with self.assertRaisesRegex(ValueError, "inventory or digest mismatch"):
            package.verify(archive)

    def test_tar_path_traversal_rejected(self):
        for name in ["../escape", "/absolute/file", "package/../../escape", "package/a\\b", "package/C:drive"]:
            with self.subTest(name=name):
                archive = self.tar([(name, b"bad", tarfile.REGTYPE)])
                with self.assertRaisesRegex(ValueError, "Unsafe"):
                    package.extract(archive, self.destination)

    def test_tar_symlink_rejected(self):
        archive = self.tar([("package/link", b"", tarfile.SYMTYPE)])
        with self.assertRaisesRegex(ValueError, "Not a regular file"):
            package.extract(archive, self.destination)

    def test_duplicate_members_rejected(self):
        archive = self.tar([("package/avr", b"one", tarfile.REGTYPE), ("package/avr", b"two", tarfile.REGTYPE)])
        with self.assertRaisesRegex(ValueError, "duplicate"):
            package.extract(archive, self.destination)

    def test_zip_path_traversal_and_symlink_rejected(self):
        for name, mode in [("../escape", stat.S_IFREG | 0o644), ("package/link", stat.S_IFLNK | 0o777)]:
            with self.subTest(name=name):
                archive = self.root / "package.zip"
                with zipfile.ZipFile(archive, "w") as bundle:
                    info = zipfile.ZipInfo(name)
                    info.external_attr = mode << 16
                    bundle.writestr(info, b"bad")
                with self.assertRaises(ValueError):
                    package.extract(archive, self.destination)

    def test_valid_tar_and_zip_extract(self):
        archive = self.tar([("package/avr", b"payload", tarfile.REGTYPE)])
        extracted = package.extract(archive, self.destination)
        self.assertEqual((extracted / "avr").read_bytes(), b"payload")
        other = self.root / "zip"
        other.mkdir()
        archive = self.root / "package.zip"
        with zipfile.ZipFile(archive, "w") as bundle:
            info = zipfile.ZipInfo("package/avr.exe")
            info.external_attr = (stat.S_IFREG | 0o755) << 16
            bundle.writestr(info, b"payload")
        extracted = package.extract(archive, other)
        self.assertEqual((extracted / "avr.exe").read_bytes(), b"payload")


if __name__ == "__main__":
    unittest.main()
