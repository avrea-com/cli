// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func billingDescription(path string) docgen.Description {
	switch path {
	case "avr billing invoices download":
		return docgen.Description{
			Help:  "Download an invoice PDF.",
			Short: "Download an invoice PDF.",
			Usage: []string{"[OPTIONS]", "INVOICE_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "invoice_id", Kind: "argument", Opts: []string{"invoice_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "out_path", Kind: "option", Opts: []string{"--out"}, Type: "text", Nargs: 1, Help: descriptionText("Output file path. Defaults to <invoice_id>.pdf.")}, CompletionKind: "String"},
			},
		}
	case "avr billing invoices list":
		return docgen.Description{
			Help: "List invoices.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    created_at, currency, has_pdf, invoice_id, period_end, period_start,\n" +
				"    status, subtotal_cents, tax_cents, total_cents\n",
			Short: "List invoices.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max invoices to return."), ShowDefault: true, Default: 50}, Suffix: "  [default: 50; 1<=x<=100]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Pagination cursor from a previous response.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr billing invoices show":
		return docgen.Description{
			Help: "Show details for a single invoice.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    created_at, currency, has_pdf, invoice_id, line_items, period_end,\n" +
				"    period_start, status, subtotal_cents, tax_cents, total_cents\n",
			Short: "Show details for a single invoice.",
			Usage: []string{"[OPTIONS]", "INVOICE_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "invoice_id", Kind: "argument", Opts: []string{"invoice_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr billing invoices":
		return docgen.Description{
			Help:  "Manage invoices.",
			Short: "Manage invoices.",
			Usage: []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Commands: []docgen.HelpCommand{
				{Name: "download", Description: "Download an invoice PDF."},
				{Name: "list", Description: "List invoices."},
				{Name: "show", Description: "Show details for a single invoice."},
			},
		}
	case "avr billing payment-methods add":
		return docgen.Description{
			Help: "Add a credit card as a payment method.\n" +
				"\n" +
				"Card details are sent directly to Stripe and never touch Avrea servers.\n",
			Short: "Add a credit card as a payment method.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "number", Kind: "option", Opts: []string{"--number"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("Credit card number. Prefer prompting over --number to avoid shell history.")}, Suffix: "  [required]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "exp_month", Kind: "option", Opts: []string{"--exp-month"}, Type: "integer range", Required: true, Nargs: 1, Help: descriptionText("Card expiration month.")}, Suffix: "  [1<=x<=12; required]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "exp_year", Kind: "option", Opts: []string{"--exp-year"}, Type: "integer", Required: true, Nargs: 1, Help: descriptionText("Card expiration year.")}, Suffix: "  [required]", CompletionKind: "Int"},
				{Param: docgen.Param{Name: "cvc", Kind: "option", Opts: []string{"--cvc"}, Type: "text", Required: true, Nargs: 1, Help: descriptionText("Card CVC/CVV code. Prefer prompting over --cvc to avoid shell history.")}, Suffix: "  [required]", CompletionKind: "String"},
			},
		}
	case "avr billing payment-methods list":
		return docgen.Description{
			Help: "List payment methods.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    card_brand, card_exp_month, card_exp_year, card_last4, is_default,\n" +
				"    payment_method_id\n",
			Short: "List payment methods.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr billing payment-methods remove":
		return docgen.Description{
			Help:  "Remove a payment method.",
			Short: "Remove a payment method.",
			Usage: []string{"[OPTIONS]", "PM_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "pm_id", Kind: "argument", Opts: []string{"pm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "confirmed", Kind: "option", Opts: []string{"--yes"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip confirmation prompt.")}, CompletionKind: "Bool"},
			},
		}
	case "avr billing payment-methods set-default":
		return docgen.Description{
			Help:  "Set a payment method as the default.",
			Short: "Set a payment method as the default.",
			Usage: []string{"[OPTIONS]", "PM_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "pm_id", Kind: "argument", Opts: []string{"pm_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
			},
		}
	case "avr billing payment-methods":
		return docgen.Description{
			Help:  "Manage payment methods.",
			Short: "Manage payment methods.",
			Usage: []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Commands: []docgen.HelpCommand{
				{Name: "add", Description: "Add a credit card as a payment method."},
				{Name: "list", Description: "List payment methods."},
				{Name: "remove", Description: "Remove a payment method."},
				{Name: "set-default", Description: "Set a payment method as the default."},
			},
		}
	case "avr billing settings":
		return docgen.Description{
			Help: "Show billing settings.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    billing_address, billing_emails, metronome_customer_id,\n" +
				"    stripe_customer_id, tax_id\n",
			Short: "Show billing settings.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr billing summary":
		return docgen.Description{
			Help: "Show billing summary for the organization.\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    billing_emails, default_payment_method, has_billing\n",
			Short: "Show billing summary for the organization.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr billing update-settings":
		return docgen.Description{
			Help: "Update billing settings.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr billing update-settings --email billing@example.com\n" +
				"    avr billing update-settings --tax-id EU123456789\n" +
				"    avr billing update-settings --email a@example.com,b@example.com --tax-id FI12345678\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    billing_address, billing_emails, metronome_customer_id,\n" +
				"    stripe_customer_id, tax_id\n",
			Short: "Update billing settings.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "billing_email", Kind: "option", Opts: []string{"--email"}, Type: "text", Nargs: 1, Help: descriptionText("Billing email address(es), comma-separated.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "tax_id", Kind: "option", Opts: []string{"--tax-id"}, Type: "text", Nargs: 1, Help: descriptionText("Tax ID (e.g. VAT number).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr billing":
		return docgen.Description{
			Help:           "Manage billing, invoices, and payment methods.",
			Short:          "Manage billing, invoices, and payment methods.",
			CompletionHelp: "Manage billing, invoices, and payment...",
			Usage:          []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Commands: []docgen.HelpCommand{
				{Name: "invoices", Description: "Manage invoices."},
				{Name: "payment-methods", Description: "Manage payment methods."},
				{Name: "settings", Description: "Show billing settings."},
				{Name: "summary", Description: "Show billing summary for the organization."},
				{Name: "update-settings", Description: "Update billing settings."},
			},
		}
	}
	return docgen.Description{}
}
