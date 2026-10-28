---
title: avr tests
description: "Read a job's tests, timings and recorded failure messages."
---

Read a job's tests, timings and recorded failure messages.

```sh
avr tests [OPTIONS] COMMAND [ARGS]...
```

Use avr tests JOB (or avr tests list JOB). JOB is an Avrea job ID from
avr job list. No report capture ID or configuration digest is required.

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

Use --output json for the complete response or --json FIELDS to select
fields. Continue with --cursor when pagination.next_cursor is present.
API refusal or a changed report fails the command without partial output.

**Arguments**

- <code class="cli-arg">JOB</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the job's repository.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Maximum occurrences to return; short pages are followed, up to 20 result pages. _(default: `100`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Continue from a previous response's next_cursor.
- <code class="cli-flag">&#x2D;&#x2D;output</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Output format. JSON includes status, context and the next cursor. _(choices: `text`, `json`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
