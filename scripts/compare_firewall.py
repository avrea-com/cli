"""Firewall command parity with local org/repository rule and flow fixtures."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
from urllib.parse import urlencode
import sys

ORG, REPO = inspection.ORG, inspection.REPO
RULE = "efr-019a0000000070008000000000000001"
OTHER = "efr-019a0000000070008000000000000002"
DEFAULT_ID = "efr-019a0000000070008000000000000003"
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG, "AVR_REPO": "ignored/ambient"}
BASE = f"/orgs/{ORG}/firewall"
REPO_BASE = f"/orgs/{ORG}/repos/{REPO}/firewall"
RULES = [
    {
        "rule_id": RULE,
        "position": 0,
        "action": "allow",
        "cidr": "10.0.0.1/8",
        "fqdn": None,
        "protocol": "tcp",
        "port_start": 443,
        "port_end": 443,
        "is_default": False,
    },
    {
        "rule_id": OTHER,
        "position": 1,
        "action": "allow",
        "cidr": None,
        "fqdn": "api.example.test",
        "protocol": "udp",
        "port_start": 30000,
        "port_end": 39999,
        "is_default": False,
    },
    {
        "rule_id": DEFAULT_ID,
        "position": 2,
        "action": "deny",
        "cidr": None,
        "fqdn": None,
        "protocol": "any",
        "port_start": None,
        "port_end": None,
        "is_default": True,
    },
]
FLOW_ROW = {
    "repository_id": REPO,
    "vm_id": "vm-019a0000000070008000000000000001",
    "vm_index": 0,
    "vm_ip": "10.0.0.2",
    "start_ts": "2026-10-01T10:00:00Z",
    "end_ts": "2026-10-01T10:01:00Z",
    "duration_s": 60,
    "bytes_egress": 1024,
    "packets_egress": 10,
    "bytes_ingress": 2048,
    "packets_ingress": 20,
    "flow_count": 4,
    "top_destinations": [
        {"dst_fqdn": "api.example.test", "dst_ip": "192.0.2.1"},
        {"dst_ip": "192.0.2.2"},
        {},
        {"dst_fqdn": "not-shown.example.test"},
    ],
    "drops": [{"label": "blocked-rule", "packets": 2}, {"label": "missing-count"}],
    "blocked_dns_queries": [{"qname": "blocked.example.test", "count": 4}, {"qname": "default.example.test"}],
}
FLOWS = {"results": [FLOW_ROW], "limit": 20, "offset": 0, "total_estimated": 2, "has_more": True}
FLOW_QUERY = {"limit": 20, "offset": 0, "only_with_drops": "false"}
FLOW_GET = ("GET", REPO_BASE + "/vm-flow-summaries?" + urlencode(FLOW_QUERY))


def responses(base=BASE):
    return {
        ("GET", base + "/rules"): (200, deepcopy(RULES)),
        ("POST", base + "/rules"): (200, RULES[0]),
        ("DELETE", base + "/rules/" + RULE): (204, None),
        ("POST", base + "/rules/reorder"): (200, deepcopy(RULES)),
        ("PATCH", base + "/rules/" + DEFAULT_ID): (200, RULES[2] | {"action": "allow"}),
        ("GET", REPO_BASE + "/resolved"): (200, {"rules": deepcopy(RULES), "extra": "retained only in API"}),
        FLOW_GET: (200, deepcopy(FLOWS)),
    }


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "firewall " + name,
                "args": ["firewall", *args],
                "env": ENV.copy(),
                "responses": responses(),
                **kwargs,
            }
        )

    descriptors = [
        ("list", ["list"], ("GET", BASE + "/rules")),
        ("show", ["show", "--repo", REPO], ("GET", REPO_BASE + "/resolved")),
        ("add", ["add", "--action", "allow", "--cidr", "10.0.0.1/8"], ("POST", BASE + "/rules")),
        ("delete", ["delete", RULE], ("DELETE", BASE + "/rules/" + RULE)),
        ("move", ["move", RULE, "--to", "2"], ("POST", BASE + "/rules/reorder")),
        ("default", ["set-default", "--action", "allow"], ("PATCH", BASE + "/rules/" + DEFAULT_ID)),
        ("flows", ["flow-summaries", "--repo", REPO], FLOW_GET),
    ]
    for name, args, route in descriptors:
        case(name + " human", args, presentation=True)
        case(name + " no auth", args, env={}, responses={}, exit=4)
        case(
            name + " org slug",
            args + ["--org", "AcMe"],
            responses=responses() | {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})},
            presentation=True,
        )
        case(name + " explicit org", args + ["--org", ORG], env=ENV | {"AVR_ORG": "org-other"}, presentation=True)
        for status in [401, 403, 404, 409, 422, 429, 503]:
            case(
                name + " HTTP " + str(status),
                args,
                responses=responses() | {route: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
        if name in ["list", "show", "flows"]:
            case(name + " JSON", args + ["--json"], json=True)
            case(name + " no JSON fields", args + ["--json", "*"], responses={}, exit=2, error_terms=["*"])
            case(name + " no jq", args + ["--jq", "."], responses={}, exit=2, error_terms=["--jq"])
        else:
            case(name + " repo scope", args + ["--repo", REPO], responses=responses(REPO_BASE), presentation=True)
            case(name + " no JSON", args + ["--json"], responses={}, exit=2, error_terms=["--json"])

    case("list repo", ["list", "--repo", REPO], responses=responses(REPO_BASE), presentation=True)
    case("list empty repo stays org", ["list", "--repo", ""], presentation=True)
    for value in [[], None]:
        case(
            "list empty " + repr(value), ["list"], responses={("GET", BASE + "/rules"): (200, value)}, presentation=True
        )
        case(
            "list empty JSON " + repr(value),
            ["list", "--json"],
            responses={("GET", BASE + "/rules"): (200, value)},
            json=True,
        )
    case(
        "show empty",
        ["show", "--repo", REPO],
        responses={("GET", REPO_BASE + "/resolved"): (200, {})},
        presentation=True,
    )
    variants = [
        {},
        {"port_start": None, "port_end": 443},
        {"port_start": 443, "port_end": None},
        {"protocol": None},
        {"cidr": None, "fqdn": None},
    ]
    for index, change in enumerate(variants):
        row = (RULES[0] | change) if change else {}
        case(
            "rule formatting " + str(index),
            ["list"],
            responses={("GET", BASE + "/rules"): (200, [row])},
            presentation=True,
        )
    for label, args in [
        ("fqdn", ["--fqdn", "api.example.test"]),
        ("catch all", ["--any"]),
        ("tcp port", ["--fqdn", "api.example.test", "--proto", "tcp", "--ports", "443"]),
        ("udp range", ["--cidr", "192.0.2.17/24", "--proto", "udp", "--ports", "1-65535", "--position", "0"]),
        ("icmp", ["--cidr", "::1/128", "--proto", "icmp"]),
        ("IPv6 host bits", ["--cidr", "2001:db8::1/64"]),
        ("plain IP", ["--cidr", "192.0.2.1"]),
        ("netmask", ["--cidr", "192.0.2.17/255.255.255.0"]),
        ("hostmask", ["--cidr", "192.0.2.17/0.0.0.255"]),
        ("space ports", ["--any", "--proto", "tcp", "--ports", " 80 - 81 "]),
        ("underscore port", ["--any", "--proto", "tcp", "--ports", "1_000"]),
        ("empty cidr included", ["--cidr", "", "--fqdn", "api.example.test"]),
        ("negative position", ["--any", "--position", "-1"]),
    ]:
        case("add " + label, ["add", "--action", "deny", *args], presentation=True)
    for label, args, terms in [
        ("no matcher", [], ["Exactly one"]),
        ("two matchers", ["--cidr", "10.0.0.0/8", "--any"], ["Exactly one"]),
        ("empty matcher", ["--fqdn", ""], ["Exactly one"]),
        ("invalid CIDR", ["--cidr", "bad"], ["Invalid --cidr"]),
        ("bad mask", ["--cidr", "10.0.0.0/255.0.255.0"], ["Invalid --cidr"]),
        ("ports protocol", ["--any", "--ports", "443"], ["--ports requires"]),
        ("uppercase protocol", ["--any", "--proto", "TCP"], ["--proto"]),
    ]:
        case("invalid " + label, ["add", "--action", "allow", *args], env={}, responses={}, exit=2, error_terms=terms)
    for ports in ["", "abc", "0", "65536", "4-3", "1-2-3", "-1", "12--3", "999999999999999999999999"]:
        case(
            "invalid ports " + repr(ports),
            ["add", "--action", "deny", "--any", "--proto", "tcp", "--ports", ports],
            responses={},
            env={},
            exit=2,
            error_terms=["Invalid --ports"],
        )
    for verb in ["add", "set-default"]:
        case(verb + " missing action", [verb], responses={}, exit=2, error_terms=["action"])
        case(verb + " bad action", [verb, "--action", "ALLOW"], responses={}, exit=2, error_terms=["--action"])
    for verb in ["show", "flow-summaries"]:
        case(verb + " missing repo", [verb], responses={}, exit=2, error_terms=["repo"])
    for verb in ["delete", "move"]:
        case(verb + " missing ID", [verb], responses={}, exit=2, error_terms=["RULE_ID"])
    case("move missing to", ["move", RULE], responses={}, exit=2, error_terms=["to"])
    for to in ["-1", "3"]:
        case("move invalid to " + to, ["move", RULE, "--to", to], exit=2, error_terms=["Target position"])
    case(
        "move absent",
        ["move", "efr-019a0000000070008000000000000009", "--to", "0"],
        exit=2,
        error_terms=["not found in this scope"],
    )
    case("move same position", ["move", RULE, "--to", "0"], presentation=True)
    case("move backwards", ["move", OTHER, "--to", "0"], presentation=True)
    case(
        "move empty",
        ["move", RULE, "--to", "0"],
        responses={("GET", BASE + "/rules"): (200, [])},
        exit=2,
        error_terms=["not found in this scope"],
    )
    for verb, args in [("move", ["move", RULE, "--to", "1"]), ("default", ["set-default", "--action", "deny"])]:
        case(
            verb + " read failure",
            args,
            responses={("GET", BASE + "/rules"): (503, {"detail": "fixture denied"})},
            exit=1,
        )
    case(
        "default create",
        ["set-default", "--action", "deny"],
        responses=responses() | {("GET", BASE + "/rules"): (200, RULES[:2])},
        presentation=True,
    )
    case(
        "default create empty",
        ["set-default", "--action", "deny"],
        responses=responses() | {("GET", BASE + "/rules"): (200, [])},
        presentation=True,
    )
    case(
        "default create failed",
        ["set-default", "--action", "deny"],
        responses={("GET", BASE + "/rules"): (200, []), ("POST", BASE + "/rules"): (503, {"detail": "fixture denied"})},
        exit=1,
    )
    case("default unchanged still patches", ["set-default", "--action", "deny"], presentation=True)
    for status, body in [(200, {}), (200, b""), (200, None)]:
        case(
            "delete empty " + repr(body),
            ["delete", RULE],
            responses={("DELETE", BASE + "/rules/" + RULE): (status, body)},
            presentation=True,
        )
    case("delete no confirmation", ["delete", RULE], env=ENV | {"AVR_PROMPT_DISABLED": "1"}, presentation=True)
    case(
        "flows empty",
        ["flow-summaries", "--repo", REPO],
        responses={FLOW_GET: (200, {"results": [], "has_more": True})},
        presentation=True,
    )
    case(
        "flows nullable fields",
        ["flow-summaries", "--repo", REPO],
        responses={
            FLOW_GET: (
                200,
                FLOWS
                | {
                    "results": [
                        FLOW_ROW
                        | {
                            "vm_id": None,
                            "end_ts": None,
                            "duration_s": None,
                            "bytes_egress": None,
                            "drops": None,
                            "blocked_dns_queries": None,
                            "top_destinations": None,
                        }
                    ]
                },
            )
        },
        presentation=True,
    )
    case(
        "flows large JSON",
        ["flow-summaries", "--repo", REPO, "--json"],
        responses={FLOW_GET: (200, FLOWS | {"extra": {"integer": 9007199254740993, "unknown": None}})},
        json=True,
    )
    filters = {
        "limit": 1,
        "offset": 5,
        "only_with_drops": "true",
        "job_id": inspection.JOB,
        "start_after": "2026-10-01T00:00:00Z",
        "end_before": "2026-10-02T00:00:00Z",
    }
    for aliases in [False, True]:
        args = [
            "flow-summaries",
            "--repo",
            REPO,
            "--job-id" if aliases else "--job",
            inspection.JOB,
            "--with-drops",
            "-L",
            "1",
            "--offset",
            "5",
            "--start-after" if aliases else "--from",
            filters["start_after"],
            "--end-before" if aliases else "--to",
            filters["end_before"],
        ]
        case(
            "flows filters " + str(aliases),
            args,
            responses={("GET", REPO_BASE + "/vm-flow-summaries?" + urlencode(filters)): (200, FLOWS)},
            presentation=True,
        )
    for flag, value in [("--limit", "0"), ("--limit", "1001"), ("--offset", "-1")]:
        case(
            "flows invalid " + flag + value,
            ["flow-summaries", "--repo", REPO, flag, value],
            responses={},
            exit=2,
            error_terms=[flag],
        )
    if sys.platform != "win32":
        for name in [
            "list human",
            "show human",
            "add human",
            "delete human",
            "move human",
            "default human",
            "flows human",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "firewall " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=watch.compare, default_report="build/go/firewall-parity.json"
        )
    )
