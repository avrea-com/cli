---
title: avr run
description: "View and manage GitHub workflow runs."
---

View and manage GitHub workflow runs.

```sh
avr run [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr run cancel`

Cancel an in-progress or queued workflow run.

```sh
avr run cancel [OPTIONS] RUN
```

RUN accepts the same Avrea IDs, GitHub run IDs, and run URLs as
`avr run view`.

```sh
Examples:
    avr run cancel run-abc123
    avr run cancel run-abc123 --yes
```

**Arguments**

- <code class="cli-arg">RUN</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug.
- <code class="cli-flag">-y, &#x2D;&#x2D;yes</code> — Skip the confirmation prompt.

### `avr run diagnose`

Explain a failed or unexpectedly slow workflow run.

```sh
avr run diagnose [OPTIONS] RUN
```

RUN accepts the same Avrea IDs, GitHub run IDs, and run URLs as
`avr run view`. The report combines jobs and failed steps, bounded
failed-job log tails, queue/execution timings, runner metrics, and a
prior-success workflow baseline.

```sh
Examples:
    avr run diagnose run-abc123
    avr run diagnose 123456789 --json
    avr run diagnose https://github.com/acme/widgets/actions/runs/123456789
```

**Arguments**

- <code class="cli-arg">RUN</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug.
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Output the diagnostic report as JSON.

### `avr run list`

List workflow runs for an organization.

```sh
avr run list [OPTIONS]
```

```sh
Examples:
    avr run list
    avr run list --status failure --limit 5
    avr run list --status in_progress --json status,conclusion,head_branch
    avr run list --branch main --status completed
    avr run list --since 24h
    avr run list --json '?'           # list available fields
    avr run list --json '*'           # all fields
    avr run list --json status,conclusion -q '[.[] | select(.status == "completed")]'
```

```sh
JSON FIELDS
    conclusion, created_at, display_title, duration_seconds, event,
    head_branch, head_sha, platform_run_id, repository, run_attempt, run_id,
    run_number, status, triggering_actor, updated_at, workflow, workflow_id
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by repository (org/repo or rep-xxx). Pass --repo more than once to filter multiple repositories. Auto-detected from git remote if omitted. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;status</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Filter by state (queued, in_progress, completed) or conclusion (success, failure, ...). Repeatable. _(choices: `action_required`, `cancelled`, `completed`, `failure`, `in_progress`, `neutral`, `queued`, `skipped`, `stale`, `startup_failure`, `success`, `timed_out` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;branch</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by head branch (repeatable). _(repeatable)_
- <code class="cli-flag">-w, &#x2D;&#x2D;workflow</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by workflow ID (wfl-xxx, repeatable). _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;since</code> <code class="cli-value">&lt;TEXT&gt;</code> — Relative time window: '7d', '24h', etc. Sugar for --created-after.
- <code class="cli-flag">&#x2D;&#x2D;from, &#x2D;&#x2D;created-after</code> <code class="cli-value">&lt;TEXT&gt;</code> — Only runs created after this ISO timestamp.
- <code class="cli-flag">&#x2D;&#x2D;to, &#x2D;&#x2D;created-before</code> <code class="cli-value">&lt;TEXT&gt;</code> — Only runs created before this ISO timestamp.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max runs to return. _(default: `20`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Pagination cursor from a previous response.
- <code class="cli-flag">&#x2D;&#x2D;order</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Sort order. _(choices: `created_at.desc`, `created_at.asc` · default: `created_at.desc`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
- <code class="cli-flag">&#x2D;&#x2D;web</code> — Open in browser.

### `avr run logs`

Fetch logs for a workflow run's GitHub jobs.

```sh
avr run logs [OPTIONS] RUN
```

RUN accepts the same Avrea IDs, GitHub run IDs, and run URLs as
`avr run view`.

Long-form alternative to `avr run view --log[-failed]`. Use --follow to
tail logs in real time for an in-progress job; pass --job to scope to a
specific job when a run has many.

```sh
Examples:
    avr run logs run-abc123
    avr run logs run-abc123 --failed
    avr run logs run-abc123 --job test
    avr run logs run-abc123 --follow
```

**Arguments**

- <code class="cli-arg">RUN</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug.
- <code class="cli-flag">&#x2D;&#x2D;job</code> <code class="cli-value">&lt;TEXT&gt;</code> — Restrict to GitHub jobs whose name contains this string.
- <code class="cli-flag">-f, &#x2D;&#x2D;follow</code> — Tail logs as they appear (running jobs only).
- <code class="cli-flag">&#x2D;&#x2D;failed</code> — Show only logs from failed jobs.
- <code class="cli-flag">&#x2D;&#x2D;all-levels</code> — Include diagnostic-level lines (off by default).
- <code class="cli-flag">&#x2D;&#x2D;no-pager</code> — Print directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.

### `avr run rerun`

Re-run a completed workflow run.

```sh
avr run rerun [OPTIONS] RUN
```

RUN accepts the same Avrea IDs, GitHub run IDs, and run URLs as
`avr run view`.

```sh
Examples:
    avr run rerun run-abc123
    avr run rerun run-abc123 --failed
    avr run rerun run-abc123 --yes
```

**Arguments**

- <code class="cli-arg">RUN</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug.
- <code class="cli-flag">&#x2D;&#x2D;failed</code> — Re-run only the failed jobs.
- <code class="cli-flag">-y, &#x2D;&#x2D;yes</code> — Skip the confirmation prompt.

### `avr run view`

View a workflow run with its jobs.

```sh
avr run view [OPTIONS] [RUN]
```

```sh
RUN accepts an Avrea run ID, a positive GitHub run ID, a GitHub Actions
run URL, or an Avrea console run URL. Without RUN, shows 10 most recent
runs.
```

```sh
Examples:
    avr run view
    avr run view run-abc123
    avr run view 123456789
    avr run view https://github.com/acme/widgets/actions/runs/123456789
    avr run view run-abc123 --steps
    avr run view run-abc123 --log-failed
    avr run view run-abc123 --job Build
    avr run view run-abc123 --json conclusion,jobs
    avr run view run-abc123 --json '*' --jq '.jobs[].job_name'
```

```sh
JSON FIELDS
    conclusion, created_at, display_title, duration_seconds, event,
    head_branch, head_sha, jobs, platform_run_id, repository, run_attempt,
    run_id, run_number, status, triggering_actor, updated_at, workflow,
    workflow_id
```

**Arguments**

- <code class="cli-arg">[RUN]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug.
- <code class="cli-flag">&#x2D;&#x2D;steps</code> — Expand each job to show its individual steps.
- <code class="cli-flag">&#x2D;&#x2D;log</code> — Print full logs for all jobs.
- <code class="cli-flag">&#x2D;&#x2D;log-failed</code> — Print logs only for failed steps.
- <code class="cli-flag">&#x2D;&#x2D;job</code> <code class="cli-value">&lt;TEXT&gt;</code> — Restrict view and logs to jobs whose name contains this string.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
- <code class="cli-flag">&#x2D;&#x2D;web</code> — Open in browser.
- <code class="cli-flag">&#x2D;&#x2D;no-pager</code> — Print logs directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.

### `avr run watch`

Watch a workflow run until it completes.

```sh
avr run watch [OPTIONS] [RUN]
```

```sh
RUN accepts the same Avrea IDs, GitHub run IDs, and run URLs as
`avr run view`. Without RUN, auto-selects the latest in-progress run.
Pass --repo (repeatable) to scope the auto-select to specific repositories.
```

```sh
Examples:
    avr run watch
    avr run watch --repo acme/web
    avr run watch --repo a/x --repo b/y
    avr run watch run-abc123 --exit-status
    avr run watch run-abc123 --ndjson | jq -c .
```

**Arguments**

- <code class="cli-arg">[RUN]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Scope the auto-select to a repo (org/name or rep-xxx, repeatable). Auto-detected from git remote if omitted. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;exit-status</code> — Exit non-zero if run failed.
- <code class="cli-flag">&#x2D;&#x2D;interval</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Refresh interval in seconds. _(default: `3`)_
- <code class="cli-flag">&#x2D;&#x2D;ndjson</code> — Force NDJSON event stream (default when stdout isn't a TTY).
