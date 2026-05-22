"""Billing CLI commands."""

from avrea_cli.api_client import ApiClient
from avrea_cli.config import CliConfig
from avrea_cli.helpers import ensure_authenticated
from avrea_cli.helpers import ensure_ctx
from avrea_cli.helpers import ensure_prompts_allowed
from avrea_cli.helpers import get_org_id
from avrea_cli.helpers import handle_http_error
from avrea_cli.helpers import validate_cursor
from avrea_cli.json_output import emit_json
from avrea_cli.json_output import emit_json_record
from avrea_cli.json_output import handle_json_meta
from avrea_cli.json_output import json_options
from avrea_cli.json_output import make_schema
from avrea_cli.json_output import split_fields
from avrea_cli.output import format_key_value
from avrea_cli.output import output_list
from typing import Any
import click
import httpx
import os
import shlex

_BILLING_SUMMARY_FIELDS = make_schema("has_billing", "billing_emails", "default_payment_method")
_INVOICE_LIST_FIELDS = make_schema(
    "invoice_id",
    "period_start",
    "period_end",
    "subtotal_cents",
    "tax_cents",
    "total_cents",
    "currency",
    "status",
    "has_pdf",
    "created_at",
)
_INVOICE_VIEW_FIELDS = {**_INVOICE_LIST_FIELDS, **make_schema("line_items")}
_PAYMENT_METHOD_FIELDS = make_schema(
    "payment_method_id", "card_brand", "card_last4", "card_exp_month", "card_exp_year", "is_default"
)
_BILLING_SETTINGS_FIELDS = make_schema(
    "billing_emails", "tax_id", "billing_address", "stripe_customer_id", "metronome_customer_id"
)


@click.group()
@click.pass_context
def billing(ctx):
    """Manage billing, invoices, and payment methods."""
    ensure_ctx(ctx)


@billing.command("summary")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@json_options
@click.pass_context
def billing_summary(ctx, org_id, json_fields, jq_expr):
    """Show billing summary for the organization.

    \b
    JSON FIELDS
        billing_emails, default_payment_method, has_billing
    """
    if handle_json_meta(json_fields, jq_expr, _BILLING_SUMMARY_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id)

    try:
        response = client.public_get(f"/orgs/{org_id}/billing")
        data = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch billing summary")

    if json_fields is not None:
        emit_json_record(data, split_fields(json_fields, _BILLING_SUMMARY_FIELDS), _BILLING_SUMMARY_FIELDS, jq_expr)
        return

    if not data.get("has_billing"):
        click.echo("Billing is not configured for this organization.")
        return

    record = {"Billing": "enabled", "Email": ", ".join(data.get("billing_emails") or []) or "-"}
    pm = data.get("default_payment_method")
    if pm:
        record["Default Card"] = f"{pm.get('card_brand', '?')} ****{pm.get('card_last4', '????')}"
        exp = f"{pm.get('card_exp_month', '?')}/{pm.get('card_exp_year', '?')}"
        record["Card Expires"] = exp
    else:
        record["Default Card"] = "none"

    click.echo(format_key_value(record))


@billing.group("invoices")
@click.pass_context
def invoices(ctx):
    """Manage invoices."""
    ensure_ctx(ctx)


@invoices.command("list")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option(
    "-L",
    "--limit",
    type=click.IntRange(1, 100),
    default=50,
    show_default=True,
    help="Max invoices to return.",
)
@click.option("--cursor", default=None, help="Pagination cursor from a previous response.")
@json_options
@click.pass_context
def invoices_list(ctx, org_id, limit, cursor, json_fields, jq_expr):
    """List invoices.

    \b
    JSON FIELDS
        created_at, currency, has_pdf, invoice_id, period_end, period_start,
        status, subtotal_cents, tax_cents, total_cents
    """
    if handle_json_meta(json_fields, jq_expr, _INVOICE_LIST_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id)

    cursor = validate_cursor(cursor)

    params: dict[str, Any] = {"limit": limit}
    if cursor:
        params["cursor"] = cursor

    try:
        response = client.public_get(f"/orgs/{org_id}/billing/invoices", params=params)
        data = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list invoices")

    next_cursor = (response.get("pagination") or {}).get("next_cursor")

    if json_fields is not None:
        emit_json(data, split_fields(json_fields, _INVOICE_LIST_FIELDS), _INVOICE_LIST_FIELDS, jq_expr)
        if next_cursor:
            # Emit the cursor on a separate stream so scripts piping stdout into
            # `jq` aren't broken, but the next page is still discoverable.
            click.echo(f"next_cursor: {shlex.quote(next_cursor)}", err=True)
        return

    for inv in data:
        inv["total_display"] = _format_cents(inv.get("total_cents", 0), inv.get("currency", "usd"))
        inv["period_display"] = f"{_format_date(inv.get('period_start'))} — {_format_date(inv.get('period_end'))}"
        inv["pdf"] = "yes" if inv.get("has_pdf") else ""

    output_list(
        data,
        columns=["invoice_id", "period_display", "total_display", "currency", "status", "pdf"],
        column_labels=["Invoice ID", "Period", "Total", "Currency", "Status", "PDF"],
    )

    if next_cursor:
        click.echo(
            f"\nMore results available. Next page: --cursor {shlex.quote(next_cursor)}",
            err=True,
        )


@invoices.command("show")
@click.argument("invoice_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@json_options
@click.pass_context
def invoices_show(ctx, invoice_id, org_id, json_fields, jq_expr):
    """Show details for a single invoice.

    \b
    JSON FIELDS
        created_at, currency, has_pdf, invoice_id, line_items, period_end,
        period_start, status, subtotal_cents, tax_cents, total_cents
    """
    if handle_json_meta(json_fields, jq_expr, _INVOICE_VIEW_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id)

    try:
        response = client.public_get(f"/orgs/{org_id}/billing/invoices/{invoice_id}")
        inv = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch invoice")

    if json_fields is not None:
        emit_json_record(inv, split_fields(json_fields, _INVOICE_VIEW_FIELDS), _INVOICE_VIEW_FIELDS, jq_expr)
        return

    record = {
        "Invoice ID": inv.get("invoice_id", "unknown"),
        "Period": f"{_format_date(inv.get('period_start'))} — {_format_date(inv.get('period_end'))}",
        "Subtotal": _format_cents(inv.get("subtotal_cents", 0), inv.get("currency", "usd")),
        "Tax": _format_cents(inv.get("tax_cents", 0), inv.get("currency", "usd")),
        "Total": _format_cents(inv.get("total_cents", 0), inv.get("currency", "usd")),
        "Currency": inv.get("currency", "usd"),
        "Status": inv.get("status", "unknown"),
        "PDF Available": "yes" if inv.get("has_pdf") else "no",
        "Created": inv.get("created_at", "unknown"),
    }
    click.echo(format_key_value(record))

    line_items = inv.get("line_items", [])
    if line_items:
        currency = inv.get("currency", "usd")
        for item in line_items:
            item["unit_price_display"] = _format_cents(item.get("unit_price", 0), currency)
            item["total_display"] = _format_cents(item.get("total", 0), currency)
        click.echo()
        output_list(
            line_items,
            columns=["name", "quantity", "unit_price_display", "total_display"],
            column_labels=["Item", "Quantity", "Unit Price", "Total"],
        )


@invoices.command("download")
@click.argument("invoice_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option("--out", "out_path", default=None, help="Output file path. Defaults to <invoice_id>.pdf.")
@click.pass_context
def invoices_download(ctx, invoice_id, org_id, out_path):
    """Download an invoice PDF."""
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id)

    url = f"{config.public_api_url}/orgs/{org_id}/billing/invoices/{invoice_id}/pdf"
    try:
        response = httpx.get(
            url,
            headers=config.get_api_headers(),
            follow_redirects=True,
            timeout=30.0,
        )
        response.raise_for_status()
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "download invoice PDF")

    safe_id = os.path.basename(invoice_id)
    filename = out_path or f"{safe_id}.pdf"
    try:
        with open(filename, "wb") as f:
            f.write(response.content)
        click.echo(f"Invoice PDF saved to {filename}")
    except OSError as e:
        click.echo(f"Error writing file: {e}", err=True)
        raise click.Abort() from None


@billing.group("payment-methods")
@click.pass_context
def payment_methods(ctx):
    """Manage payment methods."""
    ensure_ctx(ctx)


@payment_methods.command("list")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@json_options
@click.pass_context
def payment_methods_list(ctx, org_id, json_fields, jq_expr):
    """List payment methods.

    \b
    JSON FIELDS
        card_brand, card_exp_month, card_exp_year, card_last4, is_default,
        payment_method_id
    """
    if handle_json_meta(json_fields, jq_expr, _PAYMENT_METHOD_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id)

    try:
        response = client.public_get(f"/orgs/{org_id}/billing/payment-methods")
        data = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "list payment methods")

    if json_fields is not None:
        emit_json(data, split_fields(json_fields, _PAYMENT_METHOD_FIELDS), _PAYMENT_METHOD_FIELDS, jq_expr)
        return

    for pm in data:
        pm["card_display"] = f"{pm.get('card_brand', '?')} ****{pm.get('card_last4', '????')}"
        pm["expires"] = f"{pm.get('card_exp_month', '?')}/{pm.get('card_exp_year', '?')}"
        pm["default"] = "yes" if pm.get("is_default") else ""

    output_list(
        data,
        columns=["payment_method_id", "card_display", "expires", "default"],
        column_labels=["ID", "Card", "Expires", "Default"],
    )


@payment_methods.command("add")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option(
    "--number",
    required=True,
    prompt="Card number",
    hide_input=True,
    help="Credit card number. Prefer prompting over --number to avoid shell history.",
)
@click.option(
    "--exp-month",
    required=True,
    prompt="Expiration month (1-12)",
    type=click.IntRange(1, 12),
    help="Card expiration month.",
)
@click.option("--exp-year", required=True, prompt="Expiration year (e.g. 2027)", type=int, help="Card expiration year.")
@click.option(
    "--cvc",
    required=True,
    prompt="CVC",
    hide_input=True,
    help="Card CVC/CVV code. Prefer prompting over --cvc to avoid shell history.",
)
@click.pass_context
def payment_methods_add(ctx, org_id, number, exp_month, exp_year, cvc):
    """Add a credit card as a payment method.

    Card details are sent directly to Stripe and never touch Avrea servers.
    """
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id)

    # Get Stripe publishable key
    try:
        key_resp = client.public_get(f"/orgs/{org_id}/billing/stripe-key")
        stripe_key = key_resp["data"]["stripe_key"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch Stripe key")

    # Create a SetupIntent
    try:
        intent_resp = client.public_post(f"/orgs/{org_id}/billing/setup-intent")
        client_secret = intent_resp["data"]["client_secret"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "create setup intent")

    # Confirm the SetupIntent with card details directly to Stripe
    parts = client_secret.rsplit("_secret_", 1)
    if len(parts) != 2 or not parts[0].startswith("seti_"):
        click.echo("Error: Invalid SetupIntent client secret format.", err=True)
        raise click.Abort()
    setup_intent_id = parts[0]
    try:
        stripe_resp = httpx.post(
            f"https://api.stripe.com/v1/setup_intents/{setup_intent_id}/confirm",
            auth=(stripe_key, ""),
            data={
                "payment_method_data[type]": "card",
                "payment_method_data[card][number]": number,
                "payment_method_data[card][exp_month]": exp_month,
                "payment_method_data[card][exp_year]": exp_year,
                "payment_method_data[card][cvc]": cvc,
                "client_secret": client_secret,
            },
            timeout=30.0,
        )
        stripe_resp.raise_for_status()
    except httpx.HTTPStatusError as exc:
        detail = ""
        try:
            body = exc.response.json()
            detail = body.get("error", {}).get("message", "")
        except Exception:
            pass
        click.echo(f"Error: Stripe rejected the card details.{f' ({detail})' if detail else ''}", err=True)
        raise click.Abort() from None
    except httpx.RequestError as e:
        click.echo(f"Error: Failed to connect to Stripe: {e}", err=True)
        raise click.Abort() from None

    pm_id = stripe_resp.json().get("payment_method")
    if not pm_id:
        click.echo("Error: No payment method returned from Stripe.", err=True)
        raise click.Abort()

    # Attach to our backend
    try:
        response = client.public_post(
            f"/orgs/{org_id}/billing/payment-methods",
            json={"stripe_payment_method_id": pm_id},
        )
        pm = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "attach payment method")

    click.echo(
        f"Card added: {pm.get('card_brand', '?')} ****{pm.get('card_last4', '????')} "
        f"(expires {pm.get('card_exp_month', '?')}/{pm.get('card_exp_year', '?')})"
    )


@payment_methods.command("set-default")
@click.argument("pm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.pass_context
def payment_methods_set_default(ctx, pm_id, org_id):
    """Set a payment method as the default."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id)

    try:
        response = client.public_put(f"/orgs/{org_id}/billing/payment-methods/{pm_id}/default")
        pm = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "set default payment method")

    click.echo(f"Default payment method set to {pm.get('card_brand', '?')} ****{pm.get('card_last4', '????')}")


@payment_methods.command("remove")
@click.argument("pm_id")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option("--yes", "confirmed", is_flag=True, help="Skip confirmation prompt.")
@click.pass_context
def payment_methods_remove(ctx, pm_id, org_id, confirmed):
    """Remove a payment method."""
    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id)

    if not confirmed:
        ensure_prompts_allowed("payment-method remove needs confirmation")
        click.confirm(f"Remove payment method {pm_id}?", abort=True)

    try:
        client.public_delete(f"/orgs/{org_id}/billing/payment-methods/{pm_id}")
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "remove payment method")

    click.echo(f"Payment method {pm_id} removed.")


@billing.command("settings")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@json_options
@click.pass_context
def billing_settings(ctx, org_id, json_fields, jq_expr):
    """Show billing settings.

    \b
    JSON FIELDS
        billing_address, billing_emails, metronome_customer_id,
        stripe_customer_id, tax_id
    """
    if handle_json_meta(json_fields, jq_expr, _BILLING_SETTINGS_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id)

    try:
        response = client.public_get(f"/orgs/{org_id}/billing/settings")
        data = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "fetch billing settings")

    if json_fields is not None:
        emit_json_record(data, split_fields(json_fields, _BILLING_SETTINGS_FIELDS), _BILLING_SETTINGS_FIELDS, jq_expr)
        return

    click.echo(
        format_key_value(
            {
                "Email": ", ".join(data.get("billing_emails") or []) or "-",
                "Tax ID": data.get("tax_id") or "-",
                "Address": _format_address(data.get("billing_address")),
                "Stripe Customer": data.get("stripe_customer_id") or "-",
                "Metronome Customer": data.get("metronome_customer_id") or "-",
            }
        )
    )


@billing.command("update-settings")
@click.option("--org", "org_id", help="Organization ID. Uses default org if not specified (see: avr config set org).")
@click.option("--email", "billing_email", default=None, help="Billing email address(es), comma-separated.")
@click.option("--tax-id", default=None, help="Tax ID (e.g. VAT number).")
@json_options
@click.pass_context
def billing_update_settings(ctx, org_id, billing_email, tax_id, json_fields, jq_expr):
    """Update billing settings.

    \b
    Examples:
        avr billing update-settings --email billing@example.com
        avr billing update-settings --tax-id EU123456789
        avr billing update-settings --email a@example.com,b@example.com --tax-id FI12345678

    \b
    JSON FIELDS
        billing_address, billing_emails, metronome_customer_id,
        stripe_customer_id, tax_id
    """
    if handle_json_meta(json_fields, jq_expr, _BILLING_SETTINGS_FIELDS):
        return

    client: ApiClient = ctx.obj["client"]
    config: CliConfig = ctx.obj["config"]
    ensure_authenticated(config)
    org_id = get_org_id(config, org_id)

    if billing_email is None and tax_id is None:
        click.echo("Error: Provide at least one of --email or --tax-id", err=True)
        raise click.Abort()

    body = {}
    if billing_email is not None:
        body["billing_emails"] = [e.strip() for e in billing_email.split(",") if e.strip()]
    if tax_id is not None:
        body["tax_id"] = tax_id

    try:
        response = client.public_put(f"/orgs/{org_id}/billing/settings", json=body)
        data = response["data"]
    except httpx.HTTPStatusError as exc:
        handle_http_error(exc, "update billing settings")

    if json_fields is not None:
        emit_json_record(data, split_fields(json_fields, _BILLING_SETTINGS_FIELDS), _BILLING_SETTINGS_FIELDS, jq_expr)
        return

    click.echo(
        format_key_value(
            {
                "Email": ", ".join(data.get("billing_emails") or []) or "-",
                "Tax ID": data.get("tax_id") or "-",
                "Address": _format_address(data.get("billing_address")),
                "Stripe Customer": data.get("stripe_customer_id") or "-",
                "Metronome Customer": data.get("metronome_customer_id") or "-",
            }
        )
    )


_CURRENCY_SYMBOLS = {"usd": "$", "eur": "€", "gbp": "£", "jpy": "¥", "chf": "CHF "}
# Full list from Stripe docs: https://docs.stripe.com/currencies#zero-decimal
_ZERO_DECIMAL_CURRENCIES = {
    "bif",
    "clp",
    "djf",
    "gnf",
    "jpy",
    "kmf",
    "krw",
    "mga",
    "pyg",
    "rwf",
    "ugx",
    "vnd",
    "vuv",
    "xaf",
    "xof",
    "xpf",
}


def _format_cents(cents: int, currency: str = "usd") -> str:
    symbol = _CURRENCY_SYMBOLS.get(currency.lower(), f"{currency.upper()} ")
    if currency.lower() in _ZERO_DECIMAL_CURRENCIES:
        return f"{symbol}{cents}"
    return f"{symbol}{cents / 100:.2f}"


def _format_date(value: str | None) -> str:
    if not value:
        return "?"
    return value[:10]


def _format_address(addr: dict | None) -> str:
    if not addr:
        return "-"
    parts = [
        addr.get("line1"),
        addr.get("line2"),
        addr.get("city"),
        addr.get("state"),
        addr.get("postal_code"),
        addr.get("country"),
    ]
    return ", ".join(p for p in parts if p) or "-"
