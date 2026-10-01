---
title: avr skill
description: "Manage Avrea's agent skill for Codex and Claude."
---

Manage Avrea's agent skill for Codex and Claude.

```sh
avr skill [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr skill install`

Install the bundled Avrea skill.

```sh
avr skill install [OPTIONS]
```

```sh
Examples:
    avr skill install
    avr skill install --target codex
    avr skill install --target claude
    avr skill install --target all --force
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;target</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Agent host to install for. _(choices: `codex`, `claude`, `all` · default: `all`)_
- <code class="cli-flag">&#x2D;&#x2D;force</code> — Replace an existing modified or unmanaged skill.

### `avr skill status`

Show whether the bundled Avrea skill is installed and current.

```sh
avr skill status [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;target</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Agent host to inspect. _(choices: `codex`, `claude`, `all` · default: `all`)_

### `avr skill uninstall`

Uninstall the Avrea skill.

```sh
avr skill uninstall [OPTIONS]
```

Uninstall the Avrea skill. Alias: remove.

Unmanaged skill directories are never removed automatically.

```sh
Examples:
    avr skill uninstall
    avr skill uninstall --target claude
    avr skill remove --target codex
    avr skill uninstall --target all --force
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;target</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Installed agent host to uninstall from. _(choices: `codex`, `claude`, `all` · default: `all`)_
- <code class="cli-flag">&#x2D;&#x2D;force</code> — Remove a locally modified avr-managed skill.

### `avr skill update`

Update an installed Avrea skill from this avr release.

```sh
avr skill update [OPTIONS]
```

```sh
Examples:
    avr skill update
    avr skill update --target claude
    avr skill update --target all --force
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;target</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Installed agent host to update. _(choices: `codex`, `claude`, `all` · default: `all`)_
- <code class="cli-flag">&#x2D;&#x2D;force</code> — Replace a locally modified skill.
