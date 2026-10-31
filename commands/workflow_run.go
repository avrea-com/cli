package commands

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/avrea-com/cli/client"
	"github.com/avrea-com/cli/config"
	"github.com/avrea-com/cli/output"
	"github.com/spf13/cobra"
)

type inverseBool struct{ target *bool }

func (v inverseBool) String() string { return strconv.FormatBool(!*v.target) }
func (v inverseBool) Type() string   { return "bool" }
func (v inverseBool) Set(s string) error {
	value, err := strconv.ParseBool(s)
	if err == nil {
		*v.target = !value
	}
	return err
}

func newWorkflowRun(d Dependencies) *cobra.Command {
	var org, repo, ref string
	var fields []string
	var jsonInputs, watch, exitStatus bool
	var interval int
	c := &cobra.Command{Use: "run WORKFLOW", Short: "Trigger a workflow_dispatch event.", Args: cobra.ExactArgs(1)}
	f := c.Flags()
	f.StringVar(&org, "org", "", "Organization ID or slug.")
	f.StringVar(&repo, "repo", "", "Repository name or ID; otherwise detect from Git.")
	f.StringVarP(&ref, "ref", "r", "", "Branch or tag; defaults to the repository default branch.")
	f.StringArrayVarP(&fields, "raw-field", "f", nil, "Workflow input key=value (repeatable).")
	f.BoolVar(&jsonInputs, "json", false, "Read a JSON object of inputs from stdin.")
	f.BoolVarP(&watch, "watch", "w", true, "Find the new run and watch it until completion.")
	f.VarP(inverseBool{&watch}, "no-watch", "W", "Return immediately after dispatch.")
	f.Lookup("no-watch").NoOptDefVal = "true"
	f.BoolVar(&exitStatus, "exit-status", false, "With --watch, exit unsuccessfully if the run fails.")
	f.IntVar(&interval, "interval", 3, "Watch refresh interval in seconds.")
	c.RunE = run(func(cmd *cobra.Command, args []string) error {
		cfg, _, api, err := d.load()
		if err != nil {
			return err
		}
		if err = authenticated(cfg); err != nil {
			return err
		}
		if jsonInputs && len(fields) > 0 {
			return &ExitError{1, "Error: Use either -f/--raw-field or --json, not both."}
		}
		if interval < 1 || int64(interval) > int64((1<<63-1)/time.Second) {
			return &ExitError{1, "Error: --interval must be at least 1 second."}
		}
		inputs := map[string]string{}
		if jsonInputs {
			if d.IsInputTerminal() {
				return &ExitError{1, "Error: --json reads inputs from stdin. Pipe a JSON object in, e.g. echo '{\"env\":\"prod\"}' | avr workflow run ... --json"}
			}
			inputs, err = parseWorkflowInputs(cmd.Context(), d.In)
			if err != nil {
				return err
			}
		} else {
			for _, pair := range fields {
				key, value, ok := strings.Cut(pair, "=")
				if !ok {
					return &ExitError{1, "Error: Invalid -f value " + pythonQuote(output.CellText(pair)) + ", expected key=value"}
				}
				key = strings.TrimSpace(key)
				if key == "" {
					return &ExitError{1, "Error: Invalid -f value " + pythonQuote(output.CellText(pair)) + ", key is empty"}
				}
				inputs[key] = value
			}
		}
		orgID, err := resolveOrg(cmd.Context(), d, cfg, api, org)
		if err != nil {
			return err
		}
		repoID, err := requiredWorkflowRepo(cmd.Context(), d, cfg, api, orgID, repo)
		if err != nil {
			return err
		}
		identifier := args[0]
		filename, name, platform := identifier, "", ""
		if strings.Contains(identifier, "/") || (!strings.HasSuffix(identifier, ".yml") && !strings.HasSuffix(identifier, ".yaml")) {
			wf, err := lookupWorkflow(cmd.Context(), cfg, api, orgID, repoID, identifier)
			if err != nil {
				return err
			}
			if err := requireExternalCI(wf, "Manual workflow dispatch"); err != nil {
				return err
			}
			workflowPath := fieldText(wf, "path")
			if workflowPath == "" {
				return &ExitError{1, "Error: Workflow " + output.CellText(identifier) + " has no file path on disk and can't be dispatched. Pass a filename like 'build.yml' explicitly."}
			}
			filename, name, platform = path.Base(workflowPath), fieldText(wf, "name"), fieldText(wf, "platform_id")
		}
		if !f.Changed("ref") {
			record, err := api.Repository(cmd.Context(), orgID, repoID)
			if err != nil {
				return apiError(cfg, err, "get repository")
			}
			if record.Fields["default_branch"] == nil {
				return &ExitError{1, "Error: Could not determine the repository's default branch (not synced yet). Pass --ref <branch> explicitly."}
			}
			ref = fieldText(record.Fields, "default_branch")
			_, _ = fmt.Fprintln(d.Err, d.errorPaint("(using default branch: "+output.CellText(ref)+")", "2"))
		}
		dispatched := d.Now()
		response, err := api.DispatchWorkflow(cmd.Context(), orgID, repoID, filename, ref, inputs)
		if err != nil {
			return apiError(cfg, err, "dispatch workflow")
		}
		label := d.paint(output.CellText(filename), "35;1")
		if name != "" && name != filename {
			label = d.paint(output.CellText(name), "35;1") + " (" + d.paint(output.CellText(filename), "35") + ")"
		}
		if _, err := fmt.Fprintf(d.Out, "%s Triggered %s at %s.\n", d.paint("✓", "32"), label, d.paint(output.CellText(ref), "1")); err != nil {
			return err
		}
		links := linksEnabled(cmd, d)
		if response.Fields["platform_run_id"] != nil {
			runID := fieldText(response.Fields, "platform_run_id")
			label := d.paint(output.CellText(runID), "96;1")
			if links {
				fullName := ""
				if strings.Contains(repo, "/") {
					fullName = repo
				} else if record, err := api.Repository(cmd.Context(), orgID, repoID); err == nil {
					fullName = fieldText(record.Fields, "full_name")
				}
				if fullName != "" {
					label = linkCell(label, "https://github.com/"+fullName+"/actions/runs/"+runID)
				}
			}
			if _, err := fmt.Fprintln(d.Out, "  GitHub run id: "+label); err != nil {
				return err
			}
		}
		hintRepo := repo
		if hintRepo == "" {
			hintRepo = repoID
		}
		if !watch {
			_, err = fmt.Fprintln(d.Err, d.errorPaint("  → avr run list --repo "+output.CellText(hintRepo)+"    # avrea sync lands shortly", "2"))
			return err
		}
		_, _ = fmt.Fprintln(d.Err, d.errorPaint("Waiting for run to appear in Avrea...", "2"))
		row, err := pollDispatchedRun(cmd.Context(), d, cfg, api, orgID, repoID, platform, dispatched)
		if err != nil {
			return err
		}
		if row == nil {
			_, err = fmt.Fprintln(d.Err, d.errorPaint("  Avrea hasn't ingested the run yet after 90s. Re-run with `avr run watch <run-id>` once `avr run list --repo "+output.CellText(hintRepo)+string(rune(96))+" shows it.", "2"))
			return err
		}
		runID := fieldText(row, "run_id")
		slug := orgSlug(cmd.Context(), cfg, api, orgID)
		if runID != "" {
			if _, err := fmt.Fprintln(d.Out, "  Run: "+d.paint(output.CellText(runID), "96;1")+"\n  "+consoleURL(cfg.Host)+"/org/"+output.CellText(slug)+"/runs/"+output.CellText(runID)); err != nil {
				return err
			}
		}
		if !links {
			slug = ""
		}
		return watchRun(cmd.Context(), d, cfg, api, orgID, runID, time.Duration(interval)*time.Second, exitStatus, false, slug)
	})
	return c
}

func dispatchMatches(row map[string]any, dispatched time.Time, platform string) bool {
	created, err := time.Parse(time.RFC3339Nano, fieldText(row, "created_at"))
	if err != nil || created.Before(dispatched.Add(-5*time.Second)) {
		return false
	}
	return platform == "" || fieldText(row, "platform_workflow_id") == platform
}

func pollDispatchedRun(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, repo, platform string, dispatched time.Time) (map[string]any, error) {
	deadline := d.Now().Add(90 * time.Second)
	tick := d.Now().Add(5 * time.Second)
	finish := func() {
		if d.IsErrorTerminal() {
			_, _ = fmt.Fprintln(d.Err)
		}
	}
	for d.Now().Before(deadline) {
		page, err := api.Runs(ctx, org, url.Values{"repository_ids": {repo}, "limit": {"100"}, "order": {"created_at.desc"}})
		if err == nil {
			for _, row := range page.Items {
				if dispatchMatches(row.Fields, dispatched, platform) {
					finish()
					return row.Fields, nil
				}
			}
		} else {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var httpErr *client.HTTPError
			if errors.As(err, &httpErr) {
				if httpErr.Status < 500 && httpErr.Status != 429 {
					finish()
					return nil, apiError(cfg, err, "poll for dispatched run")
				}
			} else {
				var transport *client.TransportError
				if !errors.As(err, &transport) {
					return nil, err
				}
			}
		}
		if d.IsErrorTerminal() && !d.Now().Before(tick) {
			_, _ = fmt.Fprint(d.Err, ".")
			tick = d.Now().Add(5 * time.Second)
		}
		if err := d.Wait(ctx, time.Second); err != nil {
			return nil, err
		}
	}
	finish()
	return nil, nil
}
