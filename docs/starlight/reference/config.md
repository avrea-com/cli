---
title: avr config
description: "View and manage CLI configuration."
---

View and manage CLI configuration.

```sh
avr config [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr config get`

Print the value of a configuration key.

```sh
avr config get [OPTIONS] {org}
```

```sh
Available keys:
  org   Active organization ID
```

**Arguments**

- <code class="cli-arg">KEY</code> _(choices: `org`)_

### `avr config list`

Show the active CLI configuration (host, auth, org, default repo).

```sh
avr config list [OPTIONS]
```

### `avr config set`

Set a CLI configuration value.

```sh
avr config set [OPTIONS] {org} VALUE
```

```sh
Available keys:
  org   Active organization (ID or slug)
```

```sh
Examples:
    avr config set org org-abc123
    avr config set org acme
```

**Arguments**

- <code class="cli-arg">KEY</code> _(choices: `org`)_
- <code class="cli-arg">VALUE</code>

### `avr config unset`

Remove a configuration override.

```sh
avr config unset [OPTIONS] {org}
```

```sh
Available keys:
  org   Drop the stored default organization for the active host
```

```sh
Examples:
    avr config unset org
```

**Arguments**

- <code class="cli-arg">KEY</code> _(choices: `org`)_
