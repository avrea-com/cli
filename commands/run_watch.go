package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

func waitPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func newRunWatch(d Dependencies) *cobra.Command {
	var org string
	var repos []string
	var interval int
	var exitStatus, ndjson bool
	cmd := &cobra.Command{Use: "watch [RUN]", Short: "Watch a workflow run until it completes.", Args: cobra.MaximumNArgs(1)}
	f := cmd.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringArrayVar(&repos, "repo", nil, "Scope automatic run selection to repositories (repeatable).")
	f.IntVar(&interval, "interval", 3, "Polling interval in seconds (at least 1).")
	f.BoolVar(&exitStatus, "exit-status", false, "Exit unsuccessfully if the run fails.")
	f.BoolVar(&ndjson, "ndjson", false, "Stream JSON events; automatic when stdout is piped.")
	cmd.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		if interval < 1 || int64(interval) > int64((1<<63-1)/time.Second) {
			return fmt.Errorf("--interval must be at least 1 second and fit a duration")
		}
		var orgID, runID string
		if len(args) > 0 {
			var ref runReference
			ref, orgID, err = referenceAndOrg(cmd.Context(), d, cfg, api, args[0], org)
			if err != nil {
				return err
			}
			runID = ref.id
			if runID == "" {
				record, e := resolveRun(cmd.Context(), cfg, api, orgID, ref, nil, "resolve workflow run before watch")
				if e != nil {
					return e
				}
				runID = fieldText(record.Fields, "run_id")
			}
		} else {
			orgID, err = resolveOrg(cmd.Context(), d, cfg, api, org)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(d.Err, "Looking for an in-progress run…")
			ids, e := resolveRepositories(cmd.Context(), d, cfg, api, orgID, repos, true)
			if e != nil {
				return e
			}
			query := url.Values{"limit": {"1"}, "order": {"created_at.desc"}, "statuses": {"in_progress", "queued"}}
			if len(ids) > 0 {
				query["repository_ids"] = ids
			}
			page, e := api.Runs(cmd.Context(), orgID, query)
			if e != nil {
				return apiError(cfg, e, "list workflow runs")
			}
			if len(page.Items) == 0 {
				suffix := ""
				if len(ids) > 0 {
					suffix = " for these repos"
				}
				_, e = fmt.Fprintln(d.Out, "No in-progress workflow runs found"+suffix+".")
				return e
			}
			runID = fieldText(page.Items[0].Fields, "run_id")
			if runID == "" {
				return fmt.Errorf("avrea returned a workflow run without a run_id")
			}
			_, _ = fmt.Fprintf(d.Err, "Auto-selected: %s (%s)\n", output.CellText(runID), output.CellText(fieldText(page.Items[0].Fields, "display_title")))
		}
		streaming := ndjson || !d.IsTerminal()
		slug := ""
		if !streaming {
			record, e := api.RunByReference(cmd.Context(), orgID, runID, false, nil)
			if e != nil {
				return apiError(cfg, e, "look up workflow run before watch")
			}
			if fieldText(record.Fields, "status") == "completed" {
				conclusion := fieldText(record.Fields, "conclusion")
				if conclusion == "" {
					conclusion = "completed"
				}
				_, _ = fmt.Fprintln(d.Err, d.errorPaint(fmt.Sprintf("Run %s already finished (%s). Nothing to watch — see `avr run view %s`.", output.CellText(runID), output.CellText(conclusion), output.CellText(runID)), "33"))
				return watchExit(record.Fields, exitStatus)
			}
			if linksEnabled(cmd, d) {
				slug = orgSlug(cmd.Context(), cfg, api, orgID)
			}
		}
		err = watchRun(cmd.Context(), d, cfg, api, orgID, runID, time.Duration(interval)*time.Second, exitStatus, streaming, slug)
		if errors.Is(err, context.Canceled) {
			if !streaming {
				_, _ = fmt.Fprintln(d.Out, "\nStopped watching.")
				return &ExitError{Code: 130}
			}
			return &ExitError{130, "\nAborted!"}
		}
		return err
	})
	return cmd
}

func watchExit(fields map[string]any, exitStatus bool) error {
	if exitStatus && fieldText(fields, "conclusion") != "success" {
		return &ExitError{Code: 1}
	}
	return nil
}

func retryReason(err error) (string, bool) {
	var httpErr *client.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.Status == 429 || httpErr.Status >= 500 {
			return "http_" + strconv.Itoa(httpErr.Status), true
		}
		return "", false
	}
	var transport *client.TransportError
	if errors.As(err, &transport) {
		return transport.Reason, true
	}
	return "", false
}

func watchRun(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, runID string, interval time.Duration, exitStatus, streaming bool, slug string) error {
	lastState := map[string]string{}
	failures := 0
	encoder := json.NewEncoder(d.Out)
	encoder.SetEscapeHTML(false)
	emit := func(event map[string]any) error {
		if !streaming {
			return nil
		}
		event["timestamp"] = d.Now().UTC().Format(time.RFC3339Nano)
		event["run_id"] = runID
		return encoder.Encode(event)
	}
	if !streaming && d.IsTerminal() {
		// Push/pop terminal title, and always restore the visible cursor on exit.
		_, _ = fmt.Fprint(d.Out, "\x1b[22;0t\x1b[?25l")
		defer func() { _, _ = fmt.Fprint(d.Out, "\x1b[23;0t\x1b[?25h") }()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		include := []string{"jobs"}
		if !streaming {
			include = append(include, "workflow")
		}
		record, err := api.RunByReference(ctx, org, runID, false, url.Values{"include": include})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			reason, retry := retryReason(err)
			if !retry {
				return apiError(cfg, err, "get workflow run")
			}
			failures++
			if e := emit(map[string]any{"event": "transient_error", "reason": reason, "consecutive_failures": failures}); e != nil {
				return e
			}
			var httpErr *client.HTTPError
			if errors.As(err, &httpErr) {
				if streaming {
					_, _ = fmt.Fprintf(d.Err, "Error fetching run (HTTP %d); retrying.\n", httpErr.Status)
				} else {
					_, _ = fmt.Fprintln(d.Err, d.errorPaint(fmt.Sprintf("  [retry — HTTP %d]", httpErr.Status), "33"))
				}
			} else {
				if streaming {
					_, _ = fmt.Fprintln(d.Err, "Couldn't reach Avrea; retrying.")
				} else {
					_, _ = fmt.Fprintln(d.Err, d.errorPaint("  [retry — couldn't reach Avrea: "+reason+"]", "33"))
				}
			}
			// The Python event stream aborts after five consecutive failures. The
			// interactive watch keeps its last frame and retries until interrupted.
			if streaming && failures >= 5 {
				if e := emit(map[string]any{"event": "watch_aborted", "reason": reason}); e != nil {
					return e
				}
				return &ExitError{Code: 1}
			}
		} else {
			failures = 0
			fields := record.Fields
			for _, job := range objectList(fields["jobs"]) {
				id := fieldText(job, "job_id")
				state := fieldText(job, "state")
				if id == "" || lastState[id] == state {
					continue
				}
				lastState[id] = state
				event := ""
				switch state {
				case "in_progress":
					event = "job_started"
				case "completed":
					event = "job_completed"
				}
				if event != "" {
					if e := emit(map[string]any{"event": event, "ci_provider": runCIValue(fields), "avrea_job_id": job["job_id"], "platform_job_id": job["platform_job_id"], "job_name": job["job_name"], "state": job["state"], "conclusion": job["conclusion"]}); e != nil {
						return e
					}
				}
			}
			if !streaming {
				if e := renderWatch(d, cfg, fields, runID, slug); e != nil {
					return e
				}
			}
			if fieldText(fields, "status") == "completed" {
				if e := emit(map[string]any{"event": "run_completed", "ci_provider": runCIValue(fields), "status": fields["status"], "conclusion": fields["conclusion"], "duration_seconds": fields["duration_seconds"]}); e != nil {
					return e
				}
				if !streaming {
					conclusion := fieldText(fields, "conclusion")
					if conclusion == "" {
						conclusion = "completed"
					}
					notifyCompletion(d, fmt.Sprintf("Run %s %s: %s", runLabel(fields, runID), fieldText(fields, "display_title"), conclusion))
				}
				return watchExit(fields, exitStatus)
			}
		}
		if err := d.Wait(ctx, interval); err != nil {
			return err
		}
	}
}

func objectList(value any) []map[string]any {
	items, _ := value.([]any)
	objects := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]any); ok {
			objects = append(objects, object)
		}
	}
	return objects
}

func renderWatch(d Dependencies, cfg config.Config, fields map[string]any, runID, slug string) error {
	workflow, _ := fields["workflow"].(map[string]any)
	stage := fieldText(fields, "conclusion")
	if stage == "" {
		stage = fieldText(fields, "status")
	}
	jobs := objectList(fields["jobs"])
	for _, job := range jobs {
		if fieldText(job, "state") == "in_progress" {
			stage = fieldText(job, "job_name")
			break
		}
	}
	title := fieldText(workflow, "name")
	if title == "" {
		title = "run"
	}
	header := fmt.Sprintf("Watching run %s: %s [%s] -- %s", output.CellText(runLabel(fields, runID)), output.CellText(fieldText(fields, "display_title")), output.CellText(runCIProvider(fields)), d.Now().UTC().Format("15:04:05 UTC"))
	if slug != "" {
		header = linkCell(header, consoleURL(cfg.Host)+"/org/"+url.PathEscape(slug)+"/runs/"+url.PathEscape(runID))
	}
	prefix := ""
	if d.IsTerminal() {
		prefix = fmt.Sprintf("\x1b[2J\x1b[H\x1b]2;avr ▸ %s: %s\x07", output.CellText(title), output.CellText(stage))
	}
	_, err := fmt.Fprintf(d.Out, "%s%s\n\n", prefix, header)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		_, err = fmt.Fprintln(d.Out, "  No jobs yet.")
	} else {
		err = renderJobs(d, jobs, false, cfg.Host, slug)
		if err == nil {
			_, err = fmt.Fprintln(d.Out, "\n"+d.paint("  → avr job logs --follow <job-id>    # tail logs of a running job", gray)+"\n"+d.paint("  → avr job metrics --watch <job-id>  # live CPU/mem/IO gauges", gray)+"\n"+d.paint("  → avr job ssh <job-id>              # ssh into the runner VM", gray))
		}
	}
	if err != nil {
		return err
	}
	if fieldText(fields, "status") == "completed" {
		_, err = fmt.Fprintf(d.Out, "\nRun %s completed: %s (%s)\n", output.CellText(runLabel(fields, runID)), d.paint(output.CellText(fieldText(fields, "conclusion")), conclusionColor(fieldText(fields, "conclusion"))), formatDuration(fields["duration_seconds"]))
	}
	return err
}
