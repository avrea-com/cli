---
title: avr tests
description: "Inspect recorded tests, failures, and reporting status."
---

Inspect recorded tests, failures, and reporting status.

```sh
avr tests [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr tests captures`

List report deliveries, including missing, partial and failed captures.

```sh
avr tests captures [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the git remote.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Rows per page (1-100). _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Opaque next_cursor from the previous page. One page is read per invocation.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr tests history`

Read recorded outcomes for one test and configuration, newest first.

```sh
avr tests history [OPTIONS] TEST_ID
```

**Arguments**

- <code class="cli-arg">TEST_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;configuration</code> <code class="cli-value">&lt;TEXT&gt;</code> — History grouping identifier returned by tests list; not a platform label. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the git remote.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Rows per page (1-100). _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Opaque next_cursor from the previous page. One page is read per invocation.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr tests list`

List one page of test histories, grouped by test and configuration.

```sh
avr tests list [OPTIONS]
```

Counts describe each returned history window, not repository totals.
Names appear when retained private detail is available; otherwise IDs are shown.
Use --json '*' to retain status, pagination and repository context for agents.

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the git remote.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Rows per page (1-100). _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Opaque next_cursor from the previous page. One page is read per invocation.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr tests status`

Read collection settings.

```sh
avr tests status [OPTIONS]
```

Read collection settings. Use --json for retained-history lifecycle details.

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the git remote.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr tests view`

Read the exact retained result, including available failure messages.

```sh
avr tests view [OPTIONS] TEST_ID
```

An expired or unrecorded result is never replaced with a different capture.
A recorded test failure is data, not a CLI error. No tests are rerun.

**Arguments**

- <code class="cli-arg">TEST_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;capture</code> <code class="cli-value">&lt;TEXT&gt;</code> — Exact capture from tests history. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (owner/name or rep-ID). Defaults to the git remote.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Defaults to the configured organization.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Occurrences to read (1-500). _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
