// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func runDescription(path string) docgen.Description {
	switch path {
	case "avr run cancel":
		return docgen.Description{
			Help: "Cancel an in-progress or queued workflow run.\n" +
				"\n" +
				"RUN accepts the same Avrea IDs, GitHub run IDs, and run URLs as\n" +
				"`avr run view`.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr run cancel run-019e5a2e474e71c297e452279d4bcdea\n" +
				"    avr run cancel run-019e5a2e474e71c297e452279d4bcdea --yes\n",
			Short: "Cancel an in-progress or queued workflow run.",
			Usage: []string{"[OPTIONS]", "RUN"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "run", Kind: "argument", Opts: []string{"run"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "yes", Kind: "option", Opts: []string{"-y", "--yes"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip the confirmation prompt.")}, CompletionKind: "Bool"},
			},
		}
	case "avr run diagnose":
		return docgen.Description{
			Help: "Explain a failed or unexpectedly slow workflow run.\n" +
				"\n" +
				"RUN accepts the same Avrea IDs, GitHub run IDs, and run URLs as\n" +
				"`avr run view`. A job URL selects that job's attempt; the report still\n" +
				"covers the whole attempt. The report combines jobs and failed steps, bounded\n" +
				"failed-job log tails, queue/execution timings, runner metrics, and a\n" +
				"prior-success workflow baseline.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr run diagnose run-019e5a2e474e71c297e452279d4bcdea\n" +
				"    avr run diagnose 123456789 --json\n" +
				"    avr run diagnose https://github.com/acme/widgets/actions/runs/123456789\n" +
				"    avr run diagnose https://github.com/acme/widgets/actions/runs/123456789/job/987654321\n",
			Short:          "Explain a failed or unexpectedly slow workflow run.",
			CompletionHelp: "Explain a failed or unexpectedly slow...",
			Usage:          []string{"[OPTIONS]", "RUN"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "run", Kind: "argument", Opts: []string{"run"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_output", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Output the diagnostic report as JSON.")}, CompletionKind: "Bool"},
			},
		}
	case "avr run list":
		return docgen.Description{
			Help: "List workflow runs for an organization.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr run list\n" +
				"    avr run list --status failure --limit 5\n" +
				"    avr run list --status in_progress --json status,conclusion,head_branch\n" +
				"    avr run list --branch main --status completed\n" +
				"    avr run list --since 24h\n" +
				"    avr run list --json '?'           # list available fields\n" +
				"    avr run list --json '*'           # all fields\n" +
				"    avr run list --json status,conclusion -q '[.[] | select(.status == \"completed\")]'\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    ci_provider, workflow_name, workflow_path, conclusion, created_at, display_title, duration_seconds, event,\n" +
				"    head_branch, head_sha, platform_run_id, repository, run_attempt, run_id,\n" +
				"    run_number, status, triggering_actor, updated_at, workflow, workflow_id\n",
			Short: "List workflow runs for an organization.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repository_ids", Kind: "option", Opts: []string{"--repo"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Filter by repository (org/repo or rep-xxx). Pass --repo more than once to filter multiple repositories. Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "status_values", Kind: "option", Opts: []string{"--status"}, Type: "choice", Multiple: true, Nargs: 1, Help: descriptionText("Filter by state (queued, in_progress, completed) or conclusion (success, failure, ...). Repeatable."), Choices: []string{"action_required", "cancelled", "completed", "failure", "in_progress", "neutral", "queued", "skipped", "stale", "startup_failure", "success", "timed_out"}}, CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "head_branches", Kind: "option", Opts: []string{"--branch"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Filter by head branch (repeatable).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "ci_provider", Kind: "option", Opts: []string{"--ci-provider"}, Type: "choice", Nargs: 1, Help: descriptionText("Filter by CI engine."), Choices: []string{"github_actions", "gitlab_ci", "avrea_ci"}}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "workflow_ids", Kind: "option", Opts: []string{"-w", "--workflow"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Filter by workflow ID (wfl- or awf-, repeatable).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "since", Kind: "option", Opts: []string{"--since"}, Type: "text", Nargs: 1, Help: descriptionText("Relative time window: '7d', '24h', etc. Sugar for --created-after.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "created_after", Kind: "option", Opts: []string{"--from", "--created-after"}, Type: "text", Nargs: 1, Help: descriptionText("Only runs created after this ISO timestamp.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "created_before", Kind: "option", Opts: []string{"--to", "--created-before"}, Type: "text", Nargs: 1, Help: descriptionText("Only runs created before this ISO timestamp.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max runs to return."), ShowDefault: true, Default: 20}, Suffix: "  [default: 20; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Pagination cursor from a previous response.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "order", Kind: "option", Opts: []string{"--order"}, Type: "choice", Nargs: 1, Help: descriptionText("Sort order."), ShowDefault: true, Default: "created_at.desc", Choices: []string{"created_at.desc", "created_at.asc"}}, Suffix: "  [default: created_at.desc]", CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "web", Kind: "option", Opts: []string{"--web"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Open in browser.")}, CompletionKind: "Bool"},
			},
		}
	case "avr run logs":
		return docgen.Description{
			Help: "Fetch logs for a workflow run's jobs.\n" +
				"\n" +
				"RUN accepts the same Avrea IDs, GitHub run IDs, and run URLs as\n" +
				"`avr run view`. A GitHub job URL shows only that job's logs.\n" +
				"\n" +
				"Long-form alternative to `avr run view --log[-failed]`. Use --follow to\n" +
				"tail logs in real time for an in-progress job; pass --job to scope to a\n" +
				"specific job when a run has many.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr run logs run-019e5a2e474e71c297e452279d4bcdea\n" +
				"    avr run logs run-019e5a2e474e71c297e452279d4bcdea --failed\n" +
				"    avr run logs run-019e5a2e474e71c297e452279d4bcdea --job test\n" +
				"    avr run logs run-019e5a2e474e71c297e452279d4bcdea --follow\n" +
				"    avr run logs https://github.com/acme/widgets/actions/runs/123456789/job/987654321\n",
			Short: "Fetch logs for a workflow run's jobs.",
			Usage: []string{"[OPTIONS]", "RUN"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "run", Kind: "argument", Opts: []string{"run"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "job_name_filter", Kind: "option", Opts: []string{"--job"}, Type: "text", Nargs: 1, Help: descriptionText("Restrict to GitHub jobs whose name contains this string.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "follow", Kind: "option", Opts: []string{"-f", "--follow"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Tail logs as they appear (running jobs only).")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "failed_only", Kind: "option", Opts: []string{"--failed"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Show only logs from failed jobs.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "show_all_levels", Kind: "option", Opts: []string{"--all-levels"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Include diagnostic-level lines (off by default).")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "no_pager", Kind: "option", Opts: []string{"--no-pager"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.")}, CompletionKind: "Bool"},
			},
		}
	case "avr run rerun":
		return docgen.Description{
			Help: "Re-run a completed workflow run.\n" +
				"\n" +
				"RUN accepts the same Avrea IDs, GitHub run IDs, and run URLs as\n" +
				"`avr run view`.\n" +
				"\n" +
				"Avrea CI keeps the original revision. Use --failed for failed/cancelled\n" +
				"jobs or repeat --job for selected jobs. Their downstream dependents also\n" +
				"rerun; other results are retained. The new attempt can be watched immediately.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr run rerun run-019e5a2e474e71c297e452279d4bcdea\n" +
				"    avr run rerun run-019e5a2e474e71c297e452279d4bcdea --failed\n" +
				"    avr run rerun run-019e5a2e474e71c297e452279d4bcdea --yes\n" +
				"    avr run rerun awr-019e5a2e474e71c297e452279d4bcdea --job build --job test\n",
			Short: "Re-run a completed workflow run.",
			Usage: []string{"[OPTIONS]", "RUN"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "run", Kind: "argument", Opts: []string{"run"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "failed", Kind: "option", Opts: []string{"--failed"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Re-run failed jobs and dependents; includes cancelled jobs for Avrea CI.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "job_ids", Kind: "option", Opts: []string{"--job"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Avrea job name or ID to rerun with dependents; repeat for multiple jobs.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "idempotency_key", Kind: "option", Opts: []string{"--idempotency-key"}, Type: "uuid", Nargs: 1, Help: descriptionText("Reuse a previous Avrea CI rerun request UUID.")}, CompletionKind: "UUID"},
				{Param: docgen.Param{Name: "yes", Kind: "option", Opts: []string{"-y", "--yes"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Skip the confirmation prompt.")}, CompletionKind: "Bool"},
			},
		}
	case "avr run view":
		return docgen.Description{
			Help: "View a workflow run with its jobs.\n" +
				"\n" +
				"\b\n" +
				"RUN accepts an Avrea run ID, a positive GitHub run ID, a GitHub Actions\n" +
				"run or job URL, or an Avrea console run URL. A job URL opens the attempt\n" +
				"that job belongs to. Without RUN, shows 10 most recent runs.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr run view\n" +
				"    avr run view run-019e5a2e474e71c297e452279d4bcdea\n" +
				"    avr run view 123456789\n" +
				"    avr run view https://github.com/acme/widgets/actions/runs/123456789\n" +
				"    avr run view https://github.com/acme/widgets/actions/runs/123456789/job/987654321\n" +
				"    avr run view run-019e5a2e474e71c297e452279d4bcdea --steps\n" +
				"    avr run view run-019e5a2e474e71c297e452279d4bcdea --log-failed\n" +
				"    avr run view run-019e5a2e474e71c297e452279d4bcdea --job Build\n" +
				"    avr run view run-019e5a2e474e71c297e452279d4bcdea --json conclusion,jobs\n" +
				"    avr run view run-019e5a2e474e71c297e452279d4bcdea --json '*' --jq '.jobs[].job_name'\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    ci_provider, workflow_name, workflow_path, conclusion, created_at, display_title, duration_seconds, event,\n" +
				"    head_branch, head_sha, jobs, platform_run_id, repository, run_attempt,\n" +
				"    run_id, run_number, status, triggering_actor, updated_at, workflow,\n" +
				"    workflow_id\n",
			Short: "View a workflow run with its jobs.",
			Usage: []string{"[OPTIONS]", "[RUN]"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "run", Kind: "argument", Opts: []string{"run"}, Type: "text", Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "show_steps", Kind: "option", Opts: []string{"--steps"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Expand each job to show its individual steps.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "show_log", Kind: "option", Opts: []string{"--log"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print full logs for all jobs.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "log_failed", Kind: "option", Opts: []string{"--log-failed"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print logs only for failed steps.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "job_name_filter", Kind: "option", Opts: []string{"--job"}, Type: "text", Nargs: 1, Help: descriptionText("Restrict view and logs to jobs whose name contains this string.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "web", Kind: "option", Opts: []string{"--web"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Open in browser.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "no_pager", Kind: "option", Opts: []string{"--no-pager"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print logs directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.")}, CompletionKind: "Bool"},
			},
		}
	case "avr run watch":
		return docgen.Description{
			Help: "Watch a workflow run until it completes.\n" +
				"\n" +
				"\b\n" +
				"RUN accepts the same Avrea IDs, GitHub run IDs, and run URLs as\n" +
				"`avr run view`. Without RUN, auto-selects the latest in-progress run.\n" +
				"Pass --repo (repeatable) to scope the auto-select to specific repositories.\n" +
				"\n" +
				"NDJSON job and run completion events include ci_provider\n" +
				"(github_actions, gitlab_ci, or avrea_ci). Native jobs have a null\n" +
				"platform_job_id; correlate events using run_id and avrea_job_id.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr run watch\n" +
				"    avr run watch --repo acme/web\n" +
				"    avr run watch --repo a/x --repo b/y\n" +
				"    avr run watch run-019e5a2e474e71c297e452279d4bcdea --exit-status\n" +
				"    avr run watch run-019e5a2e474e71c297e452279d4bcdea --ndjson | jq -c .\n",
			Short: "Watch a workflow run until it completes.",
			Usage: []string{"[OPTIONS]", "[RUN]"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "run", Kind: "argument", Opts: []string{"run"}, Type: "text", Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_options", Kind: "option", Opts: []string{"--repo"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Scope the auto-select to a repo (org/name or rep-xxx, repeatable). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "exit_status", Kind: "option", Opts: []string{"--exit-status"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Exit non-zero if run failed.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "interval", Kind: "option", Opts: []string{"--interval"}, Type: "integer", Nargs: 1, Help: descriptionText("Refresh interval in seconds."), ShowDefault: true, Default: 3}, Suffix: "  [default: 3]", CompletionKind: "Int"},
				{Param: docgen.Param{Name: "ndjson_output", Kind: "option", Opts: []string{"--ndjson"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Force NDJSON event stream (default when stdout isn't a TTY).")}, CompletionKind: "Bool"},
			},
		}
	case "avr run":
		return docgen.Description{
			Help:     "View and manage CI workflow runs.",
			Short:    "View and manage CI workflow runs.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "cancel", Description: "Cancel an in-progress or queued workflow run.", AutoShort: true},
				{Name: "diagnose", Description: "Explain a failed or unexpectedly slow workflow run.", AutoShort: true},
				{Name: "list", Description: "List workflow runs for an organization.", AutoShort: true},
				{Name: "logs", Description: "Fetch logs for a workflow run's jobs.", AutoShort: true},
				{Name: "rerun", Description: "Re-run a completed workflow run.", AutoShort: true},
				{Name: "view", Description: "View a workflow run with its jobs.", AutoShort: true},
				{Name: "watch", Description: "Watch a workflow run until it completes.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
