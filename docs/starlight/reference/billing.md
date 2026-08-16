---
title: avr billing
description: "Manage billing, invoices, and payment methods."
---

Manage billing, invoices, and payment methods.

```sh
avr billing [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr billing invoices`

Manage invoices.

```sh
avr billing invoices [OPTIONS] COMMAND [ARGS]...
```

#### `avr billing invoices download`

Download an invoice PDF.

```sh
avr billing invoices download [OPTIONS] INVOICE_ID
```

**Arguments**

- <code class="cli-arg">INVOICE_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;out</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output file path. Defaults to &lt;invoice_id&gt;.pdf.

#### `avr billing invoices list`

List invoices.

```sh
avr billing invoices list [OPTIONS]
```

```sh
JSON FIELDS
    created_at, currency, has_pdf, invoice_id, period_end, period_start,
    status, subtotal_cents, tax_cents, total_cents
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max invoices to return. _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Pagination cursor from a previous response.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr billing invoices show`

Show details for a single invoice.

```sh
avr billing invoices show [OPTIONS] INVOICE_ID
```

```sh
JSON FIELDS
    created_at, currency, has_pdf, invoice_id, line_items, period_end,
    period_start, status, subtotal_cents, tax_cents, total_cents
```

**Arguments**

- <code class="cli-arg">INVOICE_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr billing payment-methods`

Manage payment methods.

```sh
avr billing payment-methods [OPTIONS] COMMAND [ARGS]...
```

#### `avr billing payment-methods add`

Add a credit card as a payment method.

```sh
avr billing payment-methods add [OPTIONS]
```

Card details are sent directly to Stripe and never touch Avrea servers.

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;number</code> <code class="cli-value">&lt;TEXT&gt;</code> — Credit card number. Prefer prompting over --number to avoid shell history. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;exp-month</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Card expiration month. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;exp-year</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Card expiration year. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;cvc</code> <code class="cli-value">&lt;TEXT&gt;</code> — Card CVC/CVV code. Prefer prompting over --cvc to avoid shell history. _(required)_

#### `avr billing payment-methods list`

List payment methods.

```sh
avr billing payment-methods list [OPTIONS]
```

```sh
JSON FIELDS
    card_brand, card_exp_month, card_exp_year, card_last4, is_default,
    payment_method_id
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr billing payment-methods remove`

Remove a payment method.

```sh
avr billing payment-methods remove [OPTIONS] PM_ID
```

**Arguments**

- <code class="cli-arg">PM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes</code> — Skip confirmation prompt.

#### `avr billing payment-methods set-default`

Set a payment method as the default.

```sh
avr billing payment-methods set-default [OPTIONS] PM_ID
```

**Arguments**

- <code class="cli-arg">PM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).

### `avr billing settings`

Show billing settings.

```sh
avr billing settings [OPTIONS]
```

```sh
JSON FIELDS
    billing_address, billing_emails, metronome_customer_id,
    stripe_customer_id, tax_id
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr billing summary`

Show billing summary for the organization.

```sh
avr billing summary [OPTIONS]
```

```sh
JSON FIELDS
    billing_emails, default_payment_method, has_billing
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr billing update-settings`

Update billing settings.

```sh
avr billing update-settings [OPTIONS]
```

```sh
Examples:
    avr billing update-settings --email billing@example.com
    avr billing update-settings --tax-id EU123456789
    avr billing update-settings --email a@example.com,b@example.com --tax-id FI12345678
```

```sh
JSON FIELDS
    billing_address, billing_emails, metronome_customer_id,
    stripe_customer_id, tax_id
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;email</code> <code class="cli-value">&lt;TEXT&gt;</code> — Billing email address(es), comma-separated.
- <code class="cli-flag">&#x2D;&#x2D;tax-id</code> <code class="cli-value">&lt;TEXT&gt;</code> — Tax ID (e.g. VAT number).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
