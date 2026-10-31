---
title: avr tests
description: "Read a job's tests, timings and recorded failure messages."
---

Read a job's tests, timings and recorded failure messages.

```sh
avr tests [OPTIONS] COMMAND [ARGS]...
```

Use avr tests JOB (or avr tests list JOB). JOB is an Avrea job ID
(job- or awj-) from avr job list.

## Subcommands

### `avr tests list`

Read the latest confirmed execution of JOB, including repeated tests.

```sh
avr tests list [OPTIONS] JOB
```

Names, outcomes, recorded durations and available failure messages are
shown together. A failed test is data, not a command error. Missing,
pending, partial and expired evidence are reported explicitly; an older
execution is never substituted. Complete describes collected reports.

Use --json '*' for the complete response or --json FIELDS to select
fields. Continue with --cursor when pagination.next_cursor is present.
Filters match literally and are combined with AND. Every matching
occurrence is retained, including duplicate names. A continuation means
more of the report remains to be searched, not necessarily more matches.
API refusal or a changed report fails the command without partial output.

**Arguments**

- <code class="cli-arg">JOB</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the job's repository.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Maximum occurrences to return; short pages are followed, up to 20 result pages. _(default: `100`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Continue from a previous response's next_cursor with the same filters.
- <code class="cli-flag">&#x2D;&#x2D;test</code> <code class="cli-value">&lt;TEXT&gt;</code> — Exact test name (case-sensitive; no wildcards).
- <code class="cli-flag">&#x2D;&#x2D;suite</code> <code class="cli-value">&lt;TEXT&gt;</code> — Exact suite name; combined with other filters using AND.
- <code class="cli-flag">&#x2D;&#x2D;class</code> <code class="cli-value">&lt;TEXT&gt;</code> — Exact nonempty class name.
- <code class="cli-flag">&#x2D;&#x2D;status</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Recorded test outcome. _(choices: `passed`, `failed`, `skipped`, `errored`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr tests view`

Open a saved exact result reference from avr tests JOB --json data.

```sh
avr tests view [OPTIONS] RESULT
```

Reruns never substitute a newer result. Expired reports return an explicit
status; deleted or inaccessible results fail the command. Use --repo when
opening a result outside its repository checkout.

**Arguments**

- <code class="cli-arg">RESULT</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to AVR_REPO or the current checkout.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
