"""Make native PyPI wheels and release channel files from verified Go archives."""

from pathlib import Path
from scripts.go_package import ROOT
from scripts.go_package import TARGETS
from scripts.go_package import digest
from scripts.go_package import extract
from scripts.go_package import verify
import argparse
import base64
import csv
import hashlib
import io
import json
import re
import shutil
import stat
import tempfile
import zipfile

PLATFORMS = {
    "darwin_arm64": "macosx_13_0_arm64",
    "linux_amd64": "manylinux_2_17_x86_64",
    "linux_arm64": "manylinux_2_17_aarch64",
    "windows_amd64": "win_amd64",
}


def python_version(version):
    if not re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?", version):
        raise ValueError("Expected a stable version or -rc.N")
    return version.replace("-rc.", "rc")


def wheel(package, out):
    manifest = json.loads((package / "manifest.json").read_text(encoding="utf-8"))
    version = python_version(manifest["version"])
    target = manifest["target"]
    tag = f"py3-none-{PLATFORMS[target]}"
    dist = f"avr_cli-{version}"
    info = dist + ".dist-info"
    data = dist + ".data"
    binary = "avr.exe" if target == "windows_amd64" else "avr"
    alias = "avr-cli.exe" if target == "windows_amd64" else "avr-cli"
    payload = {
        f"{data}/scripts/{binary}": ((package / binary).read_bytes(), 0o755),
        f"{data}/scripts/{alias}": ((package / binary).read_bytes(), 0o755),
        f"{data}/data/share/avr/install-method": (b"pypi\n", 0o644),
        f"{info}/METADATA": (
            (
                f"Metadata-Version: 2.4\nName: avr-cli\nVersion: {version}\n"
                "Summary: Native Avrea command-line client\nRequires-Python: >=3.9\n"
                "License-Expression: Apache-2.0\nLicense-File: LICENSE\n"
                "License-File: THIRD-PARTY-NOTICES.txt\n"
                "Project-URL: Homepage, https://avrea.com/\n"
                "Project-URL: Repository, https://github.com/avrea-com/cli\n"
                "Description-Content-Type: text/markdown\n\n"
                "Native AVR executable. No Python runtime is used when running avr.\n"
            ).encode(),
            0o644,
        ),
        f"{info}/WHEEL": (
            f"Wheel-Version: 1.0\nGenerator: avr-distribution\nRoot-Is-Purelib: false\nTag: {tag}\n".encode(),
            0o644,
        ),
        f"{info}/licenses/LICENSE": ((package / "LICENSE").read_bytes(), 0o644),
        f"{info}/licenses/THIRD-PARTY-NOTICES.txt": ((package / "THIRD-PARTY-NOTICES.txt").read_bytes(), 0o644),
    }
    for path in sorted(package.rglob("*")):
        if path.is_file() and path.name != binary:
            payload[f"{data}/data/share/avr/{path.relative_to(package).as_posix()}"] = (path.read_bytes(), 0o644)
    record = io.StringIO(newline="")
    writer = csv.writer(record, lineterminator="\n")
    for name, (content, _) in sorted(payload.items()):
        checksum = base64.urlsafe_b64encode(hashlib.sha256(content).digest()).rstrip(b"=").decode()
        writer.writerow([name, f"sha256={checksum}", len(content)])
    writer.writerow([f"{info}/RECORD", "", ""])
    payload[f"{info}/RECORD"] = (record.getvalue().encode(), 0o644)
    result = out / f"{dist}-{tag}.whl"
    with zipfile.ZipFile(result, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for name, (content, mode) in sorted(payload.items()):
            entry = zipfile.ZipInfo(name)
            entry.create_system = 3
            entry.external_attr = (stat.S_IFREG | mode) << 16
            entry.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(entry, content)
    return result


def build(archive, out, go="go"):
    verify(archive, go=go)
    out.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory() as temporary:
        package = extract(archive, Path(temporary))
        result = wheel(package, out)
    print(f"Built {result}")
    return result


def channels(directory, version, commit=None):
    """Require the complete validated release set before preparing channel metadata."""
    python_version(version)
    reports = {}
    for target in sorted(TARGETS):
        suffix = ".zip" if target == "windows_amd64" else ".tar.gz"
        name = f"avr_{version}_{target}{suffix}"
        archive = directory / name
        checksum = digest(archive)
        if (directory / (name + ".sha256")).read_text(encoding="utf-8").strip() != f"{checksum}  {name}":
            raise ValueError(f"Checksum mismatch: {name}")
        report = json.loads((directory / (name + ".validation.json")).read_text(encoding="utf-8"))
        manifest = report["manifest"]
        if (
            report["status"] != "passed"
            or report["sha256"] != checksum
            or manifest["version"] != version
            or manifest["target"] != target
        ):
            raise ValueError(f"Validation mismatch: {name}")
        if commit and (manifest["dirty"] or manifest["commit"] != commit):
            raise ValueError(f"Release requires clean source at {commit}")
        wheel_path = directory / f"avr_cli-{python_version(version)}-py3-none-{PLATFORMS[target]}.whl"
        if not wheel_path.is_file():
            raise ValueError(f"Missing wheel: {wheel_path}")
        installed = json.loads(wheel_path.with_name(wheel_path.name + ".validation.json").read_text(encoding="utf-8"))
        if installed["status"] != "passed" or installed["sha256"] != digest(wheel_path):
            raise ValueError(f"Wheel installation validation mismatch: {wheel_path}")
        reports[target] = checksum
    (directory / "avr-version.txt").write_text(version + "\n", encoding="utf-8")
    for name in ["install.sh", "install.ps1"]:
        shutil.copyfile(ROOT / "packaging" / name, directory / name)
    # The tap is a separate repository. Generate the exact formula for its PR;
    # preparing or publishing CLI artifacts never writes to the tap.
    formula = f'''class Avr < Formula
  desc "Avrea command-line client"
  homepage "https://avrea.com/"
  version "{version}"
  license "Apache-2.0"

  on_macos do
    depends_on arch: :arm64
    depends_on macos: :ventura
    on_arm do
      url "https://github.com/avrea-com/cli/releases/download/v{version}/avr_{version}_darwin_arm64.tar.gz"
      sha256 "{reports["darwin_arm64"]}"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/avrea-com/cli/releases/download/v{version}/avr_{version}_linux_amd64.tar.gz"
      sha256 "{reports["linux_amd64"]}"
    end
    on_arm do
      url "https://github.com/avrea-com/cli/releases/download/v{version}/avr_{version}_linux_arm64.tar.gz"
      sha256 "{reports["linux_arm64"]}"
    end
  end

  def install
    bin.install "avr"
    bash_completion.install "completions/avr.bash" => "avr"
    zsh_completion.install "completions/_avr"
    fish_completion.install "completions/avr.fish"
    man1.install Dir["man/man1/*.1"]
    doc.install "README.md", "THIRD-PARTY-NOTICES.txt"
  end

  test do
    assert_match "avr version #{{version}}", shell_output("#{{bin}}/avr --version")
    assert_match "USAGE", shell_output("#{{bin}}/avr repo --help")
  end
end
'''
    (directory / "avr.rb").write_text(formula, encoding="utf-8")
    mise = [
        '[tools."github:avrea-com/cli"]',
        f'version = "{version}"',
        "strip_components = 1",
        'bin_path = "."',
    ]
    for platform, target in [
        ("macos-arm64", "darwin_arm64"),
        ("linux-x64", "linux_amd64"),
        ("linux-arm64", "linux_arm64"),
        ("windows-x64", "windows_amd64"),
    ]:
        suffix = "zip" if target == "windows_amd64" else "tar.gz"
        mise += [
            "",
            f'[tools."github:avrea-com/cli".platforms.{platform}]',
            f'asset_pattern = "avr_{version}_{target}.{suffix}"',
            f'checksum = "sha256:{reports[target]}"',
        ]
    (directory / "mise.toml").write_text("\n".join(mise) + "\n", encoding="utf-8")
    lines = [
        f"{digest(path)}  {path.name}"
        for path in sorted(directory.iterdir())
        if path.is_file() and not path.name.endswith((".sha256", ".validation.json")) and path.name != "SHA256SUMS"
    ]
    (directory / "SHA256SUMS").write_text("\n".join(lines) + "\n", encoding="utf-8")


def prepare_sbom(directory, destination):
    destination.mkdir(parents=True, exist_ok=True)
    for archive in sorted(directory.iterdir()):
        if not archive.name.endswith((".tar.gz", ".zip")):
            continue
        with tempfile.TemporaryDirectory() as temporary:
            package = extract(archive, Path(temporary))
            manifest = json.loads((package / "manifest.json").read_text(encoding="utf-8"))
            target = manifest["target"]
            if target not in TARGETS:
                raise ValueError(f"Unsupported SBOM target: {target}")
            binary = "avr.exe" if target == "windows_amd64" else "avr"
            stage = destination / target
            stage.mkdir(exist_ok=True)
            shutil.copyfile(package / binary, stage / binary)


def validate_sbom(path, version):
    document = json.loads(path.read_text(encoding="utf-8"))
    components = document.get("components", [])
    names = {component["name"] for component in components}
    required = {"github.com/avrea-com/cli", "github.com/itchyny/gojq", "github.com/spf13/cobra"}
    if not required <= names:
        raise ValueError(f"Native SBOM is missing packages: {sorted(required - names)}")
    root = document.get("metadata", {}).get("component", {})
    if root.get("name") != "avr-cli" or root.get("version") != version:
        raise ValueError("Native SBOM does not identify the release version")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path)
    parser.add_argument("--out", type=Path, default=ROOT / "build/go/distribution")
    parser.add_argument("--go", default="go")
    parser.add_argument("--channels", action="store_true")
    parser.add_argument("--version", default=(ROOT / "internal/version/VERSION").read_text().strip())
    parser.add_argument("--commit")
    parser.add_argument("--sbom-source", type=Path)
    parser.add_argument("--check-sbom", type=Path)
    args = parser.parse_args()
    if args.check_sbom:
        validate_sbom(args.check_sbom, args.version)
    elif args.channels:
        channels(args.out, args.version, args.commit)
        if args.sbom_source:
            prepare_sbom(args.out, args.sbom_source)
    elif args.archive:
        build(args.archive, args.out, args.go)
    else:
        parser.error("--archive or --channels is required")


if __name__ == "__main__":
    main()
