"""Organization creation, membership and domain management differential checks."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import sys

ORG = inspection.ORG
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
ORGS = {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})}
BASE = f"/orgs/{ORG}/email-domains"
CREATED = {"organization_id": ORG, "name": "Fixture Team", "slug": "fixture-team", "role": "admin"}
MEMBER = {
    "user_id": "usr-019a0000000070008000000000000003",
    "name": "Fixture Member",
    "role": "admin",
    "state": "active",
    "avatar_url": None,
    "joined_at": "2026-10-01T12:34:56.789123+03:00",
}
DOMAIN = {
    "organization_email_domain_id": "oed-019a0000000070008000000000000004",
    "organization_id": ORG,
    "domain": "example.test",
    "created_at": "2026-10-01T12:34:56Z",
    "verified": False,
    "verified_at": None,
    "dns_record_name": "_avrea-verification.example.test",
    "dns_record_value": "fixture-verification-value",
}
VERIFIED = DOMAIN | {"verified": True, "verified_at": "2026-10-02T12:00:00Z"}
OTHER = DOMAIN | {"organization_email_domain_id": "oed-019a0000000070008000000000000005", "domain": "corp.example.test"}
DESCRIPTORS = [
    ("create", ["create", "Fixture Team"], ("POST", "/users/me/organizations"), (201, CREATED), "name", False),
    (
        "members",
        ["members"],
        ("GET", f"/orgs/{ORG}/members"),
        (200, {"data": [MEMBER, MEMBER | {"name": None, "role": "member"}]}),
        "name",
        True,
    ),
    (
        "domain list",
        ["email-domain", "list"],
        ("GET", BASE),
        (
            200,
            {
                "data": [
                    DOMAIN,
                    VERIFIED | {"domain": "corp.example.test", "dns_record_name": None, "dns_record_value": None},
                ]
            },
        ),
        "domain",
        True,
    ),
    (
        "domain claim",
        ["email-domain", "claim", "example.test"],
        ("POST", BASE + "/claim"),
        (200, {"data": DOMAIN}),
        "domain",
        False,
    ),
    (
        "domain verify",
        ["email-domain", "verify", "example.test"],
        ("POST", BASE + "/example.test/verify"),
        (200, {"data": VERIFIED}),
        "domain",
        False,
    ),
]


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append({"name": "organization " + name, "args": ["org", *args], "env": ENV.copy(), **kwargs})

    for label, args, route, response, field, is_list in DESCRIPTORS:
        responses = ORGS | {route: response}
        case(label + " human", args, responses=responses, presentation=True)
        case(label + " JSON", args + ["--json", "*"], responses=responses, json=True)
        case(label + " projection", args + ["--json", field], responses=responses, json=True)
        case(label + " jq", args + ["--json", field, "-q", (".[]." if is_list else ".") + field], responses=responses)
        case(label + " empty fields", args + ["--json", ""], responses=responses, json=True)
        case(label + " discovery", args + ["--json", "?"], env={}, responses={})
        case(
            label + " unknown field",
            args + ["--json", "wrong"],
            responses={},
            exit=1,
            error_terms=["Unknown JSON field"],
        )
        case(
            label + " jq without JSON", args + ["--jq", "."], responses={}, exit=2, error_terms=["--jq requires --json"]
        )
        case(label + " no auth", args, env={}, responses={}, exit=4)
        if label != "create":
            case(label + " org slug", args + ["--org", "AcMe"], responses=responses, presentation=True)
            case(
                label + " explicit org",
                args + ["--org", ORG],
                env=ENV | {"AVR_ORG": "org-wrong"},
                responses=responses,
                presentation=True,
            )
        for status in [401, 403, 404, 409, 422, 429, 503]:
            case(
                label + " HTTP " + str(status),
                args,
                responses={route: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
        if is_list:
            case(label + " empty", args, responses={route: (200, {"data": []})}, presentation=True)
    case(
        "create no org",
        ["create", "Fixture Team"],
        env={"AVR_TOKEN": harness.TOKEN},
        responses={("POST", "/users/me/organizations"): (201, CREATED)},
        presentation=True,
    )
    case(
        "create unicode name",
        ["create", "開発 équipe"],
        responses={("POST", "/users/me/organizations"): (201, CREATED | {"name": "開発 équipe"})},
        presentation=True,
    )
    case(
        "domain claim already verified",
        ["email-domain", "claim", "example.test"],
        responses={("POST", BASE + "/claim"): (200, {"data": VERIFIED})},
        presentation=True,
    )
    case(
        "domain claim no challenge",
        ["email-domain", "claim", "example.test"],
        responses={
            ("POST", BASE + "/claim"): (200, {"data": DOMAIN | {"dns_record_name": None, "dns_record_value": None}})
        },
        presentation=True,
    )
    case(
        "domain verify pending",
        ["email-domain", "verify", "example.test"],
        responses={("POST", BASE + "/example.test/verify"): (200, {"data": DOMAIN})},
        presentation=True,
    )
    case(
        "domain verify escaped path",
        ["email-domain", "verify", "a/b?c#d"],
        responses={("POST", BASE + "/a%2Fb%3Fc%23d/verify"): (200, {"data": VERIFIED})},
        presentation=True,
    )
    set_args = ["email-domain", "set", "example.test", "corp.example.test"]
    response = (200, {"data": [DOMAIN, OTHER]})
    case("domain set yes", set_args + ["--yes"], responses={("POST", BASE): response})
    case("domain set short yes", set_args + ["-y"], responses={("POST", BASE): response})
    case("domain set confirm", set_args, stdin="yes\n", responses={("POST", BASE): response}, presentation=True)
    case(
        "domain set retry prompt", set_args, stdin="wrong\ny\n", responses={("POST", BASE): response}, presentation=True
    )
    case("domain set decline", set_args, stdin="n\n", responses={}, exit=1, presentation=True)
    case("domain set default no", set_args, stdin="\n", responses={}, exit=1, presentation=True)
    case("domain set EOF", set_args, responses={}, exit=1, presentation=True)
    case("domain set disabled", set_args, env=ENV | {"AVR_PROMPT_DISABLED": "1"}, responses={}, exit=1)
    case("domain set no auth", set_args + ["--yes"], env={}, responses={}, exit=4)
    case("domain set org slug", set_args + ["--yes", "--org", "AcMe"], responses=ORGS | {("POST", BASE): response})
    case("domain set empty result", set_args + ["--yes"], responses={("POST", BASE): (200, {"data": []})})
    for status in [401, 403, 404, 409, 422, 429, 503]:
        case(
            "domain set HTTP " + str(status),
            set_args + ["--yes"],
            responses={("POST", BASE): (status, {"detail": "fixture denied"})},
            exit=4 if status == 401 else 1,
        )
    for args, term in [
        (["create"], "NAME"),
        (["create", "one", "two"], "unexpected extra argument"),
        (["email-domain", "claim"], "DOMAIN"),
        (["email-domain", "verify"], "DOMAIN"),
        (["email-domain", "set"], "DOMAINS"),
    ]:
        case("arguments " + " ".join(args), args, responses={}, exit=2, error_terms=[term])
    if sys.platform != "win32":
        for name in [
            "create human",
            "members human",
            "domain list human",
            "domain claim human",
            "domain verify human",
            "domain verify pending",
            "domain set confirm",
            "domain set decline",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "organization " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=watch.compare, default_report="build/go/organization-parity.json"
        )
    )
