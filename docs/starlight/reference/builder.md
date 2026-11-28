---
title: avr builder
description: "Manage remote builder sessions."
---

Manage remote builder sessions.

```sh
avr builder [OPTIONS] COMMAND [ARGS]...
```

Builder sessions are the ephemeral VMs behind `avr build`. `avr build`
normally creates and deletes its own, but a crashed client leaves its
session running until the TTL expires — `list` finds those, `stop`
reclaims them (and their quota) immediately.

## Subcommands

### `avr builder list`

List builder sessions, newest first.

```sh
avr builder list [OPTIONS]
```

Sessions are never hard-deleted, so terminal (STOPPED/FAILED) sessions
keep appearing here; filter with --state RUNNING to see only live ones.
Non-admin members see only their own sessions.

**Examples**

```sh
avr builder list
avr builder list --state RUNNING
avr builder list --json builder_session_id,state,stop_at
```

**JSON FIELDS**

`builder_session_id`, `cpu_count`, `created_at`, `disk_gb`, `memory_mb`, `platform`, `repository_full_name`, `state`, `state_reason`, `stop_at`, `updated_at`

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;state</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Filter by state. _(choices: `PENDING`, `STARTING`, `RUNNING`, `STOPPING`, `STOPPED`, `FAILED`)_
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max sessions. _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Pagination cursor from a previous response.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr builder stop`

Stop a builder session, reclaiming its VM and quota slot.

```sh
avr builder stop [OPTIONS] SESSION_ID
```

Asynchronous while the VM is live: the session shows STOPPING until the
node confirms teardown. Stopping is idempotent — repeating it on an
already-stopped session succeeds.

**Arguments**

- <code class="cli-arg">SESSION_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip the confirmation prompt.
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit the raw API response as JSON.
