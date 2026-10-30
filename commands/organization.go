package commands

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var memberFields = []string{"user_id", "name", "role", "joined_at"}
var emailDomainFields = []string{"organization_email_domain_id", "domain", "created_at", "verified", "verified_at", "dns_record_name", "dns_record_value"}

func organizationContext(ctx context.Context, d Dependencies, org string) (config.Config, *client.Client, string, error) {
	cfg, _, api, err := d.load()
	if err != nil {
		return cfg, api, "", err
	}
	if err = authenticated(cfg); err != nil {
		return cfg, api, "", err
	}
	id, err := resolveOrg(ctx, d, cfg, api, org)
	return cfg, api, id, err
}
func orgFlag(cmd *cobra.Command, org *string) {
	cmd.Flags().StringVar(org, "org", "", "Organization ID or slug; defaults to the configured organization.")
}
func newOrgCreate(d Dependencies) *cobra.Command {
	var o jsonOptions
	c := &cobra.Command{Use: "create NAME", Short: "Create a new organization.", Args: positionalArguments("NAME")}
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, orgFields); done || err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		record, err := api.CreateOrganization(cmd.Context(), args[0])
		if err != nil {
			return apiError(cfg, err, "create organization")
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, record.Fields, orgFields)
		}
		return output.KeyValues(d.Out, [][2]string{
			{"Organization ID", pythonValue(record.Fields["organization_id"])},
			{"Name", pythonValue(record.Fields["name"])}, {"Slug", pythonValue(record.Fields["slug"])}, {"Role", pythonValue(record.Fields["role"])},
		})
	})
	return c
}
func organizationCell(row map[string]any, key string) string {
	if value, ok := row[key]; ok {
		return pythonValue(value)
	}
	return "N/A"
}
func newOrgMembers(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "members", Short: "List organization members (admin only).", Args: cobra.NoArgs}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := o.prepare(cmd, d, memberFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		page, err := api.OrganizationMembers(cmd.Context(), id)
		if err != nil {
			return apiError(cfg, err, "list members")
		}
		rows := rawRecords(page)
		if cmd.Flags().Changed("json") {
			return o.list(cmd, d, rows, memberFields)
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No data found.")
			return err
		}
		var cells [][]string
		for _, row := range rows {
			cells = append(cells, []string{organizationCell(row, "user_id"), organizationCell(row, "name"), organizationCell(row, "role"), formatTimestamp(fieldText(row, "joined_at"))})
		}
		return output.Grid(d.Out, []string{"User ID", "Name", "Role", "Joined"}, cells)
	})
	return c
}
func emailDomainBase(org string) string { return "/orgs/" + url.PathEscape(org) + "/email-domains" }
func newEmailDomains(d Dependencies) *cobra.Command {
	c := &cobra.Command{Use: "email-domain", Short: "Claim and verify organization email domains."}
	c.AddCommand(newEmailDomainList(d), newEmailDomainMutation(d, false), newEmailDomainMutation(d, true), newEmailDomainSet(d))
	return c
}
func domainStatus(row map[string]any) string {
	if row["verified"] == true {
		return "Verified"
	}
	return "Pending"
}
func newEmailDomainList(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List claimed organization email domains (admin only).", Args: cobra.NoArgs}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := o.prepare(cmd, d, emailDomainFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		page, err := api.EmailDomains(cmd.Context(), emailDomainBase(id))
		if err != nil {
			return apiError(cfg, err, "list email domains")
		}
		rows := rawRecords(page)
		if cmd.Flags().Changed("json") {
			return o.list(cmd, d, rows, emailDomainFields)
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No data found.")
			return err
		}
		var cells [][]string
		for _, row := range rows {
			dns := "-"
			if name, value := fieldText(row, "dns_record_name"), fieldText(row, "dns_record_value"); name != "" && value != "" {
				dns = name + " TXT " + value
			}
			cells = append(cells, []string{organizationCell(row, "domain"), domainStatus(row), dns, organizationCell(row, "organization_email_domain_id"), formatTimestamp(fieldText(row, "created_at"))})
		}
		return output.Grid(d.Out, []string{"Domain", "Status", "DNS Record", "ID", "Created"}, cells)
	})
	return c
}
func newEmailDomainMutation(d Dependencies, verify bool) *cobra.Command {
	var org string
	var o jsonOptions
	verb, short := "claim", "Claim a company domain using DNS ownership verification."
	if verify {
		verb, short = "verify", "Check a claimed domain's DNS TXT record."
	}
	c := &cobra.Command{Use: verb + " DOMAIN", Short: short, Args: positionalArguments("DOMAIN")}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, emailDomainFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		path := emailDomainBase(id) + "/claim"
		var body any = map[string]any{"domain": args[0]}
		if verify {
			path = emailDomainBase(id) + "/" + url.PathEscape(args[0]) + "/verify"
			body = nil
		}
		record, err := api.MutateEmailDomain(cmd.Context(), path, body)
		if err != nil {
			return apiError(cfg, err, verb+" email domain")
		}
		row := record.Fields
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, row, emailDomainFields)
		}
		pairs := [][2]string{{"Domain", pythonValue(row["domain"])}, {"Status", domainStatus(row)}}
		if verify {
			pairs = append(pairs, [2]string{"Verified at", formatTimestamp(fieldText(row, "verified_at"))})
		} else {
			name, value, kind := fieldText(row, "dns_record_name"), fieldText(row, "dns_record_value"), "TXT"
			if name == "" {
				name, kind = "-", "-"
			}
			if value == "" {
				value = "-"
			}
			pairs = append(pairs, [2]string{"DNS record name", name}, [2]string{"DNS record type", kind}, [2]string{"DNS record value", value})
		}
		if err = output.KeyValues(d.Out, pairs); err != nil {
			return err
		}
		if !verify && row["verified"] != true {
			_, err = fmt.Fprintf(d.Out, "\nAfter publishing the TXT record, run:\n  avr org email-domain verify %s\n", output.CellText(fieldText(row, "domain")))
		}
		return err
	})
	return c
}
func newEmailDomainSet(d Dependencies) *cobra.Command {
	var org string
	var yes bool
	c := &cobra.Command{Use: "set DOMAINS...", Short: "Replace all organization email domains for automatic membership.", Args: positionalArguments("DOMAINS...")}
	orgFlag(c, &org)
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if !yes {
			prompt := fmt.Sprintf("Replace ALL email domains for %s with [%s]?", output.CellText(id), output.CellText(strings.Join(args, ", ")))
			if err = confirmMutation(cmd.Context(), d, "email-domain set replaces ALL existing domains", prompt); err != nil {
				return err
			}
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		page, err := api.SetEmailDomains(cmd.Context(), emailDomainBase(id), args)
		if err != nil {
			return apiError(cfg, err, "set email domains")
		}
		if _, err = fmt.Fprintf(d.Out, "Set %d email domain(s) for organization %s:\n", len(page.Items), output.CellText(id)); err != nil {
			return err
		}
		for _, row := range page.Items {
			if _, err = fmt.Fprintln(d.Out, "  "+output.CellText(fieldText(row.Fields, "domain"))); err != nil {
				return err
			}
		}
		return nil
	})
	return c
}
