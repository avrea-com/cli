package commands

import (
	"net/url"

	"github.com/avrea-com/cli/client/wire"
	"github.com/spf13/cobra"
)

func newVMUpdate(d Dependencies) *cobra.Command {
	var org, name, ttl, egress string
	var keys []string
	var rotate, asJSON bool
	c := &cobra.Command{Use: "update VM_ID", Short: "Update a VM's name, TTL, or SSH keys, or rotate its password.", Args: positionalArguments("VM_ID")}
	orgFlag(c, &org)
	c.Flags().StringVar(&name, "name", "", "New display name.")
	c.Flags().StringVar(&ttl, "ttl", "", "Extend the auto-stop window from now (e.g. 8h, 7d). Max 7d.")
	c.Flags().StringArrayVar(&keys, "ssh-key", nil, "Replace stored SSH public keys (literal or @path). Repeatable.")
	c.Flags().BoolVar(&rotate, "rotate-password", false, "Provision a fresh one-time password.")
	c.Flags().StringVar(&egress, "egress-rules", "", "Replace per-VM egress rules with a JSON array or @path.")
	c.Flags().BoolVar(&asJSON, "json", false, "Emit the raw API response as JSON.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		body := wire.CustomerVmUpdateRequest{RotatePassword: &rotate}
		if cmd.Flags().Changed("name") {
			body.DisplayName.Set(name)
		}
		if cmd.Flags().Changed("ttl") {
			seconds, err := parseVMTTL(ttl)
			if err != nil {
				return err
			}
			body.TtlSeconds.Set(seconds)
		}
		if len(keys) > 0 {
			resolved, err := resolveVMSSHKeys(keys)
			if err != nil {
				return err
			}
			body.SshPublicKeys.Set(resolved)
		}
		var rules *[]any
		if cmd.Flags().Changed("egress-rules") {
			value, err := parseVMEgress(egress)
			if err != nil {
				return err
			}
			rules = &value
		}
		if !rotate && !cmd.Flags().Changed("name") && !cmd.Flags().Changed("ttl") && len(keys) == 0 && rules == nil {
			return &ExitError{2, "Error: Nothing to update. Pass --name, --ttl, --ssh-key, --rotate-password, or --egress-rules."}
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
			return rawJSON(d, record.Fields)
		}
		data, _ := record.Fields["data"].(map[string]any)
		password := fieldText(data, "password")
		if vm, ok := data["vm"].(map[string]any); ok && len(vm) > 0 {
			if err = renderVM(d, vm, password); err != nil {
				return err
			}
		}
		return vmPrintPassword(d, password)
	})
	return c
}
