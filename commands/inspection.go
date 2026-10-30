package commands

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var orgFields = []string{"organization_id", "name", "slug", "role"}
var repoFields = []string{"full_name", "platform", "platform_repository_id", "repository_id"}

type jsonOptions struct{ fields, expression string }

func (o *jsonOptions) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.fields, "json", "", "Output comma-separated fields, * for all, or ? to list fields.")
	cmd.Flags().StringVarP(&o.expression, "jq", "q", "", "Filter JSON with an embedded jq expression.")
}

func (o jsonOptions) prepare(cmd *cobra.Command, d Dependencies, available []string) (bool, error) {
	if o.expression != "" && !cmd.Flags().Changed("json") {
		return false, &ExitError{2, "Error: --jq requires --json"}
	}
	if !cmd.Flags().Changed("json") {
		return false, nil
	}
	if o.fields == "?" {
		return true, output.Discovery(d.Out, available)
	}
	_, err := output.Fields(o.fields, available)
	return false, err
}

func (o jsonOptions) list(cmd *cobra.Command, d Dependencies, records []map[string]any, available []string) error {
	fields, err := output.Fields(o.fields, available)
	if err != nil {
		return err
	}
	return output.JSONList(cmd.Context(), d.Out, records, fields, o.expression)
}

func (o jsonOptions) record(cmd *cobra.Command, d Dependencies, record map[string]any, available []string) error {
	fields, err := output.Fields(o.fields, available)
	if err != nil {
		return err
	}
	return output.JSON(cmd.Context(), d.Out, record, fields, o.expression)
}
func rawRecords[T any](page client.Page[T]) []map[string]any {
	rows := make([]map[string]any, 0, len(page.Items))
	for _, record := range page.Items {
		rows = append(rows, record.Fields)
	}
	return rows
}

func resolveOrg(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, option string) (string, error) {
	value := option
	if value == "" {
		value = cfg.Org
	}
	if strings.HasPrefix(value, "org-") {
		return value, nil
	}
	if config.IsScoped(cfg.Token) {
		return "", &ExitError{1, "Error: A scoped token is bound to one organization and cannot look it up.\nPass --org org-... or set AVR_ORG to the organization ID."}
	}
	page, err := api.Organizations(ctx)
	if err != nil {
		return "", apiError(cfg, err, "resolve the organization")
	}
	if value != "" {
		// Exact match first, then case-insensitive slugs, matching Python.
		for _, r := range page.Items {
			if r.Value.OrganizationId == value || r.Value.Slug == value {
				return r.Value.OrganizationId, nil
			}
		}
		for _, r := range page.Items {
			if strings.EqualFold(r.Value.Slug, value) {
				return r.Value.OrganizationId, nil
			}
		}
	} else if len(page.Items) == 1 {
		r := page.Items[0].Value
		_, _ = fmt.Fprintf(d.Err, "(using org: %s)\n", output.Text(r.Slug))
		return r.OrganizationId, nil
	}
	var message strings.Builder
	switch {
	case value != "":
		fmt.Fprintf(&message, "Error: No organization matching '%s'.", output.Text(value))
	case len(page.Items) == 0:
		message.WriteString("Error: No organizations found for your account.")
	default:
		message.WriteString("Error: No organization specified. Available orgs:")
	}
	for _, r := range page.Items {
		fmt.Fprintf(&message, "\n  - %s (%s)", output.Text(r.Value.Slug), output.Text(r.Value.OrganizationId))
	}
	if value == "" && len(page.Items) > 0 {
		message.WriteString("\n\nUse --org <slug|id> or set a default with: avr config set org <slug|id>")
	}
	return "", &ExitError{1, message.String()}
}

func orgSlug(ctx context.Context, cfg config.Config, api *client.Client, org string) string {
	if config.IsScoped(cfg.Token) {
		return org
	}
	page, err := api.Organizations(ctx)
	if err != nil {
		return org
	}
	for _, r := range page.Items {
		if r.Value.OrganizationId == org && r.Value.Slug != "" {
			return r.Value.Slug
		}
	}
	return org
}

func consoleURL(host string) string {
	u, err := url.Parse(host)
	if err != nil {
		return ""
	}
	u.Host = u.Hostname()
	if strings.HasPrefix(u.Host, "api.") {
		u.Host = strings.Replace(u.Host, "api.", "console.", 1)
	}
	u.Path = ""
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/")
}

func linksEnabled(cmd *cobra.Command, d Dependencies) bool {
	if !d.IsTerminal() {
		return false
	}
	if f := cmd.Flag("no-links"); f != nil && f.Value.String() == "true" {
		return false
	}
	if f := cmd.Flag("links"); f != nil && f.Value.String() == "false" {
		return false
	}
	return true
}

// NewOrg returns an independently owned public organization command group.
func NewOrg(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	group := &cobra.Command{Use: "org", Aliases: []string{"orgs"}, Short: "Manage organizations and installations."}
	var options jsonOptions
	cmd := &cobra.Command{Use: "list", Short: "List organizations you belong to.", Args: cobra.NoArgs}
	options.flags(cmd)
	cmd.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := options.prepare(cmd, d, orgFields); done || err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		page, err := api.Organizations(cmd.Context())
		if err != nil {
			return apiError(cfg, err, "fetch organizations")
		}
		if cmd.Flags().Changed("json") {
			return options.list(cmd, d, rawRecords(page), orgFields)
		}
		if len(page.Items) == 0 {
			_, err = fmt.Fprintln(d.Out, "No data found.")
			return err
		}
		rows := make([][]string, 0, len(page.Items))
		for _, record := range page.Items {
			r := record.Value
			def := ""
			if r.OrganizationId == cfg.Org {
				def = "yes"
			}
			rows = append(rows, []string{r.OrganizationId, r.Name, r.Slug, string(r.Role), def})
		}
		return output.Grid(d.Out, []string{"ID", "Name", "Slug", "Role", "Default"}, rows)
	})
	group.AddCommand(cmd, newOrgCreate(d), newOrgMembers(d), newEmailDomains(d), newSAML(d), newOrgInstall(d))
	return group
}

// NewRepo returns public repository inspection commands.
func NewRepo(deps Dependencies) *cobra.Command {
	d := deps.defaults()
	group := &cobra.Command{Use: "repo", Aliases: []string{"repos"}, Short: "Manage repositories."}
	var options jsonOptions
	var org string
	var limit int
	cmd := &cobra.Command{Use: "list", Short: "List repositories you can access in an organization.", Args: cobra.NoArgs}
	options.flags(cmd)
	cmd.Flags().StringVar(&org, "org", "", "Organization ID or slug.")
	cmd.Flags().IntVarP(&limit, "limit", "L", 100, "Maximum repositories to return (1–1000).")
	cmd.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 1000 {
			return &ExitError{2, "Error: --limit must be between 1 and 1000."}
		}
		if done, err := options.prepare(cmd, d, repoFields); done || err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		id, err := resolveOrg(cmd.Context(), d, cfg, api, org)
		if err != nil {
			return err
		}
		page, err := api.Repositories(cmd.Context(), id, url.Values{"limit": {strconv.Itoa(limit)}})
		if err != nil {
			return apiError(cfg, err, "list repositories")
		}
		if cmd.Flags().Changed("json") {
			return options.list(cmd, d, rawRecords(page), repoFields)
		}
		if len(page.Items) == 0 {
			_, err = fmt.Fprintln(d.Out, "No repositories found.")
			return err
		}
		rows := make([][]string, 0, len(page.Items))
		for _, record := range page.Items {
			fields := record.Fields
			rows = append(rows, []string{fieldText(fields, "full_name"), fieldText(fields, "repository_id"), fieldText(fields, "platform"), fieldText(fields, "platform_repository_id")})
		}
		if !d.IsTerminal() {
			return output.Table(d.Out, []string{"repository", "repository_id", "platform", "platform_repository_id"}, rows, true)
		}
		var links [][]string
		if linksEnabled(cmd, d) {
			slug := orgSlug(cmd.Context(), cfg, api, id)
			for _, row := range rows {
				target := consoleURL(cfg.Host) + "/org/" + url.PathEscape(slug) + "/activity?repositories=" + url.QueryEscape(row[1])
				platformTarget := ""
				if row[2] == "github" && row[0] != "" && row[3] != "" {
					platformTarget = "https://github.com/" + row[0]
				}
				links = append(links, []string{target, target, "", platformTarget})
			}
		}
		return d.repoTable(rows, links)
	})
	group.AddCommand(cmd, newPublicMirrors(d))
	return group
}

func fieldText(fields map[string]any, key string) string {
	value := fields[key]
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
