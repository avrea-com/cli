"""Measure auth status with opt-in HTTP timing; never save account output.

Uses the current AVR configuration and performs read-only /users/me requests.
Alternates fresh instrumented/control processes; connections are not shared
between CLI invocations. Reports contain numeric events, not URLs or credentials.
"""

from pathlib import Path
import argparse
import hashlib
import json
import os
import platform
import statistics
import subprocess
import time

PREFIX = "avr-timing "
ARGS = ["auth", "status", "--json", "email,host", "--jq", ".email"]


def measured_run(binary, instrumented):
    env = dict(os.environ)
    env["AVR_DEBUG"] = "timing" if instrumented else ""
    started = time.perf_counter()
    result = subprocess.run([str(binary), *ARGS], env=env, capture_output=True, timeout=45)
    wall_ms = (time.perf_counter() - started) * 1000
    if result.returncode:
        raise RuntimeError(f"auth status failed with exit {result.returncode}; account output withheld")
    records = [
        json.loads(line.removeprefix(PREFIX)) for line in result.stderr.decode().splitlines() if line.startswith(PREFIX)
    ]
    sample = {"wall_ms": wall_ms}
    if instrumented:
        http = [r for r in records if r["kind"] == "http"]
        command = [r for r in records if r["kind"] == "command"]
        if len(http) != 1 or len(command) != 1:
            raise RuntimeError("Expected one API request and one command timing; scoped-token status is local-only.")
        sample.update(http=http[0], command=command[0])
        try:
            sample["phases_ms"] = phases(sample)
        except RuntimeError as exc:
            sample["phase_analysis_error"] = str(exc)
    elif records:
        raise RuntimeError("Timing output emitted without opt-in")
    return sample, result.stdout


def phases(sample):
    http = sample["http"]
    events = http["events"]

    def event(name):
        found = [e["ms"] for e in events if e["name"] == name]
        if len(found) != 1:
            raise RuntimeError(f"Expected one {name} event; inspect raw events for retries or overlapping phases")
        return found[0]

    # A first-success envelope includes failed/fallback TCP attempts, without
    # summing concurrent attempts. Retain every attempt in the raw report.
    starts = [e["ms"] for e in events if e["name"] == "connect_start"]
    successes = [e["ms"] for e in events if e["name"] == "connect_done" and not e.get("failed")]
    tcp = min(successes) - min(starts) if starts and successes else 0
    names = {e["name"] for e in events}
    result = {
        "process_startup_shutdown": sample["wall_ms"] - sample["command"]["duration_ms"],
        "command_outside_http": sample["command"]["duration_ms"] - http["duration_ms"],
        "dns": event("dns_done") - event("dns_start") if "dns_start" in names else 0,
        "tcp_to_first_success": tcp,
        "tls": event("tls_done") - event("tls_start") if "tls_start" in names else 0,
        "request_to_first_byte": event("first_response_byte") - event("request_written"),
        "response_headers": event("response_headers") - event("first_response_byte"),
        "body_and_decompression": event("body_read") - event("response_headers"),
        "decode_and_cleanup": http["duration_ms"] - event("body_read"),
    }
    if any(value < 0 for value in result.values()):
        raise RuntimeError("Overlapping trace events require manual analysis; refusing misleading additive timings")
    inside = {k: v for k, v in result.items() if k not in {"process_startup_shutdown", "command_outside_http"}}
    result["http_other"] = http["duration_ms"] - sum(inside.values())
    if result["http_other"] < -0.1:
        raise RuntimeError("Overlapping connection attempts require manual analysis")
    return result


def distribution(values):
    return {
        "median_ms": statistics.median(values),
        "p90_ms": statistics.quantiles(values, n=10, method="inclusive")[8],
        "min_ms": min(values),
        "max_ms": max(values),
    }


def main():
    root = Path(__file__).resolve().parents[1]
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", type=Path, default=root / "build/go/avr")
    parser.add_argument("--runs", type=int, default=20)
    parser.add_argument("--warmups", type=int, default=2)
    parser.add_argument("--report", type=Path, default=root / "build/go/auth-timing.json")
    args = parser.parse_args()
    if args.runs < 2 or args.warmups < 0:
        parser.error("--runs must be >= 2 and --warmups >= 0")
    binary = args.go.resolve()
    report = {
        "platform": platform.platform(),
        "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
        "command": ARGS,
        "runs_per_mode": args.runs,
        "warmups_per_mode": args.warmups,
        "controls": [],
        "instrumented": [],
        "method": "Fresh processes, alternating timing/control order; same configuration; no output saved.",
    }
    expected = None
    for phase, count in (("warmup", args.warmups), ("measured", args.runs)):
        for i in range(count):
            for instrumented in (True, False) if i % 2 == 0 else (False, True):
                sample, output = measured_run(binary, instrumented)
                if expected is None:
                    expected = output
                if output != expected:
                    raise RuntimeError("Auth output changed between runs; output withheld")
                if phase == "measured":
                    sample["round"] = i + 1
                    report["instrumented" if instrumented else "controls"].append(sample)
    analyzable = [s for s in report["instrumented"] if "phases_ms" in s]
    summary = {
        "phase_samples": len(analyzable),
        "phase_samples_requiring_manual_analysis": len(report["instrumented"]) - len(analyzable),
        "instrumented_wall": distribution([s["wall_ms"] for s in report["instrumented"]]),
        "control_wall": distribution([s["wall_ms"] for s in report["controls"]]),
        "http_total": distribution([s["http"]["duration_ms"] for s in report["instrumented"]]),
        "phases": {
            key: distribution([s["phases_ms"][key] for s in analyzable])
            if len(analyzable) >= 2
            else {"value_ms": analyzable[0]["phases_ms"][key]}
            for key in (analyzable[0]["phases_ms"] if analyzable else {})
        },
        "protocols": sorted({s["http"]["protocol"] for s in report["instrumented"]}),
        "reused_connections": sum(s["http"]["connection_reused"] for s in report["instrumented"]),
        "connect_attempts": [
            sum(e["name"] == "connect_start" for e in s["http"]["events"]) for s in report["instrumented"]
        ],
    }
    report["summary"] = summary
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(summary, indent=2))
    print(f"Report: {args.report}")


if __name__ == "__main__":
    main()
