"""VM update parity: request bodies, file inputs, validation and password output."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from scripts import compare_vm_inspection as vm
from scripts import compare_vm_lifecycle as lifecycle
import json
import sys

PATCH = ("PATCH", vm.BASE + "/" + vm.VM)
RESPONSE = {"data": {"vm": vm.ROW | {"display_name": "renamed"}, "password": None}}
KEY = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFixture fixture@example.test"
PRIVATE = "-----BEGIN OPENSSH PRIVATE KEY-----\nsynthetic-not-a-real-key\n-----END OPENSSH PRIVATE KEY-----\n"
RULES = [
    {
        "action": "allow",
        "fqdn": "api.example.test",
        "protocol": "tcp",
        "port_start": 443,
        "port_end": 443,
        "unknown": {"number": 9007199254740993},
    }
]


def scenarios():
    cases = []

    def case(name, flags=None, **kwargs):
        cases.append(
            {
                "name": "vm update " + name,
                "args": ["vm", "update", vm.VM, *(["--name", "renamed"] if flags is None else flags)],
                "env": vm.ENV.copy(),
                "responses": {PATCH: (200, RESPONSE)},
                "capture_files": [],
                **kwargs,
            }
        )

    case("human", presentation=True)
    case("JSON", ["--name", "renamed", "--json"], json=True)
    case("no auth", env={}, responses={}, exit=4)
    case(
        "explicit org", ["--name", "renamed", "--org", vm.ORG], env=vm.ENV | {"AVR_ORG": "org-other"}, presentation=True
    )
    case(
        "org slug",
        ["--name", "renamed", "--org", "AcMe"],
        responses={
            PATCH: (200, RESPONSE),
            ("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS}),
        },
        presentation=True,
    )
    for status in [401, 403, 404, 409, 422, 429, 503]:
        case(
            "HTTP " + str(status),
            responses={PATCH: (status, {"detail": "fixture denied"})},
            exit=4 if status == 401 else 1,
        )
    for name, flags in [
        ("empty name", ["--name", ""]),
        ("repeated name", ["--name", "old", "--name", "new"]),
        ("rotate", ["--rotate-password"]),
        ("key literal", ["--ssh-key", KEY]),
        ("two keys", ["--ssh-key", KEY, "--ssh-key", "ssh-rsa AAAA second"]),
        ("duplicate keys retained", ["--ssh-key", KEY, "--ssh-key", KEY]),
        ("key whitespace", ["--ssh-key", "  " + KEY + "\n"]),
        ("FIDO key", ["--ssh-key", "sk-ssh-ed25519@openssh.com AAAA fixture"]),
        ("ECDSA key", ["--ssh-key", "ecdsa-sha2-nistp256 AAAA fixture"]),
        ("egress", ["--egress-rules", json.dumps(RULES)]),
        ("clear egress", ["--egress-rules", "[]"]),
        ("server validates rule shapes", ["--egress-rules", '[null, 1, "wrong"]']),
        (
            "all flags",
            [
                "--name",
                "renamed",
                "--ttl",
                "8h",
                "--ssh-key",
                KEY,
                "--rotate-password",
                "--egress-rules",
                json.dumps(RULES),
            ],
        ),
    ]:
        case(name, flags, presentation=True)
    case(
        "rotate password display",
        ["--rotate-password"],
        responses={PATCH: (200, {"data": {"vm": lifecycle.RUNNING, "password": lifecycle.PASSWORD}})},
        presentation=True,
    )
    case(
        "rotate password JSON",
        ["--rotate-password", "--json"],
        responses={PATCH: (200, {"data": {"vm": lifecycle.RUNNING, "password": lifecycle.PASSWORD}})},
        json=True,
    )
    case(
        "password only",
        ["--rotate-password"],
        responses={PATCH: (200, {"data": {"password": lifecycle.PASSWORD}})},
        presentation=True,
    )
    for ttl in ["300", "300s", "5m", "8h", "7d", "604800", " 8H ", "3_00", "+300", " 5 m"]:
        case("TTL " + repr(ttl), ["--ttl", ttl], presentation=True)
    for ttl in ["", "bad", "1h30m", "1.5h", "299", "0", "-1", "604801", "8d", "999999999999999999999999999h"]:
        case("invalid TTL " + repr(ttl), ["--ttl", ttl], responses={}, env={}, exit=2, error_terms=["--ttl"])
    for key, content in [("@key.pub", KEY + "\n"), ("@~/key.pub", KEY), ("@nested/key.pub", KEY)]:
        path = "nested/key.pub" if key.startswith("@nested") else "key.pub"
        case("key file " + key, ["--ssh-key", key], fixture_files={path: content.encode()}, presentation=True)
    case(
        "egress file",
        ["--egress-rules", "@rules.json"],
        fixture_files={"rules.json": json.dumps(RULES).encode()},
        presentation=True,
    )
    case(
        "egress home file", ["--egress-rules", "@~/rules.json"], fixture_files={"rules.json": b"[]"}, presentation=True
    )
    for name, flags, files, terms in [
        ("private literal", ["--ssh-key", PRIVATE], {}, ["private key", ".pub"]),
        ("private file", ["--ssh-key", "@private"], {"private": PRIVATE.encode()}, ["private key", ".pub"]),
        ("empty key file", ["--ssh-key", "@empty.pub"], {"empty.pub": b"\n"}, ["--ssh-key", "is empty"]),
        ("missing key file", ["--ssh-key", "@missing.pub"], {}, ["--ssh-key", "cannot read SSH key file"]),
        ("unknown key", ["--ssh-key", "not a key"], {}, ["recognized SSH public key"]),
        ("empty key", ["--ssh-key", ""], {}, ["recognized SSH public key"]),
        ("missing egress file", ["--egress-rules", "@missing.json"], {}, ["--egress-rules", "cannot read"]),
        ("invalid egress file", ["--egress-rules", "@bad.json"], {"bad.json": b"not JSON"}, ["must be valid JSON"]),
        ("invalid JSON", ["--egress-rules", "bad"], {}, ["must be valid JSON"]),
        ("trailing JSON", ["--egress-rules", "[] {}"], {}, ["must be valid JSON"]),
        ("egress object", ["--egress-rules", "{}"], {}, ["JSON array"]),
        ("egress null", ["--egress-rules", "null"], {}, ["JSON array"]),
    ]:
        case(name, flags, fixture_files=files, responses={}, env={}, exit=2, error_terms=terms)
    case("no changes", [], responses={}, env={}, exit=2, error_terms=["Nothing to update"])
    case("missing ID", [], responses={}, exit=2, error_terms=["VM_ID"])
    cases[-1]["args"] = ["vm", "update"]
    for value in [{}, {"data": {}}, {"data": None}, None]:
        case("empty response " + repr(value), responses={PATCH: (200, value)}, presentation=True)
    case("empty response JSON", ["--name", "renamed", "--json"], responses={PATCH: (200, None)}, json=True)
    case("no jq", ["--name", "renamed", "--jq", "."], responses={}, exit=2, error_terms=["--jq"])
    if sys.platform != "win32":
        for name in ["human", "rotate password display", "password only"]:
            item = deepcopy(next(c for c in cases if c["name"] == "vm update " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=watch.compare, default_report="build/go/vm-update-parity.json"
        )
    )
