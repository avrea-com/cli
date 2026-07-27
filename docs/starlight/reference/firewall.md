---
title: avr firewall
description: "Manage the egress firewall rule list for orgs and repositories."
---

Manage the egress firewall rule list for orgs and repositories.

```sh
avr firewall [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr firewall add`

Add a rule.

```sh
avr firewall add [OPTIONS]
```

Add a rule. Exactly one of --cidr, --fqdn, --any must be specified.

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID. If provided, adds at repo scope.
- <code class="cli-flag">&#x2D;&#x2D;action</code> <code class="cli-value">&lt;CHOICE&gt;</code> _(choices: `allow`, `deny` · required)_
- <code class="cli-flag">&#x2D;&#x2D;cidr</code> <code class="cli-value">&lt;TEXT&gt;</code> — Destination CIDR (e.g. 10.0.0.0/8 or 1.2.3.4/32).
- <code class="cli-flag">&#x2D;&#x2D;fqdn</code> <code class="cli-value">&lt;TEXT&gt;</code> — Destination hostname (e.g. api.example.com).
- <code class="cli-flag">&#x2D;&#x2D;any</code> — Catch-all (default) rule.
- <code class="cli-flag">&#x2D;&#x2D;proto</code> <code class="cli-value">&lt;CHOICE&gt;</code> _(choices: `tcp`, `udp`, `icmp`, `any` · default: `any`)_
- <code class="cli-flag">&#x2D;&#x2D;ports</code> <code class="cli-value">&lt;TEXT&gt;</code> — Port or port range (e.g. 443 or 30000-39999).
- <code class="cli-flag">&#x2D;&#x2D;position</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Insert at a specific 0-indexed position.

### `avr firewall delete`

Delete a rule by ID.

```sh
avr firewall delete [OPTIONS] RULE_ID
```

**Arguments**

- <code class="cli-arg">RULE_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID. If provided, deletes a repo-level rule.

### `avr firewall flow-summaries`

Show per-VM network activity summaries captured at VM stop.

```sh
avr firewall flow-summaries [OPTIONS]
```

Each row is the totals + top-N destinations + per-rule drop counters
for one VM run. Use ``--with-drops`` to triage what the firewall
blocked after editing a rule or ``--job`` to include every execution
attempt for a job.

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;job, &#x2D;&#x2D;job-id</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter to every VM execution attempt for a job ID.
- <code class="cli-flag">&#x2D;&#x2D;with-drops</code> — Show only summaries where the firewall blocked at least one flow.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max summaries to return. _(default: `20`)_
- <code class="cli-flag">&#x2D;&#x2D;offset</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Number of summaries to skip. _(default: `0`)_
- <code class="cli-flag">&#x2D;&#x2D;from, &#x2D;&#x2D;start-after</code> <code class="cli-value">&lt;TEXT&gt;</code> — Only include summaries that started at or after this ISO-8601 timestamp.
- <code class="cli-flag">&#x2D;&#x2D;to, &#x2D;&#x2D;end-before</code> <code class="cli-value">&lt;TEXT&gt;</code> — Only include summaries that ended at or before this ISO-8601 timestamp.
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit raw JSON instead of a table.

### `avr firewall list`

List egress firewall rules for the given scope.

```sh
avr firewall list [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID. If provided, shows the repo-level list.
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Output rules as JSON instead of a table.

### `avr firewall move`

Move a rule to a new position (rewrites the full ordering atomically).

```sh
avr firewall move [OPTIONS] RULE_ID
```

**Arguments**

- <code class="cli-arg">RULE_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;to</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Target 0-indexed position. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID. If provided, moves a repo-level rule.

### `avr firewall set-default`

Set (or replace) the catch-all (default) rule for the scope.

```sh
avr firewall set-default [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID. If provided, sets the repo-level default.
- <code class="cli-flag">&#x2D;&#x2D;action</code> <code class="cli-value">&lt;CHOICE&gt;</code> _(choices: `allow`, `deny` · required)_

### `avr firewall show`

Show the resolved (merged) firewall rule list for a repository.

```sh
avr firewall show [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Output resolved rules as JSON instead of a table.
