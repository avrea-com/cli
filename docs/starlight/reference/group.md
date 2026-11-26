---
title: avr group
description: "Build and manage the organization's group tree."
---

Build and manage the organization's group tree.

```sh
avr group [OPTIONS] COMMAND [ARGS]...
```

Groups hold members, service tokens and vCPU limits. Organization
members manage them while signed in (avr auth device-login or avr auth
login); a personal access token or service token cannot. What you may
change depends on the groups you administer: `avr org governance` shows
it.

## Subcommands

### `avr group create`

Create a group below a parent group.

```sh
avr group create [OPTIONS] NAME
```

NAME is the new group's name, unique below its parent. --parent names
the parent like GROUP in other group commands; the root group,
Organization unless renamed, takes groups at the top of the tree.

Each create sends an idempotency key, printed to standard error before
the request. If the response is lost, rerun the same command with
--idempotency-key KEY: it returns the group that create made instead of
making a second one. A key belongs to one parent, name and label.

```sh
Examples:
    avr group create Platform --parent Organization
    avr group create CI --parent Organization/Platform --label runners
```

```sh
JSON FIELDS
    created_at, display_name, group_id, idempotency_key, label, member_count,
    ownership, parent_id, soft_deleted_at, state, suspension_reason,
    updated_at
```

**Arguments**

- <code class="cli-arg">NAME</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;parent</code> <code class="cli-value">&lt;TEXT&gt;</code> — The group to create it below. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;label</code> <code class="cli-value">&lt;TEXT&gt;</code> — A short label shown with the group.
- <code class="cli-flag">&#x2D;&#x2D;idempotency-key</code> <code class="cli-value">&lt;UUID&gt;</code> — Resume an earlier create with the key it printed.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr group delete`

Delete an empty group.

```sh
avr group delete [OPTIONS] GROUP
```

GROUP is a group ID (grp-...), the group's path from the root group,
such as Organization/Platform/CI, or the end of a path when only one
group has it, such as Platform/CI.

Asks first unless --yes is passed. A group that still has subgroups,
members, a service token that is not revoked or expired, VMs its
service tokens created, a vCPU limit, suspended members or workflows
whose jobs are charged to it is not deleted; the error names what to
clear, at most 100 workflows at a time.

Deleting a group also removes the resource access it grants, and stops
charging it for workflows of repositories that are no longer installed.

```sh
Examples:
    avr group delete Platform/CI
    avr group delete grp-0123456789abcdef0123456789abcdef --yes
```

```sh
JSON FIELDS
    deleted, group_id, path
```

**Arguments**

- <code class="cli-arg">GROUP</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-y, &#x2D;&#x2D;yes</code> — Delete without asking.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr group edit`

Rename a group or change its label.

```sh
avr group edit [OPTIONS] GROUP
```

GROUP is a group ID (grp-...), the group's path from the root group,
such as Organization/Platform/CI, or the end of a path when only one
group has it, such as Platform/CI.

Only the options you pass change; --label '' clears the label.

```sh
Examples:
    avr group edit Platform/CI --name Builds
    avr group edit Platform/CI --label ''
```

```sh
JSON FIELDS
    created_at, display_name, group_id, label, member_count, ownership,
    parent_id, soft_deleted_at, state, suspension_reason, updated_at
```

**Arguments**

- <code class="cli-arg">GROUP</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;name</code> <code class="cli-value">&lt;TEXT&gt;</code> — The group's new name.
- <code class="cli-flag">&#x2D;&#x2D;label</code> <code class="cli-value">&lt;TEXT&gt;</code> — The group's new label; an empty value clears it.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr group limit`

Show, set and clear a group's vCPU limit.

```sh
avr group limit [OPTIONS] COMMAND [ARGS]...
```

A group's vCPU limit caps the vCPU that the group and its subgroups use
at once. Limits on parent groups apply too: a job starts only when it
fits under every limit on its group's path, counting what the other
groups below each one already use.

#### `avr group limit clear`

Remove a group's own vCPU limit.

```sh
avr group limit clear [OPTIONS] GROUP
```

GROUP is a group ID (grp-...), the group's path from the root group,
such as Organization/Platform/CI, or the end of a path when only one
group has it, such as Platform/CI.

Asks first unless --yes is passed. Limits on parent groups still apply.

```sh
Examples:
    avr group limit clear Platform/CI --yes
```

```sh
JSON FIELDS
    cleared, group_id, limit_key
```

**Arguments**

- <code class="cli-arg">GROUP</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-y, &#x2D;&#x2D;yes</code> — Clear without asking.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr group limit list`

Show the limits that apply to a group, its own and inherited.

```sh
avr group limit list [OPTIONS] GROUP
```

GROUP is a group ID (grp-...), the group's path from the root group,
such as Organization/Platform/CI, or the end of a path when only one
group has it, such as Platform/CI.

```sh
Examples:
    avr group limit list Platform/CI
```

```sh
JSON FIELDS
    group_id, inherited, kind, limit_key, limit_type, period, timezone,
    value
```

**Arguments**

- <code class="cli-arg">GROUP</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr group limit set`

Set a group's own vCPU limit.

```sh
avr group limit set [OPTIONS] GROUP
```

GROUP is a group ID (grp-...), the group's path from the root group,
such as Organization/Platform/CI, or the end of a path when only one
group has it, such as Platform/CI.

Asks first unless --yes is passed or the change raises or keeps the
group's own limit: a first limit or a lower one can make new jobs wait.
Running jobs keep running.

```sh
Examples:
    avr group limit set Platform/CI --max-vcpu 64
```

```sh
JSON FIELDS
    group_id, inherited, kind, limit_key, limit_type, period, previous_value,
    timezone, value
```

**Arguments**

- <code class="cli-arg">GROUP</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;max-vcpu</code> <code class="cli-value">&lt;INTEGER&gt;</code> — The most vCPU the group and its subgroups may use at once. _(required)_
- <code class="cli-flag">-y, &#x2D;&#x2D;yes</code> — Set the limit without asking.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr group list`

List the organization's groups by path.

```sh
avr group list [OPTIONS]
```

A group's path is the display names from the root group down, joined
with /, such as Organization/Platform/CI; a parent is listed before its
children. Pass a group's ID or path to --group when you create or list
service tokens.

```sh
Examples:
    avr group list
    avr group list --json group_id,path --jq '.[].path'
```

```sh
JSON FIELDS
    created_at, display_name, group_id, label, member_count, ownership,
    parent_id, path, soft_deleted_at, state, suspension_reason, updated_at
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max groups to return. _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr group member`

List, add and remove a group's members.

```sh
avr group member [OPTIONS] COMMAND [ARGS]...
```

A change names each MEMBER by user ID (usr-...), @me for yourself, or an
email address the member verified. Display names are not accepted:
members choose them, so one can match the wrong person. `avr auth
status` shows your user ID.

#### `avr group member add`

Add members to a group.

```sh
avr group member add [OPTIONS] GROUP [MEMBER]...
```

GROUP is a group ID (grp-...), the group's path from the root group,
such as Organization/Platform/CI, or the end of a path when only one
group has it, such as Platform/CI.

Name each MEMBER by user ID (usr-...), @me or a verified email address,
as arguments or one per line with --file, where a line starting with #
is a comment unless it is an email address, such as #ops@example.com.
The command adds them all in one change, up to 250, and reports
each member's result; a member already in the state asked for counts
as done, so a rerun is safe.

```sh
Examples:
    avr group member add Platform/CI @me
    avr group member add Platform/CI usr-0123456789abcdef0123456789abcdef
    avr group member add Platform/CI --file members.txt
```

```sh
JSON FIELDS
    action, detail, member, status, user_id
```

**Arguments**

- <code class="cli-arg">GROUP</code>
- <code class="cli-arg">[MEMBER...]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;file</code> <code class="cli-value">&lt;TEXT&gt;</code> — Read members from a file, one per line; - reads standard input.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr group member list`

List a group's direct members.

```sh
avr group member list [OPTIONS] GROUP
```

GROUP is a group ID (grp-...), the group's path from the root group,
such as Organization/Platform/CI, or the end of a path when only one
group has it, such as Platform/CI.

Members of subgroups are listed with the subgroup.

```sh
Examples:
    avr group member list Platform/CI
    avr group member list Platform/CI -L 1000 --json user_id,email
```

```sh
JSON FIELDS
    added_at, email, name, source, stale_sources, user_id
```

**Arguments**

- <code class="cli-arg">GROUP</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max members to return. _(default: `100`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr group member remove`

Remove members from a group.

```sh
avr group member remove [OPTIONS] GROUP [MEMBER]...
```

GROUP is a group ID (grp-...), the group's path from the root group,
such as Organization/Platform/CI, or the end of a path when only one
group has it, such as Platform/CI.

Name each MEMBER by user ID (usr-...), @me or a verified email address,
as arguments or one per line with --file, where a line starting with #
is a comment unless it is an email address, such as #ops@example.com.
The command removes them all in one change, up to 250, and reports
each member's result; a member already in the state asked for counts
as done, so a rerun is safe.
Lists the members and asks first unless --yes is passed; reading
members from standard input (--file -) needs --yes.

```sh
Examples:
    avr group member remove Platform/CI @me
    avr group member remove Platform/CI usr-0123456789abcdef0123456789abcdef
    avr group member remove Platform/CI --file members.txt
```

```sh
JSON FIELDS
    action, detail, member, status, user_id
```

**Arguments**

- <code class="cli-arg">GROUP</code>
- <code class="cli-arg">[MEMBER...]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;file</code> <code class="cli-value">&lt;TEXT&gt;</code> — Read members from a file, one per line; - reads standard input.
- <code class="cli-flag">-y, &#x2D;&#x2D;yes</code> — Remove without asking.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr group usage`

Show the vCPU a group uses and the limits on its path.

```sh
avr group usage [OPTIONS] GROUP
```

GROUP is a group ID (grp-...), the group's path from the root group,
such as Organization/Platform/CI, or the end of a path when only one
group has it, such as Platform/CI.

Usage counts the group and its subgroups.

```sh
Examples:
    avr group usage Platform
    avr group usage Platform --json used_vcpu,pools
```

```sh
JSON FIELDS
    group_id, group_state, measured_at, pools, used_vcpu
```

**Arguments**

- <code class="cli-arg">GROUP</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr group view`

Show a group's details and path.

```sh
avr group view [OPTIONS] GROUP
```

GROUP is a group ID (grp-...), the group's path from the root group,
such as Organization/Platform/CI, or the end of a path when only one
group has it, such as Platform/CI.

```sh
Examples:
    avr group view Organization/Platform
    avr group view Platform/CI --json group_id,member_count
```

```sh
JSON FIELDS
    created_at, display_name, group_id, label, member_count, ownership,
    parent_id, path, soft_deleted_at, state, suspension_reason, updated_at
```

**Arguments**

- <code class="cli-arg">GROUP</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
