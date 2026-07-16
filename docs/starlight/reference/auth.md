---
title: avr auth
description: "Authenticate and manage credentials."
---

Authenticate and manage credentials.

```sh
avr auth [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr auth login`

Authenticate via browser and store credentials.

```sh
avr auth login [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;provider</code> <code class="cli-value">&lt;CHOICE&gt;</code> — OAuth provider to use for CLI login. _(choices: `google`, `github` · default: `github`)_
- <code class="cli-flag">&#x2D;&#x2D;email</code> <code class="cli-value">&lt;TEXT&gt;</code> — Work email. Routes through your company's SSO if its domain requires it, ignoring --provider.

### `avr auth logout`

Revoke the current API key and remove stored credentials.

```sh
avr auth logout [OPTIONS]
```

### `avr auth status`

Display the authenticated user and connection state.

```sh
avr auth status [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;show-token</code> — Display the auth token in plain text.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr auth switch`

Switch the default host used when AVR_HOST isn't set.

```sh
avr auth switch [OPTIONS] [HOST]
```

```sh
Examples:
    avr auth switch                     # show current default + all hosts
    avr auth switch https://api.avrea.com
```

**Arguments**

- <code class="cli-arg">[HOST]</code>
