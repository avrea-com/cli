---
title: avr reference
description: "List avr's commands, options and JSON fields."
---

List avr's commands, options and JSON fields.

```sh
avr reference [OPTIONS] [COMMAND]...
```

Lists every command, or those below the COMMAND words given, from the
same data as the reference documentation. The words may be aliases,
such as groups. With --json each row holds a command's help, its
options and arguments, and the fields its --json accepts, so a script
or coding agent can learn avr without reading help text. It needs no
sign-in and makes no requests.

Without COMMAND words the first row is avr itself, with its own
options. Each is marked inherited when every command takes it too.

json_fields lists a command's default fields. It is empty where the
fields depend on the credential or platform, as for auth status and
org install add, and an option such as tests list --summary or secret
get --version-id selects others. The command with --json ? lists the
fields of that invocation.

**Examples**

```sh
avr reference
avr reference group member
avr reference --json command,json_fields --jq '.[] | select(.json_fields | length > 0)'
```

**JSON FIELDS**

`command`, `deprecated`, `help`, `is_group`, `json_fields`, `params`, `short_help`, `usage`

**Arguments**

- <code class="cli-arg">[COMMAND...]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
