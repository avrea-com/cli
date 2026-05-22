---
title: avr log
description: "Search across runner execution logs."
---

Search across runner execution logs.

```sh
avr log [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr log search`

Search logs for a repository.

```sh
avr log search [OPTIONS]
```

Performs full-text search when --query is provided, otherwise returns
logs sorted by line number. Results are filtered to logs from repositories
you have access to.

```sh
Examples:
    avr log search --repo acme/web --query "error"
    avr log search --repo rep-abc123 --level error --limit 50
    avr log search --repo rep-abc123 --vm-id vm-xyz --stream stderr
    avr log search --repo rep-abc123 --query "OOM" --json content,timestamp,vm_id
```

```sh
JSON FIELDS
    content, group_name, id, level, line_number, repository_id, score,
    step_name, step_record_id, stream, timestamp, vm_id
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;query</code> <code class="cli-value">&lt;TEXT&gt;</code> — Full-text search query
- <code class="cli-flag">&#x2D;&#x2D;stream</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Filter by output stream _(choices: `stdout`, `stderr`)_
- <code class="cli-flag">&#x2D;&#x2D;level</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Filter by log level _(choices: `debug`, `info`, `warning`, `error`)_
- <code class="cli-flag">&#x2D;&#x2D;vm-id</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by execution/VM ID
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Maximum results to return _(default: `100`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
