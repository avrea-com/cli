---
title: avr health
description: "Check Avrea platform status."
---

Check Avrea platform status.

```sh
avr health [OPTIONS]
```

```sh
Examples:
    avr health
    avr health --json status
    avr health --json '*' -q '.status'
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
