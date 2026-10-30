"""CLI configuration parity: terminal styles, precedence and stored host effects."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from typing import Any
import json
import re
import sys

ORG = inspection.ORG
ORGS = ("GET", "/users/me/organizations")
ME = ("GET", "/users/me")
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
HOSTS: dict[str, Any] = {
    "default_host": "https://other.example.test",
    "hosts": {
        "https://other.example.test": {
            "token": "avk_other_fixture",
            "default_org": "org-019a0000000070008000000000000009",
        },
        "FIXTURE_HOST": {"token": harness.TOKEN, "default_org": ORG, "future": {"integer": 9007199254740993}},
    },
}
SAVED = deepcopy(HOSTS)
del SAVED["hosts"]["FIXTURE_HOST"]["default_org"]


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "config " + name,
                "args": ["config", *args],
                "env": ENV.copy(),
                "responses": {
                    ORGS: (200, {"data": inspection.ORGANIZATIONS}),
                    ME: (200, {"data": {"email": "fixture@example.test"}}),
                },
                **kwargs,
            }
        )

    case("status", [], presentation=True)
    case("list", ["list"], presentation=True)
    case("no credentials", [], env={}, responses={}, presentation=True)
    case("no organization", [], env={"AVR_TOKEN": harness.TOKEN}, presentation=True)
    case("unauthenticated organization", [], env={"AVR_ORG": ORG}, responses={}, presentation=True)
    case(
        "repo override",
        [],
        env=ENV | {"AVR_REPO": "acme/other"},
        git_remote="git@github.com:acme/web.git",
        presentation=True,
    )
    case("git remote", ["list"], git_remote="git@github.com:acme/web.git", presentation=True)
    case("gitlab subgroup", [], git_remote="https://gitlab.com/acme/tools/web.git", presentation=True)
    case("stored credentials", [], hosts=HOSTS, env={}, presentation=True)
    case(
        "scoped status",
        [],
        hosts=HOSTS,
        env={"AVR_TOKEN": "avs_fixture", "AVR_ORG": ORG},
        responses={},
        presentation=True,
    )
    case(
        "scoped ignores stored org", [], hosts=HOSTS, env={"AVR_TOKEN": "avs_fixture"}, responses={}, presentation=True
    )
    for status in [401, 403, 503]:
        case(
            "profile HTTP " + str(status),
            [],
            responses={ME: (status, {"detail": "denied"}), ORGS: (200, {"data": inspection.ORGANIZATIONS})},
            presentation=True,
        )
    case(
        "profile without email",
        [],
        responses={ME: (200, {"name": "Fixture"}), ORGS: (200, {"data": inspection.ORGANIZATIONS})},
        presentation=True,
    )
    case(
        "empty profile",
        [],
        responses={ME: (200, {}), ORGS: (200, {"data": inspection.ORGANIZATIONS})},
        presentation=True,
    )
    case(
        "organization lookup failure",
        [],
        responses={ME: (200, {"email": "fixture@example.test"}), ORGS: (403, {"detail": "denied"})},
        presentation=True,
    )
    case("get environment", ["get", "org"], responses={}, hosts=HOSTS)
    case("get stored", ["get", "org"], env={}, responses={}, hosts=HOSTS)
    case("get empty", ["get", "org"], env={}, responses={})
    case("get scoped empty", ["get", "org"], env={"AVR_TOKEN": "avs_fixture"}, responses={}, hosts=HOSTS)
    for selector in [ORG, "acme", "AcMe"]:
        case("set " + selector, ["set", "org", selector], hosts=HOSTS, expected_hosts=HOSTS, writes=True)
    changed = deepcopy(HOSTS)
    changed["hosts"]["FIXTURE_HOST"]["default_org"] = "org-019a0000000070008000000000000008"
    case(
        "set different organization",
        ["set", "org", "beta"],
        hosts=HOSTS,
        expected_hosts=changed,
        responses={
            ORGS: (
                200,
                {
                    "data": inspection.ORGANIZATIONS
                    + [
                        {
                            "organization_id": "org-019a0000000070008000000000000008",
                            "slug": "beta",
                            "name": "Beta",
                            "role": "admin",
                        }
                    ]
                },
            )
        },
        writes=True,
    )
    case(
        "set new host",
        ["set", "org", "acme"],
        expected_hosts={"default_host": None, "hosts": {"FIXTURE_HOST": {"default_org": ORG}}},
        writes=True,
    )
    corrupt = deepcopy(HOSTS)
    corrupt["hosts"]["FIXTURE_HOST"] = None
    repaired = deepcopy(HOSTS)
    repaired["hosts"]["FIXTURE_HOST"] = {"default_org": ORG}
    case("set repairs malformed host", ["set", "org", "acme"], hosts=corrupt, expected_hosts=repaired, writes=True)
    case("set unknown", ["set", "org", "missing"], hosts=HOSTS, expected_hosts=HOSTS, exit=1)
    case(
        "set no organizations",
        ["set", "org", "missing"],
        hosts=HOSTS,
        expected_hosts=HOSTS,
        responses={ORGS: (200, {"data": []})},
        exit=1,
    )
    case("set unauthenticated", ["set", "org", "acme"], env={}, responses={}, exit=4)
    for verb in ["set", "unset"]:
        case(
            verb + " scoped refusal",
            [verb, "org", *(["acme"] if verb == "set" else [])],
            env={"AVR_TOKEN": "avs_fixture"},
            hosts=HOSTS,
            expected_hosts=HOSTS,
            responses={},
            exit=1,
        )
    for status in [401, 403, 503]:
        case(
            "set HTTP " + str(status),
            ["set", "org", "acme"],
            hosts=HOSTS,
            expected_hosts=HOSTS,
            responses={ORGS: (status, {"detail": "denied"})},
            exit=4 if status == 401 else 1,
        )
    case("unset stored", ["unset", "org"], hosts=HOSTS, expected_hosts=SAVED, responses={}, writes=True)
    case(
        "unset without login",
        ["unset", "org"],
        hosts={"default_host": None, "hosts": {"FIXTURE_HOST": {"default_org": ORG}}},
        expected_hosts={"default_host": None, "hosts": {"FIXTURE_HOST": {}}},
        env={},
        responses={},
        writes=True,
    )
    case("unset absent", ["unset", "org"], responses={}, expected_hosts=None)
    case("unset absent key", ["unset", "org"], hosts=SAVED, expected_hosts=SAVED, responses={})
    case("unset malformed host", ["unset", "org"], hosts=corrupt, expected_hosts=corrupt, responses={})
    for verb in ["get", "set", "unset"]:
        case(
            verb + " invalid key",
            [verb, "token", *(["value"] if verb == "set" else [])],
            responses={},
            exit=2,
            error_terms=["Invalid value", "{org}", "token"],
        )
    case("set missing value", ["set", "org"], responses={}, exit=2, error_terms=["Error:"])
    case("get extra argument", ["get", "org", "extra"], responses={}, exit=2, error_terms=["Error:"])
    # Align both wrappers' default environments without any authenticated remote request.
    defaults = {"AVR_HOST": None, "BASE_DOMAIN": "avrea.com", "API_BASE_URL": None, "API_DOMAIN": None}
    case("default host source", [], env=defaults, responses={}, presentation=True)
    case(
        "stored host source",
        [],
        env=defaults | {"AVR_TOKEN": "avs_fixture"},
        hosts={"default_host": "https://api.avrea.com", "hosts": {"https://api.avrea.com": {}}},
        responses={},
        presentation=True,
    )
    if sys.platform != "win32":
        for name in [
            "status",
            "no credentials",
            "stored credentials",
            "scoped status",
            "git remote",
            "profile HTTP 503",
            "organization lookup failure",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "config " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
        item = deepcopy(next(c for c in cases if c["name"] == "config status terminal"))
        item["name"] = "config no-color terminal"
        item["args"] = ["--no-color", *item["args"]]
        cases.append(item)
    return cases


def compare(scenario, python, go):
    differences = inspection.compare(scenario, python, go)
    if "expected_hosts" in scenario:
        for label, result in [("Python", python), ("Go", go)]:
            hosts = json.loads(re.sub(r"http://127\.0\.0\.1:\d+", "FIXTURE_HOST", json.dumps(result["hosts"])))
            if hosts != scenario["expected_hosts"]:
                differences.append(label + " stored configuration")
    return differences


if __name__ == "__main__":
    sys.exit(
        harness.main(case_factory=scenarios, compare_results=compare, default_report="build/go/config-parity.json")
    )
