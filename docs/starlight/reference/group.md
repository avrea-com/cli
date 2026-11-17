---
title: avr group
description: "View the organization's groups."
---

View the organization's groups.

```sh
avr group [OPTIONS] COMMAND [ARGS]...
```

Each service token belongs to one of these groups. Organization members
list them while signed in with `avr auth login`; a scoped or service
token cannot.

## Subcommands

### `avr group list`

List the organization's groups by path.

```sh
avr group list [OPTIONS]
```

A group's path is the display names from the root group down, joined
with /, such as Acme/Platform/CI; a parent is listed before its
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

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max groups to return. _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
