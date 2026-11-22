package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func newVMFirewall(d Dependencies) *cobra.Command {
	var org string
	var asJSON bool
	c := &cobra.Command{Use: "firewall VM_ID_OR_NAME", Short: "Watch firewall blocks on a running VM or agent sandbox.", Args: positionalArguments("VM_ID_OR_NAME")}
	orgFlag(c, &org)
	c.Flags().BoolVar(&asJSON, "json", false, "Emit one live flow snapshot per line.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, orgID, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		base := vmBase(orgID)
		vm, err := api.VM(cmd.Context(), base+"/"+url.PathEscape(args[0]))
		if err != nil {
			return vmError(cfg, err, "get VM")
		}
		id := fieldText(vm, "customer_vm_id")
		if id == "" {
			return &ExitError{1, "Error: VM response is missing its ID."}
		}
		path := base + "/" + url.PathEscape(id) + "/observability/stream"
		seen := false
		err = observabilityFrames(cmd.Context(), d, api, path, "flows", "Firewall", "snapshot", func(frame map[string]any) error {
			switch frame["type"] {
			case "snapshot":
				snapshot, ok := frame["snapshot"].(map[string]any)
				if !ok {
					return errors.New("invalid firewall snapshot")
				}
				summary, ok := snapshot["summary"].(map[string]any)
				if !ok {
					return errors.New("invalid firewall summary")
				}
				seen = true
				if asJSON {
					return json.NewEncoder(d.Out).Encode(snapshot)
				}
				return renderFirewallSnapshot(d, snapshot, summary)
			case "tap_lost":
				_, err := fmt.Fprintln(d.Err, "Firewall telemetry interrupted. Waiting for a fresh snapshot.")
				return err
			case "error":
				return fmt.Errorf("firewall telemetry unavailable: %s", output.CellText(fieldText(frame, "message")))
			case "complete":
				note := "Firewall stream ended."
				if !seen {
					note = "Firewall stream ended without a live snapshot. Repository-free sandboxes have no saved flow summary."
				}
				_, err := fmt.Fprintln(d.Err, note)
				return err
			}
			return nil
		})
		return err
	})
	return c
}

func renderFirewallSnapshot(d Dependencies, snapshot, summary map[string]any) error {
	if err := clearMetricFrame(d); err != nil {
		return err
	}
	title := "Firewall snapshot " + output.CellText(fieldText(snapshot, "snapshot_ts"))
	if snapshot["partial"] == true {
		title += " (partial since " + output.CellText(fieldText(snapshot, "live_since_ts")) + ")"
	}
	if _, err := fmt.Fprintln(d.Out, title); err != nil {
		return err
	}
	dns := objectList(summary["blocked_dns_queries"])
	drops := objectList(summary["drops"])
	rows := [][]string{}
	for _, q := range dns {
		rows = append(rows, []string{fieldText(q, "qname"), billingText(q, "count", "1"), fieldText(q, "ts")})
	}
	if len(rows) > 0 {
		if err := output.Grid(d.Out, []string{"Denied DNS name", "Queries", "Last seen"}, rows); err != nil {
			return err
		}
	}
	rows = nil
	for _, drop := range drops {
		if billingText(drop, "packets", "0") != "0" {
			rows = append(rows, []string{fieldText(drop, "label"), fieldText(drop, "packets"), fieldText(drop, "bytes")})
		}
	}
	if len(rows) > 0 {
		return output.Grid(d.Out, []string{"IP drop rule", "Packets", "Bytes"}, rows)
	}
	if len(dns) == 0 {
		_, err := fmt.Fprintln(d.Out, "No firewall blocks observed.")
		return err
	}
	return nil
}
