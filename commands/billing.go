package commands

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var billingSummaryFields = []string{"has_billing", "billing_emails", "default_payment_method"}
var invoiceFields = []string{"invoice_id", "period_start", "period_end", "subtotal_cents", "tax_cents", "total_cents", "currency", "status", "has_pdf", "created_at"}
var invoiceViewFields = append(append([]string{}, invoiceFields...), "line_items")
var paymentMethodFields = []string{"payment_method_id", "card_brand", "card_last4", "card_exp_month", "card_exp_year", "is_default"}
var billingSettingsFields = []string{"billing_emails", "tax_id", "billing_address", "stripe_customer_id", "metronome_customer_id"}

// NewBilling exposes billing records for the selected organization.
func NewBilling(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	c := &cobra.Command{Use: "billing", Short: "Manage billing, invoices, and payment methods."}
	invoices := &cobra.Command{Use: "invoices", Short: "Manage invoices."}
	invoices.AddCommand(newInvoiceList(d), newInvoiceShow(d))
	methods := &cobra.Command{Use: "payment-methods", Short: "Manage payment methods."}
	methods.AddCommand(newPaymentMethodList(d), newPaymentMethodDefault(d), newPaymentMethodRemove(d))
	c.AddCommand(newBillingSummary(d), newBillingSettings(d), newBillingUpdate(d), invoices, methods)
	return c
}
func billingBase(org string) string { return "/orgs/" + url.PathEscape(org) + "/billing" }
func billingText(row map[string]any, key, defaultValue string) string {
	if value, ok := row[key]; ok {
		return pythonValue(value)
	}
	return defaultValue
}
func billingEmails(row map[string]any) string {
	emails := strings.Join(settingsStrings(row["billing_emails"]), ", ")
	if emails == "" {
		return "-"
	}
	return emails
}
func billingCard(row map[string]any) string {
	return billingText(row, "card_brand", "?") + " ****" + billingText(row, "card_last4", "????")
}
func billingExpiry(row map[string]any) string {
	return billingText(row, "card_exp_month", "?") + "/" + billingText(row, "card_exp_year", "?")
}
func billingDate(value any) string {
	if value == nil || value == "" {
		return "?"
	}
	text := pythonValue(value)
	runes := []rune(text)
	return string(runes[:min(len(runes), 10)])
}
func billingMoney(value any, currency string) string {
	code := strings.ToLower(currency)
	symbol, ok := map[string]string{"usd": "$", "eur": "€", "gbp": "£", "jpy": "¥", "chf": "CHF "}[code]
	if !ok {
		symbol = strings.ToUpper(currency) + " "
	}
	if slices.Contains([]string{"bif", "clp", "djf", "gnf", "jpy", "kmf", "krw", "mga", "pyg", "rwf", "ugx", "vnd", "vuv", "xaf", "xof", "xpf"}, code) {
		if value == nil {
			return symbol + "0"
		}
		return symbol + pythonValue(value)
	}
	return fmt.Sprintf("%s%.2f", symbol, metricNumber(value)/100)
}
func renderBillingSettings(d Dependencies, row map[string]any) error {
	var address []string
	if object, ok := row["billing_address"].(map[string]any); ok {
		for _, key := range []string{"line1", "line2", "city", "state", "postal_code", "country"} {
			if text := fieldText(object, key); text != "" {
				address = append(address, text)
			}
		}
	}
	location := strings.Join(address, ", ")
	if location == "" {
		location = "-"
	}
	optional := func(key string) string { return pythonValue(fallback(row[key], "-")) }
	return output.KeyValues(d.Out, [][2]string{{"Email", billingEmails(row)}, {"Tax ID", optional("tax_id")}, {"Address", location}, {"Stripe Customer", optional("stripe_customer_id")}, {"Metronome Customer", optional("metronome_customer_id")}})
}
func newBillingSummary(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "summary", Short: "Show billing summary for the organization.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := o.prepare(cmd, d, billingSummaryFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.BillingSummary(cmd.Context(), billingBase(id))
		if err != nil {
			return apiError(cfg, err, "fetch billing summary")
		}
		row := record.Fields
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, row, billingSummaryFields)
		}
		if row["has_billing"] != true {
			_, err = fmt.Fprintln(d.Out, "Billing is not configured for this organization.")
			return err
		}
		labels := [][2]string{{"Billing", "enabled"}, {"Email", billingEmails(row)}}
		method, _ := row["default_payment_method"].(map[string]any)
		if len(method) > 0 {
			labels = append(labels, [2]string{"Default Card", billingCard(method)}, [2]string{"Card Expires", billingExpiry(method)})
		} else {
			labels = append(labels, [2]string{"Default Card", "none"})
		}
		return output.KeyValues(d.Out, labels)
	})
	return c
}
func newBillingSettings(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "settings", Short: "Show billing settings.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := o.prepare(cmd, d, billingSettingsFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.BillingSettings(cmd.Context(), billingBase(id)+"/settings")
		if err != nil {
			return apiError(cfg, err, "fetch billing settings")
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record.Fields, billingSettingsFields)
		}
		return renderBillingSettings(d, record.Fields)
	})
	return c
}
func newPaymentMethodList(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List payment methods.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := o.prepare(cmd, d, paymentMethodFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		page, err := api.PaymentMethods(cmd.Context(), billingBase(id)+"/payment-methods")
		if err != nil {
			return apiError(cfg, err, "list payment methods")
		}
		rows := rawRecords(page)
		if cmd.Flags().Changed("json") {
			return o.list(cmd, d, rows, paymentMethodFields)
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No data found.")
			return err
		}
		var cells [][]string
		for _, row := range rows {
			def := ""
			if row["is_default"] == true {
				def = "yes"
			}
			cells = append(cells, []string{organizationCell(row, "payment_method_id"), billingCard(row), billingExpiry(row), def})
		}
		return output.Grid(d.Out, []string{"ID", "Card", "Expires", "Default"}, cells)
	})
	return c
}
func newInvoiceList(d Dependencies) *cobra.Command {
	var org, cursor string
	var limit int
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List invoices.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	o.flags(c)
	c.Flags().IntVarP(&limit, "limit", "L", 50, "Max invoices to return (1–100).")
	c.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous response.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 100 {
			return &ExitError{2, "Error: --limit must be between 1 and 100."}
		}
		if done, err := o.prepare(cmd, d, invoiceFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("cursor") && (cursor == "" || strings.TrimSpace(cursor) != cursor || utf8.RuneCountInString(cursor) > 512) {
			return &ExitError{2, "Error: --cursor must be 1..512 chars and not surrounded by whitespace"}
		}
		query := url.Values{"limit": {strconv.Itoa(limit)}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		page, err := api.Invoices(cmd.Context(), billingBase(id)+"/invoices", query)
		if err != nil {
			return apiError(cfg, err, "list invoices")
		}
		rows := rawRecords(page)
		if cmd.Flags().Changed("json") {
			if err = o.list(cmd, d, rows, invoiceFields); err != nil {
				return err
			}
			if page.NextCursor != "" {
				_, err = fmt.Fprintln(d.Err, "next_cursor: "+output.CellText(shellQuotePython(page.NextCursor)))
			}
			return err
		}
		if len(rows) == 0 {
			if _, err = fmt.Fprintln(d.Out, "No data found."); err != nil {
				return err
			}
		} else {
			var cells [][]string
			for _, row := range rows {
				pdf := ""
				if row["has_pdf"] == true {
					pdf = "yes"
				}
				cells = append(cells, []string{organizationCell(row, "invoice_id"), billingDate(row["period_start"]) + " — " + billingDate(row["period_end"]), billingMoney(row["total_cents"], billingText(row, "currency", "usd")), organizationCell(row, "currency"), organizationCell(row, "status"), pdf})
			}
			if err = output.Grid(d.Out, []string{"Invoice ID", "Period", "Total", "Currency", "Status", "PDF"}, cells); err != nil {
				return err
			}
		}
		if page.NextCursor != "" {
			_, err = fmt.Fprintln(d.Err, "\nMore results available. Next page: --cursor "+output.CellText(shellQuotePython(page.NextCursor)))
		}
		return err
	})
	return c
}
func newInvoiceShow(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "show INVOICE_ID", Short: "Show details for a single invoice.", Args: positionalArguments("INVOICE_ID")}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, invoiceViewFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.Invoice(cmd.Context(), billingBase(id)+"/invoices/"+url.PathEscape(args[0]))
		if err != nil {
			return apiError(cfg, err, "fetch invoice")
		}
		row := record.Fields
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, row, invoiceViewFields)
		}
		currency := billingText(row, "currency", "usd")
		pdf := "no"
		if row["has_pdf"] == true {
			pdf = "yes"
		}
		if err = output.KeyValues(d.Out, [][2]string{{"Invoice ID", billingText(row, "invoice_id", "unknown")}, {"Period", billingDate(row["period_start"]) + " — " + billingDate(row["period_end"])}, {"Subtotal", billingMoney(row["subtotal_cents"], currency)}, {"Tax", billingMoney(row["tax_cents"], currency)}, {"Total", billingMoney(row["total_cents"], currency)}, {"Currency", currency}, {"Status", billingText(row, "status", "unknown")}, {"PDF Available", pdf}, {"Created", billingText(row, "created_at", "unknown")}}); err != nil {
			return err
		}
		lines := objectList(row["line_items"])
		if len(lines) == 0 {
			return nil
		}
		var cells [][]string
		for _, line := range lines {
			cells = append(cells, []string{organizationCell(line, "name"), organizationCell(line, "quantity"), billingMoney(line["unit_price"], currency), billingMoney(line["total"], currency)})
		}
		if _, err = fmt.Fprintln(d.Out); err != nil {
			return err
		}
		return output.Grid(d.Out, []string{"Item", "Quantity", "Unit Price", "Total"}, cells)
	})
	return c
}
