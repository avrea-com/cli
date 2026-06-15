---
title: avr cache
description: "Inspect and manage the Avrea build cache."
---

Inspect and manage the Avrea build cache.

```sh
avr cache [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr cache delete`

Delete cache entries by key name or all entries.

```sh
avr cache delete [OPTIONS]
```

Exactly one of --key or --all must be provided.
When using --key, --type is required to scope the deletion.

```sh
Examples:
    avr cache delete --repo rep-abc123 --type gha --key "node_modules" --ref refs/heads/main --yes
    avr cache delete --repo rep-abc123 --all --yes
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;type</code> <code class="cli-value">&lt;TEXT&gt;</code> — Cache type (required with --key, e.g. gha, bazel, sccache).
- <code class="cli-flag">&#x2D;&#x2D;key</code> <code class="cli-value">&lt;TEXT&gt;</code> — Delete entries matching this cache key name.
- <code class="cli-flag">&#x2D;&#x2D;ref</code> <code class="cli-value">&lt;TEXT&gt;</code> — Ref to narrow deletion scope (used by gha).
- <code class="cli-flag">&#x2D;&#x2D;all</code> — Delete ALL cache entries for the repository.
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip confirmation prompt.

### `avr cache list`

List cache entries for a repository.

```sh
avr cache list [OPTIONS]
```

```sh
Examples:
    avr cache list --repo rep-abc123
    avr cache list --repo rep-abc123 --type gha --limit 50
    avr cache list --repo rep-abc123 --key "node_modules" --ref refs/heads/main
    avr cache list --repo rep-abc123 --json key,size_bytes,created_at
```

```sh
JSON FIELDS
    cache_type, created_at, hit_count, key, last_accessed_at, ref, size_bytes, version
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;type</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by cache type (e.g. gha, bazel, turbo, rclone).
- <code class="cli-flag">&#x2D;&#x2D;key</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by key prefix.
- <code class="cli-flag">&#x2D;&#x2D;ref</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by exact ref match.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max entries to return. _(default: `100`)_
- <code class="cli-flag">&#x2D;&#x2D;offset</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Number of entries to skip. _(default: `0`)_
- <code class="cli-flag">&#x2D;&#x2D;order</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Sort order. _(choices: `created_at.desc`, `created_at.asc` · default: `created_at.desc`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
- <code class="cli-flag">&#x2D;&#x2D;web</code> — Open in browser.

### `avr cache usage`

Show cache usage summary for a repository.

```sh
avr cache usage [OPTIONS]
```

```sh
Examples:
    avr cache usage --repo rep-abc123
    avr cache usage --repo rep-abc123 --json '*'
```

```sh
JSON FIELDS
    by_type, over_quota, quota_bytes, total_size_bytes
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
