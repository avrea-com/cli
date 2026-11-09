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
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID or owner/repo. If provided, adds at repo scope.
- <code class="cli-flag">&#x2D;&#x2D;action</code> <code class="cli-value">&lt;CHOICE&gt;</code> _(choices: `allow`, `deny` · required)_
- <code class="cli-flag">&#x2D;&#x2D;cidr</code> <code class="cli-value">&lt;TEXT&gt;</code> — Destination CIDR (e.g. 10.0.0.0/8 or 1.2.3.4/32).
- <code class="cli-flag">&#x2D;&#x2D;fqdn</code> <code class="cli-value">&lt;TEXT&gt;</code> — Destination hostname (e.g. api.example.com).
- <code class="cli-flag">&#x2D;&#x2D;any</code> — Set the catch-all default, preserving its ID if it already exists.
- <code class="cli-flag">&#x2D;&#x2D;proto</code> <code class="cli-value">&lt;CHOICE&gt;</code> _(choices: `tcp`, `udp`, `icmp`, `any` · default: `any`)_
- <code class="cli-flag">&#x2D;&#x2D;ports</code> <code class="cli-value">&lt;TEXT&gt;</code> — Port or port range (0-65535), or all.
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
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID or owner/repo. If provided, deletes a repo-level rule.

### `avr firewall edit`

Edit specified fields of an existing firewall rule.

```sh
avr firewall edit [OPTIONS] RULE_ID
```

Edit only the supplied fields, preserving rule identity and order.
Switching hostname/CIDR clears the other matcher. Any or ICMP clears ports.
Use --ports all to remove a port restriction; numeric ports require a TCP/UDP rule.

**Arguments**

- <code class="cli-arg">RULE_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;action</code> <code class="cli-value">&lt;CHOICE&gt;</code> _(choices: `allow`, `deny`)_
- <code class="cli-flag">&#x2D;&#x2D;proto</code> <code class="cli-value">&lt;CHOICE&gt;</code> _(choices: `tcp`, `udp`, `icmp`, `any`)_
- <code class="cli-flag">&#x2D;&#x2D;cidr</code> <code class="cli-value">&lt;TEXT&gt;</code> — Destination IPv4 address or CIDR; replaces the hostname.
- <code class="cli-flag">&#x2D;&#x2D;fqdn</code> <code class="cli-value">&lt;TEXT&gt;</code> — Destination hostname; replaces the CIDR.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug; defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;ports</code> <code class="cli-value">&lt;TEXT&gt;</code> — Port, range, or all to clear the port restriction.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID or owner/repo. If provided, uses repository scope.

### `avr firewall export`

Export this scope's rules as portable JSON.

```sh
avr firewall export [OPTIONS]
```

Export the selected scope in the same versioned JSON format as the console.
Inherited rules and storage IDs are excluded. Use --file PATH, or redirect stdout.

**Options**

- <code class="cli-flag">&#x2D;&#x2D;file</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output file, or - for stdout. Existing files are not overwritten. _(default: `-`)_
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug; defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID or owner/repo. If provided, uses repository scope.

### `avr firewall flow-summaries`

Show per-VM network activity summaries captured at VM stop.

```sh
avr firewall flow-summaries [OPTIONS]
```

Each row is the totals + top-N destinations for one VM run. The Blocked
column counts both per-rule packet drops and DNS queries the firewall
refused to resolve. Use ``--with-drops`` to triage what the firewall
blocked after editing a rule or ``--job`` to include every execution
attempt for a job.

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID or owner/repo. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;job, &#x2D;&#x2D;job-id</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter to every VM execution attempt for a job ID.
- <code class="cli-flag">&#x2D;&#x2D;with-drops</code> — Show only summaries where the firewall blocked at least one flow.
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max summaries to return. _(default: `20`)_
- <code class="cli-flag">&#x2D;&#x2D;offset</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Number of summaries to skip. _(default: `0`)_
- <code class="cli-flag">&#x2D;&#x2D;from, &#x2D;&#x2D;start-after</code> <code class="cli-value">&lt;TEXT&gt;</code> — Only include summaries that started at or after this ISO-8601 timestamp.
- <code class="cli-flag">&#x2D;&#x2D;to, &#x2D;&#x2D;end-before</code> <code class="cli-value">&lt;TEXT&gt;</code> — Only include summaries that ended at or before this ISO-8601 timestamp.
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit raw JSON instead of a table.

### `avr firewall import`

Append or replace this scope's rules from a portable JSON file.

```sh
avr firewall import [OPTIONS]
```

Import a console-compatible JSON policy into one organization or repository.
Append skips duplicates and supplied defaults. Replace uses the file's default;
without one, repository defaults inherit and organization defaults are retained.
Every import is validated before applying. Use --dry-run to review changes.
Replace prompts for confirmation; --yes is required with stdin or automation.
If the policy changes after preview, the import fails without overwriting it.

**Options**

- <code class="cli-flag">&#x2D;&#x2D;mode</code> <code class="cli-value">&lt;CHOICE&gt;</code> _(choices: `append`, `replace` · default: `append`)_
- <code class="cli-flag">&#x2D;&#x2D;dry-run</code> — Validate and summarize changes without applying them.
- <code class="cli-flag">&#x2D;&#x2D;file</code> <code class="cli-value">&lt;TEXT&gt;</code> — Policy file, or - to read stdin. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug; defaults to the configured organization.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID or owner/repo. If provided, uses repository scope.
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip replacement confirmation.

### `avr firewall list`

List egress firewall rules for the given scope.

```sh
avr firewall list [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID or owner/repo. If provided, shows the repo-level list.
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
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID or owner/repo. If provided, moves a repo-level rule.

### `avr firewall set-default`

Set the default action, or use --inherit with --repo to restore organization inheritance.

```sh
avr firewall set-default [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID or owner/repo. If provided, sets the repo-level default.
- <code class="cli-flag">&#x2D;&#x2D;action</code> <code class="cli-value">&lt;CHOICE&gt;</code> _(choices: `allow`, `deny`)_
- <code class="cli-flag">&#x2D;&#x2D;inherit</code> — Remove the repository default override.

### `avr firewall show`

Show rules in evaluation order and their source.

```sh
avr firewall show [OPTIONS]
```

Show rules in evaluation order with repository/organization ownership. Built-in service exceptions are not listed. Running jobs retain their dispatched policy.

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository ID or owner/repo. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Output resolved rules as JSON instead of a table.
