package commands

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var gitMirrorFields = []string{"repository_id", "full_name", "enabled", "placements"}
var gitClusterFields = []string{"cluster_id", "datacenter_id", "name"}
var repositorySegment = regexp.MustCompile("^[A-Za-z0-9_.-]+$")

func validateRepositoryName(value string) error {
	parts := strings.Split(value, "/")
	if len(parts) < 2 {
		return &ExitError{2, "Error: Invalid value for REPOSITORY: must be an owner/repository name"}
	}
	for _, part := range parts {
		if part == "." || part == ".." || !repositorySegment.MatchString(part) {
			return &ExitError{2, "Error: Invalid value for REPOSITORY: must be an owner/repository name"}
		}
	}
	return nil
}

func newGitMirrors(d Dependencies) *cobra.Command {
	c := &cobra.Command{Use: "mirror", Short: "Manage this repository's avrea-git mirror (feature-flagged)."}
	c.AddCommand(newGitMirrorCreate(d), newGitMirrorLocations(d))
	for _, verb := range []string{"status", "enable", "disable", "place", "unplace"} {
		c.AddCommand(newGitMirrorAction(d, verb))
	}
	return c
}
func mirrorBase(org, repo string) string {
	return "/orgs/" + url.PathEscape(org) + "/repos/" + url.PathEscape(repo) + "/git-mirrors"
}
func mirrorSourceFlag(c *cobra.Command, source *string) {
	c.Flags().StringVar(source, "source", "github", "Source platform: github or gitlab.")
	previous := c.Args
	c.Args = func(cmd *cobra.Command, args []string) error {
		if previous != nil {
			if err := previous(cmd, args); err != nil {
				return err
			}
		}
		if *source != "github" && *source != "gitlab" {
			return &ExitError{2, "Error: Invalid value for --source: choose github or gitlab."}
		}
		return nil
	}
}
func mirrorRepository(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, repo, source string) (string, error) {
	if repo == "" {
		repo = cfg.Repo
		if repo == "" {
			remote, err := d.GitRemote(ctx)
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if err == nil {
				repo, _ = parseGitRemote(remote)
			}
		}
		if repo == "" {
			return "", &ExitError{1, "Error: Could not determine repository. Run from a git directory, pass --repo <org/name>, or set AVR_REPO."}
		}
		if d.IsErrorTerminal() && d.Getenv("AVR_REPO") != repo {
			_, _ = fmt.Fprintln(d.Err, d.errorPaint("(using repo: "+output.CellText(repo)+")", "2"))
		}
	}
	return resolveRepository(ctx, d, cfg, api, org, repo, source)
}
func mirrorError(cfg config.Config, err error, action, source, hint, transport string) error {
	var he *client.HTTPError
	if !errors.As(err, &he) {
		var te *client.TransportError
		if errors.As(err, &te) {
			return &ExitError{1, "Error: " + transport}
		}
		return err
	}
	result := apiError(cfg, err, action)
	var hints []string
	if hint != "" && (he.Status == 404 || he.Status == 422) {
		hints = append(hints, hint)
	}
	if source == "gitlab" && he.Status == 404 {
		hints = append(hints, "GitLab repositories cannot be mirrored until GitLab mirroring is enabled for your organization.")
	}
	if len(hints) == 0 {
		return result
	}
	first, rest, found := strings.Cut(result.Error(), "\n")
	if found {
		rest = "\n" + rest
	}
	for _, value := range hints {
		first += "\n  Hint: " + value
	}
	return errors.New(first + rest)
}
func gitMirrorRead(ctx context.Context, cfg config.Config, api *client.Client, org, repo, source string) (map[string]any, error) {
	record, err := api.GitMirror(ctx, http.MethodGet, mirrorBase(org, repo), nil)
	if err != nil {
		return nil, mirrorError(cfg, err, "get git-mirror status", source, "", "Could not reach Avrea to get git-mirror status.")
	}
	return record.Fields, nil
}
func gitMirrorEnabled(ctx context.Context, cfg config.Config, api *client.Client, org, repo, source string, enabled bool) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	action := "disable git mirroring"
	if enabled {
		action = "enable git mirroring"
	}
	record, err := api.GitMirror(ctx, http.MethodPut, mirrorBase(org, repo), map[string]any{"enabled": enabled})
	if err != nil {
		return nil, mirrorError(cfg, err, action, source, "", "Could not reach Avrea to "+action+"; the update may have been applied. Check \x60avr repo mirror status\x60 or repeat the command safely.")
	}
	return record.Fields, nil
}
func gitMirrorPlace(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, repo, cluster, source string, afterEnable bool) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record, err := api.GitMirror(ctx, http.MethodPut, mirrorBase(org, repo)+"/placements/"+url.PathEscape(cluster), nil)
	if err != nil {
		var he *client.HTTPError
		if afterEnable && errors.As(err, &he) {
			message := "Mirroring is enabled, but placement failed."
			if he.Status == 429 || he.Status >= 500 && he.Status < 600 {
				message += " When the service recovers, rerun this create command to retry."
			}
			_, _ = fmt.Fprintln(d.Err, message)
		}
		prefix := "The"
		if afterEnable {
			prefix = "Mirroring is enabled, but the"
		}
		return nil, mirrorError(cfg, err, "place the git mirror", source, "run \x60avr repo mirror locations\x60 to list valid cluster ids", prefix+" placement response was lost. Repeat the command to retry safely.")
	}
	return record.Fields, nil
}
func gitClusters(ctx context.Context, cfg config.Config, api *client.Client, org string) ([]map[string]any, error) {
	rows := []map[string]any{}
	query := url.Values{"limit": {"1000"}, "order": {"cluster_id.asc"}}
	seen := map[string]bool{}
	for {
		page, err := api.GitClusters(ctx, org, query)
		if err != nil {
			message := err.Error()
			if strings.HasPrefix(message, "unexpected ") {
				return nil, &ExitError{1, "Error: " + strings.ToUpper(message[:1]) + message[1:] + "."}
			}
			return nil, mirrorError(cfg, err, "list mirror locations", "", "", "Could not reach Avrea to list mirror locations.")
		}
		rows = append(rows, rawRecords(page)...)
		if page.NextCursor == "" {
			return rows, nil
		}
		if seen[page.NextCursor] {
			return nil, &ExitError{1, "Error: Unexpected pagination response while listing mirror locations."}
		}
		seen[page.NextCursor] = true
		query.Set("cursor", page.NextCursor)
	}
}
func mirrorLocation(clusters []map[string]any, location string) (string, error) {
	for _, row := range clusters {
		if fieldText(row, "cluster_id") == location {
			return location, nil
		}
	}
	matches := map[string]bool{}
	for _, row := range clusters {
		id, ok := row["cluster_id"].(string)
		if !ok {
			continue
		}
		for _, key := range []string{"name", "datacenter_id"} {
			if value, ok := row[key].(string); ok && strings.EqualFold(value, location) {
				matches[id] = true
			}
		}
	}
	ids := make([]string, 0, len(matches))
	for id := range matches {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if len(ids) == 1 {
		return ids[0], nil
	}
	label := pythonQuote(output.CellText(location))
	if len(ids) > 1 {
		return "", &ExitError{1, "Error: Ambiguous mirror location " + label + "; use a cluster ID: " + output.CellText(strings.Join(ids, ", ")) + "."}
	}
	return "", &ExitError{1, "Error: Unknown mirror location " + label + ". Run \x60avr repo mirror locations\x60 to list available locations."}
}
func printGitMirror(d Dependencies, row map[string]any) error {
	if err := output.KeyValues(d.Out, [][2]string{{"Repository", pythonValue(fallback(row["full_name"], pythonValue(row["repository_id"])))}, {"Repository ID", pythonValue(row["repository_id"])}, {"Mirroring", map[bool]string{true: "enabled", false: "disabled"}[row["enabled"] == true]}}); err != nil {
		return err
	}
	placements, _ := row["placements"].([]any)
	if len(placements) == 0 {
		_, err := fmt.Fprintln(d.Out, "\nNo placements. Add one with \x60avr repo mirror place CLUSTER_ID\x60.")
		return err
	}
	var cells [][]string
	for _, value := range placements {
		p, ok := value.(map[string]any)
		if !ok {
			return errors.New("unexpected mirror placement response")
		}
		synced, configPushed := "-", "pending"
		if value := fieldText(p, "last_sync_at"); value != "" {
			synced = formatTimestamp(value)
		}
		if p["config_synced"] == true {
			configPushed = "yes"
		}
		cells = append(cells, []string{organizationCell(p, "cluster_id"), organizationCell(p, "role"), pythonValue(fallback(p["last_sync_status"], "pending")), synced, configPushed})
	}
	if _, err := fmt.Fprintln(d.Out); err != nil {
		return err
	}
	return output.Grid(d.Out, []string{"Cluster", "Role", "Last sync", "Synced at", "Config pushed"}, cells)
}
func newGitMirrorLocations(d Dependencies) *cobra.Command {
	var org string
	var o jsonOptions
	c := &cobra.Command{Use: "locations", Short: "List available mirror locations and their cluster IDs.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := o.prepare(cmd, d, gitClusterFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		rows, err := gitClusters(cmd.Context(), cfg, api, id)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("json") {
			return o.list(cmd, d, rows, gitClusterFields)
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No mirror locations available.")
			return err
		}
		var cells [][]string
		for _, row := range rows {
			cells = append(cells, []string{organizationCell(row, "cluster_id"), organizationCell(row, "datacenter_id"), organizationCell(row, "name")})
		}
		return output.Grid(d.Out, []string{"Cluster", "Datacenter", "Name"}, cells)
	})
	return c
}
func newGitMirrorCreate(d Dependencies) *cobra.Command {
	var org, source, location string
	var o jsonOptions
	c := &cobra.Command{Use: "create REPOSITORY", Short: "Enable mirroring and place a connected repository at a location.", Args: positionalArguments("REPOSITORY")}
	orgFlag(c, &org)
	mirrorSourceFlag(c, &source)
	o.flags(c)
	c.Flags().StringVar(&location, "location", "", "Cluster ID, datacenter ID, or unique name.")
	_ = c.MarkFlagRequired("location")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, gitMirrorFields); done || err != nil {
			return err
		}
		if err := validateRepositoryName(args[0]); err != nil {
			return err
		}
		if source == "github" && strings.Count(args[0], "/") != 1 {
			return &ExitError{2, "Error: Invalid value for REPOSITORY: GitHub repositories use owner/repo; for nested GitLab groups use --source gitlab."}
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		repo, err := resolveRepository(cmd.Context(), d, cfg, api, id, args[0], source)
		if err != nil {
			return err
		}
		clusters, err := gitClusters(cmd.Context(), cfg, api, id)
		if err != nil {
			return err
		}
		cluster, err := mirrorLocation(clusters, location)
		if err != nil {
			return err
		}
		row, err := gitMirrorRead(cmd.Context(), cfg, api, id, repo, source)
		if err != nil {
			return err
		}
		if row["enabled"] != true {
			if _, err = gitMirrorEnabled(cmd.Context(), cfg, api, id, repo, source, true); err != nil {
				return err
			}
		}
		row, err = gitMirrorPlace(cmd.Context(), d, cfg, api, id, repo, cluster, source, true)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("json") {
			return o.record(cmd, d, row, gitMirrorFields)
		}
		if err = printGitMirror(d, row); err != nil {
			return err
		}
		_, err = fmt.Fprintf(d.Out, "\nMirror configured at %s. Sync runs asynchronously.\nCheck progress: avr repo mirror status --repo %s --org %s\n", output.CellText(cluster), output.CellText(repo), output.CellText(id))
		return err
	})
	return c
}
func newGitMirrorAction(d Dependencies, verb string) *cobra.Command {
	var org, repo, source string
	var yes bool
	var o jsonOptions
	c := &cobra.Command{Use: verb, Short: map[string]string{"status": "Show the repository's git-mirror declaration and placements.", "enable": "Declare the repository mirrored into avrea-git.", "disable": "Stop mirroring the repository into avrea-git.", "place": "Place the repository's mirror in a git cluster.", "unplace": "Remove the repository's mirror from a git cluster."}[verb], Args: cobra.NoArgs}
	if verb == "place" || verb == "unplace" {
		c.Use += " CLUSTER_ID"
		c.Args = positionalArguments("CLUSTER_ID")
	}
	cacheScopeFlags(c, &org, &repo)
	mirrorSourceFlag(c, &source)
	o.flags(c)
	if verb == "disable" || verb == "unplace" {
		c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt.")
	}
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if done, err := o.prepare(cmd, d, gitMirrorFields); done || err != nil {
			return err
		}
		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		rid, err := mirrorRepository(cmd.Context(), d, cfg, api, id, repo, source)
		if err != nil {
			return err
		}
		var row map[string]any
		switch verb {
		case "status":
			row, err = gitMirrorRead(cmd.Context(), cfg, api, id, rid, source)
		case "enable", "disable":
			if verb == "disable" && !yes {
				if err = confirmMutation(cmd.Context(), d, "disabling the git mirror needs confirmation", "Disable git mirroring for "+output.CellText(rid)+"?"); err != nil {
					return err
				}
			}
			row, err = gitMirrorEnabled(cmd.Context(), cfg, api, id, rid, source, verb == "enable")
		case "place":
			row, err = gitMirrorPlace(cmd.Context(), d, cfg, api, id, rid, args[0], source, false)
		case "unplace":
			if !yes {
				if err = confirmMutation(cmd.Context(), d, "removing a git-mirror placement needs confirmation", "Remove the git-mirror placement in "+output.CellText(args[0])+"?"); err != nil {
					return err
				}
			}
			if err = cmd.Context().Err(); err != nil {
				return err
			}
			record, removeErr := api.GitMirror(cmd.Context(), http.MethodDelete, mirrorBase(id, rid)+"/placements/"+url.PathEscape(args[0]), nil)
			row = record.Fields
			if removeErr != nil {
				err = mirrorError(cfg, removeErr, "remove the git-mirror placement", source, "run \x60avr repo mirror status\x60 to see current placements", "Could not reach Avrea to remove the placement; check \x60avr repo mirror status\x60 before retrying.")
			}
		}
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("json") {
			if row == nil {
				row = map[string]any{}
			}
			return o.record(cmd, d, row, gitMirrorFields)
		}
		if verb == "unplace" && row == nil {
			_, err = fmt.Fprintf(d.Out, "Removed the git-mirror placement in %s.\n", output.CellText(args[0]))
			return err
		}
		return printGitMirror(d, row)
	})
	return c
}
