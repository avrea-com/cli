package commands

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var healthFields = []string{"status"}

// NewHealth inspects platform health without requiring a login.
func NewHealth(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	var o jsonOptions
	c := &cobra.Command{Use: "health", Short: "Check Avrea platform status.", Args: cobra.NoArgs}
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := o.prepare(cmd, d, healthFields); done || err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		isJSON := cmd.Flags().Changed("json")
		if !isJSON {
			if _, err = fmt.Fprintln(d.Out, "Checking API health..."); err != nil {
				return err
			}
		}
		record, err := api.Health(cmd.Context())
		if err != nil {
			if cmd.Context().Err() != nil {
				return cmd.Context().Err()
			}
			u, _ := url.Parse(cfg.Host)
			host := u.Hostname()
			if host == "" {
				host = cfg.Host
			}
			var he *client.HTTPError
			var te *client.TransportError
			var reason string
			switch {
			case errors.As(err, &he):
				reason = fmt.Sprintf("HTTP %d from %s.", he.Status, host)
			case errors.As(err, &te):
				if strings.Contains(te.Reason, "Timeout") {
					reason = host + " took too long to respond."
				} else {
					reason = "couldn't reach " + host + " — check your network connection."
				}
			default:
				return err
			}
			if isJSON {
				if _, err = fmt.Fprintln(d.Err, "API unreachable: "+output.CellText(reason)); err != nil {
					return err
				}
				if err = o.record(cmd, d, map[string]any{"status": "unreachable"}, healthFields); err != nil {
					return err
				}
			} else {
				if _, err = fmt.Fprintln(d.Err, "✗ API: "+output.CellText(reason)); err != nil {
					return err
				}
			}
			return &ExitError{1, "Aborted!"}
		}
		if isJSON {
			return o.record(cmd, d, record.Fields, healthFields)
		}
		status, present := record.Fields["status"]
		if !present {
			status = "unknown"
		}
		_, err = fmt.Fprintln(d.Out, "✓ API: "+output.CellText(pythonValue(status)))
		return err
	})
	return c
}
