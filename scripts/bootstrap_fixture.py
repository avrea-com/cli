"""Local-only bootstrap helpers. No generated remote script is executed."""

from pathlib import Path
import json
import sys

CREDS = {"ANTHROPIC_API_KEY": None, "CLAUDE_CODE_OAUTH_TOKEN": None, "OPENAI_API_KEY": None}
RECORDS = ".bootstrap-records.jsonl"


def make_stubs(home, options):
    directory = home / "bootstrap-bin"
    directory.mkdir()
    (home / ".bootstrap-options.json").write_text(json.dumps(options))
    script = """import json, os, sys
from pathlib import Path
home = Path(os.environ["HOME"])
options = json.loads((home / ".bootstrap-options.json").read_text())
records = home / ".bootstrap-records.jsonl"
previous = [json.loads(line) for line in records.read_text().splitlines()] if records.exists() else []
tool = Path(sys.argv[0]).name
args = sys.argv[1:]
entry = {"tool": tool, "args": args}
if tool == "ssh":
    entry["stdin"] = sys.stdin.read()
    entry["pin"] = None
    for i, arg in enumerate(args):
        if arg.startswith("UserKnownHostsFile="):
            path = Path(arg.split("=", 1)[1])
            entry["pin"] = {"path": str(path), "text": path.read_text(), "mode": path.stat().st_mode & 0o777}
            args[i] = "UserKnownHostsFile=<known_hosts>"
with records.open("a") as stream:
    stream.write(json.dumps(entry) + "\\n")
if tool == "gh":
    print(options.get("token", "fixture-github-secret"))
    print("fixture-helper-diagnostic", file=sys.stderr)
    sys.exit(options.get("gh_exit", 0))
if tool == "claude":
    print("CLAUDE SETUP STUB", flush=True)
    sys.exit(options.get("claude_exit", 0))
probe = args[-3:-1] != ["bash", "-lc"]
index = sum(r["tool"] == "ssh" and (r["args"][-3:-1] != ["bash", "-lc"]) == probe for r in previous)
print("fixture probe noise" if probe else "fixture step output")
print("fixture probe error" if probe else "fixture step stderr", file=sys.stderr)
if probe:
    sys.exit(255 if index < options.get("probe_failures", 0) else 0)
if index == options.get("fail_step"):
    sys.exit(options.get("step_exit", 42))
"""
    for tool in ("ssh", "gh", "claude"):
        if tool in options.get("missing", []):
            continue
        target = directory / tool
        target.write_text("#!" + sys.executable + "\n" + script)
        target.chmod(0o700)
    # Absolute Python shebangs keep the stubs working while preventing fallback
    # to any real SSH/authentication/vendor tools on the developer's PATH.
    return str(directory)


def read_records(home):
    target = home / RECORDS
    rows = [json.loads(line) for line in target.read_text().splitlines()] if target.exists() else []
    for row in rows:
        if pin := row.get("pin"):
            pin["removed"] = not Path(pin.pop("path")).exists()
    return rows
