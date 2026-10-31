package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/client/wire"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

// NewFirewall preserves the public CLI's explicit repository scope and boolean JSON flags.
func NewFirewall(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	c := &cobra.Command{Use: "firewall", Short: "Manage the egress firewall rule list for orgs and repositories."}
	c.AddCommand(newFirewallList(d, false), newFirewallList(d, true), newFirewallAdd(d), newFirewallDelete(d), newFirewallMove(d), newFirewallDefault(d), newFirewallFlows(d))
	return c
}
func firewallScopeFlags(c *cobra.Command, org, repo *string, required bool) {
	orgFlag(c, org)
	c.Flags().StringVar(repo, "repo", "", "Repository ID. If provided, uses repository scope.")
	if required {
		_ = c.MarkFlagRequired("repo")
	}
}
func firewallBase(org, repo string) string {
	path := "/orgs/" + url.PathEscape(org)
	if repo != "" {
		path += "/repos/" + url.PathEscape(repo)
	}
	return path + "/firewall"
}
func rawJSON(d Dependencies, value any) error {
	encoder := json.NewEncoder(d.Out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
func firewallRulesOutput(d Dependencies, rows []map[string]any, asJSON bool) error {
	if rows == nil {
		rows = []map[string]any{}
	}
	if asJSON {
		return rawJSON(d, rows)
	}
	if len(rows) == 0 {
		_, err := fmt.Fprintln(d.Out, "No data found.")
		return err
	}
	cells := [][]string{}
	for _, row := range rows {
		matcher := fieldText(row, "cidr")
		if matcher == "" {
			matcher = fieldText(row, "fqdn")
		}
		if matcher == "" {
			matcher = "?"
		}
		def := "no"
		if row["is_default"] == true {
			def = "yes"
			matcher = "*"
		}
		ports := "-"
		start, end := row["port_start"], row["port_end"]
		switch {
		case start == nil && end == nil:
		case start == nil:
			ports = pythonValue(end)
		case end == nil || pythonValue(start) == pythonValue(end):
			ports = pythonValue(start)
		default:
			ports = pythonValue(start) + "-" + pythonValue(end)
		}
		protocol := fieldText(row, "protocol")
		if protocol == "" {
			protocol = "any"
		}
		cells = append(cells, []string{organizationCell(row, "position"), organizationCell(row, "rule_id"), organizationCell(row, "action"), matcher, protocol, ports, def})
	}
	return output.Grid(d.Out, []string{"#", "ID", "Action", "Match", "Proto", "Ports", "Default"}, cells)
}
func newFirewallList(d Dependencies, resolved bool) *cobra.Command {
	var org, repo string
	var asJSON bool
	name, short := "list", "List egress firewall rules for the given scope."
	if resolved {
		name = "show"
		short = "Show the resolved (merged) firewall rule list for a repository."
	}
	c := &cobra.Command{Use: name, Short: short, Args: cobra.NoArgs}
	firewallScopeFlags(c, &org, &repo, resolved)
	c.Flags().BoolVar(&asJSON, "json", false, "Output rules as JSON instead of a table.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		base := firewallBase(id, repo)
		if resolved {
			// Unlike list, an explicitly empty required --repo still occupies its URL slot.
			base = "/orgs/" + url.PathEscape(id) + "/repos/" + url.PathEscape(repo) + "/firewall"
			record, err := api.ResolvedFirewall(cmd.Context(), base+"/resolved")
			if err != nil {
				return apiError(cfg, err, "show resolved firewall rules")
			}
			return firewallRulesOutput(d, objectList(record.Fields["rules"]), asJSON)
		}
		page, err := api.FirewallRules(cmd.Context(), base+"/rules")
		if err != nil {
			return apiError(cfg, err, "list egress firewall rules")
		}
		return firewallRulesOutput(d, rawRecords(page), asJSON)
	})
	return c
}
func validFirewallCIDR(value string) bool {
	if _, err := netip.ParsePrefix(value); err == nil {
		return true
	}
	if _, err := netip.ParseAddr(value); err == nil {
		return true
	}
	address, mask, ok := strings.Cut(value, "/")
	if !ok {
		return false
	}
	ip, err := netip.ParseAddr(address)
	if err != nil || !ip.Is4() {
		return false
	}
	parsed, err := netip.ParseAddr(mask)
	if err != nil || !parsed.Is4() {
		return false
	}
	raw := parsed.As4()
	_, bits := net.IPMask(raw[:]).Size()
	if bits == 32 {
		return true
	}
	for i := range raw {
		raw[i] = ^raw[i]
	}
	_, bits = net.IPMask(raw[:]).Size()
	return bits == 32
}
func firewallPorts(value string, specified bool) (*int, *int, error) {
	if !specified {
		return nil, nil, nil
	}
	first, last, span := strings.Cut(value, "-")
	if !span {
		last = first
	}
	values := []int{}
	for _, text := range []string{first, last} {
		trimmed := strings.TrimSpace(text)
		if !settingInteger().MatchString(trimmed) {
			return nil, nil, &ExitError{2, "Error: Invalid --ports " + pythonQuote(value) + ": must be 'N' or 'N-M'"}
		}
		port, err := strconv.Atoi(strings.ReplaceAll(trimmed, "_", ""))
		if err != nil || port < 1 || port > 65535 {
			return nil, nil, &ExitError{2, "Error: Invalid --ports " + pythonQuote(value) + ": ports must be in 1..65535"}
		}
		values = append(values, port)
	}
	if values[0] > values[1] {
		return nil, nil, &ExitError{2, "Error: Invalid --ports " + pythonQuote(value) + ": start must be <= end"}
	}
	return &values[0], &values[1], nil
}
func firewallAction(c *cobra.Command, action *string) {
	c.Flags().StringVar(action, "action", "", "Rule action: allow or deny.")
	_ = c.MarkFlagRequired("action")
}
func validateFirewallChoice(flag, value string, choices []string) error {
	if slices.Contains(choices, value) {
		return nil
	}
	return &ExitError{2, "Error: Invalid value for '--" + flag + "': " + pythonQuote(output.CellText(value)) + " is not one of " + strings.Join(choices, ", ")}
}
func newFirewallAdd(d Dependencies) *cobra.Command {
	var org, repo, action, cidr, fqdn, protocol, ports string
	var anyMatcher bool
	var position int
	c := &cobra.Command{Use: "add", Short: "Add a rule. Exactly one of --cidr, --fqdn, --any must be specified.", Args: cobra.NoArgs}
	firewallScopeFlags(c, &org, &repo, false)
	firewallAction(c, &action)
	c.Flags().StringVar(&cidr, "cidr", "", "Destination CIDR.")
	c.Flags().StringVar(&fqdn, "fqdn", "", "Destination hostname.")
	c.Flags().BoolVar(&anyMatcher, "any", false, "Catch-all (default) rule.")
	c.Flags().StringVar(&protocol, "proto", "any", "Protocol: tcp, udp, icmp, or any.")
	c.Flags().StringVar(&ports, "ports", "", "Port or port range.")
	c.Flags().IntVar(&position, "position", 0, "Insert at a specific 0-indexed position.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if err := validateFirewallChoice("action", action, []string{"allow", "deny"}); err != nil {
			return err
		}
		if err := validateFirewallChoice("proto", protocol, []string{"tcp", "udp", "icmp", "any"}); err != nil {
			return err
		}
		targets := 0
		for _, set := range []bool{cidr != "", fqdn != "", anyMatcher} {
			if set {
				targets++
			}
		}
		if targets != 1 {
			return &ExitError{2, "Error: Exactly one of --cidr, --fqdn, or --any must be provided"}
		}
		if cidr != "" && !validFirewallCIDR(cidr) {
			return &ExitError{2, "Error: Invalid --cidr: " + pythonQuote(output.CellText(cidr)) + " does not appear to be an IPv4 or IPv6 network"}
		}
		start, end, err := firewallPorts(ports, cmd.Flags().Changed("ports"))
		if err != nil {
			return err
		}
		if start != nil && protocol != "tcp" && protocol != "udp" {
			return &ExitError{2, "Error: --ports requires --proto tcp or --proto udp"}
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		proto := wire.EgressRuleProtocol(protocol)
		body := wire.EgressRuleCreate{Action: wire.EgressRuleAction(action), IsDefault: &anyMatcher, Protocol: &proto}
		body.Cidr.SetNull()
		body.Fqdn.SetNull()
		body.PortStart.SetNull()
		body.PortEnd.SetNull()
		if cmd.Flags().Changed("cidr") {
			body.Cidr.Set(cidr)
		}
		if cmd.Flags().Changed("fqdn") {
			body.Fqdn.Set(fqdn)
		}
		if start != nil {
			body.PortStart.Set(*start)
			body.PortEnd.Set(*end)
		}
		if cmd.Flags().Changed("position") {
			body.Position.Set(position)
		}
		rule, err := api.CreateFirewallRule(cmd.Context(), firewallBase(id, repo)+"/rules", body)
		if err != nil {
			return apiError(cfg, err, "add egress firewall rule")
		}
		_, err = fmt.Fprintf(d.Out, "Added rule %s at position %s\n", output.CellText(pythonValue(rule.Fields["rule_id"])), output.CellText(pythonValue(rule.Fields["position"])))
		return err
	})
	return c
}
func newFirewallDelete(d Dependencies) *cobra.Command {
	var org, repo string
	c := &cobra.Command{Use: "delete RULE_ID", Short: "Delete a rule by ID.", Args: positionalArguments("RULE_ID")}
	firewallScopeFlags(c, &org, &repo, false)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if err = api.DeleteFirewallRule(cmd.Context(), firewallBase(id, repo)+"/rules/"+url.PathEscape(args[0])); err != nil {
			return apiError(cfg, err, "delete egress firewall rule")
		}
		_, err = fmt.Fprintln(d.Out, "Deleted rule "+output.CellText(args[0]))
		return err
	})
	return c
}
func newFirewallMove(d Dependencies) *cobra.Command {
	var org, repo string
	var to int
	c := &cobra.Command{Use: "move RULE_ID", Short: "Move a rule to a new position (rewrites the full ordering atomically).", Args: positionalArguments("RULE_ID")}
	firewallScopeFlags(c, &org, &repo, false)
	c.Flags().IntVar(&to, "to", 0, "Target 0-indexed position.")
	_ = c.MarkFlagRequired("to")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		path := firewallBase(id, repo) + "/rules"
		page, err := api.FirewallRules(cmd.Context(), path)
		if err != nil {
			return apiError(cfg, err, "fetch rules for reorder")
		}
		ids := []string{}
		for _, row := range rawRecords(page) {
			ids = append(ids, fieldText(row, "rule_id"))
		}
		from := slices.Index(ids, args[0])
		if from < 0 {
			return &ExitError{2, "Error: Rule " + output.CellText(args[0]) + " not found in this scope"}
		}
		if to < 0 || to >= len(ids) {
			return &ExitError{2, fmt.Sprintf("Error: Target position must be in 0..%d", len(ids)-1)}
		}
		ids = slices.Delete(ids, from, from+1)
		ids = slices.Insert(ids, to, args[0])
		if err = api.ReorderFirewallRules(cmd.Context(), path+"/reorder", ids); err != nil {
			return apiError(cfg, err, "reorder egress firewall rules")
		}
		_, err = fmt.Fprintf(d.Out, "Moved %s to position %d\n", output.CellText(args[0]), to)
		return err
	})
	return c
}
func newFirewallDefault(d Dependencies) *cobra.Command {
	var org, repo, action string
	c := &cobra.Command{Use: "set-default", Short: "Set (or replace) the catch-all (default) rule for the scope.", Args: cobra.NoArgs}
	firewallScopeFlags(c, &org, &repo, false)
	firewallAction(c, &action)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if err := validateFirewallChoice("action", action, []string{"allow", "deny"}); err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		path := firewallBase(id, repo) + "/rules"
		page, err := api.FirewallRules(cmd.Context(), path)
		if err != nil {
			return apiError(cfg, err, "fetch rules before setting default")
		}
		rid := ""
		found := false
		for _, row := range rawRecords(page) {
			if row["is_default"] == true {
				found = true
				if fieldText(row, "rule_id") == "" {
					return errors.New("unexpected API response: missing firewall rule ID")
				}
				body := wire.EgressRuleUpdate{}
				body.Action.Set(wire.EgressRuleAction(action))
				rule, e := api.UpdateFirewallRule(cmd.Context(), path+"/"+url.PathEscape(fieldText(row, "rule_id")), body)
				if e != nil {
					return apiError(cfg, e, "update default rule")
				}
				rid = fieldText(rule.Fields, "rule_id")
				break
			}
		}
		if !found {
			def := true
			proto := wire.EgressRuleProtocol("any")
			rule, e := api.CreateFirewallRule(cmd.Context(), path, wire.EgressRuleCreate{Action: wire.EgressRuleAction(action), IsDefault: &def, Protocol: &proto})
			if e != nil {
				return apiError(cfg, e, "create default rule")
			}
			rid = fieldText(rule.Fields, "rule_id")
		}
		if rid == "" {
			return errors.New("unexpected API response: missing firewall rule ID")
		}
		_, err = fmt.Fprintf(d.Out, "Set default rule: %s (rule_id=%s)\n", action, output.CellText(rid))
		return err
	})
	return c
}
func newFirewallFlows(d Dependencies) *cobra.Command {
	var org, repo, job, after, before string
	var drops, asJSON bool
	var limit, offset int
	c := &cobra.Command{Use: "flow-summaries", Short: "Show per-VM network activity summaries captured at VM stop.", Args: cobra.NoArgs}
	firewallScopeFlags(c, &org, &repo, true)
	c.Flags().StringVar(&job, "job", "", "Filter to every VM execution attempt for a job ID.")
	c.Flags().StringVar(&job, "job-id", "", "Alias for --job.")
	c.Flags().BoolVar(&drops, "with-drops", false, "Show only summaries with blocked flows.")
	c.Flags().IntVarP(&limit, "limit", "L", 20, "Max summaries to return (1–1000).")
	c.Flags().IntVar(&offset, "offset", 0, "Number of summaries to skip.")
	c.Flags().StringVar(&after, "from", "", "Only include summaries started at or after this timestamp.")
	c.Flags().StringVar(&after, "start-after", "", "Alias for --from.")
	c.Flags().StringVar(&before, "to", "", "Only include summaries ended at or before this timestamp.")
	c.Flags().StringVar(&before, "end-before", "", "Alias for --to.")
	c.Flags().BoolVar(&asJSON, "json", false, "Emit raw JSON instead of a table.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		if offset < 0 {
			return &ExitError{2, "Error: --offset must be at least 0."}
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		query := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}, "only_with_drops": {strconv.FormatBool(drops)}}
		if job != "" {
			query.Set("job_id", job)
		}
		if after != "" {
			query.Set("start_after", after)
		}
		if before != "" {
			query.Set("end_before", before)
		}
		record, err := api.FirewallFlows(cmd.Context(), "/orgs/"+url.PathEscape(id)+"/repos/"+url.PathEscape(repo)+"/firewall/vm-flow-summaries", query)
		if err != nil {
			return apiError(cfg, err, "list flow summaries")
		}
		if asJSON {
			return rawJSON(d, record.Fields)
		}
		rows := objectList(record.Fields["results"])
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No flow summaries yet. They are captured at VM stop, so wait until a run finishes.")
			return err
		}
		cells := [][]string{}
		for _, row := range rows {
			blocked := []string{}
			for _, drop := range objectList(row["drops"]) {
				blocked = append(blocked, pythonValue(drop["label"])+"("+billingText(drop, "packets", "0")+" pkt)")
			}
			for _, dns := range objectList(row["blocked_dns_queries"]) {
				blocked = append(blocked, "DNS:"+pythonValue(dns["qname"])+"("+billingText(dns, "count", "1")+")")
			}
			destinations := []string{}
			top := objectList(row["top_destinations"])
			for _, dst := range top[:min(3, len(top))] {
				destinations = append(destinations, pythonValue(fallback(dst["dst_fqdn"], pythonValue(fallback(dst["dst_ip"], "-")))))
			}
			denied := strings.Join(blocked, ", ")
			if denied == "" {
				denied = "-"
			}
			cells = append(cells, []string{pythonValue(fallback(row["vm_id"], "-")), pythonValue(fallback(row["end_ts"], "-")), billingText(row, "duration_s", "0") + "s",
				billingText(row, "bytes_egress", "0") + " B / " + billingText(row, "packets_egress", "0") + " pkt",
				billingText(row, "bytes_ingress", "0") + " B / " + billingText(row, "packets_ingress", "0") + " pkt",
				billingText(row, "flow_count", "0"), strings.Join(destinations, ", "), denied})
		}
		if err = output.Grid(d.Out, []string{"VM", "Ended", "Run", "Egress", "Ingress", "Flows", "Top destinations", "Blocked"}, cells); err != nil {
			return err
		}
		if record.Fields["has_more"] == true {
			_, err = fmt.Fprintf(d.Err, "\nMore results available. Re-run with --offset %d\n", offset+len(rows))
		}
		return err
	})
	return c
}
