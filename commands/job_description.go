// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func jobDescription(path string) docgen.Description {
	switch path {
	case "avr job list":
		return docgen.Description{
			Help: "List jobs for an organization.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr job list\n" +
				"    avr job list --status failure --limit 5\n" +
				"    avr job list --status in_progress --json job_name,state,conclusion\n" +
				"    avr job list --since 24h\n" +
				"    avr job list --json '?'           # list available fields\n" +
				"    avr job list --json '*'           # all fields\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    ci_provider, run_id, completed_at, conclusion, created_at, duration_seconds, platform_job_id,\n" +
				"    platform_run_id, platform, job_id, job_name, labels, repository, repository_id,\n" +
				"    running_on_avrea, started_at, state\n",
			Short: "List jobs for an organization.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repository_ids", Kind: "option", Opts: []string{"--repo"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Filter by repository (org/repo or rep-xxx). Pass --repo more than once to filter multiple repositories. Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "job_names", Kind: "option", Opts: []string{"--name"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Filter by job name (repeatable).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "status_values", Kind: "option", Opts: []string{"--status"}, Type: "choice", Multiple: true, Nargs: 1, Help: descriptionText("Filter by state (queued, in_progress, completed) or conclusion (success, failure, ...). Repeatable."), Choices: []string{"action_required", "cancelled", "completed", "failure", "in_progress", "neutral", "queued", "skipped", "stale", "startup_failure", "success", "timed_out"}}, CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "running_on_avrea", Kind: "option", Opts: []string{"--on-avrea"}, Secondary: []string{"--shadowing"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Filter by Avrea-run vs shadowing jobs.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "ci_provider", Kind: "option", Opts: []string{"--ci-provider"}, Type: "choice", Nargs: 1, Help: descriptionText("Filter by CI engine."), Choices: []string{"github_actions", "gitlab_ci", "avrea_ci"}}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "workflow_ids", Kind: "option", Opts: []string{"-w", "--workflow"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Filter by workflow ID (wfl- or awf-, repeatable).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "since", Kind: "option", Opts: []string{"--since"}, Type: "text", Nargs: 1, Help: descriptionText("Relative time window: '7d', '24h', etc.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max jobs to return."), ShowDefault: true, Default: 20}, Suffix: "  [default: 20; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Pagination cursor from a previous response.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "order", Kind: "option", Opts: []string{"--order"}, Type: "choice", Nargs: 1, Help: descriptionText("Sort order."), ShowDefault: true, Default: "created_at.desc", Choices: []string{"created_at.desc", "created_at.asc"}}, Suffix: "  [default: created_at.desc]", CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr job logs":
		return docgen.Description{
			Help: "View logs for a job, grouped by step.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr job logs job-abc123\n" +
				"    avr job logs job-abc123 --failed\n" +
				"    avr job logs job-abc123 --step \"Build\" --level error\n" +
				"    avr job logs job-abc123 --follow\n",
			Short: "View logs for a job, grouped by step.",
			Usage: []string{"[OPTIONS]", "JOB_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "job_id", Kind: "argument", Opts: []string{"job_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "failed", Kind: "option", Opts: []string{"--failed"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Only show logs from failed steps.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "step_name", Kind: "option", Opts: []string{"--step"}, Type: "text", Nargs: 1, Help: descriptionText("Filter to a specific step by name.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "level", Kind: "option", Opts: []string{"--level"}, Type: "choice", Nargs: 1, Help: descriptionText("Filter by log level."), Choices: []string{"debug", "info", "notice", "warning", "error"}}, CompletionKind: "Choice"},
				{Param: docgen.Param{Name: "follow", Kind: "option", Opts: []string{"--follow", "-f"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Follow logs for in-progress jobs.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "all_levels", Kind: "option", Opts: []string{"--all-levels"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Include diagnostic-level lines (hidden by default).")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "no_pager", Kind: "option", Opts: []string{"--no-pager"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.")}, CompletionKind: "Bool"},
			},
		}
	case "avr job metrics":
		return docgen.Description{
			Help: "Show CPU/memory/IO gauges for a job's VM.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr job metrics job-abc123\n" +
				"    avr job metrics job-abc123 --source cpu --source network\n" +
				"    avr job metrics job-abc123 --watch\n",
			Short: "Show CPU/memory/IO gauges for a job's VM.",
			Usage: []string{"[OPTIONS]", "JOB_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "job_id", Kind: "argument", Opts: []string{"job_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "sources", Kind: "option", Opts: []string{"--source"}, Type: "choice", Multiple: true, Nargs: 1, Help: descriptionText("Metric source (repeatable). Defaults to cpu and memory."), Choices: []string{"cpu", "memory", "filesystem", "load", "disk-io", "disk-ops", "network"}}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "start", Kind: "option", Opts: []string{"--start"}, Type: "integer", Nargs: 1, Help: descriptionText("Start time (Unix seconds). Single-shot only; defaults to execution start.")}, CompletionKind: "Int"},
				{Param: docgen.Param{Name: "end", Kind: "option", Opts: []string{"--end"}, Type: "integer", Nargs: 1, Help: descriptionText("End time (Unix seconds). Single-shot only; defaults to execution end or now.")}, CompletionKind: "Int"},
				{Param: docgen.Param{Name: "live", Kind: "option", Opts: []string{"-w", "--watch"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Stream live metrics until completion (Ctrl-C to exit).")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "json_output", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Output raw metrics responses as JSON.")}, CompletionKind: "Bool"},
			},
		}
	case "avr job ssh":
		return docgen.Description{
			Help:  "SSH into a running job's VM.",
			Short: "SSH into a running job's VM.",
			Usage: []string{"[OPTIONS]", "JOB_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "job_id", Kind: "argument", Opts: []string{"job_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "print_command", Kind: "option", Opts: []string{"--print-command"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print the SSH command instead of connecting.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "show_password", Kind: "option", Opts: []string{"--show-password"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Display the SSH password (use with caution).")}, CompletionKind: "Bool"},
			},
		}
	case "avr job view":
		return docgen.Description{
			Help: "View a single job with its steps.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr job view job-abc123\n" +
				"    avr job view job-abc123 --log-failed\n" +
				"    avr job view job-abc123 --json conclusion,steps\n" +
				"    avr job view job-abc123 --json '*' --jq '.steps[] | select(.conclusion==\"failure\")'\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    ci_provider, run_id, completed_at, conclusion, created_at, duration_seconds, platform_job_id,\n" +
				"    platform_run_id, platform, job_id, job_name, labels, repository, repository_id,\n" +
				"    running_on_avrea, started_at, state, steps, workflow_run\n",
			Short: "View a single job with its steps.",
			Usage: []string{"[OPTIONS]", "JOB_ID"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "job_id", Kind: "argument", Opts: []string{"job_id"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "show_log", Kind: "option", Opts: []string{"--log"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print full logs for the job.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "log_failed", Kind: "option", Opts: []string{"--log-failed"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print logs only for failed steps.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "web", Kind: "option", Opts: []string{"--web"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Open in browser.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "no_pager", Kind: "option", Opts: []string{"--no-pager"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Print logs directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.")}, CompletionKind: "Bool"},
			},
		}
	case "avr job watch":
		return docgen.Description{
			Help:           "Watch active jobs with auto-refresh (Ctrl+C to stop).",
			Short:          "Watch active jobs with auto-refresh (Ctrl+C to stop).",
			CompletionHelp: "Watch active jobs with auto-refresh...",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repository_ids", Kind: "option", Opts: []string{"--repo"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Filter by repository (org/repo or rep-xxx). Pass --repo more than once to filter multiple repositories. Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "job_names", Kind: "option", Opts: []string{"--name"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Filter by job name (repeatable).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "interval", Kind: "option", Opts: []string{"--interval"}, Type: "integer", Nargs: 1, Help: descriptionText("Refresh interval in seconds."), ShowDefault: true, Default: 5}, Suffix: "  [default: 5]", CompletionKind: "Int"},
				{Param: docgen.Param{Name: "ndjson_output", Kind: "option", Opts: []string{"--ndjson"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Emit one JSON object per refresh (default when stdout isn't a TTY).")}, CompletionKind: "Bool"},
			},
		}
	case "avr job":
		return docgen.Description{
			Help:     "Inspect Avrea job VMs (SSH, metrics, logs).",
			Short:    "Inspect Avrea job VMs (SSH, metrics, logs).",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "list", Description: "List jobs for an organization.", AutoShort: true},
				{Name: "logs", Description: "View logs for a job, grouped by step.", AutoShort: true},
				{Name: "metrics", Description: "Show CPU/memory/IO gauges for a job's VM.", AutoShort: true},
				{Name: "ssh", Description: "SSH into a running job's VM.", AutoShort: true},
				{Name: "view", Description: "View a single job with its steps.", AutoShort: true},
				{Name: "watch", Description: "Watch active jobs with auto-refresh (Ctrl+C to stop).", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
