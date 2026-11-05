package commands

import (
	"fmt"
	"net/url"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func newVMFork(d Dependencies) *cobra.Command {
	var org, name, ttl string
	var wait, asJSON bool
	var timeout int
	c := &cobra.Command{Use: "fork VM_ID_OR_NAME", Short: "Boot a copy of a PAUSED VM's disk; the source remains paused.", Args: positionalArguments("VM_ID_OR_NAME")}
	orgFlag(c, &org)
	c.Flags().StringVar(&name, "name", "", "Fork name; defaults to a name derived from the source.")
	c.Flags().StringVar(&ttl, "ttl", "", "Fork TTL, e.g. 8h or 7d. Default 8h, max 7d.")
	c.Flags().BoolVar(&wait, "wait", false, "Wait for the fork to become connectable.")
	c.Flags().IntVar(&timeout, "wait-timeout", 300, "Seconds to wait when --wait is set.")
	c.Flags().BoolVar(&asJSON, "json", false, "Emit the fork VM as JSON.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if timeout < 0 {
			return &ExitError{2, fmt.Sprintf("Error: Invalid value for '--wait-timeout': %d is not in the range x>=0.", timeout)}
		}
		body := map[string]any{}
		if cmd.Flags().Changed("name") {
			body["display_name"] = name
		}
		if cmd.Flags().Changed("ttl") {
			seconds, err := parseVMTTL(ttl)
			if err != nil {
				return err
			}
			body["ttl_seconds"] = seconds
		}
		cfg, api, orgID, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.TransitionVM(cmd.Context(), vmBase(orgID)+"/"+url.PathEscape(args[0])+"/fork", body)
		if err != nil {
			return apiError(cfg, err, "fork VM")
		}
		data, ok := record.Fields["data"].(map[string]any)
		if !ok {
			return &ExitError{1, "Error: unexpected API response: expected VM data"}
		}
		if !wait {
			if asJSON {
				return rawJSON(d, data)
			}
			return renderVM(d, data, "")
		}
		id := fieldText(data, "customer_vm_id")
		if id == "" {
			return &ExitError{1, "Error: unexpected API response: expected a fork VM ID"}
		}
		if _, err = fmt.Fprintf(d.Err, "Waiting up to %ds for %s to become RUNNING...\n", timeout, output.CellText(id)); err != nil {
			return err
		}
		final, disposition, err := waitForVM(cmd.Context(), d, cfg, api, vmBase(orgID)+"/"+url.PathEscape(id), timeout, vmConnectable, false, nil)
		if err != nil {
			return err
		}
		shown := final
		if len(shown) == 0 {
			shown = data
		}
		if asJSON {
			return vmWaitJSON(d, shown, nil, disposition)
		}
		if err = renderVM(d, shown, ""); err != nil {
			return err
		}
		return vmWaitExit(d, final, disposition, "connectable", timeout, id)
	})
	return c
}

func newVMRotateHTTPSLabel(d Dependencies) *cobra.Command {
	var org string
	var asJSON bool
	c := &cobra.Command{Use: "rotate-https-label VM_ID_OR_NAME", Short: "Replace public HTTPS URLs to invalidate leaked URLs.", Args: positionalArguments("VM_ID_OR_NAME")}
	orgFlag(c, &org)
	c.Flags().BoolVar(&asJSON, "json", false, "Emit the updated VM as JSON.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, orgID, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.TransitionVM(cmd.Context(), vmBase(orgID)+"/"+url.PathEscape(args[0])+"/https-label-rotations", map[string]any{})
		if err != nil {
			return apiError(cfg, err, "rotate VM HTTPS label")
		}
		data, ok := record.Fields["data"].(map[string]any)
		if !ok {
			return &ExitError{1, "Error: unexpected API response: expected VM data"}
		}
		if asJSON {
			return rawJSON(d, data)
		}
		return renderVM(d, data, "")
	})
	return c
}
