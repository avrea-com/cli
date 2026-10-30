"""Billing writes compared against Python using isolated local API fixtures."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_billing as billing
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import sys

ENV = billing.ENV
BASE = billing.BASE
PM = billing.PAYMENT
UPDATE = ("PUT", BASE + "/settings")
DEFAULT = ("PUT", BASE + "/payment-methods/" + PM + "/default")
REMOVE = ("DELETE", BASE + "/payment-methods/" + PM)
RESPONSES = {
    UPDATE: (200, {"data": billing.SETTINGS}),
    DEFAULT: (200, {"data": billing.METHOD}),
    REMOVE: (204, None),
}


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "billing write " + name,
                "args": ["billing", *args],
                "env": ENV.copy(),
                "responses": RESPONSES.copy(),
                **kwargs,
            }
        )

    descriptors = [
        ("update", ["update-settings", "--email", "billing@example.test"], UPDATE),
        ("default", ["payment-methods", "set-default", PM], DEFAULT),
        ("remove", ["payment-methods", "remove", PM, "--yes"], REMOVE),
    ]
    for name, args, route in descriptors:
        case(name + " human", args, presentation=True)
        case(name + " no auth", args, env={}, responses={}, exit=4)
        case(
            name + " explicit org",
            args + ["--org", billing.ORG],
            env=ENV | {"AVR_ORG": "org-other", "AVR_REPO": "irrelevant/repo"},
            presentation=True,
        )
        case(
            name + " org slug",
            args + ["--org", "AcMe"],
            responses=RESPONSES | {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})},
            presentation=True,
        )
        for status in [401, 403, 404, 409, 422, 429, 503]:
            case(
                name + " HTTP " + str(status),
                args,
                responses={route: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )

    update = ["update-settings"]
    for name, args in [
        ("tax only", ["--tax-id", "FI12345678"]),
        ("both", ["--email", " a@example.test, , b@example.test ,", "--tax-id", "FI12345678"]),
        ("clear emails", ["--email", ""]),
        ("clear tax", ["--tax-id", ""]),
        ("clear both", ["--email", " , ", "--tax-id", ""]),
        ("repeated email", ["--email", "old@example.test", "--email", "new@example.test"]),
        ("repeated tax", ["--tax-id", "old", "--tax-id", "new"]),
    ]:
        case("update " + name, update + args, presentation=True)
    case("update JSON", update + ["--tax-id", "FI12345678", "--json", "*"], json=True)
    case("update projection", update + ["--email", "", "--json", "billing_emails,tax_id"], json=True)
    case("update jq", update + ["--tax-id", "", "--json", "*", "-q", ".tax_id"])
    case("update empty fields", update + ["--email", "", "--json", ""], json=True)
    case("update discovery", update + ["--json", "?"], env={}, responses={})
    case(
        "update bad fields",
        update + ["--email", "", "--json", "wrong"],
        responses={},
        exit=1,
        error_terms=["Unknown JSON field"],
    )
    case("update jq without JSON", update + ["--jq", "."], responses={}, exit=2, error_terms=["--jq requires --json"])
    case("update no changes", update, responses={}, exit=1)
    case("update no changes no auth", update, env={}, responses={}, exit=4)
    case("update empty settings", update + ["--tax-id", ""], responses={UPDATE: (200, {"data": {}})}, presentation=True)
    case(
        "default missing card",
        ["payment-methods", "set-default", PM],
        responses={DEFAULT: (200, {"data": {}})},
        presentation=True,
    )
    case(
        "default nullable card",
        ["payment-methods", "set-default", PM],
        responses={DEFAULT: (200, {"data": {"card_brand": None, "card_last4": None}})},
        presentation=True,
    )

    remove = ["payment-methods", "remove", PM]
    case("remove confirm", remove, stdin="yes\n", presentation=True)
    case("remove decline", remove, stdin="n\n", responses={}, exit=1, presentation=True)
    case("remove default no", remove, stdin="\n", responses={}, exit=1, presentation=True)
    case("remove EOF", remove, responses={}, exit=1, presentation=True)
    case("remove disabled", remove, env=ENV | {"AVR_PROMPT_DISABLED": "1"}, responses={}, exit=1)
    case("remove yes disabled", remove + ["--yes"], env=ENV | {"AVR_PROMPT_DISABLED": "1"})
    for status, body in [(200, {}), (200, None), (200, b""), (200, {"success": True})]:
        case("remove body " + repr(body), remove + ["--yes"], responses={REMOVE: (status, body)})
    for verb in ["remove", "set-default"]:
        case(verb + " missing argument", ["payment-methods", verb], responses={}, exit=2, error_terms=["PM_ID"])
        case(
            verb + " no JSON",
            ["payment-methods", verb, PM, "--json", "*"],
            responses={},
            exit=2,
            error_terms=["--json"],
        )
    case("remove no short yes", remove + ["-y"], responses={}, exit=2, error_terms=["-y"])
    if sys.platform != "win32":
        for name in ["update human", "default human", "remove confirm", "remove decline"]:
            item = deepcopy(next(c for c in cases if c["name"] == "billing write " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios, compare_results=watch.compare, default_report="build/go/billing-write-parity.json"
        )
    )
