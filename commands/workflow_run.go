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

var workflowIDPattern = lazyRegexp(`^wfl-[0-9a-f]{32}$`)

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
	var org, repo, ref, invocationKey string
	var fields []string
	var jsonInputs, watch, exitStatus bool
	var interval int
	c := &cobra.Command{Use: "run WORKFLOW", Short: "Trigger a workflow_dispatch event.", Args: cobra.ExactArgs(1)}
	f := c.Flags()
	f.StringVarP(&org, "org", "o", "", "Organization ID or slug.")
	f.StringVarP(&repo, "repo", "R", "", "Repository name or ID; otherwise detect from Git.")
	f.StringVarP(&ref, "ref", "r", "", "Branch or tag; defaults to the repository default branch.")
	f.StringArrayVarP(&fields, "raw-field", "f", nil, "Workflow input key=value (repeatable).")
	f.BoolVar(&jsonInputs, "json", false, "Read a JSON object of inputs from stdin.")
	f.BoolVarP(&watch, "watch", "w", true, "Find the new run and watch it until completion.")
	f.VarP(inverseBool{&watch}, "no-watch", "W", "Return immediately after dispatch.")
	f.Lookup("no-watch").NoOptDefVal = "true"
	f.BoolVar(&exitStatus, "exit-status", false, "With --watch, exit unsuccessfully if the run fails.")
	f.IntVar(&interval, "interval", 3, "Watch refresh interval in seconds.")
	invocationKeyFlag(c, &invocationKey, "For Avrea CI, reuse this key to retry or resume the same dispatch.")
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
		// spelled holds --json inputs as JSON spells them, for Avrea CI.
		inputs, spelled := map[string]string{}, map[string]string(nil)
		if jsonInputs {
			if d.IsInputTerminal() {
				return &ExitError{1, "Error: --json reads inputs from stdin. Pipe a JSON object in, e.g. echo '{\"env\":\"prod\"}' | avr workflow run ... --json"}
			}
			inputs, spelled, err = parseWorkflowInputs(cmd.Context(), d.In)
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
		// workflow is what the API dispatches: a GitHub workflow file name, or
		// the Avrea workflow ID for Avrea CI. file is what the user sees.
		workflow, file, name, platform, avreaCI := identifier, identifier, "", "", false
		if strings.Contains(identifier, "/") || (!strings.HasSuffix(identifier, ".yml") && !strings.HasSuffix(identifier, ".yaml")) {
			wf, err := lookupWorkflow(cmd.Context(), cfg, api, orgID, repoID, identifier)
			if err != nil {
				return err
			}
			workflowPath := fieldText(wf, "path")
			if workflowPath == "" {
				return &ExitError{1, "Error: Workflow " + output.CellText(identifier) + " has no file path on disk and can't be dispatched. Pass a filename like 'build.yml' explicitly."}
			}
			name = fieldText(wf, "name")
			switch provider := fieldText(wf, "ci_provider"); provider {
			case "avrea_ci":
				workflow, file, avreaCI = fieldText(wf, "workflow_id"), workflowPath, true
				if !workflowIDPattern().MatchString(workflow) {
					return &ExitError{1, "Error: The API returned an Avrea CI workflow without a valid workflow ID."}
				}
			case "", "github_actions":
				workflow, file, platform = path.Base(workflowPath), path.Base(workflowPath), fieldText(wf, "platform_id")
			case "gitlab_ci":
				return &ExitError{1, "Error: Manual dispatch is not available for GitLab CI."}
			default:
				return &ExitError{1, fmt.Sprintf("Error: Unsupported CI provider: %s", output.CellText(provider))}
			}
		}
		if invocationKey != "" && !avreaCI {
			return &ExitError{1, "Error: --idempotency-key is supported only for Avrea CI workflows."}
		}
		shownRef := ref
		if !f.Changed("ref") {
			record, err := api.Repository(cmd.Context(), orgID, repoID)
			if err != nil {
				return apiError(cfg, err, "get repository")
			}
			if record["default_branch"] == nil {
				return &ExitError{1, "Error: Could not determine the repository's default branch (not synced yet). Pass --ref <branch> explicitly."}
			}
			ref = fieldText(record, "default_branch")
			shownRef = ref
			_, _ = fmt.Fprintln(d.Err, d.errorPaint("(using default branch: "+output.CellText(ref)+")", "2"))
			if avreaCI {
				// Avrea CI refuses a short name that a tag shares.
				ref = "refs/heads/" + ref
			}
		}
		dispatched := d.Now()
		var response map[string]any
		resume, follow := "", ""
		if avreaCI {
			if invocationKey == "" {
				if invocationKey, err = newInvocationKey(); err != nil {
					return err
				}
			}
			if spelled != nil {
				inputs = spelled
			}
			var flags []string
			if exitStatus {
				flags = append(flags, "--exit-status")
			}
			if f.Changed("interval") {
				flags = append(flags, "--interval", strconv.Itoa(interval))
			}
			follow = avreaCIDispatchCommand(cfg, orgID, repoID, workflow, ref, invocationKey, inputs, flags...)
			resume = follow
			if !watch {
				resume += " --no-watch"
			}
			// Printed first: an interrupted dispatch may already be accepted,
			// and only this command repeats it without starting another run.
			_, _ = fmt.Fprintln(d.Err, d.errorPaint("Retry or resume this dispatch with:\n  "+resume, "2"))
			response, err = submitAvreaCIDispatch(cmd.Context(), d, api, orgID, repoID, workflow, ref, invocationKey, resume, inputs)
			var exit *ExitError
			if errors.As(err, &exit) || cmd.Context().Err() != nil {
				return err
			}
		} else {
			response, err = api.DispatchWorkflow(cmd.Context(), orgID, repoID, workflow, ref, inputs)
		}
		if err != nil {
			failure := apiError(cfg, err, "dispatch workflow")
			var he *client.HTTPError
			hint := ""
			if errors.As(err, &he) {
				if !strings.Contains(identifier, "/") && (strings.HasSuffix(identifier, ".yml") || strings.HasSuffix(identifier, ".yaml")) && (he.Status == 404 || he.Status == 422) {
					hint = "\n  Hint: For Avrea CI, pass the full .avrea/workflows/ path or a wfl- workflow ID."
				}
			}
			if hint != "" {
				message := failure.Error()
				if before, after, ok := strings.Cut(message, "\n  API:"); ok {
					message = before + hint + "\n  API:" + after
				} else {
					message += hint
				}
				return errors.New(message)
			}
			return failure
		}
		label := d.paint(output.CellText(file), "35;1")
		if name != "" && name != file {
			label = d.paint(output.CellText(name), "35;1") + " (" + d.paint(output.CellText(file), "35") + ")"
		}
		if _, err := fmt.Fprintf(d.Out, "%s Triggered %s at %s.\n", d.paint("✓", "32"), label, d.paint(output.CellText(shownRef), "1")); err != nil {
			return err
		}
		links := linksEnabled(cmd, d)
		runID := fieldText(response, "platform_run_id")
		if runID != "" {
			label := d.paint(output.CellText(runID), "96;1")
			if links {
				fullName := ""
				if strings.Contains(repo, "/") {
					fullName = repo
				} else if record, err := api.Repository(cmd.Context(), orgID, repoID); err == nil {
					fullName = fieldText(record, "full_name")
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
		if !watch && avreaCI {
			if run := fieldText(response, "run_id"); run != "" {
				return printDispatchedRun(d, cfg, orgSlug(cmd.Context(), cfg, api, orgID), run)
			}
			_, err = fmt.Fprintln(d.Err, d.errorPaint("  The run has not started yet. Follow the run with:\n  "+follow, "2"))
			return err
		}
		if !watch {
			_, err = fmt.Fprintln(d.Err, d.errorPaint("  → avr run list --repo "+output.CellText(hintRepo)+"    # avrea sync lands shortly", "2"))
			return err
		}
		_, _ = fmt.Fprintln(d.Err, d.errorPaint("Waiting for run to appear in Avrea...", "2"))
		if avreaCI {
			run := fieldText(response, "run_id")
			if run == "" {
				var read bool
				if run, read, err = awaitAvreaCIRun(cmd.Context(), d, cfg, api, orgID, repoID, fieldText(response, "admission_id")); err != nil {
					return err
				}
				if !read {
					return &ExitError{1, "Error: Could not read the dispatch receipt for 90 seconds, so the run is unknown. Resume with:\n  " + resume}
				}
			}
			if run == "" {
				message := "Avrea CI has not started the run 90 seconds after the dispatch. Resume with:\n  " + resume
				if exitStatus {
					// The exit code reports the dispatched run, which was never seen.
					return &ExitError{1, "Error: " + message}
				}
				_, err = fmt.Fprintln(d.Err, d.errorPaint("  "+message, "2"))
				return err
			}
			return watchDispatchedWorkflow(cmd, d, cfg, api, orgID, run, interval, exitStatus)
		}
		target := dispatchTarget{at: dispatched, workflow: platform, file: file, ref: ref, runID: runID}
		row, err := pollDispatchedRun(cmd.Context(), d, cfg, api, orgID, repoID, target)
		if err != nil {
			return err
		}
		if row == nil {
			// --org keeps the suggestions on the organization this dispatch used.
			orgArg := " --org " + output.CellText(orgSlug(cmd.Context(), cfg, api, orgID))
			resume := "avr run watch <run-id>" + orgArg
			if runID != "" {
				resume = "avr run watch " + runID + orgArg
			}
			message := "Avrea has not recorded the run 90 seconds after the dispatch. Once `avr run list --repo " + output.CellText(hintRepo) + orgArg + "` shows it, follow it with `" + resume + "`."
			if exitStatus {
				// The exit code reports the dispatched run, which was never seen.
				return &ExitError{1, "Error: " + message}
			}
			_, err = fmt.Fprintln(d.Err, d.errorPaint("  "+message, "2"))
			return err
		}
		if runID == "" {
			_, _ = fmt.Fprintln(d.Err, d.errorPaint("  GitHub returned no run ID; following the newest workflow_dispatch run of this workflow on "+output.CellText(ref)+", which a concurrent dispatch could also have started.", "2"))
		}
		return watchDispatchedWorkflow(cmd, d, cfg, api, orgID, fieldText(row, "run_id"), interval, exitStatus)
	})
	return c
}

func printDispatchedRun(d Dependencies, cfg config.Config, slug, runID string) error {
	_, err := fmt.Fprintln(d.Out, "  Run: "+d.paint(output.CellText(runID), "96;1")+"\n  "+consoleURL(cfg.Host)+"/org/"+output.CellText(slug)+"/runs/"+output.CellText(runID))
	return err
}

func watchDispatchedWorkflow(cmd *cobra.Command, d Dependencies, cfg config.Config, api *client.Client, orgID, runID string, interval int, exitStatus bool) error {
	slug := orgSlug(cmd.Context(), cfg, api, orgID)
	if runID != "" {
		if err := printDispatchedRun(d, cfg, slug, runID); err != nil {
			return err
		}
	}
	if !linksEnabled(cmd, d) {
		slug = ""
	}
	return watchRun(cmd.Context(), d, cfg, api, orgID, runID, time.Duration(interval)*time.Second, exitStatus, false, slug)
}

// dispatchTarget identifies the run a GitHub dispatch started. GitHub returns
// the run ID for most dispatches; without it, the closest match is a
// workflow_dispatch run of the same workflow on the dispatched ref, created
// after the dispatch. A workflow is its GitHub ID when known, otherwise its
// file name. A run records its branch or tag by short name, whichever form
// --ref used.
type dispatchTarget struct {
	at                         time.Time
	workflow, file, ref, runID string
}

func dispatchMatches(row map[string]any, target dispatchTarget) bool {
	// Both providers share the repository's run list. A row without
	// ci_provider is GitHub Actions, as everywhere else in the CLI.
	if runCIProvider(row) != "github_actions" {
		return false
	}
	if target.runID != "" {
		return fieldText(row, "platform_run_id") == target.runID
	}
	if fieldText(row, "event") != "workflow_dispatch" {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, fieldText(row, "created_at"))
	if err != nil || created.Before(target.at.Add(-5*time.Second)) {
		return false
	}
	ref := strings.TrimPrefix(strings.TrimPrefix(target.ref, "refs/heads/"), "refs/tags/")
	if fieldText(row, "head_branch") != ref {
		return false
	}
	if target.workflow != "" {
		return fieldText(row, "platform_workflow_id") == target.workflow
	}
	return path.Base(fieldText(row, "workflow_path")) == path.Base(target.file)
}

func pollDispatchedRun(ctx context.Context, d Dependencies, cfg config.Config, api *client.Client, org, repo string, target dispatchTarget) (map[string]any, error) {
	page := url.Values{"repository_ids": {repo}, "limit": {"100"}, "order": {"created_at.desc"}}
	queries := []url.Values{page}
	if target.runID != "" {
		// The exact lookup finds the run however busy the repository is, but
		// covers only GitHub.com runs; a run from a registered GitHub host is
		// still found on the repository's newest page.
		byID := url.Values{"repository_ids": {repo}, "platform_run_id": {target.runID}, "limit": {"10"}, "order": {"created_at.desc"}}
		queries = []url.Values{byID, page}
	}
	return pollRuns(ctx, d, cfg, api, org, queries, 90*time.Second, "poll for dispatched run", true, func(row map[string]any) bool {
		return dispatchMatches(row, target)
	})
}
