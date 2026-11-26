---
title: avr workflow
description: "List and view workflow definitions."
---

List and view workflow definitions.

```sh
avr workflow [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr workflow list`

List workflows with aggregate run stats.

```sh
avr workflow list [OPTIONS]
```

**Examples**

```sh
avr workflow list
avr workflow list --repo acme/web
avr workflow list --since 7d
avr workflow list --limit 5
avr workflow list --since all
avr workflow list --json name,runs,median_duration_seconds
```

**JSON FIELDS**

`ci_provider`, `completed_runs`, `failure_count`, `flaked_count`, `median_duration_seconds`, `name`, `path`, `platform_workflow_id`, `repository`, `runs`, `workflow_id`

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by repository (org/repo or rep-xxx ID, repeatable). _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;since</code> <code class="cli-value">&lt;TEXT&gt;</code> — Time window: '30d', '7d', '24h', or 'all'. _(default: `30d`)_
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max workflows to show. _(default: `20`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr workflow run`

Trigger a workflow_dispatch event.

```sh
avr workflow run [OPTIONS] WORKFLOW
```

WORKFLOW can be an Avrea workflow ID (wfl-...), a GitHub numeric
workflow ID, a workflow path, or the workflow's display name. Bare
filenames (build.yml) select GitHub Actions; use the full
.avrea/workflows/build.yml path for Avrea CI.

Inputs are sent as strings. Avrea CI converts each value to the type
the workflow declares.

--watch follows the run the dispatch started. When GitHub returns no
run ID, it follows the newest workflow_dispatch run of the workflow
on the dispatched ref. With --exit-status, a run that does not appear
within 90 seconds exits 1. An Avrea CI dispatch that is rejected, or
whose receipt cannot be read, exits 1 with or without --exit-status.

An Avrea CI dispatch prints a command that repeats it with its
--idempotency-key. Repeating a dispatch never starts a second run:
use the command to retry after a lost response or to resume
following the run. The command is written to stderr and includes
the input values.

**Examples**

```sh
avr workflow run build.yml
avr workflow run "Build and Deploy" --ref feat/x
avr workflow run wfl-abc123 -f env=prod -f region=eu
echo '{"env":"prod"}' | avr workflow run build.yml --json
avr workflow run build.yml --watch --exit-status
```

**Arguments**

- <code class="cli-arg">WORKFLOW</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug.
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">-r, &#x2D;&#x2D;ref</code> <code class="cli-value">&lt;TEXT&gt;</code> — Branch or tag to run at. Defaults to the repository's default branch.
- <code class="cli-flag">-f, &#x2D;&#x2D;raw-field</code> <code class="cli-value">&lt;TEXT&gt;</code> — Workflow input: key=value (repeatable). _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Read a JSON object of inputs from stdin.
- <code class="cli-flag">-w, &#x2D;&#x2D;watch / -W, &#x2D;&#x2D;no-watch</code> — Poll for the new run and watch it until completion. Pass --no-watch / -W to return immediately. _(default: `True`)_
- <code class="cli-flag">&#x2D;&#x2D;exit-status</code> — With --watch, exit non-zero if the run failed.
- <code class="cli-flag">&#x2D;&#x2D;interval</code> <code class="cli-value">&lt;INTEGER&gt;</code> — With --watch, refresh interval in seconds. _(default: `3`)_
- <code class="cli-flag">&#x2D;&#x2D;idempotency-key</code> <code class="cli-value">&lt;UUID&gt;</code> — For Avrea CI, reuse this key to retry or resume the same dispatch.

### `avr workflow view`

View a workflow with aggregate stats and per-job breakdown.

```sh
avr workflow view [OPTIONS] WORKFLOW
```

WORKFLOW can be an Avrea workflow ID (wfl-...), a GitHub numeric
workflow ID (the integer in the GH URL), a workflow filename
(build.yml), or the workflow's display name. All forms except wfl-...
need a repository — pass --repo or run from inside the repo's git
checkout.

**Examples**

```sh
avr workflow view wfl-abc123
avr workflow view 200589168
avr workflow view ci.yml
avr workflow view "Build and Deploy" --since 7d
avr workflow view ci --json runs,median_duration_seconds
avr workflow view wfl-abc123 --json '*' --jq '.jobs[].job.name'
```

**JSON FIELDS**

`ci_provider`, `completed_runs`, `failure_count`, `flaked_count`, `jobs`, `median_duration_seconds`, `name`, `p95_duration_seconds`, `path`, `platform_workflow_id`, `repository`, `runs`, `workflow_id`

**Arguments**

- <code class="cli-arg">WORKFLOW</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug.
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote when WORKFLOW is a filename or display name.
- <code class="cli-flag">&#x2D;&#x2D;since</code> <code class="cli-value">&lt;TEXT&gt;</code> — Time window: '30d', '7d', '24h', or 'all'. _(default: `30d`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
- <code class="cli-flag">&#x2D;&#x2D;web</code> — Open in browser.
