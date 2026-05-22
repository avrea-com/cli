---
title: avr repo
description: "List repositories connected to Avrea."
---

List repositories connected to Avrea.

```sh
avr repo [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr repo list`

List repositories you can access in an organization.

```sh
avr repo list [OPTIONS]
```

```sh
Examples:
    avr repo list
    avr repo list --json full_name,repository_id
    avr repo list --json '*' -q '.[].full_name'
```

```sh
JSON FIELDS
    full_name, platform, platform_repository_id, repository_id
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max repositories to return. _(default: `100`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
