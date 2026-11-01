package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newPaymentMethodAdd(d Dependencies) *cobra.Command {
	var org, number, cvc string
	var month, year int
	c := &cobra.Command{Use: "add", Short: "Add a credit card as a payment method.", Long: "Add a credit card as a payment method.\n\nCard details are sent directly to Stripe and never touch Avrea servers.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().StringVar(&number, "number", "", "Credit card number. Prefer prompting over --number to avoid shell history.")
	c.Flags().IntVar(&month, "exp-month", 0, "Card expiration month.")
	c.Flags().IntVar(&year, "exp-year", 0, "Card expiration year.")
	c.Flags().StringVar(&cvc, "cvc", "", "Card CVC/CVV code. Prefer prompting over --cvc to avoid shell history.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if cmd.Flags().Changed("exp-month") && (month < 1 || month > 12) {
			return &ExitError{2, "Error: Invalid value for '--exp-month': must be in range 1<=x<=12."}
		}
		reader := bufio.NewReader(d.In)
		var err error
		for _, field := range []struct {
			name, label string
			hidden      bool
			value       *string
			integer     *int
		}{
			{"number", "Card number", true, &number, nil},
			{"exp-month", "Expiration month (1-12)", false, nil, &month},
			{"exp-year", "Expiration year (e.g. 2027)", false, nil, &year},
			{"cvc", "CVC", true, &cvc, nil},
		} {
			if cmd.Flags().Changed(field.name) {
				continue
			}
			for {
				value, e := cardPrompt(cmd.Context(), d, reader, field.label, field.hidden)
				if e != nil {
					return e
				}
				if value == "" {
					continue
				}
				if field.value != nil {
					*field.value = value
					break
				}
				parsed, e := strconv.Atoi(strings.ReplaceAll(strings.TrimSpace(value), "_", ""))
				if !settingInteger().MatchString(strings.TrimSpace(value)) {
					e = errors.New("invalid integer")
				}
				if e != nil {
					kind := "integer"
					if field.name == "exp-month" {
						kind = "integer range"
					}
					if _, err = fmt.Fprintf(d.Out, "Error: %s is not a valid %s.\n", pythonQuote(output.CellText(value)), kind); err != nil {
						return err
					}
					continue
				}
				if field.name == "exp-month" && (parsed < 1 || parsed > 12) {
					if _, err = fmt.Fprintf(d.Out, "Error: %d is not in the range 1<=x<=12.\n", parsed); err != nil {
						return err
					}
					continue
				}
				*field.integer = parsed
				break
			}
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		base := billingBase(id)
		key, err := api.StripeKey(cmd.Context(), base+"/stripe-key")
		if err != nil {
			return apiError(cfg, err, "fetch Stripe key")
		}
		intent, err := api.SetupIntent(cmd.Context(), base+"/setup-intent")
		if err != nil {
			return apiError(cfg, err, "create setup intent")
		}
		pm, err := api.ConfirmCard(cmd.Context(), fieldText(key.Fields, "stripe_key"), fieldText(intent.Fields, "client_secret"), client.StripeCard{Number: number, CVC: cvc, Month: month, Year: year})
		if err != nil {
			var stripe *client.StripeError
			if errors.As(err, &stripe) {
				return &ExitError{1, "Error: " + output.CellText(stripe.Message) + "\nAborted!"}
			}
			return err
		}
		record, err := api.AttachPaymentMethod(cmd.Context(), base+"/payment-methods", pm)
		if err != nil {
			return apiError(cfg, err, "attach payment method")
		}
		_, err = fmt.Fprintf(d.Out, "Card added: %s (expires %s)\n", output.CellText(billingCard(record.Fields)), output.CellText(billingExpiry(record.Fields)))
		return err
	})
	return c
}

func cardPrompt(ctx context.Context, d Dependencies, reader *bufio.Reader, label string, hidden bool) (string, error) {
	writer := d.Out
	if hidden {
		writer = d.Err
	}
	file, isFile := d.In.(*os.File)
	terminal := isFile && d.IsInputTerminal()
	if hidden && !terminal {
		if _, err := fmt.Fprintln(writer, "Warning: Password input may be echoed."); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprint(writer, label+": "); err != nil {
		return "", err
	}
	type result struct {
		value string
		err   error
	}
	ch := make(chan result, 1)
	if hidden && terminal {
		state, err := term.GetState(int(file.Fd()))
		if err != nil {
			return "", err
		}
		defer func() { _ = term.Restore(int(file.Fd()), state) }()
		go func() { raw, err := term.ReadPassword(int(file.Fd())); ch <- result{string(raw), err} }()
	} else {
		go func() {
			value, err := reader.ReadString('\n')
			ch <- result{strings.TrimSuffix(strings.TrimSuffix(value, "\n"), "\r"), err}
		}()
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case got := <-ch:
		if got.err != nil && (!errors.Is(got.err, io.EOF) || got.value == "") {
			if hidden {
				_, _ = fmt.Fprintln(d.Out)
			}
			return "", &ExitError{1, "Aborted!"}
		}
		if hidden {
			if _, err := fmt.Fprintln(writer); err != nil {
				return "", err
			}
		}
		return got.value, nil
	}
}
