"""Compare every internal docs format and generated file against pinned Python."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_job_watch as watch
import json
import sys

README = b"# Fixture CLI\r\n\r\nCurated overview with `<example>` and a > quote.\r\n"


def scenarios():
    cases = []

    def case(name, flags=None, **kwargs):
        cases.append(
            {
                "name": "internal docs " + name,
                "args": ["internal", "docs", *(flags or [])],
                "env": {},
                "responses": {},
                "capture_files": ["out"],
                "capture_text_files": True,
                **kwargs,
            }
        )

    case("default all", ["--out", "out"])
    case("explicit all", ["--format", "all", "--out", "out"])
    case("environment independent defaults", ["--format", "json", "--stdout"], env={"AVR_LINKS": "0"}, json=True)
    for fmt in ["markdown", "json", "starlight", "man"]:
        case(fmt + " file", ["--format", fmt, "--out", "out"])
        case(fmt + " existing directory", ["--format", fmt, "--out", "out"], fixture_dirs=["out"])
        case(fmt + " nested directory", ["--format", fmt, "--out", "out/nested/result"])
    for fmt in ["markdown", "json"]:
        case(fmt + " stdout", ["--format", fmt, "--stdout"], json=fmt == "json")
        case(fmt + " stdout ignores out", ["--format", fmt, "--stdout", "--out", "out"], json=fmt == "json")
    for fmt in ["starlight", "all"]:
        case(
            fmt + " README",
            ["--format", fmt, "--out", "out", "--readme", "intro.md"],
            fixture_files={"intro.md": README},
        )
        case(fmt + " missing README", ["--format", fmt, "--out", "out", "--readme", "missing.md"])
        case(
            fmt + " empty README",
            ["--format", fmt, "--out", "out", "--readme", "intro.md"],
            fixture_files={"intro.md": b""},
        )
    case(
        "stale starlight",
        ["--format", "starlight", "--out", "out"],
        fixture_files={
            "out/old.md": b"remove",
            "out/reference/nested/old.md": b"remove",
            "out/old.1": b"remove",
            "out/keep.txt": b"keep",
            "out/nested/keep.json": b'{"keep":true}',
            "out/_cli-sidebar.mjs": b"replace",
        },
    )
    case(
        "stale man",
        ["--format", "man", "--out", "out"],
        fixture_files={
            "out/man1/old.1": b"remove",
            "out/man1/nested/old.md": b"remove",
            "out/man1/keep.txt": b"keep",
            "out/outside.md": b"keep",
        },
    )
    case("overwrite file", ["--format", "markdown", "--out", "out"], fixture_files={"out": b"old content"})
    case("trailing slash file", ["--format", "markdown", "--out", "out/"])
    for name, flags, terms in [
        ("missing out", [], ["--out is required"]),
        ("invalid format", ["--format", "other", "--out", "out"], ["--format"]),
        ("case-sensitive format", ["--format", "JSON", "--stdout"], ["--format"]),
        ("all stdout", ["--stdout"], ["--stdout is only valid"]),
        ("man stdout", ["--format", "man", "--stdout"], ["--stdout is only valid"]),
        ("starlight stdout", ["--format", "starlight", "--stdout"], ["--stdout is only valid"]),
        ("extra argument", ["--format", "json", "--stdout", "extra"], ["extra"]),
    ]:
        case(name, flags, exit=2, error_terms=terms)
    for flag in ["--out", "--format", "--readme"]:
        case("missing value " + flag, [flag], exit=2, error_terms=[flag, "argument"])
    case(
        "README directory",
        ["--format", "json", "--stdout", "--readme", "intro"],
        fixture_dirs=["intro"],
        exit=2,
        error_terms=["--readme", "directory"],
    )
    if sys.platform != "win32":
        for name in ["markdown stdout", "default all"]:
            row = deepcopy(next(c for c in cases if c["name"] == "internal docs " + name))
            row["name"] += " terminal"
            row["tty"] = row["merge_terminal"] = True
            row["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(row)
    return cases


def comparable_text(name, value):
    value = value.replace("\r\n", "\n")
    if name.endswith(".json") or value.lstrip().startswith("{"):
        try:
            return json.loads(value)
        except ValueError:
            pass
    # The runtime is now Go; this generated comment accurately names the
    # command tree without the old implementation language.
    if name.endswith("_cli-sidebar.mjs"):
        value = value.replace("from the avr Click tree", "from the avr command tree")
    return value


def compare(scenario, python, go):
    differences = watch.compare(scenario, python, go)
    a, b = python["files"], go["files"]
    if a.keys() != b.keys():
        differences.append("generated file membership: " + repr(sorted(a.keys() ^ b.keys())))
    for name in a.keys() & b.keys():
        if a[name]["mode"] != b[name]["mode"]:
            differences.append("file mode: " + name)
        if comparable_text(name, a[name]["text"]) != comparable_text(name, b[name]["text"]):
            differences.append("file content: " + name)
    for label, result in [("Python", python), ("Go", go)]:
        if result["requests"]:
            differences.append(label + " docs made an API call")
        for stale in [
            "out/old.md",
            "out/reference/nested/old.md",
            "out/old.1",
            "out/man1/old.1",
            "out/man1/nested/old.md",
        ]:
            if stale in result["files"]:
                differences.append(label + " failed to prune " + stale)
    if (
        "--stdout" in scenario["args"]
        and "--format" in scenario["args"]
        and scenario["args"][scenario["args"].index("--format") + 1] == "markdown"
    ):
        if python["stdout"].replace("\r\n", "\n") != go["stdout"].replace("\r\n", "\n"):
            differences.append("Markdown stdout content")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/internal-docs-parity.json"
        )
    )
