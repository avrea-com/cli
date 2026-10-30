"""Billing inspection output and JSON comparisons against Python."""

from copy import deepcopy
from scripts import compare_auth as harness
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import sys

ORG = inspection.ORG
INVOICE = "inv-019a0000000070008000000000000004"
PAYMENT = "pm-019a0000000070008000000000000005"
ENV = {"AVR_TOKEN": harness.TOKEN, "AVR_ORG": ORG}
BASE = f"/orgs/{ORG}/billing"
METHOD = {
    "payment_method_id": PAYMENT,
    "card_brand": "visa",
    "card_last4": "4242",
    "card_exp_month": 3,
    "card_exp_year": 2028,
    "is_default": True,
}
SUMMARY = {
    "has_billing": True,
    "billing_emails": ["billing@example.test", "finance@example.test"],
    "default_payment_method": METHOD,
}
SETTINGS = {
    "billing_emails": ["billing@example.test"],
    "tax_id": "FI12345678",
    "billing_address": {
        "line1": "Example 1",
        "line2": None,
        "city": "Helsinki",
        "state": None,
        "postal_code": "00100",
        "country": "FI",
    },
    "stripe_customer_id": "cus_fixture",
    "metronome_customer_id": None,
}
INVOICE_ROW = {
    "invoice_id": INVOICE,
    "period_start": "2026-09-01T00:00:00+03:00",
    "period_end": "2026-10-01T00:00:00+03:00",
    "subtotal_cents": 12345,
    "tax_cents": 3148,
    "total_cents": 15493,
    "currency": "eur",
    "status": "paid",
    "has_pdf": True,
    "has_receipt_pdf": False,
    "created_at": "2026-10-02T12:30:45.789123Z",
    "line_items": [
        {"name": "Linux runner 🦫", "quantity": 12.5, "unit_price": 100, "total": 1250},
        {"name": "Credits", "quantity": 1, "unit_price": -99, "total": -99},
    ],
}
SUMMARY_GET = ("GET", BASE)
SETTINGS_GET = ("GET", BASE + "/settings")
METHODS_GET = ("GET", BASE + "/payment-methods")
INVOICES_GET = ("GET", BASE + "/invoices?limit=50")
INVOICE_GET = ("GET", BASE + "/invoices/" + INVOICE)
DEFAULT = {
    SUMMARY_GET: (200, {"data": SUMMARY}),
    SETTINGS_GET: (200, {"data": SETTINGS}),
    METHODS_GET: (
        200,
        {"data": [METHOD, METHOD | {"payment_method_id": "pm-019a0000000070008000000000000006", "is_default": False}]},
    ),
    INVOICES_GET: (200, {"data": [INVOICE_ROW], "pagination": {"next_cursor": "next/cursor+="}}),
    INVOICE_GET: (200, {"data": INVOICE_ROW}),
}


def scenarios():
    cases = []

    def case(name, args, **kwargs):
        cases.append(
            {
                "name": "billing " + name,
                "args": ["billing", *args],
                "env": ENV.copy(),
                "responses": DEFAULT.copy(),
                **kwargs,
            }
        )

    descriptors = [
        ("summary", ["summary"], SUMMARY_GET, "has_billing", False),
        ("settings", ["settings"], SETTINGS_GET, "tax_id", False),
        ("methods", ["payment-methods", "list"], METHODS_GET, "payment_method_id", True),
        ("invoices", ["invoices", "list"], INVOICES_GET, "invoice_id", True),
        ("invoice", ["invoices", "show", INVOICE], INVOICE_GET, "total_cents", False),
    ]
    for label, args, route, field, is_list in descriptors:
        case(label + " human", args, presentation=True)
        case(label + " JSON", args + ["--json", "*"], json=True)
        case(label + " projection", args + ["--json", field], json=True)
        case(label + " jq", args + ["--json", field, "-q", (".[]." if is_list else ".") + field])
        case(label + " empty fields", args + ["--json", ""], json=True)
        case(label + " discovery", args + ["--json", "?"], env={}, responses={})
        case(
            label + " bad fields", args + ["--json", "wrong"], responses={}, exit=1, error_terms=["Unknown JSON field"]
        )
        case(
            label + " jq without JSON", args + ["--jq", "."], responses={}, exit=2, error_terms=["--jq requires --json"]
        )
        case(label + " no auth", args, env={}, responses={}, exit=4)
        case(
            label + " org slug",
            args + ["--org", "AcMe"],
            responses=DEFAULT | {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})},
            presentation=True,
        )
        case(
            label + " explicit org",
            args + ["--org", ORG],
            env=ENV | {"AVR_ORG": "org-other", "AVR_REPO": "irrelevant/repo"},
            presentation=True,
        )
        for status in [401, 403, 404, 409, 422, 429, 503]:
            case(
                label + " HTTP " + str(status),
                args,
                responses={route: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
            )
    case(
        "summary not configured",
        ["summary"],
        responses={SUMMARY_GET: (200, {"data": {"has_billing": False}})},
        presentation=True,
    )
    case(
        "summary not configured JSON",
        ["summary", "--json", "*"],
        responses={SUMMARY_GET: (200, {"data": {"has_billing": False}})},
        json=True,
    )
    case(
        "summary no card",
        ["summary"],
        responses={SUMMARY_GET: (200, {"data": SUMMARY | {"default_payment_method": None, "billing_emails": None}})},
        presentation=True,
    )
    case(
        "summary nullable card",
        ["summary"],
        responses={
            SUMMARY_GET: (
                200,
                {
                    "data": SUMMARY
                    | {
                        "default_payment_method": METHOD
                        | {"card_brand": None, "card_last4": None, "card_exp_month": None, "card_exp_year": None}
                    }
                },
            )
        },
        presentation=True,
    )
    case("settings empty", ["settings"], responses={SETTINGS_GET: (200, {"data": {}})}, presentation=True)
    case(
        "settings full address",
        ["settings"],
        responses={
            SETTINGS_GET: (
                200,
                {
                    "data": SETTINGS
                    | {
                        "billing_address": {
                            "line1": "Line one",
                            "line2": "Office",
                            "city": "City",
                            "state": "State",
                            "postal_code": "12345",
                            "country": "GB",
                        }
                    }
                },
            )
        },
        presentation=True,
    )
    case("methods empty", ["payment-methods", "list"], responses={METHODS_GET: (200, {"data": []})}, presentation=True)
    case(
        "methods nullable",
        ["payment-methods", "list"],
        responses={
            METHODS_GET: (
                200,
                {
                    "data": [
                        METHOD | {"card_brand": None, "card_last4": None, "card_exp_month": None, "card_exp_year": None}
                    ]
                },
            )
        },
        presentation=True,
    )
    case(
        "methods missing",
        ["payment-methods", "list"],
        responses={METHODS_GET: (200, {"data": [{"payment_method_id": PAYMENT}]})},
        presentation=True,
    )
    case("invoices empty", ["invoices", "list"], responses={INVOICES_GET: (200, {"data": []})}, presentation=True)
    case(
        "invoices empty cursor",
        ["invoices", "list", "--json", "*"],
        responses={INVOICES_GET: (200, {"data": [], "pagination": {"next_cursor": "needs 'quoting'"}})},
        json=True,
    )
    case(
        "invoices quoted cursor",
        ["invoices", "list"],
        responses={INVOICES_GET: (200, {"data": [INVOICE_ROW], "pagination": {"next_cursor": "needs 'quoting'"}})},
        presentation=True,
    )
    case(
        "invoices cursor",
        ["invoices", "list", "--cursor", "opaque /?+", "-L", "1"],
        responses={("GET", BASE + "/invoices?limit=1&cursor=opaque+%2F%3F%2B"): (200, {"data": [INVOICE_ROW]})},
        presentation=True,
    )
    case(
        "invoices maximum",
        ["invoices", "list", "--limit", "100"],
        responses={("GET", BASE + "/invoices?limit=100"): (200, {"data": [INVOICE_ROW]})},
        presentation=True,
    )
    for currency in ["usd", "EUR", "gbp", "jpy", "JPY", "krw", "chf", "cad", "xaf"]:
        case(
            "invoice currency " + currency,
            ["invoices", "show", INVOICE],
            responses={INVOICE_GET: (200, {"data": INVOICE_ROW | {"currency": currency}})},
            presentation=True,
        )
    case(
        "invoice no line items",
        ["invoices", "show", INVOICE],
        responses={INVOICE_GET: (200, {"data": INVOICE_ROW | {"line_items": [], "has_pdf": False}})},
        presentation=True,
    )
    case(
        "invoice omitted fields",
        ["invoices", "show", INVOICE],
        responses={INVOICE_GET: (200, {"data": {"invoice_id": INVOICE}})},
        presentation=True,
    )
    case(
        "invoice nullable dates",
        ["invoices", "show", INVOICE],
        responses={INVOICE_GET: (200, {"data": INVOICE_ROW | {"period_start": None, "period_end": None}})},
        presentation=True,
    )
    case(
        "invoice large integers JSON",
        ["invoices", "show", INVOICE, "--json", "*"],
        responses={
            INVOICE_GET: (
                200,
                {
                    "data": INVOICE_ROW
                    | {
                        "total_cents": 9007199254740993,
                        "line_items": [{"quantity": 9007199254740995, "extra": {"unknown": None}}],
                    }
                },
            )
        },
        json=True,
    )
    case("invoice missing argument", ["invoices", "show"], responses={}, exit=2, error_terms=["INVOICE_ID"])
    for limit in ["0", "101"]:
        case(
            "invalid limit " + limit,
            ["invoices", "list", "--limit", limit],
            responses={},
            exit=2,
            error_terms=["--limit"],
        )
    for cursor in ["", " space", "space ", "x" * 513]:
        case(
            "invalid cursor " + repr(cursor[:10]),
            ["invoices", "list", "--cursor", cursor],
            responses={},
            exit=2,
            error_terms=["--cursor"],
        )
    case("discovery before cursor", ["invoices", "list", "--cursor", "", "--json", "?"], env={}, responses={})
    if sys.platform != "win32":
        for name in [
            "summary human",
            "settings human",
            "methods human",
            "invoices human",
            "invoice human",
            "summary no card",
        ]:
            item = deepcopy(next(c for c in cases if c["name"] == "billing " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


if __name__ == "__main__":
    sys.exit(
        harness.main(
            case_factory=scenarios,
            compare_results=watch.compare,
            default_report="build/go/billing-inspection-parity.json",
        )
    )
