"""Compare fresh-process CLI startup with paired, alternating before/after runs.

Uses an isolated home, piped output, and local-only commands. Filesystem caches
are warmed; these are fresh processes, not a machine-reboot/cold-disk benchmark.
Outputs and exit codes must match. Use identical version labels in both builds.
"""

from pathlib import Path
import argparse
import hashlib
import json
import os
import platform
import random
import statistics
import subprocess
import tempfile
import time

CASES = [
    ("root help", ["--help"], {}),
    ("help command", ["help"], {}),
    ("nested help", ["repo", "list", "--help"], {}),
    ("version", ["--version"], {}),
    ("JSON field discovery", ["auth", "status", "--json", "?"], {}),
    ("root completion", [], {"_AVR_COMPLETE": "bash_complete", "COMP_WORDS": "avr ", "COMP_CWORD": "1"}),
    ("nested completion", [], {"_AVR_COMPLETE": "bash_complete", "COMP_WORDS": "avr repo list --", "COMP_CWORD": "3"}),
]


def run(binary, args, env, home):
    started = time.perf_counter_ns()
    process = subprocess.run([str(binary), *args], env=env, cwd=home, capture_output=True, timeout=20)
    elapsed = (time.perf_counter_ns() - started) / 1e6
    if process.returncode:
        raise RuntimeError(f"{binary.name} {args!r}: exit {process.returncode}")
    return elapsed, (process.returncode, process.stdout, process.stderr)


def distribution(values):
    return {
        "median_ms": statistics.median(values),
        "p90_ms": statistics.quantiles(values, n=10, method="inclusive")[8],
        "min_ms": min(values),
        "max_ms": max(values),
        "samples_ms": values,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--before", type=Path)
    mode.add_argument("--binary", type=Path, help="Report one build for CI trends; no timing threshold.")
    parser.add_argument("--after", type=Path)
    parser.add_argument("--samples", type=int, default=101)
    parser.add_argument("--warmup", type=int, default=5)
    parser.add_argument("--report", type=Path, default=Path("build/go/startup/comparison.json"))
    args = parser.parse_args()
    if args.samples < 10 or args.warmup < 0:
        parser.error("use at least 10 samples and a nonnegative warmup")
    if bool(args.before) != bool(args.after):
        parser.error("--before and --after must be used together")
    binaries = (
        {"current": args.binary.resolve()}
        if args.binary
        else {"before": args.before.resolve(), "after": args.after.resolve()}
    )
    report = {
        "platform": platform.platform(),
        "machine": platform.machine(),
        "samples": args.samples,
        "warmup": args.warmup,
        "binary_sha256": {name: hashlib.sha256(path.read_bytes()).hexdigest() for name, path in binaries.items()},
        "measurement": "fresh process; warmed filesystem; isolated HOME; piped output; no API requests",
        "cases": {},
    }
    randomizer = random.Random(75)
    with tempfile.TemporaryDirectory(prefix="avr-startup-") as directory:
        home = Path(directory)
        env = {key: value for key, value in os.environ.items() if not key.startswith(("AVR_", "_AVR_", "COMP_"))} | {
            "HOME": directory,
            "USERPROFILE": directory,
            "AVR_CONFIG_DIR": directory,
            "XDG_CONFIG_HOME": directory,
            "APPDATA": directory,
            "LOCALAPPDATA": directory,
            "NO_COLOR": "1",
            "TERM": "dumb",
            "COLUMNS": "80",
            "AVR_DEBUG": "",
            "AVR_CPU_PROFILE": "",
            "AVR_NO_UPDATE_NOTIFIER": "1",
            "_AVR_COMPLETE": "",
            "GODEBUG": "",
        }
        for name, command, extra in CASES:
            samples = {label: [] for label in binaries}
            reference = None
            for iteration in range(args.warmup + args.samples):
                order = list(binaries)
                randomizer.shuffle(order)
                for label in order:
                    elapsed, result = run(binaries[label], command, env | extra, home)
                    if reference is None:
                        reference = result
                    elif result != reference:
                        raise RuntimeError(f"{name}: output/exit changed between runs")
                    if iteration >= args.warmup:
                        samples[label].append(elapsed)
            result = {label: distribution(values) for label, values in samples.items()}
            report["cases"][name] = result
            if args.binary:
                print(
                    f"{name}: {result['current']['median_ms']:.3f} ms (p90 {result['current']['p90_ms']:.3f} ms)",
                    flush=True,
                )
            else:
                result["reduction_percent"] = 100 * (1 - result["after"]["median_ms"] / result["before"]["median_ms"])
                print(
                    f"{name}: {result['before']['median_ms']:.3f} → {result['after']['median_ms']:.3f} ms "
                    f"({result['reduction_percent']:.1f}% less)",
                    flush=True,
                )
        # Kept separate from wall-clock samples: tracing itself distorts timing.
        trace = subprocess.run(
            [str(list(binaries.values())[-1]), "--version"],
            env=env | {"GODEBUG": "inittrace=1"},
            cwd=home,
            capture_output=True,
            timeout=20,
            check=True,
        )
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.with_suffix(".inittrace.txt").write_bytes(trace.stderr)
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
