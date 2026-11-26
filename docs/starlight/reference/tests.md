---
title: avr tests
description: "Read recorded tests: a job's results, repository search and ranking, one test's history and one result."
---

Read recorded tests: a job's results, repository search and ranking, one test's history and one result.

```sh
avr tests [OPTIONS] COMMAND [ARGS]...
```

Use avr tests RUN_OR_JOB (or avr tests list RUN_OR_JOB) for one job's
tests, timings and failure messages. Accepts Avrea run/job IDs,
GitHub run IDs and run or job URLs, and Avrea console run URLs.
A single-job run is selected automatically. Use --job NAME to select
one job from a run with several jobs.

Use avr tests search to find tests across a repository's reports,
avr tests rank to rank them by mean duration or failed reports,
avr tests history for one test's earlier runs, and avr tests view
for one result's failure summary and when its failure started.

## Subcommands

### `avr tests history`

Read one test's recorded runs in a repository, newest first.

```sh
avr tests history [OPTIONS] TEST
```

TEST is a test name. An exact name is preferred over tests that only
contain it. When several recorded tests match, the command lists them
with the test ID and configuration to pass instead; it never picks one.
If some reports were not searched, only an exact name can be selected.

Each row is one report: how often the test passed, failed, errored or
was skipped in it, the mean recorded duration (none when the test
recorded no duration) and the job that ran it. Open a row's job with
avr tests JOB --test NAME to read its failure message.

Reports still being prepared are marked; a read that stops early does
not mean the test never ran. Continue with --cursor for older runs.

```sh
JSON FIELDS
    configuration_digest, data, identity, organization_id, pagination,
    repository_id, scope, status, test_id
```

**Arguments**

- <code class="cli-arg">TEST</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the current git repository.
- <code class="cli-flag">&#x2D;&#x2D;configuration</code> <code class="cli-value">&lt;TEXT&gt;</code> — Configuration digest of the test; required with a test ID.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Maximum recorded runs to return. _(default: `20`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Continue from a previous response's next_cursor.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr tests list`

Read one job's latest confirmed execution, or every job of a run.

```sh
avr tests list [OPTIONS] RUN_OR_JOB
```

RUN_OR_JOB accepts the same run references as avr run view, or an Avrea
job ID (job-). A run selects one job: --job NAME must match exactly one,
and without it a run must have one job. A GitHub job URL selects that job
first; --job must also match it.

--all-jobs reads every job of RUN instead, job by job, each at its latest
execution, and lists the jobs with their counts. --status failed --status
errored finds a run's failures. A job whose results could not be read is
named with the command that reads it alone; the rest of the run is still
listed.

Names, outcomes, recorded durations and available failure messages are
shown together, and each failed or errored row is followed by the
avr tests view command that reads when it started failing.
A failed test is data, not a command error. Missing,
pending, partial and expired evidence are reported explicitly; an older
execution is never substituted. Complete describes collected reports.

Use --json '*' for the complete response or --json FIELDS to select
fields. Continue with --cursor when pagination.next_cursor is present.
All outcomes are included by default. Repeat --status to match any selected
outcome, for example --status failed --status errored. Names match literally
and combine with the status selection using AND. Every matching
occurrence is retained, including duplicate names. A continuation means
more of the report remains to be searched, not necessarily more matches.
API refusal or a changed report fails the command without partial output.

Counts cover all recorded occurrences before filtering, including repeats.
Unknown counts stay unknown. Use --summary for counts and collection state
without downloading results; it cannot combine with test filters, --cursor
or an explicit --limit. Indexing, unavailable and limited listings do not
mean no tests were reported.

```sh
JSON FIELDS
    commit_sha, counts, data, execution_id, filters, job_id, observed_at,
    organization_id, pagination, reason, reporting_state, repository_id,
    status, total_count
```

**Arguments**

- <code class="cli-arg">RUN_OR_JOB</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;job</code> <code class="cli-value">&lt;TEXT&gt;</code> — Select one run job by case-insensitive name substring.
- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the job's or run's repository.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Maximum occurrences to return; short pages are followed, up to 20 result pages. _(default: `100`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Continue from a previous response's next_cursor with the same filters.
- <code class="cli-flag">&#x2D;&#x2D;test</code> <code class="cli-value">&lt;TEXT&gt;</code> — Exact test name (case-sensitive; no wildcards).
- <code class="cli-flag">&#x2D;&#x2D;suite</code> <code class="cli-value">&lt;TEXT&gt;</code> — Exact suite name; combined with other filters using AND.
- <code class="cli-flag">&#x2D;&#x2D;class</code> <code class="cli-value">&lt;TEXT&gt;</code> — Exact nonempty class name.
- <code class="cli-flag">&#x2D;&#x2D;status</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Recorded test outcome (repeatable; matches any selected outcome). _(choices: `passed`, `failed`, `skipped`, `errored` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;summary</code> — Show report counts and collection state without downloading test results.
- <code class="cli-flag">&#x2D;&#x2D;all-jobs</code> — Read every job of RUN, job by job, instead of selecting one.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr tests rank`

Rank a repository's tests by mean duration or failed reports.

```sh
avr tests rank [OPTIONS] [QUERY]
```

Each row is one test under one job configuration, with statistics over
the repository's most recent reports: in how many it failed or errored,
out of those where it ran, and its mean and longest duration. Durations
are test time, not job time. A later passing attempt of the same job is
another report.

The statistics describe the reports listed in the header, not the whole
retained history; the header says when older reports were left out.
Use avr tests history for one test's earlier runs.

--order duration_mean_ms.desc ranks the longest mean duration first and
failed_reports.desc the tests that failed in the most reports. QUERY,
--status, --framework and --min-duration select tests as avr tests search
does. --status and --min-duration keep the tests with such a result and
show the newest one; every searched report still counts.

Each failed or errored row is followed by the avr tests view command
that reads its failure summary and when it started failing. Use
--json '*' for every row's result ID.

```sh
JSON FIELDS
    data, filters, order, organization_id, pagination, repository_id, scope,
    status, truncated
```

**Arguments**

- <code class="cli-arg">[QUERY]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the current git repository.
- <code class="cli-flag">&#x2D;&#x2D;order</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Ranking order. _(choices: `duration_mean_ms.desc`, `failed_reports.desc` · default: `duration_mean_ms.desc`)_
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Maximum tests to return; short pages are followed, up to 20 result pages. _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Continue from a previous response's next_cursor with the same query, order and filters.
- <code class="cli-flag">&#x2D;&#x2D;status</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Rank only tests with a result of one of these outcomes (repeatable); every searched report still counts. _(choices: `passed`, `failed`, `skipped`, `errored` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;framework</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Test framework (repeatable; matches any selected framework). _(choices: `pytest`, `vitest`, `jest`, `go`, `junit`, `playwright`, `bun`, `rust` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;min-duration</code> <code class="cli-value">&lt;TEXT&gt;</code> — Only tests with a result at least this long: e.g. 500ms, 2s, 1m, or a number of milliseconds. 1 ms to 99,999,999 ms.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr tests search`

Search the tests recorded for a repository.

```sh
avr tests search [OPTIONS] [QUERY]
```

Search the tests recorded for a repository, newest report first.

Each row is a test's latest recorded result across the repository's
retained reports. QUERY matches a substring of the test name, suite or
class, case-insensitively and literally. Repeat --status to match any
selected outcome, for example --status failed --status errored. With
--status, each row is the test's most recent result with a selected
outcome, so a test that has passed since still appears.
--framework keeps tests of the selected frameworks, and --min-duration
keeps tests with a result at least that long, listing that result.

The header reports how many reports were searched and their time range.
Reports still being indexed or unavailable are counted, not hidden; a
test absent from an incomplete search may still be recorded.

Each failed or errored row is followed by the avr tests view command
that reads its failure summary and when it started failing. Read a
test's earlier runs with avr tests history. Use --json '*' for test
IDs, configuration digests and result IDs.

```sh
JSON FIELDS
    data, filters, organization_id, pagination, repository_id, scope, status,
    truncated
```

**Arguments**

- <code class="cli-arg">[QUERY]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the current git repository.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Maximum tests to return; short pages are followed, up to 20 result pages. _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Continue from a previous response's next_cursor with the same query and filters.
- <code class="cli-flag">&#x2D;&#x2D;status</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Show each test's most recent result with one of these outcomes (repeatable). A test that has passed since still appears. _(choices: `passed`, `failed`, `skipped`, `errored` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;framework</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Test framework (repeatable; matches any selected framework). _(choices: `pytest`, `vitest`, `jest`, `go`, `junit`, `playwright`, `bun`, `rust` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;min-duration</code> <code class="cli-value">&lt;TEXT&gt;</code> — Only tests with a result at least this long: e.g. 500ms, 2s, 1m, or a number of milliseconds. 1 ms to 99,999,999 ms.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr tests view`

Read one recorded test result and when its failure started.

```sh
avr tests view [OPTIONS] RESULT
```

RESULT is a result_id. avr tests search, rank and list print this
command under their table for each failed or errored row. Every row's
result_id is in their --json output, for example:

```sh
avr tests search login --status failed --json data -q '.data[].result_id'
```

It identifies one exact occurrence; a re-run never replaces it with a
newer result. The result shows the test, outcome, duration, job, commit
and the recorded failure summary.

For a failed or errored result, the command also checks earlier runs
of the same job configuration on the same branch: where the failure
started, the last run that passed and how many reports failed since.
A commit range is named only as a candidate: it holds the change only
when runs on the branch follow commit order. Reports still indexing or
unavailable are counted and say why a start could not be named.

Use --json '*' for the result and its failure_start; failure_start is
null when the result did not fail, and is not read when --json leaves
it out. Use --web to open the result in the console.

```sh
JSON FIELDS
    commit_sha, data, execution_id, failure_start, job_id, organization_id,
    report_finished_at, repository_id, result_id, status
```

**Arguments**

- <code class="cli-arg">RESULT</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the current git repository.
- <code class="cli-flag">&#x2D;&#x2D;web</code> — Open in browser.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
