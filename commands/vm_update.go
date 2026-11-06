package commands

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func newVMUpdate(d Dependencies) *cobra.Command {
	var org, name, ttl, egress string
	var keys, ports []string
	var clearPorts bool
	var rotate, asJSON bool
	c := &cobra.Command{Use: "update VM_ID_OR_NAME", Short: "Update a VM's name, TTL, or SSH keys, or rotate its password.", Args: positionalArguments("VM_ID_OR_NAME")}
	orgFlag(c, &org)
	c.Flags().StringVar(&name, "name", "", "New display name.")
	c.Flags().StringVar(&ttl, "ttl", "", "Extend the auto-stop window from now (e.g. 8h, 7d). Max 7d.")
	c.Flags().StringArrayVar(&keys, "ssh-key", nil, "Replace stored SSH public keys (literal or @path). Repeatable.")
	c.Flags().BoolVar(&rotate, "rotate-password", false, "Provision a fresh one-time password.")
	c.Flags().StringVar(&egress, "egress-rules", "", "Replace per-VM egress rules with a JSON array or @path.")
	c.Flags().BoolVar(&asJSON, "json", false, "Emit the raw API response as JSON.")
	c.Flags().StringArrayVar(&ports, "https-port", nil, "Replace public HTTPS ports; repeatable.")
	c.Flags().BoolVar(&clearPorts, "clear-https-ports", false, "Publish no HTTPS ports.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if clearPorts && len(ports) > 0 {
			return &ExitError{2, "Error: --clear-https-ports cannot be combined with --https-port."}
		}
		parsedPorts, err := parseVMHTTPSPorts(ports)
		if err != nil {
			return err
		}
		body := map[string]any{"rotate_password": rotate}
		if len(ports) > 0 || clearPorts {
			body["https_ports"] = parsedPorts
		}
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
		if len(keys) > 0 {
			resolved, err := resolveVMSSHKeys(keys)
			if err != nil {
				return err
			}
			body["ssh_public_keys"] = resolved
		}
		var rules *[]any
		if cmd.Flags().Changed("egress-rules") {
			value, err := parseVMEgress(egress)
			if err != nil {
				return err
			}
			rules = &value
		}
		if !rotate && !cmd.Flags().Changed("name") && !cmd.Flags().Changed("ttl") && len(keys) == 0 && rules == nil && len(ports) == 0 && !clearPorts {
			return &ExitError{2, "Error: Nothing to update. Pass --name, --ttl, --ssh-key, --rotate-password, --egress-rules, --https-port, or --clear-https-ports."}
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		record, err := api.UpdateVM(cmd.Context(), vmBase(id)+"/"+url.PathEscape(args[0]), body, rules)
		if err != nil {
			return vmError(cfg, err, "update VM")
		}
		if asJSON {
			return rawJSON(d, record)
		}
		data, _ := record["data"].(map[string]any)
		password := fieldText(data, "password")
		if vm, ok := data["vm"].(map[string]any); ok && len(vm) > 0 {
			if err = renderVM(d, vm, password); err != nil {
				return err
			}
		}
		if err := vmPrintPassword(d, password); err != nil {
			return err
		}
		for _, item := range []struct{ key, label, yes, no string }{
			{"egress_rules_applied", "Egress rules: ", "applied to the running guest.", "live enforcement unconfirmed; stored rules apply on next resume."},
			{"https_ports_applied", "HTTPS ports: ", "served by the running VM now.", "stored; they apply on next start or resume."},
		} {
			if data[item.key] != nil {
				text := item.no
				if data[item.key] == true {
					text = item.yes
				}
				if _, err := fmt.Fprintln(d.Out, item.label+text); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return c
}
