package commands

import (
	"fmt"
	"github.com/avrea-com/cli/client/wire"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
	"net/url"
	"slices"
	"strings"
)

func newVMLifecycle(d Dependencies, operation string) *cobra.Command {
	var org string
	var wait, asJSON, memory, discard, yes bool
	var timeout int
	summaries := map[string]string{
		"pause":  "Pause a RUNNING Linux x86_64 VM while preserving its disk.",
		"resume": "Resume a PAUSED VM from its preserved snapshot.",
		"delete": "Delete a VM. Asynchronous while live: shows DELETING until the node confirms the stop.",
	}
	c := &cobra.Command{Use: operation + " VM_ID_OR_NAME", Short: summaries[operation], Args: positionalArguments("VM_ID_OR_NAME")}
	orgFlag(c, &org)
	c.Flags().BoolVar(&wait, "wait", false, "Wait for the requested VM state.")
	c.Flags().IntVar(&timeout, "wait-timeout", 300, "Seconds to wait when --wait is set.")
	c.Flags().BoolVar(&asJSON, "json", false, "Emit the raw API response as JSON.")
	if operation == "pause" {
		c.Flags().BoolVar(&memory, "memory", false, "Include guest memory and device state.")
	}
	if operation == "resume" {
		c.Flags().BoolVar(&discard, "discard-memory", false, "Fresh-boot the preserved disk without restoring saved memory.")
	}
	if operation == "delete" {
		c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")
	}
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, orgID, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		id := args[0]
		path := vmBase(orgID) + "/" + url.PathEscape(id)
		target := "RUNNING"
		var response map[string]any
		switch operation {
		case "pause":
			target = "PAUSED"
			record, e := api.TransitionVM(cmd.Context(), path+"/pause", wire.CustomerVmPauseRequest{Memory: &memory})
			response, err = record.Fields, e
		case "resume":
			record, e := api.TransitionVM(cmd.Context(), path+"/resume", wire.CustomerVmResumeRequest{DiscardMemory: &discard})
			response, err = record.Fields, e
		case "delete":
			target = "DELETED"
			if !yes {
				if err = confirmMutation(cmd.Context(), d, "deleting a VM", "Delete VM "+output.CellText(id)+"? This is permanent."); err != nil {
					return err
				}
			}
			record, e := api.DeleteVM(cmd.Context(), path)
			response, err = record.Fields, e
		}
		if err != nil {
			return vmError(cfg, err, operation+" VM")
		}
		data, _ := response["data"].(map[string]any)
		if !wait {
			if asJSON {
				return rawJSON(d, response)
			}
			if operation == "delete" {
				_, err = fmt.Fprintf(d.Out, "VM %s is now %s.\n", output.CellText(id), output.CellText(billingText(data, "state", "DELETING")))
				return err
			}
			if len(data) > 0 {
				return renderVM(d, data, "")
			}
			return nil
		}
		ready := func(row map[string]any) bool { return strings.ToUpper(fieldText(row, "state")) == target }
		var failed func(map[string]any) bool
		if operation != "delete" {
			allowed := []string{"PAUSING", "PAUSED"}
			if operation == "resume" {
				allowed = []string{"PENDING", "STARTING", "RUNNING"}
				ready = vmConnectable
			}
			failed = func(row map[string]any) bool {
				return !slices.Contains(allowed, strings.ToUpper(fieldText(row, "state")))
			}
		}
		label := "become " + target
		if operation == "delete" {
			label = "be deleted"
		}
		if _, err = fmt.Fprintf(d.Err, "Waiting up to %ds for %s to %s...\n", timeout, output.CellText(id), label); err != nil {
			return err
		}
		state, disposition, err := waitForVM(cmd.Context(), d, cfg, api, path, timeout, ready, operation == "delete", failed)
		if err != nil {
			return err
		}
		if operation == "delete" {
			return vmDeleteOutcome(d, id, state, disposition, timeout, asJSON)
		}
		shown := state
		if len(shown) == 0 {
			shown = data
		}
		if asJSON {
			return vmWaitJSON(d, shown, nil, disposition)
		}
		if len(shown) > 0 {
			if _, err = fmt.Fprintln(d.Out); err != nil {
				return err
			}
			if err = renderVM(d, shown, ""); err != nil {
				return err
			}
		}
		return vmWaitExit(d, state, disposition, target, timeout, id)
	})
	return c
}
func vmDeleteOutcome(d Dependencies, id string, state map[string]any, disposition string, timeout int, asJSON bool) error {
	gone := disposition == "ready"
	if asJSON {
		value := map[string]any{"customer_vm_id": id, "deleted": gone, "disposition": disposition}
		if disposition == "failed" {
			value["state"] = state["state"]
			value["state_reason"] = state["state_reason"]
		}
		if err := rawJSON(d, value); err != nil {
			return err
		}
	} else {
		var message string
		switch {
		case gone:
			message = "VM " + id + " deleted."
		case disposition == "failed":
			message = vmFailureText(state, id)
		default:
			message = fmt.Sprintf("Still deleting after %ds. Check with: avr vm show %s", timeout, id)
		}
		if _, err := fmt.Fprintln(d.Out, output.CellText(message)); err != nil {
			return err
		}
	}
	if !gone {
		return &ExitError{1, ""}
	}
	return nil
}
