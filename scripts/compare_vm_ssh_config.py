"""SSH config parity, including persistent pins and managed-file preservation."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from scripts import compare_vm_inspection as vm
import sys

KEY = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFixtureHost fixture"
EP = vm.ENDPOINTS["ssh"] | {"host_key": KEY}
ALIAS = "avr-" + vm.VM
BEGIN = "# >>> avrea vm " + ALIAS + " >>>"
END = "# <<< avrea vm " + ALIAS + " <<<"


def scenarios():
    cases = []

    def case(name, flags=None, endpoint=None, **kwargs):
        row = vm.ROW | {"state": "RUNNING", "endpoints": {"ssh": EP if endpoint is None else endpoint}}
        cases.append(
            {
                "name": "vm ssh-config " + name,
                "args": ["vm", "ssh-config", vm.VM, *(flags or [])],
                "env": vm.ENV.copy(),
                "responses": {vm.SHOW: (200, {"data": row})},
                "capture_files": [".ssh", "custom"],
                "normalize_file_home": True,
                "normalize_file_newlines": True,
                **kwargs,
            }
        )

    case("print", presentation=True)
    case("append", ["--append"], presentation=True)
    case("no key", endpoint=vm.ENDPOINTS["ssh"], presentation=True)
    case("no key append", ["--append"], endpoint=vm.ENDPOINTS["ssh"], presentation=True)
    case("null key", endpoint=EP | {"host_key": None}, presentation=True)
    case("empty key", endpoint=EP | {"host_key": ""}, presentation=True)
    case("port 22", endpoint=EP | {"external_port": 22}, presentation=True)
    case("IPv6", endpoint=EP | {"external_ip": "2001:db8::1"}, presentation=True)
    case("default user", endpoint=EP | {"username": ""}, presentation=True)
    case("identity", ["-i", "FIXTURE_HOME/.ssh/id_ed25519"], presentation=True)
    case("identity spaced", ["--identity", "path with spaces/key"], presentation=True)
    case("alias", ["--host-alias", "dev_box.example-1"], presentation=True)
    case("empty alias default", ["--host-alias", ""], presentation=True)
    case("custom pins", ["--known-hosts-file", "custom/nested/known_hosts"], presentation=True)
    case("custom pins home", ["--known-hosts-file", "~/custom/known_hosts"], presentation=True)
    case(
        "custom spaced paths",
        ["--append", "--config-file", "custom/ssh config", "--known-hosts-file", "custom/known hosts"],
        presentation=True,
    )
    case("custom config", ["--append", "--config-file", "custom/nested/config"], presentation=True)
    case("empty config", ["--config-file", ""], presentation=True)
    case("explicit org", ["--org", vm.ORG], env=vm.ENV | {"AVR_ORG": "org-other"}, presentation=True)
    case(
        "org slug",
        ["--org", "AcMe"],
        responses={
            vm.SHOW: (200, {"data": vm.ROW | {"endpoints": {"ssh": EP}}}),
            ("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS}),
        },
        presentation=True,
    )
    case("no auth", env={}, responses={}, exit=4)
    for status in [401, 403, 404, 422, 429, 503]:
        case(
            "HTTP " + str(status),
            responses={vm.SHOW: (status, {"detail": "fixture denied"})},
            exit=4 if status == 401 else 1,
        )
    for row in [vm.ROW, vm.ROW | {"endpoints": {}}, {"endpoints": {"ssh": {}}}]:
        case("pending " + repr(row.get("endpoints")), responses={vm.SHOW: (200, {"data": row})}, exit=1)
    for name, flags, terms in [
        ("missing append", ["--config-file", "custom/config"], ["--config-file requires --append"]),
        ("invalid alias", ["--host-alias", "bad alias"], ["--host-alias", "not valid"]),
        ("alias injection", ["--host-alias", "safe\nProxyCommand bad"], ["--host-alias", "not valid"]),
        ("identity newline", ["-i", "key\nProxyCommand bad"], ["IdentityFile contains an embedded newline"]),
        ("identity quote", ["-i", 'key"name'], ["IdentityFile contains a double quote"]),
    ]:
        before = name in ["missing append", "invalid alias", "alias injection"]
        case(
            name, flags, **({"env": {}, "responses": {}} if before else {}), exit=2 if before else 1, error_terms=terms
        )
    case(
        "multiline host key",
        endpoint=EP | {"host_key": KEY + "\nother key"},
        exit=1,
        error_terms=["malformed SSH host key"],
    )
    case(
        "host newline",
        endpoint=EP | {"external_ip": "192.0.2.1\nProxyCommand bad"},
        exit=1,
        error_terms=["IP contains an embedded newline"],
        expected_go_files={},
    )
    case(
        "user newline",
        endpoint=EP | {"username": "runner\nProxyCommand bad"},
        exit=1,
        error_terms=["username contains an embedded newline"],
        expected_go_files={},
    )
    for name, text in [
        ("unrelated config", "# Keep this\nHost another\n    HostName 198.51.100.2\n"),
        ("unrelated no newline", "Host another\n    HostName 198.51.100.2"),
        ("trailing blanks", "Host another\n    HostName 198.51.100.2\n\n\n"),
        ("replace block", "# before\n" + BEGIN + "\nHost old\n    Port 1\n" + END + "\n# after\n"),
        ("duplicate blocks", BEGIN + "\nHost old\n" + END + "\n# middle\n" + BEGIN + "\nHost older\n" + END + "\n"),
        ("catchall strict", "Host *\n    StrictHostKeyChecking=no\n"),
        ("catchall both", "Host * other\n UserKnownHostsFile /dev/null\n StrictHostKeyChecking no\n"),
        ("noncatchall", "Host other\n StrictHostKeyChecking no\n"),
        ("match resets", "Host *\nMatch user another\n StrictHostKeyChecking no\n"),
    ]:
        case(name, ["--append"], fixture_files={".ssh/config": text.encode()}, presentation=True)
    for name, text in [
        ("other pins", "other ssh-ed25519 OTHER\n"),
        ("replace pin", "[192.0.2.1]:30022 ssh-ed25519 OLD\nother ssh-ed25519 OTHER\n"),
        ("duplicate pins", "[192.0.2.1]:30022 ssh-ed25519 OLD\n[192.0.2.1]:30022 ssh-rsa OLDER\n"),
        ("unterminated last pin", "other ssh-ed25519 OTHER"),
    ]:
        case(name, fixture_files={".ssh/avr_known_hosts": text.encode()}, presentation=True)
    case(
        "unterminated block",
        ["--append"],
        fixture_files={".ssh/config": (BEGIN + "\nHost old\n# keep me\n").encode()},
        exit=1,
        error_terms=["unterminated", "missing its", "fix it by hand"],
    )
    case("missing ID", args=["vm", "ssh-config"], responses={}, exit=2, error_terms=["VM_ID"])
    case("no JSON", ["--json"], responses={}, exit=2, error_terms=["--json"])
    if sys.platform != "win32":
        for name in ["print", "append", "no key", "replace block", "catchall both"]:
            item = deepcopy(next(c for c in cases if c["name"] == "vm ssh-config " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


def compare(scenario, python, go):
    differences = watch.compare(scenario, python, go)
    if "expected_go_files" in scenario:
        if go["files"] != scenario["expected_go_files"]:
            differences.append("rejected endpoint wrote SSH files")
    elif python["files"] != go["files"]:
        differences.append("SSH configuration file effects")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=compare, default_report="build/go/vm-ssh-config-parity.json"
        )
    )
