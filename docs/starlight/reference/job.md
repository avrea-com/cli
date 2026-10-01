---
title: avr job
description: "Inspect Avrea job VMs (SSH, metrics, logs)."
---

Inspect Avrea job VMs (SSH, metrics, logs).

```sh
avr job [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr job list`

List jobs for an organization.

```sh
avr job list [OPTIONS]
```

```sh
Examples:
    avr job list
    avr job list --status failure --limit 5
    avr job list --status in_progress --json job_name,state,conclusion
    avr job list --since 24h
    avr job list --json '?'           # list available fields
    avr job list --json '*'           # all fields
```

```sh
JSON FIELDS
    completed_at, conclusion, created_at, duration_seconds, platform_job_id,
    platform_run_id, job_id, job_name, labels, repository, repository_id,
    running_on_avrea, started_at, state
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by repository (org/repo or rep-xxx). Pass --repo more than once to filter multiple repositories. Auto-detected from git remote if omitted. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;name</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by job name (repeatable). _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;status</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Filter by state (queued, in_progress, completed) or conclusion (success, failure, ...). Repeatable. _(choices: `action_required`, `cancelled`, `completed`, `failure`, `in_progress`, `neutral`, `queued`, `skipped`, `stale`, `startup_failure`, `success`, `timed_out` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;on-avrea / &#x2D;&#x2D;shadowing</code> — Filter by Avrea-run vs shadowing jobs.
- <code class="cli-flag">-w, &#x2D;&#x2D;workflow</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by workflow ID (wfl-xxx, repeatable). _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;since</code> <code class="cli-value">&lt;TEXT&gt;</code> — Relative time window: '7d', '24h', etc.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max jobs to return. _(default: `20`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Pagination cursor from a previous response.
- <code class="cli-flag">&#x2D;&#x2D;order</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Sort order. _(choices: `created_at.desc`, `created_at.asc` · default: `created_at.desc`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr job logs`

View logs for a job, grouped by step.

```sh
avr job logs [OPTIONS] JOB_ID
```

```sh
Examples:
    avr job logs job-abc123
    avr job logs job-abc123 --failed
    avr job logs job-abc123 --step "Build" --level error
    avr job logs job-abc123 --follow
```

**Arguments**

- <code class="cli-arg">JOB_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug.
- <code class="cli-flag">&#x2D;&#x2D;failed</code> — Only show logs from failed steps.
- <code class="cli-flag">&#x2D;&#x2D;step</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter to a specific step by name.
- <code class="cli-flag">&#x2D;&#x2D;level</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Filter by log level. _(choices: `debug`, `info`, `notice`, `warning`, `error`)_
- <code class="cli-flag">&#x2D;&#x2D;follow, -f</code> — Follow logs for in-progress jobs.
- <code class="cli-flag">&#x2D;&#x2D;all-levels</code> — Include diagnostic-level lines (hidden by default).
- <code class="cli-flag">&#x2D;&#x2D;no-pager</code> — Print directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.

### `avr job metrics`

Show CPU/memory/IO gauges for a job's VM.

```sh
avr job metrics [OPTIONS] JOB_ID
```

```sh
Examples:
    avr job metrics job-abc123
    avr job metrics job-abc123 --source cpu --source network
    avr job metrics job-abc123 --watch
```

**Arguments**

- <code class="cli-arg">JOB_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug.
- <code class="cli-flag">&#x2D;&#x2D;source</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Metric source (repeatable). Defaults to cpu and memory. _(choices: `cpu`, `memory`, `filesystem`, `load`, `disk-io`, `disk-ops`, `network` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;start</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Start time (Unix seconds). Defaults to execution start.
- <code class="cli-flag">&#x2D;&#x2D;end</code> <code class="cli-value">&lt;INTEGER&gt;</code> — End time (Unix seconds). Defaults to execution end or now.
- <code class="cli-flag">-w, &#x2D;&#x2D;watch</code> — Refresh every 5 seconds (Ctrl-C to exit).
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Output raw metrics responses as JSON.

### `avr job ssh`

SSH into a running job's VM.

```sh
avr job ssh [OPTIONS] JOB_ID
```

**Arguments**

- <code class="cli-arg">JOB_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;print-command</code> — Print the SSH command instead of connecting.
- <code class="cli-flag">&#x2D;&#x2D;show-password</code> — Display the SSH password (use with caution).

### `avr job view`

View a single job with its steps.

```sh
avr job view [OPTIONS] JOB_ID
```

```sh
Examples:
    avr job view job-abc123
    avr job view job-abc123 --log-failed
    avr job view job-abc123 --json conclusion,steps
    avr job view job-abc123 --json '*' --jq '.steps[] | select(.conclusion=="failure")'
```

```sh
JSON FIELDS
    completed_at, conclusion, created_at, duration_seconds, platform_job_id,
    platform_run_id, job_id, job_name, labels, repository, repository_id,
    running_on_avrea, started_at, state, steps, workflow_run
```

**Arguments**

- <code class="cli-arg">JOB_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug.
- <code class="cli-flag">&#x2D;&#x2D;log</code> — Print full logs for the job.
- <code class="cli-flag">&#x2D;&#x2D;log-failed</code> — Print logs only for failed steps.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
- <code class="cli-flag">&#x2D;&#x2D;web</code> — Open in browser.
- <code class="cli-flag">&#x2D;&#x2D;no-pager</code> — Print logs directly to stdout instead of paging through `less`. Same as setting AVR_PAGER=''.

### `avr job watch`

Watch active jobs with auto-refresh (Ctrl+C to stop).

```sh
avr job watch [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by repository (org/repo or rep-xxx). Pass --repo more than once to filter multiple repositories. Auto-detected from git remote if omitted. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;name</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by job name (repeatable). _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;interval</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Refresh interval in seconds. _(default: `5`)_
- <code class="cli-flag">&#x2D;&#x2D;ndjson</code> — Emit one JSON object per refresh (default when stdout isn't a TTY).
