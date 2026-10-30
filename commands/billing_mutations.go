package commands

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/avrea-com/cli/client/wire"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func newBillingUpdate(d Dependencies) *cobra.Command {
	var org, email, taxID string
	var o jsonOptions
	c := &cobra.Command{Use: "update-settings", Short: "Update billing settings.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().StringVar(&email, "email", "", "Billing email address(es), comma-separated.")
	c.Flags().StringVar(&taxID, "tax-id", "", "Tax ID (e.g. VAT number).")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := o.prepare(cmd, d, billingSettingsFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if !cmd.Flags().Changed("email") && !cmd.Flags().Changed("tax-id") {
			return &ExitError{1, "Error: Provide at least one of --email or --tax-id\nAborted!"}
		}
		body := wire.BillingSettingsRequest{}
		if cmd.Flags().Changed("email") {
			emails := []string{}
			for _, part := range strings.Split(email, ",") {
				if text := strings.TrimSpace(part); text != "" {
					emails = append(emails, text)
				}
			}
			body.BillingEmails.Set(emails)
		}
		if cmd.Flags().Changed("tax-id") {
			body.TaxId.Set(taxID)
		}
		record, err := api.UpdateBillingSettings(cmd.Context(), billingBase(id)+"/settings", body)
		if err != nil {
			return apiError(cfg, err, "update billing settings")
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record.Fields, billingSettingsFields)
		}
		return renderBillingSettings(d, record.Fields)
	})
	return c
}
func newPaymentMethodDefault(d Dependencies) *cobra.Command {
	var org string
	c := &cobra.Command{Use: "set-default PM_ID", Short: "Set a payment method as the default.", Args: positionalArguments("PM_ID")}
	orgFlag(c, &org)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.SetDefaultPaymentMethod(cmd.Context(), billingBase(id)+"/payment-methods/"+url.PathEscape(args[0])+"/default")
		if err != nil {
			return apiError(cfg, err, "set default payment method")
		}
		_, err = fmt.Fprintln(d.Out, "Default payment method set to "+output.CellText(billingCard(record.Fields)))
		return err
	})
	return c
}
func newPaymentMethodRemove(d Dependencies) *cobra.Command {
	var org string
	var yes bool
	c := &cobra.Command{Use: "remove PM_ID", Short: "Remove a payment method.", Args: positionalArguments("PM_ID")}
	orgFlag(c, &org)
	c.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if !yes {
			if err = confirmMutation(cmd.Context(), d, "payment-method remove needs confirmation", "Remove payment method "+output.CellText(args[0])+"?"); err != nil {
				return err
			}
		}
		if err = api.RemovePaymentMethod(cmd.Context(), billingBase(id)+"/payment-methods/"+url.PathEscape(args[0])); err != nil {
			return apiError(cfg, err, "remove payment method")
		}
		_, err = fmt.Fprintln(d.Out, "Payment method "+output.CellText(args[0])+" removed.")
		return err
	})
	return c
}
