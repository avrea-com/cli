---
title: avr status
description: "Show recent runs, performance stats, and cache health."
---

Show recent runs, performance stats, and cache health.

```sh
avr status [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;since</code> <code class="cli-value">&lt;TEXT&gt;</code> — Time window for stats panels: '7d', '24h', etc. _(default: `7d`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Output raw JSON.
