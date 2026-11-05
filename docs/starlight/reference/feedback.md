---
title: avr feedback
description: "Send feedback or report a bug."
---

Send feedback or report a bug.

```sh
avr feedback [OPTIONS] [MESSAGE] COMMAND [ARGS]...
```

Send MESSAGE to the Avrea team. Sign in with avr auth login first.

Use --bug for a bug report and --org SLUG_OR_ID to attribute it to an
organization. Without --org, feedback is personal, regardless of your default
organization. Contact details come from your account.

The idempotency key is printed before sending. If the response is lost or
the server asks you to retry, repeat the same message, kind and organization
with --idempotency-key KEY. For a key conflict, use the original content
or omit --idempotency-key to create a new submission.
Acceptance queues delivery; use avr feedback status FEEDBACK_ID to check it.
Use -- before a message beginning with a dash or the literal word status.

**Arguments**

- <code class="cli-arg">[MESSAGE]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;bug</code> — Mark the feedback as a bug report.
- <code class="cli-flag">&#x2D;&#x2D;idempotency-key</code> <code class="cli-value">&lt;TEXT&gt;</code> — Reuse a previous submission key (32 lowercase hex characters).
- <code class="cli-flag">&#x2D;&#x2D;jq, -q</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter JSON with an embedded jq expression.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output comma-separated fields, * for all, or ? to list fields.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Attribute feedback to this organization slug or ID.

## Subcommands

### `avr feedback status`

Read the delivery status of your feedback.

```sh
avr feedback status [OPTIONS] FEEDBACK_ID
```

**Arguments**

- <code class="cli-arg">FEEDBACK_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;jq, -q</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter JSON with an embedded jq expression.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output comma-separated fields, * for all, or ? to list fields.
