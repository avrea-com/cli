package commands

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

var runViewFields = append(append([]string{}, runFields...), "jobs")

func newRunView(d Dependencies) *cobra.Command {
	var options jsonOptions
	var org, jobFilter string
	var steps, logs, failed, web, noPager bool
	cmd := &cobra.Command{Use: "view [RUN]", Short: "View a workflow run and its jobs.", Args: cobra.MaximumNArgs(1)}
	options.flags(cmd)
	f := cmd.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringVar(&jobFilter, "job", "", "Restrict jobs and logs to names containing this text.")
	f.BoolVar(&steps, "steps", false, "Show individual job steps.")
	f.BoolVar(&logs, "log", false, "Print full logs for the selected jobs.")
	f.BoolVar(&failed, "log-failed", false, "Print logs only for failed steps.")
	f.BoolVar(&web, "web", false, "Open the run in a browser.")
	f.BoolVar(&noPager, "no-pager", false, "Print logs without a pager.")
	cmd.RunE = run(func(cmd *cobra.Command, args []string) error {
		if web && f.Changed("json") {
			return &ExitError{2, "Error: --web cannot be combined with --json."}
		}
		if done, err := options.prepare(cmd, d, runViewFields); done || err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		if len(args) == 0 {
			id, e := resolveOrg(cmd.Context(), d, cfg, api, org)
			if e != nil {
				return e
			}
			page, e := api.Runs(cmd.Context(), id, url.Values{"limit": {"10"}, "order": {"created_at.desc"}, "include": {"jobs"}})
			if e != nil {
				return apiError(cfg, e, "list workflow runs")
			}
			if len(page.Items) == 0 {
				_, e = fmt.Fprintln(d.Out, "No workflow runs found.")
				return e
			}
			// Preserve Python's no-argument recent-runs view even when --json was
			// supplied; JSON projection applies to an individual RUN.
			for _, r := range page.Items {
				repo, _ := r.Fields["repository"].(map[string]any)
				_, e = fmt.Fprintf(d.Out, "  %s  %s %s -- %s -- %s -- %s\n     %s\n", d.indicator(r.Fields, "status"), output.CellText(fieldText(repo, "full_name")), output.CellText(fieldText(r.Fields, "display_title")), output.CellText(fieldText(r.Fields, "head_branch")), formatDuration(r.Fields["duration_seconds"]), relativeAge(fieldText(r.Fields, "created_at"), d.Now()), d.paint(output.CellText(fieldText(r.Fields, "run_id")), gray))
				if e != nil {
					return e
				}
			}
			_, e = fmt.Fprintln(d.Err, "\nTo view a run, try: avr run view <run-id>")
			return e
		}
		ref, id, err := referenceAndOrg(cmd.Context(), d, cfg, api, args[0], org)
		if err != nil {
			return err
		}
		record, err := resolveRun(cmd.Context(), cfg, api, id, ref, []string{"jobs", "workflow"}, "get workflow run")
		if err != nil {
			return err
		}
		fields := record.Fields
		runID := fieldText(fields, "run_id")
		if f.Changed("json") {
			selected, e := output.Fields(options.fields, runViewFields)
			if e != nil {
				return e
			}
			return output.JSON(cmd.Context(), d.Out, fields, selected, options.expression)
		}
		if web {
			target := consoleURL(cfg.Host) + "/org/" + url.PathEscape(orgSlug(cmd.Context(), cfg, api, id)) + "/runs/" + url.PathEscape(runID)
			if _, err = fmt.Fprintln(d.Out, "Avrea:  "+target); err != nil {
				return err
			}
			repo, _ := fields["repository"].(map[string]any)
			if name, platform := fieldText(repo, "full_name"), fieldText(fields, "platform_run_id"); name != "" && platform != "" {
				github := "https://github.com/" + name + "/actions/runs/" + platform
				if attempt := fieldText(fields, "run_attempt"); attempt != "" && attempt != "1" && attempt != "0" {
					github += "/attempts/" + attempt
				}
				if _, err = fmt.Fprintln(d.Out, "GitHub: "+output.CellText(github)); err != nil {
					return err
				}
			}
			if d.IsTerminal() {
				return d.OpenBrowser(cmd.Context(), target)
			}
			return nil
		}
		slug := ""
		if linksEnabled(cmd, d) {
			slug = orgSlug(cmd.Context(), cfg, api, id)
		}
		if err = renderRunMetadata(d, fields, cfg.Host, slug); err != nil {
			return err
		}
		jobs := objectList(fields["jobs"])
		if jobFilter != "" {
			matched := []map[string]any{}
			for _, job := range jobs {
				if strings.Contains(strings.ToLower(fieldText(job, "job_name")), strings.ToLower(jobFilter)) {
					matched = append(matched, job)
				}
			}
			if len(matched) == 0 {
				_, err = fmt.Fprintf(d.Err, "No job matching '%s' in this run.\n", output.CellText(jobFilter))
				return err
			}
			jobs = matched
		}
		if len(jobs) > 0 {
			if err = renderJobs(d, jobs, steps, cfg.Host, slug); err != nil {
				return err
			}
		}
		if logs || failed {
			content, e := runLogs(cmd.Context(), d, api, jobs, failed, cfg.Host, slug)
			if e != nil {
				return apiError(cfg, e, "fetch logs")
			}
			if e = pageOutput(cmd.Context(), d, content, noPager); e != nil {
				return e
			}
		} else if len(jobs) == 1 {
			_, _ = fmt.Fprintf(d.Err, "\nTo view the job, try: avr job view %s\n", output.CellText(fieldText(jobs[0], "job_id")))
		} else if len(jobs) > 0 {
			_, _ = fmt.Fprintln(d.Err, "\nTo view a job, try: avr job view <job-id>")
		}
		if platform := fieldText(fields, "platform_run_id"); platform != "" {
			page, e := api.Runs(cmd.Context(), id, url.Values{"platform_run_id": {platform}, "limit": {"10"}, "order": {"created_at.desc"}})
			if e != nil {
				if cmd.Context().Err() != nil {
					return cmd.Context().Err()
				}
				_, _ = fmt.Fprintf(d.Err, "(could not load other attempts: %s)\n", output.CellText(e.Error()))
			} else {
				if e = renderOtherAttempts(d, page, runID); e != nil {
					return e
				}
			}
		}
		slug = orgSlug(cmd.Context(), cfg, api, id)
		_, err = fmt.Fprintln(d.Err, d.hint(fmt.Sprintf("View this run on Avrea: %s/org/%s/runs/%s", consoleURL(cfg.Host), url.PathEscape(slug), url.PathEscape(runID))))
		return err
	})
	return cmd
}

func renderRunMetadata(d Dependencies, fields map[string]any, host, slug string) error {
	repo, _ := fields["repository"].(map[string]any)
	actor, _ := fields["triggering_actor"].(map[string]any)
	workflow, _ := fields["workflow"].(map[string]any)
	actorName := fieldText(actor, "login")
	if actorName == "" {
		actorName = fieldText(actor, "platform_login")
	}
	wf := workflowName(fields)
	file := fieldText(workflow, "path")
	if file == "" {
		file = fieldText(fields, "workflow_path")
	}
	if file != "" && wf != "" {
		wf += " (" + path.Base(file) + ")"
	}
	sha := fieldText(fields, "head_sha")
	if len(sha) > 8 {
		sha = sha[:8]
	}
	rows := [][2]string{
		{"Title", fieldText(fields, "display_title")}, {"Conclusion", runState(fields, "status")}, {"Branch", fieldText(fields, "head_branch")}, {"Commit", sha},
		{"Event", fieldText(fields, "event")}, {"Actor", actorName}, {"Duration", formatDuration(fields["duration_seconds"])},
		{"Run", func() string {
			label := runLabel(fields, fieldText(fields, "run_id"))
			if number, present := fields["run_number"]; !present || number != nil {
				attempt, ok := fields["run_attempt"]
				if !ok {
					attempt = 1
				}
				label += " (attempt " + pythonValue(attempt) + ")"
			}
			return label
		}()}, {"CI", runCIProvider(fields)}, {"Workflow", wf}, {"Repository", fieldText(repo, "full_name")},
	}
	for _, row := range rows {
		value := row[1]
		if value == "" {
			value = "-"
		}
		value = output.CellText(value)
		if row[0] == "Conclusion" && fieldText(fields, "conclusion") != "" {
			value = d.indicator(fields, "status") + " " + output.CellText(fieldText(fields, "conclusion"))
		}
		target := ""
		if slug != "" {
			base := consoleURL(host) + "/org/" + url.PathEscape(slug)
			switch row[0] {
			case "Title":
				target = base + "/runs/" + url.PathEscape(fieldText(fields, "run_id"))
			case "Workflow":
				if id := fieldText(workflow, "workflow_id"); id != "" {
					target = base + "/workflows/" + url.PathEscape(id)
				}
			case "Repository":
				if id := fieldText(repo, "id"); id != "" {
					target = base + "/activity?repositories=" + url.QueryEscape(id)
				}
			case "Branch", "Commit":
				if name := fieldText(repo, "full_name"); strings.Contains(name, "/") && fields["ci_provider"] != "avrea_ci" {
					if row[0] == "Branch" {
						target = "https://github.com/" + name + "/tree/" + url.PathEscape(fieldText(fields, "head_branch"))
					} else {
						target = "https://github.com/" + name + "/commit/" + url.PathEscape(fieldText(fields, "head_sha"))
					}
				}
			}
		}
		if target != "" {
			value = "\x1b]8;;" + output.CellText(target) + "\x1b\\" + value + "\x1b]8;;\x1b\\"
		}
		if _, err := fmt.Fprintf(d.Out, "%-11s %s\n", row[0], value); err != nil {
			return err
		}
	}
	return nil
}

func renderOtherAttempts(d Dependencies, page client.Page[client.Run], runID string) error {
	printed := false
	for _, record := range page.Items {
		r := record.Fields
		if fieldText(r, "run_id") == runID {
			continue
		}
		if !printed {
			if _, err := fmt.Fprintln(d.Out, "\n"+d.paint("OTHER ATTEMPTS", "97;1")+"\n"); err != nil {
				return err
			}
			printed = true
		}
		if _, err := fmt.Fprintf(d.Out, "  %s  attempt %-3s  %8s   %s\n", d.indicator(r, "status"), output.CellText(fieldText(r, "run_attempt")), formatDuration(r["duration_seconds"]), d.paint(output.CellText(fieldText(r, "run_id")), "36")); err != nil {
			return err
		}
	}
	return nil
}
