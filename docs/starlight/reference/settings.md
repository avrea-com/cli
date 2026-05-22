---
title: avr settings
description: "View and toggle cache and runner settings."
---

View and toggle cache and runner settings.

```sh
avr settings [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr settings list`

List settings with their current values and source.

```sh
avr settings list [OPTIONS]
```

```sh
Examples:
    avr settings list --org org-abc123
    avr settings list --org org-abc123 --repo rep-xyz789
    avr settings list --repo acme/web
    avr settings list --prefix cache.
    avr settings list --json key,value,source
```

```sh
JSON FIELDS
    key, source, value
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;prefix</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by key prefix (e.g. 'cache.').
- <code class="cli-flag">&#x2D;&#x2D;web</code> — Open in browser.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr settings reset`

Remove a setting override, reverting to the inherited or default value.

```sh
avr settings reset [OPTIONS] KEY
```

```sh
Examples:
    avr settings reset cache.gha.enabled --repo rep-xyz789
    avr settings reset cache.packages.enabled --org org-abc123
```

**Arguments**

- <code class="cli-arg">KEY</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.

### `avr settings schema`

List available setting definitions.

```sh
avr settings schema [OPTIONS]
```

```sh
Examples:
    avr settings schema
    avr settings schema --prefix cache. --scope repository
    avr settings schema --json '*'
```

```sh
JSON FIELDS
    choices, default, description, inherits, key, max_value, min_value,
    scopes, value_type
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;prefix</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by key prefix (e.g. 'cache.').
- <code class="cli-flag">&#x2D;&#x2D;scope</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Filter by scope. _(choices: `repository`, `organization`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr settings set`

Set a setting value.

```sh
avr settings set [OPTIONS] KEY VALUE
```

VALUE is parsed as a boolean (true/false) or integer when possible,
otherwise treated as a string.

```sh
Examples:
    avr settings set cache.gha.enabled false --org org-abc123
    avr settings set cache.packages.enabled true --repo rep-xyz789
```

**Arguments**

- <code class="cli-arg">KEY</code>
- <code class="cli-arg">VALUE</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
