package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

type logOptions struct {
	failed, all, follow, noPager bool
	step, level                  string
}

func fetchLogBatch(ctx context.Context, api *client.Client, id string, cursor any, level string) ([]map[string]any, any, bool, error) {
	payload := map[string]any{"job_id": id, "limit": 1000, "order_by": "line_number"}
	if cursor != nil {
		payload["after_line"] = cursor
	}
	if level != "" {
		payload["level"] = level
	}
	rec, err := api.SearchLogs(ctx, payload)
	if err != nil {
		return nil, cursor, false, err
	}
	raw := objectList(rec.Fields["results"])
	next := cursor
	if len(raw) > 0 {
		next = rec.Fields["next_cursor"]
		if next == nil || fmt.Sprint(next) == "0" {
			next = raw[len(raw)-1]["line_number"]
		}
	}
	more, _ := rec.Fields["has_more"].(bool)
	return raw, next, more, nil
}
func filterLogEntries(raw []map[string]any, o logOptions) []map[string]any {
	entries := []map[string]any{}
	for _, e := range raw {
		if !o.all && o.level == "" && fieldText(e, "level") == "diagnostic" {
			continue
		}
		if o.step != "" && fieldText(e, "step_name") != o.step {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}
func fetchJobLogs(ctx context.Context, api *client.Client, id string, o logOptions) ([]map[string]any, error) {
	entries := []map[string]any{}
	var cursor any
	seen := map[string]bool{}
	for {
		raw, next, more, err := fetchLogBatch(ctx, api, id, cursor, o.level)
		if err != nil {
			var he *client.HTTPError
			if errors.As(err, &he) && he.Status == 404 {
				return entries, nil
			}
			return nil, err
		}
		entries = append(entries, filterLogEntries(raw, o)...)
		if len(raw) == 0 || !more {
			return entries, nil
		}
		key := fmt.Sprint(next)
		if next == nil || seen[key] {
			return nil, fmt.Errorf("log pagination did not advance")
		}
		seen[key] = true
		cursor = next
	}
}
func stepExit(conclusion string) int {
	switch conclusion {
	case "failure", "cancelled", "timed_out", "action_required", "startup_failure", "stale", "neutral":
		return 1
	}
	return 0
}
func logMark(d Dependencies, kind string) string {
	if !d.IsTerminal() {
		return ""
	}
	return "\x1b]133;" + kind + "\x1b\\"
}
func renderLogEntries(d Dependencies, w io.Writer, entries []map[string]any, current *string, target string, marks bool, conclusions map[string]string) error {
	for _, e := range entries {
		step := fieldText(e, "step_name")
		if step == "" {
			step = "(unknown step)"
		}
		if step != *current {
			header := linkCell(d.paint("--- "+output.CellText(step)+" ---", "1"), target)
			if marks {
				close := ""
				if *current != "" {
					close = logMark(d, fmt.Sprintf("D;%d", stepExit(conclusions[*current])))
				}
				header = close + logMark(d, "A") + header + logMark(d, "C")
			}
			if _, err := fmt.Fprintln(w, "\n"+header); err != nil {
				return err
			}
			*current = step
		}
		var line strings.Builder
		writeStyledLogLine(&line, e, d, target)
		if _, err := io.WriteString(w, line.String()); err != nil {
			return err
		}
	}
	return nil
}
func jobLogContent(ctx context.Context, d Dependencies, api *client.Client, id string, steps []map[string]any, o logOptions, target string) (string, error) {
	var b strings.Builder
	if o.failed {
		o.step = ""
		o.level = ""
	}
	failedSteps := []map[string]any{}
	if o.failed {
		for _, s := range steps {
			v := fieldText(s, "conclusion")
			if v != "" && v != "success" && v != "skipped" {
				failedSteps = append(failedSteps, s)
			}
		}
		if len(failedSteps) == 0 {
			return "No failed steps.", nil
		}
	}
	entries, err := fetchJobLogs(ctx, api, id, o)
	if err != nil {
		return "", err
	}
	if o.failed {
		for _, s := range failedSteps {
			printed := false
			for _, e := range entries {
				if fieldText(e, "step_name") != fieldText(s, "name") {
					continue
				}
				if !printed {
					header := "--- " + d.indicator(s, "status") + " " + output.CellText(fieldText(s, "name")) + " (" + output.CellText(fieldText(s, "conclusion")) + ") ---"
					fmt.Fprintln(&b, "\n"+linkCell(d.paint(header, "1"), target))
					printed = true
				}
				writeStyledLogLine(&b, e, d, target)
			}
		}
	} else {
		current := ""
		if err = renderLogEntries(d, &b, entries, &current, target, false, nil); err != nil {
			return "", err
		}
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}
func newJobLogs(d Dependencies) *cobra.Command {
	var org string
	var o logOptions
	c := &cobra.Command{Use: "logs JOB_ID", Short: "View job logs grouped by step.", Args: cobra.ExactArgs(1)}
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.BoolVar(&o.failed, "failed", false, "Only failed-step logs.")
	f.StringVar(&o.step, "step", "", "Exact step name.")
	f.StringVar(&o.level, "level", "", "Log level.")
	f.BoolVarP(&o.follow, "follow", "f", false, "Follow new log lines.")
	f.BoolVar(&o.all, "all-levels", false, "Include diagnostic log lines.")
	f.BoolVar(&o.noPager, "no-pager", false, "Print without a pager.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		if o.follow && o.failed {
			return &ExitError{2, "Error: --follow and --failed cannot be combined"}
		}
		if o.level != "" {
			valid := false
			for _, v := range []string{"debug", "info", "notice", "warning", "error"} {
				if strings.EqualFold(o.level, v) {
					valid = true
				}
			}
			if !valid {
				return &ExitError{2, "Error: invalid --level"}
			}
			o.level = strings.ToLower(o.level)
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
		rec, err := api.Job(cmd.Context(), orgID, args[0], url.Values{"include": {"steps"}})
		if err != nil {
			return apiError(cfg, err, "get job")
		}
		for _, pair := range [][2]string{{"Started:   ", "started_at"}, {"Completed: ", "completed_at"}} {
			if v := fieldText(rec.Fields, pair[1]); v != "" {
				if _, err = fmt.Fprintln(d.Out, d.paint(pair[0]+output.CellText(v), gray)); err != nil {
					return err
				}
			}
		}
		target := ""
		if linksEnabled(cmd, d) {
			target = jobTarget(cfg.Host, orgSlug(cmd.Context(), cfg, api, orgID), args[0])
		}
		if o.follow {
			return followJobLogs(cmd.Context(), d, cfg, api, orgID, args[0], fieldText(rec.Fields, "job_name"), o.all, target)
		}
		content, err := jobLogContent(cmd.Context(), d, api, args[0], objectList(rec.Fields["steps"]), o, target)
		if err != nil {
			return apiError(cfg, err, "fetch logs")
		}
		if content == "" && !o.failed {
			message := "No log entries found."
			state := fieldText(rec.Fields, "state")
			if state == "queued" || state == "in_progress" {
				message = "No log entries found (job is " + state + "). Use --follow to wait for logs."
			}
			_, err = fmt.Fprintln(d.Out, message)
			return err
		}
		return pageOutput(cmd.Context(), d, content, o.noPager)
	})
	return c
}

func followJobLogs(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, id, name string, all bool, target string) (err error) {
	current := ""
	conclusions := map[string]string{}
	var cursor any
	idle := 0
	if name == "" {
		name = "job"
	}
	setTitle := func(stage string) {
		if d.IsErrorTerminal() {
			title := "avr ▸ " + output.CellText(name)
			if stage != "" {
				title += ": " + output.CellText(stage)
			}
			_, _ = fmt.Fprintf(d.Err, "\x1b]2;%s\x07", title)
		}
	}
	setTitle("")
	defer func() {
		if d.IsErrorTerminal() {
			_, _ = fmt.Fprint(d.Err, "\x1b]2;\x07")
		}
		if errors.Is(err, context.Canceled) {
			_, _ = fmt.Fprintln(d.Out, "\nStopped following.")
			err = &ExitError{Code: 130}
		}
	}()
	finish := func(message, conclusion string) error {
		if current != "" && d.IsTerminal() {
			if _, e := fmt.Fprintln(d.Out, logMark(d, fmt.Sprintf("D;%d", stepExit(conclusion)))); e != nil {
				return e
			}
		}
		_, e := fmt.Fprintln(d.Out, "\n"+message)
		return e
	}
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		raw, next, _, e := fetchLogBatch(ctx, api, id, cursor, "")
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			_, retry := retryReason(e)
			var he *client.HTTPError
			if errors.As(e, &he) && he.Status == 404 {
				retry = true
			}
			if !retry {
				return apiError(cfg, e, "fetch logs")
			}
			idle++
			if err = d.Wait(ctx, 2*time.Second); err != nil {
				return err
			}
			continue
		}
		if len(raw) > 0 {
			if next == nil || cursor != nil && fmt.Sprint(next) == fmt.Sprint(cursor) {
				return fmt.Errorf("log pagination did not advance")
			}
			cursor = next
		}
		entries := filterLogEntries(raw, logOptions{all: all})
		if len(entries) > 0 {
			idle = 0
			if err = renderLogEntries(d, d.Out, entries, &current, target, true, conclusions); err != nil {
				return err
			}
			setTitle(current)
		} else if len(raw) == 0 {
			idle++
		}
		finished := false
		for _, entry := range raw {
			if strings.HasPrefix(fieldText(entry, "content"), "Cleaning up orphan processes") {
				finished = true
			}
		}
		if finished {
			_, _ = api.Job(ctx, org, id, nil)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return finish("Job completed.", conclusions[current])
		}
		if idle > 0 && idle%2 == 0 {
			job, e := api.Job(ctx, org, id, url.Values{"include": {"steps"}})
			if e == nil {
				for _, s := range objectList(job.Fields["steps"]) {
					conclusions[fieldText(s, "name")] = fieldText(s, "conclusion")
				}
				if fieldText(job.Fields, "state") == "completed" {
					if err = d.Wait(ctx, 8*time.Second); err != nil {
						return err
					}
					for attempt := 0; attempt < 3; attempt++ {
						tail, next, more, e := fetchLogBatch(ctx, api, id, cursor, "")
						if e != nil {
							if ctx.Err() != nil {
								return ctx.Err()
							}
							_, _ = fmt.Fprintln(d.Err, d.hint("(warning: failed to drain final log batch — re-run "+string(rune(96))+"avr job logs "+output.CellText(id)+string(rune(96))+" to see the tail)"))
							break
						}
						if len(tail) > 0 {
							if next == nil || cursor != nil && fmt.Sprint(next) == fmt.Sprint(cursor) {
								return fmt.Errorf("log pagination did not advance")
							}
							cursor = next
						}
						if err = renderLogEntries(d, d.Out, filterLogEntries(tail, logOptions{all: all}), &current, target, true, conclusions); err != nil {
							return err
						}
						if !more {
							break
						}
					}
					conclusion := fieldText(job.Fields, "conclusion")
					if conclusion == "" {
						conclusion = "unknown"
					}
					codeConclusion := conclusions[current]
					if codeConclusion == "" {
						codeConclusion = conclusion
					}
					return finish("Job completed: "+output.CellText(conclusion), codeConclusion)
				}
			} else if ctx.Err() != nil {
				return ctx.Err()
			}
		}
		if err = d.Wait(ctx, 2*time.Second); err != nil {
			return err
		}
	}
}
