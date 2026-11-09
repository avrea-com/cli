package commands

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func resolveFirewallRepository(cmd *cobra.Command, d Dependencies, cfg config.Config, api *client.Client, org, repo string) (string, error) {
	if strings.TrimSpace(repo) == "" {
		return "", nil
	}
	return resolveRepository(cmd.Context(), d, cfg, api, org, repo, "")
}

func newFirewallEdit(d Dependencies) *cobra.Command {
	var org, repo, action, cidr, fqdn, protocol, ports string
	c := &cobra.Command{Use: "edit RULE_ID", Short: "Edit specified fields of an existing firewall rule.", Args: positionalArguments("RULE_ID")}
	firewallScopeFlags(c, &org, &repo, false)
	c.Flags().StringVar(&action, "action", "", "Rule action: allow or deny.")
	c.Flags().StringVar(&cidr, "cidr", "", "Destination IPv4 address or CIDR; replaces the hostname.")
	c.Flags().StringVar(&fqdn, "fqdn", "", "Destination hostname; replaces the CIDR.")
	c.Flags().StringVar(&protocol, "proto", "", "Protocol: tcp, udp, icmp, or any. Any/ICMP clears ports.")
	c.Flags().StringVar(&ports, "ports", "", "Port, range, or all to clear the port restriction.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		changed := cmd.Flags().Changed
		if changed("action") {
			if err := validateFirewallChoice("action", action, []string{"allow", "deny"}); err != nil {
				return err
			}
			body["action"] = action
		}
		if changed("cidr") && changed("fqdn") {
			return &ExitError{2, "Error: Specify either --cidr or --fqdn, not both."}
		}
		if changed("cidr") {
			if !validFirewallCIDR(cidr) {
				return &ExitError{2, "Error: --cidr must be an IPv4 address or network."}
			}
			body["cidr"] = cidr
			body["fqdn"] = nil
		}
		if changed("fqdn") {
			if strings.TrimSpace(fqdn) == "" {
				return &ExitError{2, "Error: --fqdn cannot be empty."}
			}
			body["fqdn"] = fqdn
			body["cidr"] = nil
		}
		if changed("proto") {
			if err := validateFirewallChoice("proto", protocol, []string{"tcp", "udp", "icmp", "any"}); err != nil {
				return err
			}
			body["protocol"] = protocol
			if protocol == "any" || protocol == "icmp" {
				body["port_start"] = nil
				body["port_end"] = nil
			}
		}
		if changed("ports") {
			start, end, err := firewallPorts(ports, true)
			if err != nil {
				return err
			}
			if start != nil && changed("proto") && protocol != "tcp" && protocol != "udp" {
				return &ExitError{2, "Error: --ports requires a TCP or UDP rule."}
			}
			body["port_start"] = start
			body["port_end"] = end
		}
		if len(body) == 0 {
			return &ExitError{2, "Error: Specify at least one field to edit."}
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		repo, err := resolveFirewallRepository(cmd, d, cfg, api, id, repo)
		if err != nil {
			return err
		}
		_, err = api.UpdateFirewallRule(cmd.Context(), firewallBase(id, repo)+"/rules/"+url.PathEscape(args[0]), body)
		if err != nil {
			return apiError(cfg, err, "edit firewall rule")
		}
		_, err = fmt.Fprintln(d.Out, "Updated rule "+output.CellText(args[0]))
		return err
	})
	return c
}
