"""Card addition parity with a test-only loopback Stripe transport.

Native binaries cover prompts and failures before Stripe. Successful flows use
the same public/admin command roots with an injected HTTP client; no production
endpoint override or real Stripe request is involved.
"""

from copy import deepcopy
from pathlib import Path
from scripts import compare_auth as harness
from scripts import compare_billing as billing
from scripts import compare_inspection as inspection
from scripts import compare_job_watch as watch
import argparse
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile

NUMBER = "4242424242424242"
CVC = "123"
SECRET = "seti_fixture_secret_synthetic"
KEY = "pk_test_fixture"
PM = "pm_fixture"
KEY_ROUTE = ("GET", billing.BASE + "/stripe-key")
INTENT_ROUTE = ("POST", billing.BASE + "/setup-intent")
STRIPE_ROUTE = ("POST", "/stripe-fixture/v1/setup_intents/seti_fixture/confirm")
ATTACH_ROUTE = ("POST", billing.BASE + "/payment-methods")
FLAGS = ["--number", NUMBER, "--exp-month", "3", "--exp-year", "2028", "--cvc", CVC]
RESPONSES = {
    KEY_ROUTE: (200, {"data": {"stripe_key": KEY}}),
    INTENT_ROUTE: (200, {"data": {"client_secret": SECRET}}),
    STRIPE_ROUTE: (200, {"payment_method": PM}),
    ATTACH_ROUTE: (200, {"data": billing.METHOD}),
}


def scenarios():
    cases = []

    def case(name, args=None, **kwargs):
        cases.append(
            {
                "name": "card add " + name,
                "args": ["billing", "payment-methods", "add", *(FLAGS if args is None else args)],
                "env": billing.ENV.copy(),
                "responses": RESPONSES.copy(),
                **kwargs,
            }
        )

    case("success", presentation=True)
    case("explicit org", FLAGS + ["--org", billing.ORG], env=billing.ENV | {"AVR_ORG": "org-other"}, presentation=True)
    case(
        "org slug",
        FLAGS + ["--org", "AcMe"],
        responses=RESPONSES | {("GET", "/users/me/organizations"): (200, {"data": inspection.ORGANIZATIONS})},
        presentation=True,
    )
    case("verbose", FLAGS + ["--verbose"], presentation=True)
    case("empty card response", responses=RESPONSES | {ATTACH_ROUTE: (200, {"data": {}})}, presentation=True)
    case(
        "nullable card",
        responses=RESPONSES
        | {
            ATTACH_ROUTE: (
                200,
                {"data": {"card_brand": None, "card_last4": None, "card_exp_month": None, "card_exp_year": None}},
            )
        },
        presentation=True,
    )
    case("no auth", env={}, responses={}, exit=4, native=True)
    case("missing flags prompt EOF", [], env={}, responses={}, exit=1, native=True, hidden_prompt=True)
    case("numeric prompt EOF", ["--number", NUMBER, "--cvc", CVC], env={}, responses={}, exit=1, native=True)
    case("numeric prompts", ["--number", NUMBER, "--cvc", CVC], stdin="3\n2028\n", presentation=True)
    case("numeric prompt retries", ["--number", NUMBER, "--cvc", CVC], stdin="0\nabc\n3\nno\n2028\n", presentation=True)
    case("numeric underscore retry", ["--number", NUMBER, "--cvc", CVC], stdin="1__2\n3\n2028\n", presentation=True)
    case("hidden prompts", [], stdin=NUMBER + "\n3\n2028\n" + CVC + "\n", presentation=True, hidden_prompt=True)
    case(
        "prompt disabled follows Python",
        ["--number", NUMBER, "--cvc", CVC],
        stdin="3\n2028\n",
        env=billing.ENV | {"AVR_PROMPT_DISABLED": "1"},
        presentation=True,
    )
    for month in ["0", "13"]:
        case(
            "month range " + month,
            FLAGS + ["--exp-month", month],
            responses={},
            exit=2,
            error_terms=["--exp-month"],
            native=True,
        )
    for flag in ["--exp-month", "--exp-year"]:
        case("invalid integer " + flag, FLAGS + [flag, "abc"], responses={}, exit=2, error_terms=[flag], native=True)
    case("unexpected argument", FLAGS + ["extra"], responses={}, exit=2, error_terms=["extra"], native=True)
    case("no JSON", FLAGS + ["--json", "*"], responses={}, exit=2, error_terms=["--json"], native=True)
    for label, route in [("key", KEY_ROUTE), ("intent", INTENT_ROUTE), ("attach", ATTACH_ROUTE)]:
        for status in [401, 403, 404, 409, 422, 429, 503]:
            case(
                label + " HTTP " + str(status),
                responses=RESPONSES | {route: (status, {"detail": "fixture denied"})},
                exit=4 if status == 401 else 1,
                native=label != "attach",
            )
    for value in ["invalid", "pi_fixture_secret_value", "seti_fixture"]:
        case(
            "invalid intent " + value,
            responses=RESPONSES | {INTENT_ROUTE: (200, {"data": {"client_secret": value}})},
            exit=1,
            native=True,
        )
    for status in [400, 402, 429, 500]:
        case(
            "Stripe HTTP " + str(status),
            responses=RESPONSES | {STRIPE_ROUTE: (status, {"error": {"message": "Your card was declined."}})},
            exit=1,
        )
    case("Stripe non JSON error", responses=RESPONSES | {STRIPE_ROUTE: (502, b"upstream failed")}, exit=1)
    case("Stripe missing payment method", responses=RESPONSES | {STRIPE_ROUTE: (200, {})}, exit=1)
    case("Stripe empty payment method", responses=RESPONSES | {STRIPE_ROUTE: (200, {"payment_method": ""})}, exit=1)
    if sys.platform != "win32":
        for name in ["success", "numeric prompts", "numeric prompt retries", "Stripe HTTP 402"]:
            item = deepcopy(next(c for c in cases if c["name"] == "card add " + name))
            item["name"] += " terminal"
            item["tty"] = item["merge_terminal"] = True
            item["env"] |= {"NO_COLOR": "", "TERM": "xterm-256color"}
            cases.append(item)
    return cases


def compare(scenario, python, go):
    python = deepcopy(python)
    if scenario.get("hidden_prompt"):
        # The interpreter emits a source-location warning once before getpass's
        # actual warning/prompt. It is not part of the CLI's terminal contract.
        python["stderr"] = re.sub(
            r"[^\n]*(?:getpass|termui)\.py:\d+: GetPassWarning: Can not control echo on the terminal.\n[^\n]*\n",
            "",
            python["stderr"],
        )
    differences = watch.compare(scenario, python, go)
    for label, result in [("Python", python), ("Go", go)]:
        for secret in [NUMBER, CVC, SECRET, KEY]:
            if secret in result["stdout"] or secret in result["stderr"]:
                differences.append(label + " sensitive output")
        for request in result["requests"]:
            if request["path"].startswith("/stripe-fixture/"):
                expected = {
                    "payment_method_data[type]": "card",
                    "payment_method_data[card][number]": NUMBER,
                    "payment_method_data[card][exp_month]": "3",
                    "payment_method_data[card][exp_year]": "2028",
                    "payment_method_data[card][cvc]": CVC,
                    "client_secret": SECRET,
                }
                if request["body"] != expected:
                    differences.append(label + " Stripe form")
                if request["authorization"] == "Bearer " + harness.TOKEN:
                    differences.append(label + " API token sent to Stripe")
            elif any(value in json.dumps(request.get("body")) for value in [NUMBER, CVC, SECRET]):
                differences.append(label + " card data sent to Avrea")
    return differences


def prepare_oracle(root):
    (root / "sitecustomize.py").write_text(
        "import os\nfrom urllib.parse import urlsplit\nimport httpx\n"
        "_post = httpx.post\n"
        "def fixture_post(url, **kwargs):\n"
        '    if url.startswith("https://api.stripe.com/"):\n'
        '        host = os.environ["AVR_HOST"]\n'
        '        assert urlsplit(host).hostname == "127.0.0.1"\n'
        '        url = host + "/stripe-fixture" + urlsplit(url).path\n'
        "    return _post(url, **kwargs)\n"
        "httpx.post = fixture_post\n"
    )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", required=True)
    parser.add_argument("--go-tool", default="go")
    parser.add_argument("--admin-module", type=Path)
    parser.add_argument("--report", default="build/go/billing-card-parity.json")
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
        template = (root / "scripts/fixtures/billing_card_main.go.tmpl").read_text()
        template = template.replace(
            "ADMIN_IMPORT", 'admin "github.com/avrea-com/avrea-core/cli-go/commands"' if args.admin_module else ""
        )
        template = template.replace("EXECUTE", "admin.Execute" if args.admin_module else "public.Execute")
        with tempfile.TemporaryDirectory(prefix="avr-card-fixture-") as tmp:
            source = Path(tmp) / "main.go"
            source.write_text(template)
            binary = Path(tmp) / ("card-fixture.exe" if os.name == "nt" else "card-fixture")
            env = os.environ.copy()
            if args.admin_module:
                env["GOWORK"] = str(module / "go.work")
            subprocess.run([args.go_tool, "build", "-o", str(binary), str(source)], cwd=module, env=env, check=True)
            sys.argv = [original[0], "--go", str(binary), "--report", args.report]
            fixture = harness.main(case_factory=scenarios, compare_results=compare, prepare_oracle=prepare_oracle)
        path = Path(args.report)
        report = json.loads(path.read_text())
        report["validation_mode"] = "command-root with injected loopback Stripe transport"
        report["native_binary_sha256"] = hashlib.sha256(Path(args.go).read_bytes()).hexdigest()
        report["native_report"] = native_report
        path.write_text(json.dumps(report, indent=2) + "\n")
        return max(native, fixture)
    finally:
        sys.argv = original


if __name__ == "__main__":
    sys.exit(main())
