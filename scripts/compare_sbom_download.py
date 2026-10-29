"""SBOM download parity: formats, bytes, destination effects and failures."""

from copy import deepcopy
from scripts import compare_artifact as artifact
from scripts import compare_auth as harness
from scripts import compare_sbom as sbom
import sys

ORG, REPO, SHA, BASE = sbom.ORG, sbom.REPO, sbom.SHA, sbom.BASE
CONTENTS = {
    "cyclonedx": b'{"bomFormat":"CycloneDX","components":[]}\n',
    "spdx": b'{"spdxVersion":"SPDX-2.3","packages":[]}\n',
    "inventory": b'{"dependencies":[{"name":"caf\xc3\xa9","version":"1.0"}]}\n',
}
FILENAMES = {"cyclonedx": "bom.cyclonedx.json", "spdx": "bom.spdx.json", "inventory": "inventory.json"}


def scenarios():
    cases = []

    def case(name, args, kind="cyclonedx", expected=None, **kwargs):
        filename = FILENAMES[kind]
        cases.append(
            {
                "name": "sbom download " + name,
                "args": ["sbom", "download", *args],
                "env": sbom.ENV.copy(),
                "responses": {
                    sbom.LATEST: (200, sbom.SNAPSHOT),
                    ("GET", BASE + "/commits/" + SHA + "/artifacts/" + filename): (200, CONTENTS[kind]),
                },
                "capture_files": ["downloads", *FILENAMES.values()],
                "fixture_dirs": ["downloads"],
                "expected_files": {filename: artifact.file_record(CONTENTS[kind], 0o644)}
                if expected is None
                else expected,
                **kwargs,
            }
        )

    case("default", ["--repo", REPO], presentation=True)
    for kind in FILENAMES:
        case(kind + " file", [SHA, "--repo", REPO, "--format", kind], kind=kind, presentation=True)
        case(
            kind + " stdout",
            [SHA, "--repo", REPO, "--format", kind, "--out", "-"],
            kind=kind,
            expected={},
            exact_stdout=True,
        )
    case(
        "custom file",
        [SHA, "--repo", REPO, "--out", "downloads/custom.json"],
        expected={"downloads/custom.json": artifact.file_record(CONTENTS["cyclonedx"], 0o644)},
        presentation=True,
    )
    case("empty out", [SHA, "--repo", REPO, "--out", ""], presentation=True)
    case(
        "replace",
        [SHA, "--repo", REPO, "--out", "downloads/custom.json"],
        fixture_files={"downloads/custom.json": b"old"},
        expected={"downloads/custom.json": artifact.file_record(CONTENTS["cyclonedx"], 0o644)},
        presentation=True,
    )
    case("prefix", ["aaaaaaa", "--repo", REPO], presentation=True)
    cases[-1]["responses"][sbom.HISTORY] = (200, {"data": [sbom.ROW]})
    case("option SHA", ["--commit", " " + SHA.upper() + " ", "--repo", REPO], presentation=True)
    case("same commit twice", [SHA, "--commit", SHA, "--repo", REPO], presentation=True)
    case(
        "conflicting commit",
        [SHA, "--commit", sbom.OTHER, "--repo", REPO],
        responses={},
        expected={},
        exit=2,
        error_terms=["Pass the commit"],
    )
    for value in ["", "abc", "main"]:
        case(
            "invalid commit " + repr(value),
            ["--commit", value, "--repo", REPO],
            responses={},
            expected={},
            exit=2,
            error_terms=["hex commit SHA"],
        )
    for value in ["", "SPDX", "invalid"]:
        case(
            "invalid format " + repr(value),
            ["--format", value, "--repo", REPO],
            responses={},
            expected={},
            exit=2,
            error_terms=["--format"],
        )
    case("missing auth", ["--repo", REPO], responses={}, env={}, expected={}, exit=4)
    case("missing repo", [], responses={}, expected={}, exit=1)
    case(
        "parent missing",
        [SHA, "--repo", REPO, "--out", "downloads/missing/output.json"],
        expected={},
        exit=1,
        error_terms=["Error writing file", "Aborted!"],
    )
    case(
        "out is directory",
        [SHA, "--repo", REPO, "--out", "downloads"],
        expected={},
        exit=1,
        error_terms=["Error writing file", "Aborted!"],
    )
    for status in [401, 403, 404, 422, 503]:
        case(
            "HTTP " + str(status),
            [SHA, "--repo", REPO, "--out", "downloads/existing.json"],
            responses={
                ("GET", BASE + "/commits/" + SHA + "/artifacts/bom.cyclonedx.json"): (
                    status,
                    {"detail": "fixture denied"},
                )
            },
            fixture_files={"downloads/existing.json": b"old"},
            expected={"downloads/existing.json": artifact.file_record(b"old", 0o644)},
            exit=4 if status == 401 else 1,
        )
    for encoding in ["gzip", "zstd"]:
        case(
            encoding,
            [SHA, "--repo", REPO, "--format", "inventory"],
            kind="inventory",
            encoding=encoding,
            presentation=True,
        )
    if sys.platform != "win32":
        for name in ["default", "custom file", "inventory stdout"]:
            item = deepcopy(next(c for c in cases if c["name"] == "sbom download " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


def compare(scenario, python, go):
    differences = artifact.compare(scenario, python, go)
    if scenario.get("exact_stdout") and python["stdout"] != go["stdout"]:
        differences.append("stdout bytes")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/sbom-download-parity.json"
        )
    )
