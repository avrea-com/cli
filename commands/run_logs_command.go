package commands

import (
	"fmt"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
	"strings"
)

func newRunLogs(d Dependencies) *cobra.Command {
	var org, filter string
	var o logOptions
	c := &cobra.Command{Use: "logs RUN", Short: "Fetch logs for a workflow run's jobs.", Args: cobra.ExactArgs(1)}
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringVar(&filter, "job", "", "Filter job names by substring.")
	f.BoolVarP(&o.follow, "follow", "f", false, "Follow one active job.")
	f.BoolVar(&o.failed, "failed", false, "Show failed jobs.")
	f.BoolVar(&o.all, "all-levels", false, "Include diagnostic lines.")
	f.BoolVar(&o.noPager, "no-pager", false, "Print without a pager.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if o.follow && o.failed {
			return &ExitError{2, "Error: --follow and --failed cannot be combined"}
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		ref, orgID, err := referenceAndOrg(cmd.Context(), d, cfg, api, args[0], org)
		if err != nil {
			return err
		}
		rec, err := resolveRun(cmd.Context(), cfg, api, orgID, ref, []string{"jobs"}, "get workflow run")
		if err != nil {
			return err
		}
		jobs := []map[string]any{}
		for _, j := range objectList(rec.Fields["jobs"]) {
			if filter == "" || strings.Contains(strings.ToLower(fieldText(j, "job_name")), strings.ToLower(filter)) {
				jobs = append(jobs, j)
			}
		}
		if len(jobs) == 0 {
			_, err = fmt.Fprintln(d.Out, "No matching jobs found.")
			return err
		}
		slug := ""
		if linksEnabled(cmd, d) {
			slug = orgSlug(cmd.Context(), cfg, api, orgID)
		}
		header := func(j map[string]any) string {
			return linkCell(d.paint("--- "+output.CellText(fieldText(j, "job_name"))+" ("+output.CellText(fieldText(j, "job_id"))+") ---", "1"), jobTarget(cfg.Host, slug, fieldText(j, "job_id")))
		}
		if o.follow {
			target := jobs[0]
			for _, j := range jobs {
				if state := fieldText(j, "state"); state == "in_progress" || state == "queued" {
					target = j
					break
				}
			}
			id := fieldText(target, "job_id")
			if id == "" {
				return &ExitError{1, "Error: Cannot follow: target job has no ID yet."}
			}
			if _, err = fmt.Fprintln(d.Out, header(target)); err != nil {
				return err
			}
			return followJobLogs(cmd.Context(), d, cfg, api, orgID, id, fieldText(target, "job_name"), o.all, jobTarget(cfg.Host, slug, id))
		}
		if o.failed {
			failed := []map[string]any{}
			for _, j := range jobs {
				switch fieldText(j, "conclusion") {
				case "failure", "timed_out", "cancelled", "action_required":
					failed = append(failed, j)
				}
			}
			jobs = failed
			if len(jobs) == 0 {
				_, err = fmt.Fprintln(d.Out, "No failed jobs in this run.")
				return err
			}
		}
		var b strings.Builder
		for _, j := range jobs {
			id := fieldText(j, "job_id")
			if id == "" {
				continue
			}
			fmt.Fprintln(&b, "\n"+header(j))
			opts := o
			steps := objectList(j["steps"])
			opts.failed = o.failed && len(steps) > 0
			content, e := jobLogContent(cmd.Context(), d, api, id, steps, opts, jobTarget(cfg.Host, slug, id))
			if e != nil {
				return apiError(cfg, e, "fetch logs")
			}
			if content != "" {
				fmt.Fprintln(&b, content)
			}
		}
		return pageOutput(cmd.Context(), d, strings.TrimSuffix(b.String(), "\n"), o.noPager)
	})
	return c
}
