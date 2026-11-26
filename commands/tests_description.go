// Command prose and semantic parameter descriptions. Maintain these with the command implementation.
package commands

import "github.com/avrea-com/cli/docgen"

func testsDescription(path string) docgen.Description {
	switch path {
	case "avr tests list":
		return docgen.Description{
			Help: "Read one job's latest confirmed execution, or every job of a run.\n" +
				"\n" +
				"RUN_OR_JOB accepts the same run references as avr run view, or an Avrea\n" +
				"job ID (job-). A run selects one job: --job NAME must match exactly one,\n" +
				"and without it a run must have one job. A GitHub job URL selects that job\n" +
				"first; --job must also match it.\n" +
				"\n" +
				"--all-jobs reads every job of RUN instead, job by job, each at its latest\n" +
				"execution, and lists the jobs with their counts. --status failed --status\n" +
				"errored finds a run's failures. A job whose results could not be read is\n" +
				"named with the command that reads it alone; the rest of the run is still\n" +
				"listed.\n" +
				"\n" +
				"Names, outcomes, recorded durations and available failure messages are\n" +
				"shown together, and each failed or errored row is followed by the\n" +
				"avr tests view command that reads when it started failing.\n" +
				"A failed test is data, not a command error. Missing,\n" +
				"pending, partial and expired evidence are reported explicitly; an older\n" +
				"execution is never substituted. Complete describes collected reports.\n" +
				"\n" +
				"Use --json '*' for the complete response or --json FIELDS to select\n" +
				"fields. Continue with --cursor when pagination.next_cursor is present.\n" +
				"All outcomes are included by default. Repeat --status to match any selected\n" +
				"outcome, for example --status failed --status errored. Names match literally\n" +
				"and combine with the status selection using AND. Every matching\n" +
				"occurrence is retained, including duplicate names. A continuation means\n" +
				"more of the report remains to be searched, not necessarily more matches.\n" +
				"API refusal or a changed report fails the command without partial output.\n" +
				"\n" +
				"Counts cover all recorded occurrences before filtering, including repeats.\n" +
				"Unknown counts stay unknown. Use --summary for counts and collection state\n" +
				"without downloading results; it cannot combine with test filters, --cursor\n" +
				"or an explicit --limit. Indexing, unavailable and limited listings do not\n" +
				"mean no tests were reported.\n" +
				"\n" +
				jsonFieldsHelp(testResultFields),
			Short:          "Read one job's latest confirmed execution, or every job of a run.",
			CompletionHelp: "Read one job's latest confirmed execution,...",
			Usage:          []string{"[OPTIONS]", "RUN_OR_JOB"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "target", Kind: "argument", Opts: []string{"target"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "job_name", Kind: "option", Opts: []string{"--job"}, Type: "text", Nargs: 1, Help: descriptionText("Select one run job by case-insensitive name substring.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Defaults to the configured organization.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (owner/name or rep-ID). Defaults to the job's or run's repository.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Maximum occurrences to return; short pages are followed, up to 20 result pages."), ShowDefault: true, Default: 100}, Suffix: "  [default: 100; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Continue from a previous response's next_cursor with the same filters.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "test_name", Kind: "option", Opts: []string{"--test"}, Type: "text", Nargs: 1, Help: descriptionText("Exact test name (case-sensitive; no wildcards).")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "suite", Kind: "option", Opts: []string{"--suite"}, Type: "text", Nargs: 1, Help: descriptionText("Exact suite name; combined with other filters using AND.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "class_name", Kind: "option", Opts: []string{"--class"}, Type: "text", Nargs: 1, Help: descriptionText("Exact nonempty class name.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "outcomes", Kind: "option", Opts: []string{"--status"}, Type: "choice", Multiple: true, Nargs: 1, Help: descriptionText("Recorded test outcome (repeatable; matches any selected outcome)."), Choices: testOutcomes}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "summary", Kind: "option", Opts: []string{"--summary"}, Type: "boolean", IsFlag: true, Default: false, Help: descriptionText("Show report counts and collection state without downloading test results.")}},
				{Param: docgen.Param{Name: "all_jobs", Kind: "option", Opts: []string{"--all-jobs"}, Type: "boolean", IsFlag: true, Default: false, Help: descriptionText("Read every job of RUN, job by job, instead of selecting one.")}},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr tests search":
		return docgen.Description{
			Help: "Search the tests recorded for a repository, newest report first.\n" +
				"\n" +
				"Each row is a test's latest recorded result across the repository's\n" +
				"retained reports. QUERY matches a substring of the test name, suite or\n" +
				"class, case-insensitively and literally. Repeat --status to match any\n" +
				"selected outcome, for example --status failed --status errored. With\n" +
				"--status, each row is the test's most recent result with a selected\n" +
				"outcome, so a test that has passed since still appears.\n" +
				"--framework keeps tests of the selected frameworks, and --min-duration\n" +
				"keeps tests with a result at least that long, listing that result.\n" +
				"\n" +
				"The header reports how many reports were searched and their time range.\n" +
				"Reports still being indexed or unavailable are counted, not hidden; a\n" +
				"test absent from an incomplete search may still be recorded.\n" +
				"\n" +
				"Each failed or errored row is followed by the avr tests view command\n" +
				"that reads its failure summary and when it started failing. Read a\n" +
				"test's earlier runs with avr tests history. Use --json '*' for test\n" +
				"IDs, configuration digests and result IDs.\n" +
				"\n" +
				jsonFieldsHelp(testSearchFields),
			Short:          "Search the tests recorded for a repository.",
			CompletionHelp: "Search the tests recorded for a repository.",
			Usage:          []string{"[OPTIONS]", "[QUERY]"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "query", Kind: "argument", Opts: []string{"query"}, Type: "text", Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Defaults to the configured organization.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (owner/name or rep-ID). Defaults to the current git repository.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Maximum tests to return; short pages are followed, up to 20 result pages."), ShowDefault: true, Default: 50}, Suffix: "  [default: 50; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Continue from a previous response's next_cursor with the same query and filters.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "outcomes", Kind: "option", Opts: []string{"--status"}, Type: "choice", Multiple: true, Nargs: 1, Help: descriptionText("Show each test's most recent result with one of these outcomes (repeatable). A test that has passed since still appears."), Choices: testOutcomes}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "frameworks", Kind: "option", Opts: []string{"--framework"}, Type: "choice", Multiple: true, Nargs: 1, Help: descriptionText("Test framework (repeatable; matches any selected framework)."), Choices: testFrameworks}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "min_duration", Kind: "option", Opts: []string{"--min-duration"}, Type: "text", Nargs: 1, Help: descriptionText("Only tests with a result at least this long: e.g. 500ms, 2s, 1m, or a number of milliseconds. 1 ms to 99,999,999 ms.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr tests history":
		return docgen.Description{
			Help: "Read one test's recorded runs in a repository, newest first.\n" +
				"\n" +
				"TEST is a test name. An exact name is preferred over tests that only\n" +
				"contain it. When several recorded tests match, the command lists them\n" +
				"with the test ID and configuration to pass instead; it never picks one.\n" +
				"If some reports were not searched, only an exact name can be selected.\n" +
				"\n" +
				"Each row is one report: how often the test passed, failed, errored or\n" +
				"was skipped in it, the mean recorded duration (none when the test\n" +
				"recorded no duration) and the job that ran it. Open a row's job with\n" +
				"avr tests JOB --test NAME to read its failure message.\n" +
				"\n" +
				"Reports still being prepared are marked; a read that stops early does\n" +
				"not mean the test never ran. Continue with --cursor for older runs.\n" +
				"\n" +
				jsonFieldsHelp(testHistoryFields),
			Short:          "Read one test's recorded runs in a repository, newest first.",
			CompletionHelp: "Read one test's recorded runs in a repository,...",
			Usage:          []string{"[OPTIONS]", "TEST"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "test", Kind: "argument", Opts: []string{"test"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Defaults to the configured organization.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (owner/name or rep-ID). Defaults to the current git repository.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "configuration", Kind: "option", Opts: []string{"--configuration"}, Type: "text", Nargs: 1, Help: descriptionText("Configuration digest of the test; required with a test ID.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Maximum recorded runs to return."), ShowDefault: true, Default: 20}, Suffix: "  [default: 20; 1<=x<=100]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Continue from a previous response's next_cursor.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr tests rank":
		return docgen.Description{
			Help: "Rank a repository's tests by mean duration or failed reports.\n" +
				"\n" +
				"Each row is one test under one job configuration, with statistics over\n" +
				"the repository's most recent reports: in how many it failed or errored,\n" +
				"out of those where it ran, and its mean and longest duration. Durations\n" +
				"are test time, not job time. A later passing attempt of the same job is\n" +
				"another report.\n" +
				"\n" +
				"The statistics describe the reports listed in the header, not the whole\n" +
				"retained history; the header says when older reports were left out.\n" +
				"Use avr tests history for one test's earlier runs.\n" +
				"\n" +
				"--order duration_mean_ms.desc ranks the longest mean duration first and\n" +
				"failed_reports.desc the tests that failed in the most reports. QUERY,\n" +
				"--status, --framework and --min-duration select tests as avr tests search\n" +
				"does. --status and --min-duration keep the tests with such a result and\n" +
				"show the newest one; every searched report still counts.\n" +
				"\n" +
				"Each failed or errored row is followed by the avr tests view command\n" +
				"that reads its failure summary and when it started failing. Use\n" +
				"--json '*' for every row's result ID.\n" +
				"\n" +
				jsonFieldsHelp(testRankFields),
			Short:          "Rank a repository's tests by mean duration or failed reports.",
			CompletionHelp: "Rank a repository's tests by mean duration or...",
			Usage:          []string{"[OPTIONS]", "[QUERY]"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "query", Kind: "argument", Opts: []string{"query"}, Type: "text", Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Defaults to the configured organization.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (owner/name or rep-ID). Defaults to the current git repository.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "order", Kind: "option", Opts: []string{"--order"}, Type: "choice", Nargs: 1, Help: descriptionText("Ranking order."), ShowDefault: true, Default: "duration_mean_ms.desc", Choices: testRankOrders}, Suffix: "  [default: duration_mean_ms.desc]", CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "limit", Kind: "option", Opts: []string{"-L", "--limit"}, Type: "integer range", Nargs: 1, Help: descriptionText("Maximum tests to return; short pages are followed, up to 20 result pages."), ShowDefault: true, Default: 50}, Suffix: "  [default: 50; 1<=x<=1000]", CompletionKind: "IntRange"},
				{Param: docgen.Param{Name: "cursor", Kind: "option", Opts: []string{"--cursor"}, Type: "text", Nargs: 1, Help: descriptionText("Continue from a previous response's next_cursor with the same query, order and filters.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "outcomes", Kind: "option", Opts: []string{"--status"}, Type: "choice", Multiple: true, Nargs: 1, Help: descriptionText("Rank only tests with a result of one of these outcomes (repeatable); every searched report still counts."), Choices: testOutcomes}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "frameworks", Kind: "option", Opts: []string{"--framework"}, Type: "choice", Multiple: true, Nargs: 1, Help: descriptionText("Test framework (repeatable; matches any selected framework)."), Choices: testFrameworks}, CompletionKind: "Choice", CaseSensitive: true},
				{Param: docgen.Param{Name: "min_duration", Kind: "option", Opts: []string{"--min-duration"}, Type: "text", Nargs: 1, Help: descriptionText("Only tests with a result at least this long: e.g. 500ms, 2s, 1m, or a number of milliseconds. 1 ms to 99,999,999 ms.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr tests view":
		return docgen.Description{
			Help: "Read one recorded test result and when its failure started.\n" +
				"\n" +
				"RESULT is a result_id. avr tests search, rank and list print this\n" +
				"command under their table for each failed or errored row. Every row's\n" +
				"result_id is in their --json output, for example:\n" +
				"\n" +
				"\b\n" +
				"    avr tests search login --status failed --json data -q '.data[].result_id'\n" +
				"\n" +
				"It identifies one exact occurrence; a re-run never replaces it with a\n" +
				"newer result. The result shows the test, outcome, duration, job, commit\n" +
				"and the recorded failure summary.\n" +
				"\n" +
				"For a failed or errored result, the command also checks earlier runs\n" +
				"of the same job configuration on the same branch: where the failure\n" +
				"started, the last run that passed and how many reports failed since.\n" +
				"A commit range is named only as a candidate: it holds the change only\n" +
				"when runs on the branch follow commit order. Reports still indexing or\n" +
				"unavailable are counted and say why a start could not be named.\n" +
				"\n" +
				"Use --json '*' for the result and its failure_start; failure_start is\n" +
				"null when the result did not fail, and is not read when --json leaves\n" +
				"it out. Use --web to open the result in the console.\n" +
				"\n" +
				jsonFieldsHelp(testViewFields),
			Short:          "Read one recorded test result and when its failure started.",
			CompletionHelp: "Read one recorded test result and when its...",
			Usage:          []string{"[OPTIONS]", "RESULT"},
			Parameters: []docgen.Parameter{
				{Param: docgen.Param{Name: "result", Kind: "argument", Opts: []string{"result"}, Type: "text", Required: true, Nargs: 1}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "org_id", Kind: "option", Opts: []string{"--org"}, Type: "text", Nargs: 1, Help: descriptionText("Organization ID or slug. Defaults to the configured organization.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "repo_id", Kind: "option", Opts: []string{"--repo"}, Type: "text", Nargs: 1, Help: descriptionText("Repository (owner/name or rep-ID). Defaults to the current git repository.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "web", Kind: "option", Opts: []string{"--web"}, Type: "boolean", Nargs: 1, IsFlag: true, Help: descriptionText("Open in browser.")}, CompletionKind: "Bool"},
				{Param: docgen.Param{Name: "json_fields", Kind: "option", Opts: []string{"--json"}, Type: "text", Nargs: 1, Help: descriptionText("Output JSON. Pass comma-separated field names, \"*\" for all fields, or \"?\" to list available fields.")}, CompletionKind: "String"},
				{Param: docgen.Param{Name: "jq_expr", Kind: "option", Opts: []string{"-q", "--jq"}, Type: "text", Nargs: 1, Help: descriptionText("Filter --json output through a jq expression.")}, CompletionKind: "String"},
			},
		}
	case "avr tests":
		return docgen.Description{
			Help: "Read recorded tests: a job's results, repository search and ranking, one test's history and one result.\n" +
				"\n" +
				"Use avr tests RUN_OR_JOB (or avr tests list RUN_OR_JOB) for one job's\n" +
				"tests, timings and failure messages. Accepts Avrea run/job IDs,\n" +
				"GitHub run IDs and run or job URLs, and Avrea console run URLs.\n" +
				"A single-job run is selected automatically. Use --job NAME to select\n" +
				"one job from a run with several jobs.\n" +
				"\n" +
				"Use avr tests search to find tests across a repository's reports,\n" +
				"avr tests rank to rank them by mean duration or failed reports,\n" +
				"avr tests history for one test's earlier runs, and avr tests view\n" +
				"for one result's failure summary and when its failure started.\n",
			Short:          "Read recorded tests: a job's results, repository search and ranking, one test's history and one result.",
			CompletionHelp: "Read recorded tests: a job's results, a...",
			Usage:          []string{"[OPTIONS]", "COMMAND [ARGS]..."},
			Sections:       true,
			Commands: []docgen.HelpCommand{
				{Name: "history", Description: "Read one test's recorded runs in a repository, newest first.", AutoShort: true},
				{Name: "rank", Description: "Rank a repository's tests by mean duration or failed reports.", AutoShort: true},
				{Name: "list", Description: "Read one job's latest confirmed execution, or every job of a run.", AutoShort: true},
				{Name: "search", Description: "Search the tests recorded for a repository.", AutoShort: true},
				{Name: "view", Description: "Read one recorded test result and when its failure started.", AutoShort: true},
			},
		}
	}
	return docgen.Description{}
}
