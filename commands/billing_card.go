package commands

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

// cardAddTimeout bounds the wait for a card added in the console.
const cardAddTimeout = 15 * time.Minute

func newPaymentMethodAdd(d Dependencies) *cobra.Command {
	var org string
	c := &cobra.Command{Use: "add", Short: "Add a credit card as a payment method.", Long: "Add a credit card as a payment method.\n\nOpens the billing page of the Avrea console, where you enter the card and complete any 3D Secure authentication your card issuer requires. Card details go directly to Stripe and never pass through the CLI or Avrea servers.\n\nThe command waits until the new card appears.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		path := billingBase(id) + "/payment-methods"
		existing, err := paymentMethodIDs(cmd.Context(), api, path)
		if err != nil {
			return apiError(cfg, err, "list payment methods")
		}
		target := consoleURL(cfg.Host) + "/org/" + url.PathEscape(orgSlug(cmd.Context(), cfg, api, id)) + "/settings/billing"
		if _, err = fmt.Fprintf(d.Out, "Add the card in the Avrea console:\n\n  %s\n\nWaiting for the new card (up to %d minutes). Press Ctrl+C to stop waiting.\n", output.CellText(target), int(cardAddTimeout.Minutes())); err != nil {
			return err
		}
		if d.IsTerminal() {
			if err = d.openWeb(cmd.Context(), target); err != nil {
				return err
			}
		}
		deadline := d.Now().Add(cardAddTimeout)
		for {
			remaining := deadline.Sub(d.Now())
			if remaining <= 0 {
				return &ExitError{1, "Error: No new card was added within " + fmt.Sprint(int(cardAddTimeout.Minutes())) + " minutes.\nAborted!"}
			}
			if err = d.Wait(cmd.Context(), min(3*time.Second, remaining)); err != nil {
				return err
			}
			page, err := api.PaymentMethods(cmd.Context(), path)
			if err != nil {
				return apiError(cfg, err, "list payment methods")
			}
			for _, row := range rawRecords(page) {
				if !existing[fieldText(row, "payment_method_id")] {
					_, err = fmt.Fprintf(d.Out, "Card added: %s (expires %s)\n", output.CellText(billingCard(row)), output.CellText(billingExpiry(row)))
					return err
				}
			}
		}
	})
	return c
}

func paymentMethodIDs(ctx context.Context, api *client.Client, path string) (map[string]bool, error) {
	page, err := api.PaymentMethods(ctx, path)
	if err != nil {
		return nil, err
	}
	ids := map[string]bool{}
	for _, row := range rawRecords(page) {
		ids[fieldText(row, "payment_method_id")] = true
	}
	return ids, nil
}
