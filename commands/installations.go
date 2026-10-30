package commands

import (
	"context"
	"errors"
	"fmt"
	"github.com/avrea-com/cli/client"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var installationFields = []string{"installation_id", "platform_installation_id", "platform", "platform_settings_url", "target_id", "target_type", "target_name", "organization_name", "organization_slug", "state", "created_at", "configured_runner_count", "degraded_runner_count"}

func newOrgInstall(d Dependencies) *cobra.Command {
	c := &cobra.Command{Use: "install", Short: "Manage GitHub and GitLab installations."}
	c.AddCommand(newInstallationList(d), newInstallationAdd(d), newInstallationRemove(d), newGitLabSelection(d), newGitLabRotate(d), newGitLabRunner(d))
	return c
}
func newInstallationList(d Dependencies) *cobra.Command {
	var o jsonOptions
	c := &cobra.Command{Use: "list", Short: "List accessible installations across all your organizations.", Args: cobra.NoArgs}
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if done, err := o.prepare(cmd, d, installationFields); done || err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		page, err := api.Installations(cmd.Context())
		if err != nil {
			return gitLabAPIError(cfg, err, "fetch installations", "")
		}
		rows := rawRecords(page)
		fields, _ := output.Fields(o.fields, installationFields)
		if !cmd.Flags().Changed("json") || slices.Contains(fields, "configured_runner_count") || slices.Contains(fields, "degraded_runner_count") {
			summaries := map[string]map[string]any{}
			orgs := []string{}
			for _, row := range rows {
				if installationPlatform(row) == "gitlab" && fieldText(row, "organization_id") != "" {
					orgs = append(orgs, fieldText(row, "organization_id"))
				}
			}
			slices.Sort(orgs)
			for _, org := range slices.Compact(orgs) {
				result, e := api.Get(cmd.Context(), "/orgs/"+url.PathEscape(org)+"/gitlab-installations", nil)
				if e != nil {
					var he *client.HTTPError
					reason := "Avrea could not be reached"
					if errors.As(e, &he) {
						if he.Status == 401 {
							return apiError(cfg, e, "fetch GitLab runner counts")
						}
						if he.Status == 403 || he.Status == 404 {
							continue
						}
						reason = fmt.Sprintf("HTTP %d", he.Status)
					}
					if _, e = fmt.Fprintf(d.Err, "Warning: runner counts for %s are unavailable (%s).\n", output.CellText(org), reason); e != nil {
						return e
					}
					continue
				}
				for _, row := range gitLabRows(result["data"]) {
					summaries[fieldText(row, "installation_id")] = row
				}
			}
			for _, row := range rows {
				for _, key := range []string{"configured_runner_count", "degraded_runner_count"} {
					row[key] = summaries[fieldText(row, "installation_id")][key]
				}
			}
		}
		if cmd.Flags().Changed("json") {
			return o.list(cmd, d, rows, installationFields)
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No data found.")
			return err
		}
		var cells [][]string
		hasGitLab := false
		for _, row := range rows {
			hasGitLab = hasGitLab || installationPlatform(row) == "gitlab"
		}
		for _, row := range rows {
			org := fieldText(row, "organization_name")
			if org == "" {
				org = fieldText(row, "organization_slug")
			}
			if org == "" {
				org = "-"
			}
			platform := fieldText(row, "platform")
			switch strings.ToLower(platform) {
			case "github":
				platform = "GitHub"
			case "gitlab":
				platform = "GitLab"
			case "":
				platform = "-"
			}
			provider := row["platform_installation_id"]
			if provider == nil || provider == "" || provider == 0 {
				provider = row["target_id"]
			}
			if provider == nil || provider == "" || provider == 0 {
				provider = "-"
			}
			cellsRow := []string{organizationCell(row, "installation_id"), platform, pythonValue(provider), organizationCell(row, "target_name"), org, organizationCell(row, "state"), formatTimestamp(fieldText(row, "created_at"))}
			if hasGitLab {
				for _, key := range []string{"configured_runner_count", "degraded_runner_count"} {
					value := "-"
					if row[key] != nil {
						value = pythonValue(row[key])
					}
					cellsRow = append(cellsRow, value)
				}
			}
			cells = append(cells, cellsRow)
		}
		headers := []string{"Installation ID", "Platform", "Provider ID", "Target", "Organization", "State", "Created"}
		if hasGitLab {
			headers = append(headers, "Configured runners", "Degraded runners")
		}
		return output.Grid(d.Out, headers, cells)
	})
	return c
}
func newInstallationAdd(d Dependencies) *cobra.Command {
	var org, platform, group, selection string
	var projectIDs []int
	var tokenStdin bool
	var o jsonOptions
	var noBrowser bool
	var waitSeconds int
	c := &cobra.Command{Use: "add", Short: "Start the GitHub App installation flow.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().StringVar(&platform, "platform", "github", "Source platform to install.")
	c.Flags().StringVar(&group, "group-full-path", "", "GitLab group full path.")
	c.Flags().StringVar(&selection, "project-selection", "", "GitLab project selection mode.")
	c.Flags().IntSliceVar(&projectIDs, "project-id", nil, "GitLab project ID (repeatable).")
	c.Flags().BoolVar(&tokenStdin, "access-token-stdin", false, "Read the GitLab group access token from stdin.")
	o.flags(c)
	c.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open browser automatically.")
	c.Flags().IntVar(&waitSeconds, "wait-seconds", 120, "Seconds to wait for detection.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) (err error) {
		defer func() {
			if cmd.Context().Err() == context.Canceled {
				err = &ExitError{130, "\nAborted!"}
			}
		}()

		platform = strings.ToLower(platform)
		if platform != "github" && platform != "gitlab" {
			return &ExitError{2, "Error: Invalid value for '--platform': choose github or gitlab."}
		}
		if err = validateGitLabSelectionArgs(selection, projectIDs); err != nil {
			return err
		}
		available := installationFields
		if platform == "gitlab" {
			available = gitLabWriteFields
		}
		if done, e := o.prepare(cmd, d, available); done || e != nil {
			return e
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		if platform == "gitlab" {
			return addGitLabInstallation(cmd, d, cfg, api, org, group, selection, projectIDs, tokenStdin, o)
		}
		invalid := []string{}
		for _, flag := range []string{"group-full-path", "project-selection", "project-id", "access-token-stdin"} {
			if cmd.Flags().Changed(flag) && (flag != "access-token-stdin" || tokenStdin) {
				invalid = append(invalid, "--"+flag)
			}
		}
		if len(invalid) > 0 {
			return &ExitError{2, "Error: " + strings.Join(invalid, ", ") + " can only be used with --platform gitlab"}
		}
		hasJSON := cmd.Flags().Changed("json")
		if hasJSON && waitSeconds <= 0 {
			return &ExitError{2, "Error: --json needs a positive --wait-seconds with --platform github"}
		}
		id, err := resolveOrg(cmd.Context(), d, cfg, api, org)
		if err != nil {
			return err
		}
		progress := d
		if hasJSON {
			progress.Out = d.Err
		}
		if err != nil {
			return err
		}
		before, err := api.Installations(cmd.Context())
		if err != nil {
			return apiError(cfg, err, "fetch installations")
		}
		target := cfg.Host + "/oauth/github/install?organization_id=" + url.QueryEscape(id)
		if _, err = fmt.Fprintln(progress.Out, "GitHub installation URL: "+target); err != nil {
			return err
		}
		if !noBrowser {
			if err = openFlowBrowser(cmd.Context(), progress, target, "Opened browser for GitHub installation flow."); err != nil {
				return err
			}
		}
		if waitSeconds <= 0 {
			_, err = fmt.Fprintln(d.Out, "Skipping wait for installation detection. Re-run 'avr org install list' to verify.")
			return err
		}
		if _, err = fmt.Fprintln(progress.Out, "Waiting for installation to appear..."); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, row := range before.Items {
			if githubInstallation(row.Fields) {
				seen[pythonValue(row.Fields["platform_installation_id"])] = true
			}
		}
		started := d.Now()
		for d.Now().Sub(started).Seconds() < float64(waitSeconds) {
			// Python sleeps before each poll, including the final poll after a short
			// wait budget. Preserve that behavior while making the sleep cancellable.
			if err = d.Wait(cmd.Context(), 5*time.Second); err != nil {
				return err
			}
			if err = cmd.Context().Err(); err != nil {
				return err
			}
			current, e := api.Installations(cmd.Context())
			if e != nil {
				return apiError(cfg, e, "poll installations")
			}
			added := map[string]bool{}
			for _, row := range current.Items {
				value := pythonValue(row.Fields["platform_installation_id"])
				if githubInstallation(row.Fields) && !seen[value] {
					added[value] = true
				}
			}
			if len(added) > 0 {
				ids := make([]string, 0, len(added))
				for value := range added {
					ids = append(ids, value)
				}
				sort.Strings(ids)
				if hasJSON {
					rows := []map[string]any{}
					for _, row := range current.Items {
						if added[pythonValue(row.Fields["platform_installation_id"])] {
							rows = append(rows, row.Fields)
						}
					}
					return o.list(cmd, d, rows, installationFields)
				}
				_, err = fmt.Fprintln(d.Out, "Detected new installation(s): "+output.CellText(strings.Join(ids, ", ")))
				return err
			}
		}
		_, err = fmt.Fprintln(d.Err, "Installation not detected yet. Complete the GitHub flow and rerun 'avr org install list' to verify.")
		if err == nil && hasJSON {
			return o.list(cmd, d, []map[string]any{}, installationFields)
		}
		return err
	})
	return c
}

func installationPlatform(row map[string]any) string {
	return strings.ToLower(fieldText(row, "platform"))
}
func githubInstallation(row map[string]any) bool {
	p, ok := row["platform"]
	return (!ok || strings.ToLower(pythonValue(p)) == "github") && row["platform_installation_id"] != nil
}
func newInstallationRemove(d Dependencies) *cobra.Command {
	var org, id string
	var yes bool
	var o jsonOptions
	fields := []string{"installation_id", "organization_id", "platform", "action"}
	c := &cobra.Command{Use: "remove", Short: "Remove an installation using its provider-specific lifecycle.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().StringVar(&id, "installation-id", "", "Installation ID to remove.")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt.")
	o.flags(c)
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if !cmd.Flags().Changed("installation-id") {
			return &ExitError{2, "Error: Missing option '--installation-id'."}
		}
		if done, e := o.prepare(cmd, d, fields); done || e != nil {
			return e
		}
		cfg, api, orgID, e := organizationContext(cmd.Context(), d, org)
		if e != nil {
			return e
		}
		path := "/orgs/" + url.PathEscape(orgID) + "/installations/" + url.PathEscape(id)
		if e = cmd.Context().Err(); e != nil {
			return e
		}
		row, e := api.Get(cmd.Context(), path, nil)
		if e != nil {
			return apiError(cfg, e, "fetch installation before removal")
		}
		if data, ok := row["data"].(map[string]any); ok {
			row = data
		}
		platform := installationPlatform(row)
		action, promptAction, prompt := "suspended", "installation remove suspends the GitHub App for this org", fmt.Sprintf("Suspend installation %s for %s?", id, orgID)
		switch platform {
		case "gitlab":
			action = "disconnected"
			promptAction = "permanent GitLab installation disconnect requires confirmation"
			prompt = fmt.Sprintf("Permanently disconnect installation %s from %s?", id, orgID)
			if !yes && d.Getenv("AVR_PROMPT_DISABLED") == "" {
				if _, e = fmt.Fprintln(d.Err, "This permanently disconnects the GitLab installation and removes its Avrea history. It cannot be undone. Use 'avr org install rotate' instead for credential problems."); e != nil {
					return e
				}
			}
		case "github":
		default:
			if platform == "" {
				platform = "unknown"
			}
			return &ExitError{1, fmt.Sprintf("Error: Installation %s has unsupported platform %s", output.CellText(id), output.CellText(platform))}
		}
		if !yes {
			if e = confirmMutation(cmd.Context(), d, promptAction, output.CellText(prompt)); e != nil {
				return e
			}
		}
		if _, e = api.Raw(cmd.Context(), http.MethodDelete, path, nil); e != nil {
			return apiError(cfg, e, "remove installation")
		}
		if cmd.Flags().Changed("json") {
			row["action"] = action
			return o.record(cmd, d, row, fields)
		}
		if platform == "github" {
			_, e = fmt.Fprintf(d.Out, "Installation %s suspended.\n", output.CellText(id))
			return e
		}
		_, e = fmt.Fprintf(d.Out, "Installation %s permanently disconnected.\nRemove the Avrea webhook, the group access token and the Avrea group runners from GitLab if they still exist.\n", output.CellText(id))
		return e
	})
	return c
}
