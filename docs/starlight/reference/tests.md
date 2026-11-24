---
title: avr tests
description: "Read recorded tests: a job's results, a repository search and one test's history."
---

Read recorded tests: a job's results, a repository search and one test's history.

```sh
avr tests [OPTIONS] COMMAND [ARGS]...
```

Use avr tests RUN_OR_JOB (or avr tests list RUN_OR_JOB) for one job's
tests, timings and failure messages. Accepts Avrea run/job IDs,
GitHub run IDs and run or job URLs, and Avrea console run URLs.
A single-job run is selected automatically. Use --job NAME to select
one job from a run with several jobs.

Use avr tests search to find tests across a repository's reports, and
avr tests history for one test's earlier runs.

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

Read one job's latest confirmed execution, including repeated tests.

```sh
avr tests list [OPTIONS] RUN_OR_JOB
```

RUN_OR_JOB accepts the same run references as avr run view, or an Avrea
job ID (job-). A run selects one job, never combines all jobs.
--job NAME must match exactly one job; without it a run must have one job.
A GitHub job URL selects that job first; --job must also match it.

Names, outcomes, recorded durations and available failure messages are
shown together. A failed test is data, not a command error. Missing,
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
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the job's repository.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Maximum occurrences to return; short pages are followed, up to 20 result pages. _(default: `100`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Continue from a previous response's next_cursor with the same filters.
- <code class="cli-flag">&#x2D;&#x2D;test</code> <code class="cli-value">&lt;TEXT&gt;</code> — Exact test name (case-sensitive; no wildcards).
- <code class="cli-flag">&#x2D;&#x2D;suite</code> <code class="cli-value">&lt;TEXT&gt;</code> — Exact suite name; combined with other filters using AND.
- <code class="cli-flag">&#x2D;&#x2D;class</code> <code class="cli-value">&lt;TEXT&gt;</code> — Exact nonempty class name.
- <code class="cli-flag">&#x2D;&#x2D;status</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Recorded test outcome (repeatable; matches any selected outcome). _(choices: `passed`, `failed`, `skipped`, `errored` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;summary</code> — Show report counts and collection state without downloading test results.
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

The header reports how many reports were searched and their time range.
Reports still being indexed or unavailable are counted, not hidden; a
test absent from an incomplete search may still be recorded.

Read a row's failure message with avr tests JOB --status failed, and
its earlier runs with avr tests history. Use --json '*' for test IDs,
configuration digests and result IDs.

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
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
