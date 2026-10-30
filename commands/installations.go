package commands

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var installationFields = []string{"installation_id", "platform_installation_id", "target_name", "organization_name", "organization_slug", "state", "created_at"}

func newOrgInstall(d Dependencies) *cobra.Command {
	c := &cobra.Command{Use: "install", Short: "Manage GitHub App installations."}
	c.AddCommand(newInstallationList(d), newInstallationAdd(d), newInstallationRemove(d))
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
			return apiError(cfg, err, "fetch installations")
		}
		rows := rawRecords(page)
		if cmd.Flags().Changed("json") {
			return o.list(cmd, d, rows, installationFields)
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(d.Out, "No data found.")
			return err
		}
		var cells [][]string
		for _, row := range rows {
			org := fieldText(row, "organization_name")
			if org == "" {
				org = fieldText(row, "organization_slug")
			}
			if org == "" {
				org = "-"
			}
			cells = append(cells, []string{organizationCell(row, "installation_id"), organizationCell(row, "platform_installation_id"), organizationCell(row, "target_name"), org, organizationCell(row, "state"), formatTimestamp(fieldText(row, "created_at"))})
		}
		return output.Grid(d.Out, []string{"Installation ID", "GitHub ID", "Target", "Organization", "State", "Created"}, cells)
	})
	return c
}
func newInstallationAdd(d Dependencies) *cobra.Command {
	var org string
	var noBrowser bool
	var waitSeconds int
	c := &cobra.Command{Use: "add", Short: "Start the GitHub App installation flow.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().BoolVar(&noBrowser, "no-browser", false, "Do not open browser automatically.")
	c.Flags().IntVar(&waitSeconds, "wait-seconds", 120, "Seconds to wait for detection.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) (err error) {
		defer func() {
			if cmd.Context().Err() == context.Canceled {
				err = &ExitError{130, "\nAborted!"}
			}
		}()

		cfg, api, id, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		before, err := api.Installations(cmd.Context())
		if err != nil {
			return apiError(cfg, err, "fetch installations")
		}
		target := cfg.Host + "/oauth/github/install?organization_id=" + url.QueryEscape(id)
		if _, err = fmt.Fprintln(d.Out, "GitHub installation URL: "+target); err != nil {
			return err
		}
		if !noBrowser {
			if err = openFlowBrowser(cmd.Context(), d, target, "Opened browser for GitHub installation flow."); err != nil {
				return err
			}
		}
		if waitSeconds <= 0 {
			_, err = fmt.Fprintln(d.Out, "Skipping wait for installation detection. Re-run 'avr org install list' to verify.")
			return err
		}
		if _, err = fmt.Fprintln(d.Out, "Waiting for installation to appear..."); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, row := range before.Items {
			seen[pythonValue(row.Fields["platform_installation_id"])] = true
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
				if !seen[value] {
					added[value] = true
				}
			}
			if len(added) > 0 {
				ids := make([]string, 0, len(added))
				for value := range added {
					ids = append(ids, value)
				}
				sort.Strings(ids)
				_, err = fmt.Fprintln(d.Out, "Detected new installation(s): "+output.CellText(strings.Join(ids, ", ")))
				return err
			}
		}
		_, err = fmt.Fprintln(d.Err, "Installation not detected yet. Complete the GitHub flow and rerun 'avr org install list' to verify.")
		return err
	})
	return c
}
func newInstallationRemove(d Dependencies) *cobra.Command {
	var org, id string
	var yes bool
	c := &cobra.Command{Use: "remove", Short: "Remove/suspend a GitHub installation.", Args: cobra.NoArgs}
	orgFlag(c, &org)
	c.Flags().StringVar(&id, "installation-id", "", "Installation ID to remove.")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt.")
	c.RunE = run(func(cmd *cobra.Command, _ []string) error {
		if !cmd.Flags().Changed("installation-id") {
			return &ExitError{2, "Error: Missing option '--installation-id'."}
		}
		cfg, api, orgID, err := organizationContext(cmd.Context(), d, org)
		if err != nil {
			return err
		}
		if !yes {
			prompt := fmt.Sprintf("Suspend installation %s for %s?", output.CellText(id), output.CellText(orgID))
			if err = confirmMutation(cmd.Context(), d, "installation remove suspends the GitHub App for this org", prompt); err != nil {
				return err
			}
		}
		if err = cmd.Context().Err(); err != nil {
			return err
		}
		_, err = api.Raw(cmd.Context(), http.MethodDelete, "/orgs/"+url.PathEscape(orgID)+"/installations/"+url.PathEscape(id), nil)
		if err != nil {
			return apiError(cfg, err, "remove installation")
		}
		_, err = fmt.Fprintf(d.Out, "Installation %s suspended.\n", output.CellText(id))
		return err
	})
	return c
}
