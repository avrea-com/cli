"""Fail the differential gate on missing, empty, stale or differing reports."""

from pathlib import Path
import argparse
import hashlib
import json
import sys

ROOT = Path(__file__).resolve().parents[1]
EXCLUDED_SUITES = {"compare_staging_readonly"}  # Opt-in live reads, never a fixture CI gate.


def inventory():
    suites = json.loads((ROOT / "scripts/parity_suites.json").read_text(encoding="utf-8"))
    discovered = {path.stem for path in (ROOT / "scripts").glob("compare_*.py")} - EXCLUDED_SUITES
    if set(suites) != discovered:
        raise ValueError("Parity suite inventory differs from scripts/compare_*.py")
    return suites


def validate_report(report, minimum, baseline, binary_sha):
    cases = report.get("scenarios")
    if not isinstance(cases, list) or len(cases) < max(1, minimum):
        raise ValueError(f"Report must contain at least {max(1, minimum)} cases")
    names = [case.get("name") for case in cases]
    if any(not name for name in names) or len(set(names)) != len(names):
        raise ValueError("Report has missing or duplicate scenario names")
    if any(case.get("differences") != [] for case in cases):
        raise ValueError("Report contains differences or incomplete results")
    if report.get("python_baseline") != baseline:
        raise ValueError("Report uses a different Python baseline")
    if report.get("native_binary_sha256", report.get("go_binary_sha256")) != binary_sha:
        raise ValueError("Report uses a different executable")


def validate(directory, binary, baseline, shard=None, platform=None):
    platform = platform or ("windows" if sys.platform == "win32" else "posix")
    binary_sha = hashlib.sha256(binary.read_bytes()).hexdigest()
    count = 0
    for name, suite in inventory().items():
        if shard is not None and suite["shard"] != shard:
            continue
        path = directory / suite["report"]
        try:
            report = json.loads(path.read_text(encoding="utf-8"))
            validate_report(report, suite["minimum_cases"][platform], baseline, binary_sha)
            # GitLab also exercises the shipped executable separately from its
            # vendor-transport fixture binary. Neither report may mask the other.
            if "native_report" in suite:
                if Path(report.get("native_report", "")).name != suite["native_report"]:
                    raise ValueError("Missing required native subreport")
                native = json.loads((directory / suite["native_report"]).read_text(encoding="utf-8"))
                validate_report(native, suite["minimum_native_cases"][platform], baseline, binary_sha)
        except (OSError, ValueError, TypeError, KeyError) as error:
            raise ValueError(f"{name}: {error}") from error
        count += 1
    if not count:
        raise ValueError("No suites selected")
    return count


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", type=Path, default=ROOT / "build/go")
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--shard", type=int, choices=range(4))
    args = parser.parse_args()
    # Read the same pinned baseline without importing or executing suite code.
    import ast  # noqa: PLC0415 - CLI entry point only

    tree = ast.parse((ROOT / "scripts/compare_auth.py").read_text(encoding="utf-8"))
    baseline = next(
        ast.literal_eval(node.value)
        for node in tree.body
        if isinstance(node, ast.Assign) and any(isinstance(t, ast.Name) and t.id == "BASELINE" for t in node.targets)
    )
    count = validate(args.directory, args.binary, baseline, args.shard)
    print(f"Validated {count} differential suites: required counts, baseline, executable and zero differences")


if __name__ == "__main__":
    main()
