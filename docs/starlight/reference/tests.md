---
title: avr tests
description: "Read a job's tests, timings and recorded failure messages."
---

Read a job's tests, timings and recorded failure messages.

```sh
avr tests [OPTIONS] COMMAND [ARGS]...
```

Use avr tests RUN_OR_JOB (or avr tests list RUN_OR_JOB). Accepts Avrea
run/job IDs, GitHub run IDs and run or job URLs, and Avrea console run URLs.
A single-job run is selected automatically. Use --job NAME to select
one job from a run with several jobs.

## Subcommands

### `avr tests list`

Read one job's latest confirmed execution, including repeated tests.

```sh
avr tests list [OPTIONS] RUN_OR_JOB
```

RUN_OR_JOB accepts the same run references as avr run view, or an Avrea
job ID (job- or awj-). A run selects one job, never combines all jobs.
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

**Arguments**

- <code class="cli-arg">TARGET</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;job</code> <code class="cli-value">&lt;TEXT&gt;</code> — Select one run job by case-insensitive name substring.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the job's repository.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Maximum occurrences to return; short pages are followed, up to 20 result pages. _(default: `100`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Continue from a previous response's next_cursor with the same filters.
- <code class="cli-flag">&#x2D;&#x2D;test</code> <code class="cli-value">&lt;TEXT&gt;</code> — Exact test name (case-sensitive; no wildcards).
- <code class="cli-flag">&#x2D;&#x2D;suite</code> <code class="cli-value">&lt;TEXT&gt;</code> — Exact suite name; combined with other filters using AND.
- <code class="cli-flag">&#x2D;&#x2D;class</code> <code class="cli-value">&lt;TEXT&gt;</code> — Exact nonempty class name.
- <code class="cli-flag">&#x2D;&#x2D;status</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Recorded test outcome (repeatable; matches any selected outcome). _(choices: `passed`, `failed`, `skipped`, `errored` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;summary</code> — Show report counts and collection state without downloading test results.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
