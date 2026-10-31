"""GitLab installation and runner parity using synthetic credentials and loopback APIs."""

from copy import deepcopy
from pathlib import Path
from scripts import compare_auth as harness
from scripts import compare_installations as installations
from scripts import compare_job_watch as watch
import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile

ORG, INSTALL, ENV = installations.ORG, installations.INSTALL, installations.ENV
BASE = f"/orgs/{ORG}/gitlab-installations"
PATH = BASE + "/" + INSTALL
RUNNERS = PATH + "/runners"
VENDOR = ("POST", "/gitlab-fixture/api/v4/user/runners")
SECRET = "webhook.synthetic.once"
TOKEN = "gitlab.synthetic.access"
RUNNER_TOKEN = "glrt-synthetic-fixture"
DETAIL = {
    "installation_id": INSTALL,
    "organization_id": ORG,
    "gitlab_namespace_id": 42,
    "gitlab_namespace_full_path": "acme/platform",
    "target_type": "Group",
    "state": "active",
    "credential_kind": "group_access_token",
    "access_token_expires_on": "2027-01-01",
    "access_token_last_validated_at": None,
    "access_token_invalid_at": None,
    "webhook_secret_rotated_at": "2026-10-30T12:00:00Z",
    "configured_runner_count": 1,
    "degraded_runner_count": 0,
    "project_selection_mode": "SELECTED",
    "selected_project_count": 2,
    "gitlab_project_ids": [11, 22],
    "created_at": "2026-10-30T12:00:00Z",
    "updated_at": None,
}
SETUP = {
    "url": "https://api.example.test/gitlab/webhooks",
    "secret_header": "X-Gitlab-Token",
    "secret": SECRET,
    "enabled_triggers": ["pipeline_events", "job_events"],
    "ssl_verification": True,
    "unlisted_triggers_enabled": False,
    "warning": "Save this synthetic secret.",
}
WRITE = DETAIL | {"webhook_setup": SETUP}
MACHINE = "avrea-2cpu-amd"
RUNNER = {
    "machine_type": MACHINE,
    "tag": MACHINE,
    "architecture": "amd64",
    "cpu_count": 2,
    "memory_mb": 4096,
    "disk_gb": 20,
    "description": "Avrea fixture",
    "run_untagged": False,
    "paused": False,
    "configured": False,
    "status": "unconfigured",
    "runner_id": None,
    "token_expires_at": None,
    "verified_at": None,
    "invalid_at": None,
}
MACHINE2 = "avrea-4cpu-amd"
CONFIGURED = RUNNER | {"configured": True, "status": "ready", "runner_id": 101}
RUNNER_SETUP = {
    "group_id": 42,
    "group_full_path": "acme/platform",
    "protected_branches": "Choose whether to protect these runners.",
    "fallback_image": "Use an image for every job.",
    "job_example": "build:\n  tags: [avrea-2cpu-amd]",
    "documentation_url": "https://docs.example.test/gitlab",
}
COLLECTION = {"data": [RUNNER, RUNNER | {"machine_type": MACHINE2, "tag": MACHINE2}], "setup": RUNNER_SETUP}
RESPONSES = {
    ("POST", BASE): (200, WRITE),
    ("PATCH", PATH): (200, DETAIL),
    ("POST", PATH + "/credential-rotations"): (200, WRITE),
    ("GET", RUNNERS): (200, COLLECTION),
    ("PUT", RUNNERS + "/" + MACHINE): (200, CONFIGURED),
    ("DELETE", RUNNERS + "/" + MACHINE): (204, None),
    ("PUT", RUNNERS + "/" + MACHINE2): (200, CONFIGURED | {"machine_type": MACHINE2, "runner_id": 102}),
    VENDOR: (201, {"id": 101, "token": RUNNER_TOKEN}),
    ("GET", f"/orgs/{ORG}/installations/{INSTALL}"): (200, {"data": DETAIL | {"platform": "gitlab"}}),
    ("DELETE", f"/orgs/{ORG}/installations/{INSTALL}"): (204, None),
}
ID = ["--installation-id", INSTALL]
ADD = [
    "add",
    "--platform",
    "gitlab",
    "--group-full-path",
    " acme/platform ",
    "--project-selection",
    "selected",
    "--project-id",
    "11",
    "--project-id",
    "22",
    "--access-token-stdin",
]
ROTATE = ["rotate", *ID, "--access-token-stdin"]
SELECT = ["set-selection", *ID, "--project-selection", "selected", "--project-id", "11", "--project-id", "22"]
CREATE = ["runner", "create", *ID, "--machine-type", MACHINE, "--personal-access-token-stdin"]


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "GitLab " + name,
                "args": ["org", "install", *args],
                "env": ENV.copy(),
                "responses": deepcopy(RESPONSES),
                "stdin": TOKEN + "\n",
                "native": True,
                **kwargs,
            }
        )

    for name, args in [
        ("add", ADD),
        ("rotate", ROTATE),
        ("selection", SELECT),
        ("runner list", ["runner", "list", *ID]),
        ("runner set", ["runner", "set", MACHINE, *ID, "--runner-token-stdin"]),
        ("runner delete", ["runner", "delete", MACHINE, *ID, "--yes"]),
        ("disconnect", ["remove", *ID, "--yes"]),
    ]:
        case(name, args, presentation=True)
        case(name + " JSON", args + ["--json", "*"], json=True)
        case(name + " explicit org", args + ["--org", ORG], env=ENV | {"AVR_ORG": "org-other"}, presentation=True)
        field, expression = {
            "add": ("webhook_setup", ".webhook_setup.secret"),
            "rotate": ("webhook_setup", ".webhook_setup.secret"),
            "selection": ("installation_id", ".installation_id"),
            "runner list": ("data", ".data[].machine_type"),
            "runner set": ("machine_type", ".machine_type"),
            "runner delete": ("machine_type", ".machine_type"),
            "disconnect": ("installation_id", ".installation_id"),
        }[name]
        for flag in ("--jq", "-q"):
            case(name + " filter " + flag, args + ["--json", field, flag, expression])
        case(name + " discovery", args + ["--json", "?"], env={}, responses={}, stdin="")
        case(name + " unknown field", args + ["--json", "typo"], exit=1, responses={})
        case(
            name + " jq requires json", args + ["--jq", "."], exit=2, responses={}, error_terms=["--jq requires --json"]
        )
        case(name + " no auth", args, env={}, responses={}, exit=4)
        for status in [401, 403, 404, 409, 422, 429, 503]:
            route = {
                "add": ("POST", BASE),
                "rotate": ("POST", PATH + "/credential-rotations"),
                "selection": ("PATCH", PATH),
                "runner list": ("GET", RUNNERS),
                "runner set": ("PUT", RUNNERS + "/" + MACHINE),
                "runner delete": ("DELETE", RUNNERS + "/" + MACHINE),
                "disconnect": ("DELETE", f"/orgs/{ORG}/installations/{INSTALL}"),
            }[name]
            case(
                name + " HTTP " + str(status),
                args,
                responses=RESPONSES | {route: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
    for name, args in [("add", ADD), ("rotate", ROTATE)]:
        case(name + " retained jq", args + ["--json", "webhook_setup", "--jq", ".webhook_setup.secret"])
        case(
            name + " omitted secret",
            args + ["--json", "installation_id"],
            exit=2,
            responses={},
            error_terms=["webhook_setup"],
        )
        case(name + " dropped secret", args + ["--json", "*", "--jq", ".installation_id"], exit=1, json=True)
        case(
            name + " jq error secret", args + ["--json", "*", "--jq", "error(.webhook_setup.secret)"], exit=1, json=True
        )
        case(name + " empty token", args, stdin="\n", exit=2, responses={}, error_terms=["1 to 4096"])
        case(name + " too long token", args, stdin="x" * 4097 + "\n", exit=2, responses={}, error_terms=["1 to 4096"])
        no_stdin = [a for a in args if a != "--access-token-stdin"]
        case(name + " prompts disabled", no_stdin, env=ENV | {"AVR_PROMPT_DISABLED": "1"}, exit=1, responses={})
        case(name + " hidden prompt", no_stdin, hidden_prompt=True)
    for flag, value in [("--project-id", "0"), ("--project-selection", "wrong"), ("--platform", "wrong")]:
        case("invalid " + flag, ADD + [flag, value], exit=2, responses={}, error_terms=[flag])
    case(
        "selection all with IDs",
        SELECT + ["--project-selection", "all"],
        exit=2,
        responses={},
        error_terms=["--project-id"],
    )
    case("selection empty", ["set-selection", *ID, "--project-selection", "selected"], json=False, presentation=True)
    case("selection 204", SELECT, responses=RESPONSES | {("PATCH", PATH): (204, None)}, exit=1)
    case(
        "add GitHub options",
        ADD + ["--no-browser", "--wait-seconds", "120"],
        exit=2,
        responses={},
        error_terms=["--no-browser", "--wait-seconds"],
    )
    case(
        "GitHub rejects GitLab flags",
        ["add", "--group-full-path", "acme"],
        exit=2,
        responses={},
        error_terms=["--platform gitlab"],
    )
    case(
        "GitHub JSON no wait",
        ["add", "--wait-seconds", "0", "--json", "*"],
        exit=2,
        responses={},
        error_terms=["positive --wait-seconds"],
    )
    case("disconnect confirm", ["remove", *ID], stdin="yes\n", presentation=True)
    case("disconnect declined", ["remove", *ID], stdin="no\n", exit=1, presentation=True)
    case("disconnect disabled", ["remove", *ID], env=ENV | {"AVR_PROMPT_DISABLED": "1"}, exit=1)
    case("runner delete confirm", ["runner", "delete", MACHINE, *ID], stdin="yes\n", presentation=True)
    case("runner delete disabled", ["runner", "delete", MACHINE, *ID], env=ENV | {"AVR_PROMPT_DISABLED": "1"}, exit=1)
    case(
        "runner create unknown",
        CREATE + ["--machine-type", "unknown"],
        exit=2,
        error_terms=["Unknown runner machine type"],
    )
    case(
        "runner create configured",
        CREATE,
        responses=RESPONSES | {("GET", RUNNERS): (200, {"data": [CONFIGURED], "setup": RUNNER_SETUP})},
        exit=2,
        error_terms=["already configured"],
    )
    case(
        "runner create disabled",
        [a for a in CREATE if a != "--personal-access-token-stdin"],
        env=ENV | {"AVR_PROMPT_DISABLED": "1"},
        exit=1,
    )
    case("runner create", CREATE, native=False, presentation=True)
    case(
        "runner create explicit org",
        CREATE + ["--org", ORG],
        env=ENV | {"AVR_ORG": "org-other"},
        native=False,
        presentation=True,
    )
    case("runner create JSON", CREATE + ["--json", "*"], native=False, json=True)
    for flag in ("--jq", "-q"):
        case("runner create filter " + flag, CREATE + ["--json", "data", flag, ".data[].machine_type"], native=False)
    case(
        "runner create protected duplicate",
        CREATE + ["--protected", "--machine-type", MACHINE],
        native=False,
        presentation=True,
    )
    for status in [302, 400, 403, 429, 500]:
        case(
            "vendor HTTP " + str(status),
            CREATE,
            native=False,
            responses=RESPONSES | {VENDOR: (status, {"detail": "private never echo"})},
            exit=1,
        )
    for value in [
        {},
        {"id": True, "token": "x"},
        {"id": 0, "token": "x"},
        {"id": 1, "token": ""},
        {"id": "1", "token": "x"},
    ]:
        case(
            "vendor invalid " + repr(value), CREATE, native=False, responses=RESPONSES | {VENDOR: (201, value)}, exit=1
        )
    for status in [401, 403, 422, 503]:
        case(
            "runner created Avrea HTTP " + str(status),
            CREATE,
            native=False,
            responses=RESPONSES | {("PUT", RUNNERS + "/" + MACHINE): (status, {"detail": "fixture denied"})},
            exit=1,
        )
    case(
        "runner partial failure",
        CREATE + ["--machine-type", MACHINE2, "--json", "*"],
        native=False,
        responses=RESPONSES | {("PUT", RUNNERS + "/" + MACHINE2): (503, {})},
        exit=1,
    )
    row = installations.ROW | {"platform": "gitlab", "platform_installation_id": None}
    for mode, flags in [
        ("human", []),
        ("JSON", ["--json", "*"]),
        ("no counts", ["--json", "installation_id,platform"]),
    ]:
        for status in [200, 401, 403, 404, 500]:
            case(
                "counts " + mode + " " + str(status),
                ["list", *flags],
                responses={
                    installations.LIST: (200, {"data": [installations.ROW, row]}),
                    ("GET", BASE): (status, {"data": [DETAIL], "detail": "fixture denied"}),
                },
                exit=4 if status == 401 and mode != "no counts" else 0,
                json=bool(flags),
                presentation=not flags,
            )
    if sys.platform != "win32":
        for name in ["add", "selection", "runner list", "runner set", "disconnect confirm", "runner create"]:
            item = deepcopy(next(c for c in cases if c["name"] == "GitLab " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


def compare(scenario, python, go):
    python, go = deepcopy(python), deepcopy(go)
    if scenario.get("json"):
        for result in (python, go):
            if result["stdout"].strip():
                try:
                    result["stdout"] = json.dumps(json.loads(result["stdout"]), sort_keys=True)
                except ValueError:
                    # Error scenarios can emit non-JSON output; retain it for comparison.
                    continue
    if scenario.get("hidden_prompt"):
        python["stderr"] = re.sub(
            r"[^\n]*(?:getpass|termui)\.py:\d+: GetPassWarning: Can not control echo on the terminal.\n[^\n]*\n",
            "",
            python["stderr"],
        )
    differences = watch.compare(scenario, python, go)
    for label, result in [("Python", python), ("Go", go)]:
        if any(s in result["stdout"] + result["stderr"] for s in [TOKEN, RUNNER_TOKEN]):
            differences.append(label + " leaked access/runner token")
        if SECRET in result["stderr"]:
            differences.append(label + " leaked webhook secret on stderr")
        for req in result["requests"]:
            if req["path"].startswith("/gitlab-fixture") and req["authorization"]:
                differences.append(label + " API credential sent to GitLab")
    return differences


def prepare_oracle(root):
    (root / "sitecustomize.py").write_text(
        "import os\nfrom urllib.parse import urlsplit\nimport httpx\n"
        "_request=httpx.Client.request\n"
        "def fixture_request(self,method,url,**kwargs):\n"
        "    if str(url).startswith('https://gitlab.com/'):\n"
        "        host=os.environ['AVR_HOST']\n"
        "        assert urlsplit(host).hostname=='127.0.0.1'\n"
        "        url=host+'/gitlab-fixture'+urlsplit(str(url)).path\n"
        "    return _request(self,method,url,**kwargs)\n"
        "httpx.Client.request=fixture_request\n"
    )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", required=True)
    parser.add_argument("--go-tool", default="go")
    parser.add_argument("--admin-module", type=Path)
    parser.add_argument("--report", default="build/go/gitlab-parity.json")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    original = sys.argv
    native_report = str(Path(args.report).with_name(Path(args.report).stem + "-native.json"))
    try:
        sys.argv = [original[0], "--go", args.go, "--report", native_report]
        native = harness.main(
            case_factory=lambda: [case for case in scenarios() if case.get("native")], compare_results=compare
        )
        module = args.admin_module.resolve() if args.admin_module else root
        template = (root / "scripts/fixtures/gitlab_runner_main.go.tmpl").read_text()
        template = template.replace(
            "ADMIN_IMPORT", 'admin "github.com/avrea-com/avrea-core/cli-go/commands"' if args.admin_module else ""
        )
        template = template.replace("EXECUTE", "admin.Execute" if args.admin_module else "public.Execute")
        with tempfile.TemporaryDirectory(prefix="avr-gitlab-fixture-") as tmp:
            source = Path(tmp) / "main.go"
            source.write_text(template)
            binary = Path(tmp) / ("gitlab-fixture.exe" if os.name == "nt" else "gitlab-fixture")
            env = os.environ.copy()
            if args.admin_module:
                env["GOWORK"] = str(module / "go.work")
            subprocess.run([args.go_tool, "build", "-o", str(binary), str(source)], cwd=module, env=env, check=True)
            sys.argv = [original[0], "--go", str(binary), "--report", args.report]
            fixture = harness.main(case_factory=scenarios, compare_results=compare, prepare_oracle=prepare_oracle)
        path = Path(args.report)
        report = json.loads(path.read_text())
        report["validation_mode"] = "command-root with injected loopback GitLab transport"
        report["native_binary_sha256"] = hashlib.sha256(Path(args.go).read_bytes()).hexdigest()
        report["native_report"] = native_report
        path.write_text(json.dumps(report, indent=2) + "\n")
        return max(native, fixture)
    finally:
        sys.argv = original


if __name__ == "__main__":
    sys.exit(main())
