// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func workflowDescription(path string) docgen.Description {
	switch path {
	case "avr workflow list":
		return docgen.Description{
			Help: "List workflows with aggregate run stats.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr workflow list\n" +
				"    avr workflow list --repo acme/web\n" +
				"    avr workflow list --since 7d\n" +
				"    avr workflow list --limit 5\n" +
				"    avr workflow list --since all\n" +
				"    avr workflow list --json name,runs,median_duration_seconds\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    completed_runs, failure_count, flaked_count, median_duration_seconds, name,\n" +
				"    path, platform_workflow_id, repository, runs, workflow_id\n",
			Short: "List workflows with aggregate run stats.",
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Uses default org if not specified (see: avr config set org).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_ids", Kind: "option", Opts: []string{"--repo"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Filter by repository (org/repo or rep-xxx ID, repeatable).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "since", Kind: "option", Opts: []string{"--since"}, Type: "text", Nargs: 1, Help: descriptionText("Time window: '30d', '7d', '24h', or 'all'."), ShowDefault: true, Default: "30d"}, Suffix: "  [default: 30d]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Max workflows to show."), ShowDefault: true, Default: 20}, Suffix: "  [default: 20; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr workflow run":
		return docgen.Description{
			Help: "Trigger a workflow_dispatch event.\n" +
				"\n" +
				"WORKFLOW can be an Avrea workflow ID (wfl-... or awf-...), a GitHub\n" +
				"numeric workflow ID, a workflow path, or the workflow's display name.\n" +
				"Bare filenames (build.yml) select GitHub Actions; use the full\n" +
				".avrea/workflows/build.yml path for Avrea CI.\n" +
				"\n" +
				"Native -f inputs use the workflow's declared types. JSON preserves\n" +
				"booleans and numbers. A receipt identifies the exact native run. If a\n" +
				"response is lost, use the printed retry command to recover the same\n" +
				"invocation with its resolved ref, inputs and --idempotency-key.\n" +
				"--no-watch prints the accepted ref and commit plus a command to resume\n" +
				"following a pending native dispatch.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr workflow run build.yml\n" +
				"    avr workflow run \"Build and Deploy\" --ref feat/x\n" +
				"    avr workflow run wfl-abc123 -f env=prod -f region=eu\n" +
				"    echo '{\"env\":\"prod\"}' | avr workflow run build.yml --json\n" +
				"    avr workflow run build.yml --watch --exit-status\n",
			Short: "Trigger a workflow_dispatch event.",
			Usage: []string{"[OPTIONS]", "WORKFLOW"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "workflow_identifier", Kind: "argument", Opts: []string{"workflow_identifier"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_flag", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "ref", Kind: "option", Opts: []string{"-r", "--ref"}, Type: "text", Nargs: 1, Help: descriptionText("Branch or tag to run at. Defaults to the repository's default branch.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "raw_fields", Kind: "option", Opts: []string{"-f", "--raw-field"}, Type: "text", Multiple: true, Nargs: 1, Help: descriptionText("Workflow input: key=value (repeatable).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_inputs", Kind: "option", Opts: []string{"--json"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Read a JSON object of inputs from stdin.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "idempotency_key", Kind: "option", Opts: []string{"--idempotency-key"}, Type: "uuid", Nargs: 1, Help: descriptionText("For Avrea CI, reuse this key to retry the same invocation.")}, CompletionKind: "UUID"},
				{Param: docgen.Param{Name: "watch", Kind: "option", Opts: []string{"-w", "--watch"}, Secondary: []string{"-W", "--no-watch"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Poll for the new run and watch it until completion. Pass --no-watch / -W to return immediately."), ShowDefault: true, Default: true}, Suffix: "  [default: w]", CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "exit_status", Kind: "option", Opts: []string{"--exit-status"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("With --watch, exit non-zero if the run failed.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "interval", Kind: "option", Opts: []string{"--interval"}, Type: "integer", Nargs: 1, Help: descriptionText("With --watch, refresh interval in seconds."), ShowDefault: true, Default: 3}, Suffix: "  [default: 3]", CompletionKind: "Int"},
			},
		}
	case "avr workflow view":
		return docgen.Description{
			Help: "View a workflow with aggregate stats and per-job breakdown.\n" +
				"\n" +
				"WORKFLOW can be an Avrea workflow ID (wfl-...), a GitHub numeric\n" +
				"workflow ID (the integer in the GH URL), a workflow filename\n" +
				"(build.yml), or the workflow's display name. All forms except wfl-...\n" +
				"need a repository — pass --repo or run from inside the repo's git\n" +
				"checkout.\n" +
				"\n" +
				"\b\n" +
				"Examples:\n" +
				"    avr workflow view wfl-abc123\n" +
				"    avr workflow view 200589168\n" +
				"    avr workflow view ci.yml\n" +
				"    avr workflow view \"Build and Deploy\" --since 7d\n" +
				"    avr workflow view ci --json runs,median_duration_seconds\n" +
				"    avr workflow view wfl-abc123 --json '*' --jq '.jobs[].job.name'\n" +
				"\n" +
				"\b\n" +
				"JSON FIELDS\n" +
				"    completed_runs, failure_count, flaked_count, jobs, median_duration_seconds,\n" +
				"    name, p95_duration_seconds, path, platform_workflow_id, repository, runs,\n" +
				"    workflow_id\n",
			Short:          "View a workflow with aggregate stats and per-job breakdown.",
			CompletionHelp: "View a workflow with aggregate stats and...",
			Usage:          []string{"[OPTIONS]", "WORKFLOW"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "workflow_identifier", Kind: "argument", Opts: []string{"workflow_identifier"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_flag", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (org/repo or rep-xxx). Auto-detected from git remote when WORKFLOW is a filename or display name.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "since", Kind: "option", Opts: []string{"--since"}, Type: "text", Nargs: 1, Help: descriptionText("Time window: '30d', '7d', '24h', or 'all'."), ShowDefault: true, Default: "30d"}, Suffix: "  [default: 30d]", CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "web", Kind: "option", Opts: []string{"--web"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Open in browser.")}, CompletionKind: "Bool"},
			},
		}
	case "avr workflow":
		return docgen.Description{
			Help:     "List and view workflow definitions.",
			Short:    "List and view workflow definitions.",
			Usage:    []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections: true,
			Commands: []docgen.HelpCommand{
				{Name: "list", Description: "List workflows with aggregate run stats.", AutoShort: true},
				{Name: "run", Description: "Trigger a workflow_dispatch event.", AutoShort: true},
				{Name: "view", Description: "View a workflow with aggregate stats and per-job breakdown.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
