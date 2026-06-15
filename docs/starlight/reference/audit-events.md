---
title: avr audit-events
description: "View audit events for organization writes."
---

View audit events for organization writes.

```sh
avr audit-events [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr audit-events list`

List audit events for the organization.

```sh
avr audit-events list [OPTIONS]
```

```sh
JSON FIELDS
    acting_api_key_id, action, actor_type, actor_user_id, client_ip,
    created_at, event_data, event_id, resource_id, resource_type
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;resource-type</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by resource type (e.g. api_key, user).
- <code class="cli-flag">&#x2D;&#x2D;action</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by action (CREATE, UPDATE, DELETE, ...).
- <code class="cli-flag">&#x2D;&#x2D;actor-user-id</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by acting user id.
- <code class="cli-flag">&#x2D;&#x2D;from, &#x2D;&#x2D;created-after</code> <code class="cli-value">&lt;TEXT&gt;</code> — ISO-8601 lower bound (inclusive) on created_at.
- <code class="cli-flag">&#x2D;&#x2D;to, &#x2D;&#x2D;created-before</code> <code class="cli-value">&lt;TEXT&gt;</code> — ISO-8601 upper bound (exclusive) on created_at.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max events to return. _(default: `100`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Opaque cursor from a previous response's next_cursor.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
