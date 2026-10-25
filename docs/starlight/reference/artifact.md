---
title: avr artifact
description: "Manage workflow artifacts."
---

Manage workflow artifacts.

```sh
avr artifact [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr artifact delete`

Delete ART_ID from the selected repository.

```sh
avr artifact delete [OPTIONS] ART_ID
```

**Arguments**

- <code class="cli-arg">ARTIFACT_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug; defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository name or ID; defaults to the current git repository.
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip confirmation.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr artifact download`

Download ART_ID and verify its size and SHA-256 before replacing the destination.

```sh
avr artifact download [OPTIONS] ART_ID
```

**Arguments**

- <code class="cli-arg">ARTIFACT_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug; defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository name or ID; defaults to the current git repository.
- <code class="cli-flag">&#x2D;&#x2D;path</code> <code class="cli-value">&lt;PATH&gt;</code> — Destination directory or filename. ZIPs stay archived. _(default: `.`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr artifact get`

Get metadata for ART_ID in the selected repository.

```sh
avr artifact get [OPTIONS] ART_ID
```

**Arguments**

- <code class="cli-arg">ARTIFACT_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug; defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository name or ID; defaults to the current git repository.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr artifact list`

List artifacts in REPO, optionally restricted to one run.

```sh
avr artifact list [OPTIONS] REPO
```

**Arguments**

- <code class="cli-arg">REPO</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug.
- <code class="cli-flag">&#x2D;&#x2D;run-id</code> <code class="cli-value">&lt;TEXT&gt;</code> — Numeric GitHub run ID, run- ID, or native awr- ID.
- <code class="cli-flag">&#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Maximum artifacts per page. _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Continue from a previous page's cursor.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
