---
title: avr pr
description: "View pull requests."
---

View pull requests.

```sh
avr pr [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr pr list`

List pull requests across repositories.

```sh
avr pr list [OPTIONS]
```

```sh
Examples:
    avr pr list
    avr pr list --scope authored
    avr pr list --repo acme/widgets --state merged
    avr pr list --json number,title,mergeability
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by repository (org/repo or rep-xxx). Auto-detected from git remote if omitted. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;scope</code> <code class="cli-value">&lt;CHOICE&gt;</code> — List every readable PR, PRs you authored, or PRs you are involved in. _(choices: `all`, `authored`, `involved` · default: `all`)_
- <code class="cli-flag">&#x2D;&#x2D;state</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Filter by pull request state. 'all' removes the state filter. _(choices: `open`, `closed`, `merged`, `all` · default: `open`)_
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max PRs to return. _(default: `20`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Pagination cursor from a previous response.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
