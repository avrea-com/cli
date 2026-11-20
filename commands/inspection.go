package commands

import (
	"context"
	"errors"
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
	if _, err := output.Fields(o.fields, available); err != nil {
		return false, err
	}
	if o.expression != "" {
		if _, err := output.CompileJQ(o.expression); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (o jsonOptions) list(cmd *cobra.Command, d Dependencies, records []map[string]any, available []string) error {
	fields, err := output.Fields(o.fields, available)
	if err != nil {
		return err
	}
	return output.JSONList(cmd.Context(), d.Out, records, fields, o.expression, d.IsTerminal())
}

func (o jsonOptions) record(cmd *cobra.Command, d Dependencies, record map[string]any, available []string) error {
	fields, err := output.Fields(o.fields, available)
	if err != nil {
		return err
	}
	return output.JSON(cmd.Context(), d.Out, record, fields, o.expression, d.IsTerminal())
}
func rawRecords(page client.Page) []map[string]any { return page.Items }

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
	if config.IsService(cfg.Token) {
		if value != "" {
			return "", &ExitError{1, fmt.Sprintf("Error: A service token is bound to one organization and cannot look up '%s'.\nOmit --org and AVR_ORG to use the token's organization, or pass its ID (org-...).", output.Text(value))}
		}
		identity, err := serviceIdentity(ctx, cfg, api)
		if err != nil {
			return "", apiError(cfg, err, "resolve the service token's organization")
		}
		if id := fieldText(identity, "organization_id"); strings.HasPrefix(id, "org-") {
			return id, nil
		}
		return "", errors.New("unexpected API response: organization_id is missing")
	}
	page, err := api.Organizations(ctx)
	if err != nil {
		return "", apiError(cfg, err, "resolve the organization")
	}
	for _, r := range page.Items {
		rememberOrganizationSlug(ctx, cfg, fieldText(r, "organization_id"), fieldText(r, "slug"))
	}
	if value != "" {
		// Exact match first, then case-insensitive slugs, matching Python.
		for _, r := range page.Items {
			if fieldText(r, "organization_id") == value || fieldText(r, "slug") == value {
				return fieldText(r, "organization_id"), nil
			}
		}
		for _, r := range page.Items {
			if strings.EqualFold(fieldText(r, "slug"), value) {
				return fieldText(r, "organization_id"), nil
			}
		}
	} else if len(page.Items) == 1 {
		r := page.Items[0]
		_, _ = fmt.Fprintf(d.Err, "(using org: %s)\n", output.Text(fieldText(r, "slug")))
		return fieldText(r, "organization_id"), nil
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
		fmt.Fprintf(&message, "\n  - %s (%s)", output.Text(fieldText(r, "slug")), output.Text(fieldText(r, "organization_id")))
	}
	if value == "" && len(page.Items) > 0 {
		message.WriteString("\n\nUse --org <slug|id> or set a default with: avr config set org <slug|id>")
	}
	return "", &ExitError{1, message.String()}
}

func orgSlug(ctx context.Context, cfg config.Config, api *client.Client, org string) string {
	if slug := knownOrganizationSlug(ctx, cfg, org); slug != "" {
		return slug
	}
	if config.IsOrgBound(cfg.Token) {
		return org
	}
	page, err := api.Organizations(ctx)
	if err != nil {
		return org
	}
	for _, r := range page.Items {
		if fieldText(r, "organization_id") == org && fieldText(r, "slug") != "" {
			rememberOrganizationSlug(ctx, cfg, org, fieldText(r, "slug"))
			return fieldText(r, "slug")
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

// repositoryRefLink returns the page for a branch (kind "tree") or a commit of
// a run's repository. Mirrored repositories have those pages in the console.
// The rest link to the platform that hosts the repository, which an API
// without repository.platform implies through the CI provider.
func repositoryRefLink(orgBase, provider string, repo map[string]any, kind, ref string) string {
	if ref == "" {
		return ""
	}
	if id := fieldText(repo, "id"); orgBase != "" && id != "" && repo["mirror_enabled"] == true {
		return orgBase + "/repos/" + url.PathEscape(id) + "/" + kind + "/" + escapePathSegments(ref)
	}
	name := fieldText(repo, "full_name")
	if !strings.Contains(name, "/") {
		return ""
	}
	platform := fieldText(repo, "platform")
	if platform == "" {
		switch provider {
		case "", "github_actions":
			platform = "github"
		case "gitlab_ci":
			platform = "gitlab"
		}
	}
	switch platform {
	case "github":
		return "https://github.com/" + name + "/" + kind + "/" + escapePathSegments(ref)
	case "gitlab":
		return "https://gitlab.com/" + name + "/-/" + kind + "/" + escapePathSegments(ref)
	default:
		return ""
	}
}

// platformRunLink returns the label and URL of a run's page on its source
// platform. A GitLab run is a pipeline.
func platformRunLink(provider, fullName, platformRunID, attempt string) (string, string) {
	if fullName == "" || platformRunID == "" {
		return "", ""
	}
	switch provider {
	case "", "github_actions":
		target := "https://github.com/" + fullName + "/actions/runs/" + platformRunID
		if attempt != "" && attempt != "1" && attempt != "0" {
			target += "/attempts/" + attempt
		}
		return "GitHub", target
	case "gitlab_ci":
		return "GitLab", "https://gitlab.com/" + fullName + "/-/pipelines/" + platformRunID
	default:
		return "", ""
	}
}

func escapePathSegments(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
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
			r := record
			def := ""
			if fieldText(r, "organization_id") == cfg.Org {
				def = "yes"
			}
			rows = append(rows, []string{fieldText(r, "organization_id"), fieldText(r, "name"), fieldText(r, "slug"), fieldText(r, "role"), def})
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
	cmd.Flags().StringVarP(&org, "org", "o", "", "Organization ID or slug.")
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
			fields := record
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
				if row[0] != "" && row[3] != "" {
					switch row[2] {
					case "github":
						platformTarget = "https://github.com/" + row[0]
					case "gitlab":
						platformTarget = "https://gitlab.com/" + row[0]
					}
				}
				links = append(links, []string{target, target, "", platformTarget})
			}
		}
		return d.repoTable(rows, links)
	})
	group.AddCommand(cmd, newPublicMirrors(d), newGitMirrors(d), newRepoClone(d))
	return group
}

func fieldText(fields map[string]any, key string) string {
	value := fields[key]
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
