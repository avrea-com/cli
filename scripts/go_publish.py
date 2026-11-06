"""Resume a gated release without replacing already-published bytes.

Called only by the gated publish job. Preparation uploads verified artifacts to
a draft (or checks an existing release); finalization happens after PyPI succeeds.
"""

from pathlib import Path
from scripts.go_distribution import python_version
from scripts.go_package import digest
from urllib.error import HTTPError
from urllib.request import urlopen
import argparse
import json
import subprocess
import tempfile


def gh(*args):
    return subprocess.run(["gh", *args], check=True, capture_output=True, text=True).stdout


def verify_pypi(files, published):
    expected = {path.name: digest(path) for path in files if path.suffix == ".whl"}
    for item in published:
        if item["filename"] not in expected or item["digests"]["sha256"] != expected[item["filename"]]:
            raise ValueError("PyPI already contains different artifacts for this version; refusing to resume")


def prepare(directory, tag):
    version = (directory / "avr-version.txt").read_text().strip()
    if tag != "v" + version:
        raise ValueError("Release tag must match tested artifact version")

    # GitHub list distinguishes a missing release from a failed API request.
    def find_release():
        releases = json.loads(gh("api", "--paginate", "--slurp", "repos/{owner}/{repo}/releases?per_page=100"))
        return next((r for page in releases for r in page if r["tag_name"] == tag), None)

    release = find_release()
    if release is None:
        gh("release", "create", tag, "--draft", "--verify-tag", "--title", tag, "--generate-notes")
        release = find_release()
    if release is None:
        raise ValueError("Created release draft could not be found")
    existing = json.loads(gh("api", "repos/{owner}/{repo}/releases/" + str(release["id"])))["assets"]
    files = sorted(path for path in directory.iterdir() if path.is_file())
    by_name = {path.name: path for path in files}
    incomplete = []
    # Compare all existing assets before adding anything. Never use --clobber:
    # rerunning all jobs might rebuild different bytes under the same version.
    with tempfile.TemporaryDirectory() as temp:
        for asset in existing:
            name = asset["name"]
            if name not in by_name:
                raise ValueError("GitHub release contains an unexpected artifact")
            if asset["state"] == "starter" and asset["size"] == 0:
                incomplete.append(asset)
                continue
            if asset["state"] != "uploaded":
                raise ValueError("GitHub release contains an unrecognized incomplete upload")
            gh("release", "download", tag, "--pattern", name, "--dir", temp)
            if digest(Path(temp) / name) != digest(by_name[name]):
                raise ValueError(f"GitHub release artifact differs: {name}")
    url = f"https://pypi.org/pypi/avr-cli/{python_version(version)}/json"
    try:
        with urlopen(url, timeout=30) as response:
            published = json.load(response)["urls"]
    except HTTPError as error:
        if error.code != 404:
            raise
        published = []
    verify_pypi(files, published)
    # GitHub may leave zero-byte 'starter' placeholders after an upload 502.
    # These contain no published bytes; remove only those exact placeholders.
    for asset in incomplete:
        gh("api", "--method", "DELETE", "repos/{owner}/{repo}/releases/assets/" + str(asset["id"]))
    names = {asset["name"] for asset in existing if asset not in incomplete}
    for path in files:
        if path.name not in names:
            gh("release", "upload", tag, str(path))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("phase", choices=["prepare", "finalize"])
    parser.add_argument("--tag", required=True)
    parser.add_argument("--directory", type=Path, default=Path("release"))
    args = parser.parse_args()
    if args.phase == "prepare":
        prepare(args.directory, args.tag)
    else:
        prerelease = "-rc." in args.tag
        gh(
            "release",
            "edit",
            args.tag,
            "--draft=false",
            f"--prerelease={str(prerelease).lower()}",
            f"--latest={str(not prerelease).lower()}",
        )


if __name__ == "__main__":
    main()
