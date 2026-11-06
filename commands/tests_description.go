// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func testsDescription(path string) docgen.Description {
	switch path {
	case "avr tests list":
		return docgen.Description{
			Help: "Read one job's latest confirmed execution, including repeated tests.\n" +
				"\n" +
				"RUN_OR_JOB accepts the same run references as avr run view, or an Avrea\n" +
				"job ID (job- or awj-). A run selects one job, never combines all jobs.\n" +
				"--job NAME must match exactly one job; without it a run must have one job.\n" +
				"\n" +
				"Names, outcomes, recorded durations and available failure messages are\n" +
				"shown together. A failed test is data, not a command error. Missing,\n" +
				"pending, partial and expired evidence are reported explicitly; an older\n" +
				"execution is never substituted. Complete describes collected reports.\n" +
				"\n" +
				"Use --json '*' for the complete response or --json FIELDS to select\n" +
				"fields. Continue with --cursor when pagination.next_cursor is present.\n" +
				"Filters match literally and are combined with AND. Every matching\n" +
				"occurrence is retained, including duplicate names. A continuation means\n" +
				"more of the report remains to be searched, not necessarily more matches.\n" +
				"API refusal or a changed report fails the command without partial output.\n",
			Short:          "Read one job's latest confirmed execution, including repeated tests.",
			CompletionHelp: "Read one job's latest confirmed execution,...",
			Usage:          []string{"[OPTIONS]", "RUN_OR_JOB"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "target", Kind: "argument", Opts: []string{"target"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "job_name", Kind: "option", Opts: []string{"--job"}, Type: "text", Nargs: 1, Help: descriptionText("Select one run job by case-insensitive name substring.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Defaults to the configured organization.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (owner/name or rep-ID). Defaults to the job's repository.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Maximum occurrences to return; short pages are followed, up to 20 result pages."), ShowDefault: true, Default: 100}, Suffix: "  [default: 100; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Continue from a previous response's next_cursor with the same filters.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "test_name", Kind: "option", Opts: []string{"--test"}, Type: "text", Nargs: 1, Help: descriptionText("Exact test name (case-sensitive; no wildcards).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "suite", Kind: "option", Opts: []string{"--suite"}, Type: "text", Nargs: 1, Help: descriptionText("Exact suite name; combined with other filters using AND.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "class_name", Kind: "option", Opts: []string{"--class"}, Type: "text", Nargs: 1, Help: descriptionText("Exact nonempty class name.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "outcome", Kind: "option", Opts: []string{"--status"}, Type: "choice", Nargs: 1, Help: descriptionText("Recorded test outcome."), Choices: []string{"passed", "failed", "skipped", "errored"}}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr tests":
		return docgen.Description{
			Help: "Read a job's tests, timings and recorded failure messages.\n" +
				"\n" +
				"Use avr tests RUN_OR_JOB (or avr tests list RUN_OR_JOB). Accepts Avrea\n" +
				"run/job IDs, GitHub run IDs and run URLs, and Avrea console run URLs.\n" +
				"A single-job run is selected automatically. Use --job NAME to select\n" +
				"one job from a run with several jobs.\n",
			Short:          "Read a job's tests, timings and recorded failure messages.",
			CompletionHelp: "Read a job's tests, timings and recorded...",
			Usage:          []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections:       true,
			Commands: []docgen.HelpCommand{
				{Name: "list", Description: "Read one job's latest confirmed execution, including repeated tests.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
