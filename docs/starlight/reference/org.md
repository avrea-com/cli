---
title: avr org
description: "Manage organizations and installations."
---

Manage organizations and installations.

```sh
avr org [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr org create`

Create a new organization.

```sh
avr org create [OPTIONS] NAME
```

```sh
JSON FIELDS
    name, organization_id, role, slug
```

**Arguments**

- <code class="cli-arg">NAME</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr org email-domain`

Manage email domains for automatic org membership.

```sh
avr org email-domain [OPTIONS] COMMAND [ARGS]...
```

#### `avr org email-domain list`

List email domains for automatic org membership (admin only).

```sh
avr org email-domain list [OPTIONS]
```

```sh
JSON FIELDS
    created_at, domain, organization_email_domain_id
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr org email-domain set`

Set email domains for automatic org membership (admin only).

```sh
avr org email-domain set [OPTIONS] DOMAINS...
```

Replaces all existing domains — a typo wipes the org's auto-membership
policy. Confirms before applying; pass --yes to skip the prompt (required
when stdout isn't a TTY, e.g. in CI).

```sh
Examples:
    avr org email-domain set example.com
    avr org email-domain set example.com corp.example.com --yes
```

**Arguments**

- <code class="cli-arg">DOMAINS...</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip confirmation prompt.

### `avr org install`

Manage GitHub App installations.

```sh
avr org install [OPTIONS] COMMAND [ARGS]...
```

#### `avr org install add`

Start the GitHub App installation flow.

```sh
avr org install add [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;no-browser</code> — Do not open browser automatically.
- <code class="cli-flag">&#x2D;&#x2D;wait-seconds</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Seconds to wait for detection. _(default: `120`)_

#### `avr org install list`

List accessible installations across all your organizations.

```sh
avr org install list [OPTIONS]
```

```sh
JSON FIELDS
    created_at, platform_installation_id, installation_id, organization_name,
    organization_slug, state, target_name
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr org install remove`

Remove/suspend a GitHub installation.

```sh
avr org install remove [OPTIONS]
```

Confirms before suspending; pass --yes to skip the prompt (required when
stdout isn't a TTY, e.g. in CI).

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;installation-id</code> <code class="cli-value">&lt;TEXT&gt;</code> — Installation ID to remove (ins-xxx format) _(required)_
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip confirmation prompt.

### `avr org list`

List organizations you belong to.

```sh
avr org list [OPTIONS]
```

```sh
Examples:
    avr org list
    avr org list --json slug,role
    avr org list --json '*' -q '.[] | select(.role == "admin")'
```

```sh
JSON FIELDS
    name, organization_id, role, slug
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr org members`

List organization members (admin only).

```sh
avr org members [OPTIONS]
```

```sh
Examples:
    avr org members
    avr org members --org org-abc123
    avr org members --json name,role
```

```sh
JSON FIELDS
    joined_at, name, role, user_id
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
