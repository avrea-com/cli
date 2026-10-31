"""Compare explicitly allowlisted read-only CLI paths against avrea-core staging.

Never invokes writes, login/credential issuance, SSH, browser opening or VM
sessions. Downloads go to a private temporary directory. Reports contain
private staging output and are stored under build/ with owner-only permissions.
Use --only to repeat a case family after a fix. Python is the checkout oracle.
"""

from datetime import datetime
from pathlib import Path
from scripts.compare_inspection import presentation
from scripts.compare_inspection import terminal_text
from urllib.parse import parse_qsl
from urllib.parse import urlencode
from urllib.parse import urlsplit
from urllib.parse import urlunsplit
import argparse
import hashlib
import json
import math
import os
import re
import signal
import struct
import subprocess
import sys
import tempfile
import threading

READ_ONLY = {
    "auth status",
    "org list",
    "org members",
    "org email-domain list",
    "org install list",
    "org install runner list",
    "org saml show",
    "org saml sp-metadata",
    "org saml test",
    "repo list",
    "repo mirror status",
    "repo mirror locations",
    "repo public-mirror check",
    "repo public-mirror requests",
    "repo public-mirror view",
    "run list",
    "run view",
    "run logs",
    "run diagnose",
    "run watch",
    "job list",
    "job view",
    "job logs",
    "job metrics",
    "job watch",
    "log search",
    "pr list",
    "workflow list",
    "workflow view",
    "tests list",
    "artifact list",
    "artifact get",
    "artifact download",
    "sbom list",
    "sbom view",
    "sbom download",
    "cache list",
    "cache usage",
    "settings list",
    "settings schema",
    "audit-events list",
    "health",
    "status",
    "config get",
    "config list",
    "billing summary",
    "billing settings",
    "billing invoices list",
    "billing invoices show",
    "billing invoices download",
    "billing payment-methods list",
    "firewall list",
    "firewall show",
    "firewall flow-summaries",
    "token list",
    "token view",
    "vm list",
    "vm show",
    "vm usage",
}
# No IDs are fabricated to pretend these success paths were exercised.
UNAVAILABLE = {
    "artifact get": "No existing artifacts returned for avrea-core.",
    "artifact download": "No existing artifacts returned for avrea-core.",
    "billing invoices show": "Staging credential lacks billing capability (403 on invoice list).",
    "billing invoices download": "Staging credential lacks billing capability (403 on invoice list).",
    "org install runner list": "Only a GitHub installation exists; runner management requires GitLab.",
    "repo public-mirror view": "No existing public-mirror requests returned.",
    "token view": "No existing scoped tokens returned.",
    "vm show": "No existing VMs returned in the staging Avrea organization.",
}


def validate_invocation(path, argv):
    if path not in READ_ONLY or argv[: len(path.split())] != path.split():
        raise ValueError("Not an allowlisted read-only invocation: " + path)
    forbidden = {"--web", "--browser", "--show-token", "--show-password", "--launch"}
    if any(arg.split("=", 1)[0] in forbidden for arg in argv):
        raise ValueError("Side-effecting or secret-revealing option in read-only matrix")
    if path == "org saml test" and "--no-browser" not in argv:
        raise ValueError("SAML test must not open a browser")


def inventory(root):
    return {root["path"].removeprefix("avr "): root} | {
        k: v for c in root.get("commands", []) for k, v in inventory(c).items()
    }


def cases(nodes, targets):
    REPO = targets["REPO"]
    RUN_OK = targets["RUN_OK"]
    RUN_FAIL = targets["RUN_FAIL"]
    RUN_NATIVE = targets["RUN_NATIVE"]
    JOB_OK = targets["JOB_OK"]
    JOB_FAIL = targets["JOB_FAIL"]
    JOB_NATIVE = targets["JOB_NATIVE"]
    WORKFLOW = targets["WORKFLOW"]
    COMMIT = targets["COMMIT"]
    result = []

    def add(path, args=(), label=None, modes=None, seconds=45):
        argv = path.split() + list(args)
        validate_invocation(path, argv)
        flags = {f["name"]: f for f in nodes[path]["flags"]}
        variants = modes or ["pipe", "tty"] + (["json"] if "json" in flags else [])
        for mode in variants:
            actual = list(argv)
            if mode == "json":
                actual += ["--json"] + ([] if flags["json"]["type"] == "bool" else ["*"])
            result.append(
                {"name": (label or path) + " / " + mode, "path": path, "args": actual, "mode": mode, "timeout": seconds}
            )

    for path, args in [
        ("auth status", []),
        ("org list", []),
        ("org members", []),
        ("org email-domain list", []),
        ("org install list", []),
        ("org saml show", []),
        ("org saml sp-metadata", []),
        ("org saml test", ["--no-browser"]),
        ("repo list", []),
        ("repo mirror status", []),
        ("repo mirror locations", []),
        ("repo public-mirror check", ["avrea-com/avrea-core"]),
        ("repo public-mirror requests", []),
        ("run list", ["--repo", REPO, "--limit", "5", "--created-before", "2026-10-31T18:20:00Z"]),
        ("job list", ["--repo", REPO, "--limit", "5", "--status", "failure"]),
        ("workflow list", ["--repo", REPO, "--limit", "5"]),
        ("workflow view", [WORKFLOW, "--repo", REPO]),
        ("pr list", ["--repo", REPO, "--limit", "5"]),
        ("log search", ["--repo", REPO, "--query", "error", "--limit", "5"]),
        ("artifact list", [REPO, "--limit", "5"]),
        ("sbom list", ["--repo", REPO, "--limit", "3"]),
        ("sbom view", [COMMIT, "--repo", REPO]),
        ("cache list", ["--repo", REPO, "--limit", "5"]),
        ("cache usage", ["--repo", REPO]),
        ("settings list", ["--repo", REPO]),
        ("settings schema", []),
        ("audit-events list", ["--limit", "5"]),
        ("health", []),
        ("status", ["--repo", REPO, "--since", "1d"]),
        ("config get", ["org"]),
        ("config list", []),
        ("billing summary", []),
        ("billing settings", []),
        ("billing invoices list", ["--limit", "2"]),
        ("billing payment-methods list", []),
        ("firewall list", ["--repo", REPO]),
        ("firewall show", ["--repo", REPO]),
        ("firewall flow-summaries", ["--repo", REPO, "--job", JOB_FAIL, "--limit", "5"]),
        ("token list", ["--limit", "5"]),
        ("vm list", ["--limit", "5"]),
        ("vm usage", ["--start", "2026-10-01", "--end", "2026-10-30"]),
    ]:
        add(path, args)
    add("pr list", ["--repo", REPO, "--scope", "authored", "--limit", "5"], "pr list authored")
    add("firewall flow-summaries", ["--job", JOB_FAIL], "firewall flow-summaries missing repo")
    for label, job in [("success", JOB_OK), ("failure", JOB_FAIL), ("native", JOB_NATIVE)]:
        add("job view", [job], "job view " + label)
        add("job logs", [job, "--no-pager"], "job logs " + label)
        add("job metrics", [job], "job metrics " + label)
        add("tests list", [job, "--repo", REPO, "--limit", "5"], "tests list " + label)
    for label, run in [("success", RUN_OK), ("failure", RUN_FAIL), ("native", RUN_NATIVE)]:
        add("run view", [run, "--steps"], "run view " + label)
        add("run logs", [run, "--no-pager", "--failed"], "run logs " + label)
        add("run watch", [run, "--interval", "1"], "run watch " + label, seconds=8)
    add("run diagnose", [RUN_FAIL], seconds=90)
    add("job view", [JOB_FAIL, "--log-failed", "--no-pager"], "job view failure with logs", ["pipe", "tty"])
    add("job logs", [JOB_OK, "--follow", "--no-pager"], "job logs follow completed", ["pipe", "tty"], 12)
    add("run logs", [RUN_OK, "--follow", "--no-pager"], "run logs follow completed", ["pipe", "tty"], 20)
    add("run logs", [RUN_FAIL, "--follow", "--failed", "--no-pager"], "run logs incompatible flags", ["pipe", "tty"])
    add("job watch", ["--repo", REPO, "--interval", "1"], seconds=3)
    add("sbom download", [COMMIT, "--repo", REPO, "--format", "inventory", "--out", "{download}"], modes=["pipe"])
    return result


def normalized(value, download):
    value = value.replace("\r\n", "\n").replace(download, "<download>")
    # Compare presentation, not the wall-clock second when a dashboard rendered.
    value = re.sub(r"\b\d{2}:\d{2}:\d{2} UTC\b", "<clock>", value)
    return re.sub(r"\b(?:\d+[smhd])+ ago[ ]*", "<age>", value)


def timestamp(value):
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise ValueError("timestamp lacks timezone")
    return parsed


def json_equal(a, b, floating=False):
    if isinstance(a, dict) and isinstance(b, dict):
        return a.keys() == b.keys() and all(json_equal(a[k], b[k], floating) for k in a)
    if isinstance(a, list) and isinstance(b, list):
        return len(a) == len(b) and all(json_equal(x, y, floating) for x, y in zip(a, b, strict=True))
    if isinstance(a, bool) or isinstance(b, bool):
        return type(a) is type(b) and a == b
    if floating and isinstance(a, float) and isinstance(b, float):
        return math.isclose(a, b, rel_tol=1e-12, abs_tol=1e-12)
    return a == b


def stable_urls(value):
    def replace(match):
        parts = urlsplit(match[0])
        return urlunsplit(
            parts._replace(
                query=urlencode(sorted(parse_qsl(parts.query, keep_blank_values=True), key=lambda item: item[0]))
            )
        )

    return re.sub(r"https://[^\s\x1b]+", replace, value)


def snapshots(value, events=False):
    records, trailer = [], []
    for line in value.splitlines():
        if not line.strip():
            continue
        if not line.startswith("{"):
            trailer.append(line)
            continue
        record = json.loads(line)
        if events:
            timestamp(record.pop("timestamp"))
        if events or not records or record != records[-1]:
            records.append(record)
    return {"records": records, "trailer": trailer}


def frames(value):
    # Refresh cadence is not a contract. Preserve every distinct rendered frame,
    # its semantic styles and its final interruption message.
    unique = []
    for frame in re.split(r"\x1b\[2J", value):
        item = (terminal_text(frame).strip(), presentation(frame))
        if item[0] and (not unique or item != unique[-1]):
            unique.append(item)
    return unique


def compare(case, a, b):
    diffs = []
    if a["exit"] != b["exit"]:
        diffs.append("exit")
    if a["bounded"] != b["bounded"]:
        diffs.append("termination")
    for stream in ("stdout", "stderr"):
        x, y = a[stream], b[stream]
        x, y = normalized(x, "<download>"), normalized(y, "<download>")
        if a["exit"] != 0:
            x, y = stable_urls(x), stable_urls(y)
        if case["path"] == "org saml sp-metadata":
            pattern = r'validUntil="([^"]+)"'
            left, right = re.search(pattern, x), re.search(pattern, y)
            if left and right and abs((timestamp(left[1]) - timestamp(right[1])).total_seconds()) <= 60:
                x, y = re.sub(pattern, 'validUntil="<expiry>"', x), re.sub(pattern, 'validUntil="<expiry>"', y)
        if stream == "stdout" and case["mode"] == "json" and a["exit"] == b["exit"] == 0:
            try:
                left, right = json.loads(x), json.loads(y)
                diagnose = case["path"] == "run diagnose"
                if diagnose:
                    if (
                        abs(
                            (timestamp(left.pop("generated_at")) - timestamp(right.pop("generated_at"))).total_seconds()
                        )
                        > 120
                    ):
                        diffs.append("diagnostic generation timestamp")
                if not json_equal(left, right, floating=diagnose):
                    diffs.append("JSON values")
            except ValueError, KeyError, TypeError:
                diffs.append("invalid JSON")
        elif stream == "stdout" and case["mode"] == "pipe" and case["path"] in ("run watch", "job watch"):
            try:
                if not json_equal(snapshots(x, case["path"] == "run watch"), snapshots(y, case["path"] == "run watch")):
                    diffs.append("watch snapshots/events")
            except ValueError, KeyError, TypeError:
                diffs.append("invalid watch NDJSON")
        elif stream == "stdout" and case["mode"] == "tty" and case["path"] in ("run watch", "job watch"):
            if frames(x) != frames(y):
                diffs.append("watch frames/styles")
        else:
            # Python emits an extra empty OSC-marker line in piped follow output.
            # One extra blank row before the completion footer is nonmaterial.
            if case["path"] in ("job logs", "run logs") and "--follow" in case["args"] and case["mode"] == "pipe":
                x, y = re.sub(r"\n{2,}(?=Job completed)", "\n\n", x), re.sub(r"\n{2,}(?=Job completed)", "\n\n", y)
            if terminal_text(x).strip() != terminal_text(y).strip():
                diffs.append(stream + " text/layout")
            if presentation(x) != presentation(y):
                diffs.append(stream + " glyphs/styles")
    if a.get("download") != b.get("download"):
        diffs.append("download contents")
    return diffs


def capture(prefix, case, env, directory, token):
    validate_invocation(case["path"], case["args"])
    target = str(directory / "download.json")
    args = [arg.replace("{download}", target) for arg in case["args"]]
    tty = case["mode"] == "tty"
    child_env = env | {"COLUMNS": "120", "LINES": "40", "TERM": "xterm-256color" if tty else "dumb"}
    child_env.pop("NO_COLOR", None)
    if not tty:
        child_env["NO_COLOR"] = "1"
    master, slave, chunks = None, None, []
    if tty:
        import fcntl  # noqa: PLC0415 -- POSIX-only live capture; catalog imports on Windows.
        import pty  # noqa: PLC0415
        import termios  # noqa: PLC0415

        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
    proc = subprocess.Popen(
        prefix + args,
        cwd=directory,
        env=child_env,
        stdin=subprocess.DEVNULL,
        stdout=slave if tty else subprocess.PIPE,
        stderr=slave if tty else subprocess.PIPE,
        start_new_session=True,
    )
    if tty:
        os.close(slave)

        def drain():
            while True:
                try:
                    data = os.read(master, 65536)
                except OSError:
                    return
                if not data:
                    return
                chunks.append(data)

        reader = threading.Thread(target=drain)
        reader.start()
    bounded = False
    try:
        stdout, stderr = proc.communicate(timeout=case["timeout"])
    except subprocess.TimeoutExpired:
        bounded = True
        os.killpg(proc.pid, signal.SIGINT)
        try:
            stdout, stderr = proc.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(proc.pid, signal.SIGKILL)
            stdout, stderr = proc.communicate()
    if tty:
        reader.join(timeout=5)
        os.close(master)
        stdout, stderr = b"".join(chunks), b""
    result = {"exit": proc.returncode, "bounded": bounded}
    for label, data in (("stdout", stdout), ("stderr", stderr)):
        result[label] = normalized(
            (data or b"").decode("utf-8", errors="replace").replace(token, "<credential>"), target
        )
    path = Path(target)
    if path.exists():
        data = path.read_bytes()
        result["download"] = {"bytes": len(data), "sha256": hashlib.sha256(data).hexdigest()}
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", type=Path, default=Path("build/go/avr"))
    parser.add_argument("--targets", type=Path, required=True, help="Private staging IDs JSON; never committed.")
    parser.add_argument("--only", help="Case-name substring to repeat")
    parser.add_argument("--report", type=Path, default=Path("build/go/staging-readonly/live.json"))
    options = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    nodes = inventory(json.loads((root / "build/go/go-command-inventory.json").read_text()))
    targets = json.loads(options.targets.read_text())
    HOST, ORG, REPO = (targets[k] for k in ("HOST", "ORG", "REPO"))
    host = urlsplit(HOST)
    if host.scheme != "https" or "staging" not in (host.hostname or "").split(".") or host.username or host.path:
        parser.error("Live comparisons require an explicit HTTPS staging host")
    selected = [c for c in cases(nodes, targets) if not options.only or options.only in c["name"]]
    if not selected:
        parser.error("No read-only cases matched --only")
    source = Path(os.environ.get("AVR_CONFIG_DIR", str(Path.home() / ".config/avrea"))) / "hosts.json"
    credential = json.loads(source.read_text())["hosts"][HOST]["token"]
    if not credential:
        raise SystemExit("No stored staging credential")
    env = {k: v for k, v in os.environ.items() if not k.startswith(("AVR_", "_AVR_", "COMP_"))}
    env.update(
        AVR_HOST=HOST,
        AVR_TOKEN=credential,
        AVR_ORG=ORG,
        AVR_REPO=REPO,
        AVR_DEBUG="",
        AVR_CPU_PROFILE="",
        AVR_NO_UPDATE_NOTIFIER="1",
        AVR_PAGER="",
        PAGER="cat",
        PYTHONPATH=str(root),
        GODEBUG="",
    )
    prefixes = {"python": [sys.executable, "-m", "avrea_cli.main"], "go": [str(options.go.resolve())]}
    options.report.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    options.report.parent.chmod(0o700)
    report = {
        "host": HOST,
        "org": ORG,
        "repo": REPO,
        "go_sha256": hashlib.sha256(options.go.read_bytes()).hexdigest(),
        "python_source": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip(),
        "unavailable": UNAVAILABLE,
        "cases": [],
        "coverage": {},
    }
    for index, case in enumerate(selected):
        captured = {}
        for language in ["python", "go"] if index % 2 == 0 else ["go", "python"]:
            with tempfile.TemporaryDirectory(prefix="avr-staging-readonly-") as tmp:
                directory = Path(tmp)
                captured[language] = capture(
                    prefixes[language], case, env | {"AVR_CONFIG_DIR": str(directory / "config")}, directory, credential
                )
        differences = compare(case, captured["python"], captured["go"])
        record = case | captured | {"differences": differences}
        report["cases"].append(record)
        print(("DIFF" if differences else "MATCH"), case["name"], ", ".join(differences), flush=True)
        fd = os.open(options.report, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "w") as output:
            json.dump(report, output, indent=2)
            output.write("\n")
    exercised = {c["path"] for c in selected}
    for name, node in nodes.items():
        report["coverage"][name] = (
            "live read-only"
            if name in exercised
            else UNAVAILABLE.get(name)
            or ("group/help" if node.get("commands") else "local fixtures only: write, session or local-output command")
        )
    options.report.write_text(json.dumps(report, indent=2) + "\n")
    print(
        "Report:",
        options.report,
        "cases:",
        len(selected),
        "differences:",
        sum(bool(c["differences"]) for c in report["cases"]),
        flush=True,
    )
    return 1 if any(c["differences"] for c in report["cases"]) else 0


if __name__ == "__main__":
    raise SystemExit(main())
