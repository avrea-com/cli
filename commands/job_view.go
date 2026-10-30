package commands

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func newJobView(d Dependencies) *cobra.Command {
	var org string
	var logs, failed, web, noPager bool
	var o jsonOptions
	c := &cobra.Command{Use: "view JOB_ID", Short: "View a job and its steps.", Args: cobra.ExactArgs(1)}
	o.flags(c)
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.BoolVar(&logs, "log", false, "Print full job logs.")
	f.BoolVar(&failed, "log-failed", false, "Print logs from failed steps.")
	f.BoolVar(&web, "web", false, "Open the job in a browser.")
	f.BoolVar(&noPager, "no-pager", false, "Print without a pager.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if web && f.Changed("json") {
			return &ExitError{2, "Error: --web cannot be combined with --json."}
		}
		if done, err := o.prepare(cmd, d, jobViewFields); done || err != nil {
			return err
		}
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		orgID, err := resolveOrg(cmd.Context(), d, cfg, api, org)
		if err != nil {
			return err
		}
		rec, err := api.Job(cmd.Context(), orgID, args[0], url.Values{"include": {"steps", "workflow_run"}})
		if err != nil {
			return apiError(cfg, err, "get job")
		}
		j := rec.Fields
		if f.Changed("json") {
			selected, e := output.Fields(o.fields, jobViewFields)
			if e != nil {
				return e
			}
			return output.JSON(cmd.Context(), d.Out, projectJob(j), selected, o.expression)
		}
		slug := ""
		if web || linksEnabled(cmd, d) {
			slug = orgSlug(cmd.Context(), cfg, api, orgID)
		}
		target := jobTarget(cfg.Host, slug, args[0])
		if web {
			if _, err = fmt.Fprintln(d.Out, "Avrea:  "+target); err != nil {
				return err
			}
			repo, runID, jobID := fieldText(j, "repository_full_name"), fieldText(j, "platform_run_id"), fieldText(j, "platform_job_id")
			if sourcePlatform(j) == "gitlab" && repo != "" && jobID != "" {
				if _, err = fmt.Fprintln(d.Out, "GitLab: https://gitlab.com/"+output.CellText(repo)+"/-/jobs/"+output.CellText(jobID)); err != nil {
					return err
				}
			} else if sourcePlatform(j) == "github" && repo != "" && runID != "" && jobID != "" {
				_, err = fmt.Fprintln(d.Out, "GitHub: https://github.com/"+output.CellText(repo)+"/actions/runs/"+output.CellText(runID)+"/job/"+output.CellText(jobID))
				if err != nil {
					return err
				}
			}
			if d.IsTerminal() {
				return d.OpenBrowser(cmd.Context(), target)
			}
			return nil
		}
		if err = renderJobView(d, j, args[0], cfg.Host, slug); err != nil {
			return err
		}
		if logs || failed {
			logTarget := ""
			if linksEnabled(cmd, d) {
				logTarget = jobTarget(cfg.Host, orgSlug(cmd.Context(), cfg, api, orgID), args[0])
			}
			content, e := jobLogContent(cmd.Context(), d, api, args[0], objectList(j["steps"]), logOptions{failed: failed}, logTarget)
			if e != nil {
				return apiError(cfg, e, "fetch logs")
			}
			if e = pageOutput(cmd.Context(), d, content, noPager); e != nil {
				return e
			}
		} else {
			_, _ = fmt.Fprintf(d.Err, "\nTo see the full job log, try: avr job logs %s\n", output.CellText(args[0]))
		}
		slug = orgSlug(cmd.Context(), cfg, api, orgID)
		_, err = fmt.Fprintln(d.Err, d.hint("View this job on Avrea: "+jobTarget(cfg.Host, slug, args[0])))
		return err
	})
	return c
}

func formatTimestamp(value string) string {
	if value == "" {
		return "unknown"
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05", value)
	}
	if err != nil {
		return output.CellText(value)
	}
	return t.UTC().Format("2006-01-02T15:04:05.") + fmt.Sprintf("%02dZ", t.Nanosecond()/10000000)
}

func renderJobView(d Dependencies, j map[string]any, id, host, slug string) error {
	conclusion := output.CellText(fieldText(j, "state"))
	if fieldText(j, "conclusion") != "" {
		conclusion = d.indicator(j, "state") + " " + output.CellText(fieldText(j, "conclusion"))
	}
	repo := fieldText(j, "repository_full_name")
	if repo == "" {
		repo = "-"
	}
	runDisplay := "-"
	runTarget := ""
	wf, _ := j["workflow_run"].(map[string]any)
	if wf != nil {
		rid := fieldText(wf, "run_id")
		short := rid
		if prefix, tail, ok := strings.Cut(rid, "-"); ok && len(tail) > 8 {
			short = prefix + "-" + tail[:8]
		}
		number := fieldText(wf, "run_number")
		if number == "" {
			number = "?"
		}
		runDisplay = "#" + number + " (" + short + ")"
		if wf["run_number"] == nil {
			runDisplay = rid
		}
		if slug != "" && rid != "" {
			runTarget = consoleURL(host) + "/org/" + url.PathEscape(slug) + "/runs/" + url.PathEscape(rid)
		}
	}
	repoTarget := ""
	if slug != "" && fieldText(j, "repository_id") != "" && repo != "-" {
		repoTarget = consoleURL(host) + "/org/" + url.PathEscape(slug) + "/activity?repositories=" + url.QueryEscape(fieldText(j, "repository_id"))
	}
	labels := []string{}
	for _, v := range anyList(j["job_labels"]) {
		labels = append(labels, fmt.Sprint(v))
	}
	label := strings.Join(labels, ", ")
	if label == "" {
		label = "-"
	}
	on := "shadow"
	if j["running_on_avrea"] == true {
		on = "yes"
	}
	rows := [][2]string{{"Job", linkCell(output.CellText(id), jobTarget(host, slug, id))}, {"CI", output.CellText(jobCIProvider(j))}, {"Name", output.CellText(fieldText(j, "job_name"))}, {"Repository", linkCell(output.CellText(repo), repoTarget)}, {"Platform", output.CellText(sourcePlatform(j))}, {"Run", linkCell(output.CellText(runDisplay), runTarget)}, {"Conclusion", conclusion}, {"Duration", formatDuration(j["duration_seconds"])}, {"Labels", output.CellText(label)}, {"On Avrea", on}, {"Created", formatTimestamp(fieldText(j, "created_at"))}, {"Started", formatTimestamp(fieldText(j, "started_at"))}, {"Completed", formatTimestamp(fieldText(j, "completed_at"))}}
	for _, r := range rows {
		if _, err := fmt.Fprintf(d.Out, "%-11s %s\n", r[0], r[1]); err != nil {
			return err
		}
	}
	steps := objectList(j["steps"])
	if len(steps) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(d.Out, "\n"+d.paint("STEPS", "97;1")+"\n"); err != nil {
		return err
	}
	for _, s := range steps {
		duration := ""
		a, e1 := time.Parse(time.RFC3339Nano, fieldText(s, "started_at"))
		b, e2 := time.Parse(time.RFC3339Nano, fieldText(s, "completed_at"))
		if e1 == nil && e2 == nil {
			duration = " (" + formatDuration(b.Sub(a).Seconds()) + ")"
		}
		if _, err := fmt.Fprintf(d.Out, "  %s  %s%s\n", d.indicator(s, "status"), output.CellText(fieldText(s, "name")), duration); err != nil {
			return err
		}
	}
	return nil
}

func anyList(value any) []any { values, _ := value.([]any); return values }
