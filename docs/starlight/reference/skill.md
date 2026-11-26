---
title: avr skill
description: "Teach coding agents to use avr."
---

Teach coding agents to use avr.

```sh
avr skill [OPTIONS] COMMAND [ARGS]...
```

The skill follows the open Agent Skills format. It tells an agent when
avr is relevant and points it at avr skill show, so the guidance always
matches the installed avr. Claude Code reads it from .claude/skills;
Codex, Cursor, GitHub Copilot, Gemini CLI, Devin, OpenCode, Pi and
other Agent Skills tools read it from .agents/skills, and Kiro from
.kiro/skills. For an agent that reads none of them, add a line to its
instructions that says to run avr skill show before working with
Avrea CI.

## Subcommands

### `avr skill install`

Install the avr skill for coding agents.

```sh
avr skill install [OPTIONS]
```

Writes one SKILL.md per location: ~/.claude/skills/avrea-cli for
Claude Code (or under CLAUDE_CONFIG_DIR) and ~/.agents/skills/avrea-cli
for Codex, Cursor, GitHub Copilot, Gemini CLI, Devin, OpenCode, Pi and
other Agent Skills tools; codex is accepted for agents. Without
--agent, both are installed. kiro writes ~/.kiro/skills/avrea-cli for
Kiro, which reads only its own folder. Running it again updates the
file. A SKILL.md that avr did not install is left alone unless --force
is given.

With --project, the skill goes into .claude/skills and .agents/skills
(or .kiro/skills) at the top of the current git repository instead.
Commit it so cloud
agents working on the repository, such as Devin or Codex cloud, find it.
Those environments also need avr installed and AVR_TOKEN set to a personal
access token or service token.

Examples:

```sh
avr skill install
avr skill install --agent claude
avr skill install --project --agent agents
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;agent</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Location to install for (repeatable): claude; agents for Codex, Cursor, Copilot, Gemini CLI, Devin and others; or kiro. Defaults to claude and agents. _(choices: `claude`, `agents`, `codex`, `kiro` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;project</code> — Use the current git repository instead of your home directory.
- <code class="cli-flag">&#x2D;&#x2D;force</code> — Replace a skill that avr did not install.

### `avr skill show`

Print the avr guide for coding agents.

```sh
avr skill show [OPTIONS] [TOPIC]
```

Without a topic, prints the rules every task follows and the list of
topics. With a topic, prints that part of the guide. The guide belongs
to the installed avr version.

Topics:

```sh
test      Push a change and follow its CI run, or dispatch a workflow
diagnose  Find why a run failed
speed     Find why CI is slow, caches and right-sized runners
vms       On-demand Linux, macOS and Windows VMs
```

**Arguments**

- <code class="cli-arg">[TOPIC]</code> _(choices: `test`, `diagnose`, `speed`, `vms`)_

### `avr skill uninstall`

Remove the installed avr skill.

```sh
avr skill uninstall [OPTIONS]
```

Without --agent, the skill is removed from the claude and agents
locations. With
--project, it is removed from the current git repository. Other files
in the skill directory are kept.

**Options**

- <code class="cli-flag">&#x2D;&#x2D;agent</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Location to uninstall from (repeatable): claude, agents or kiro. Defaults to claude and agents. _(choices: `claude`, `agents`, `codex`, `kiro` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;project</code> — Use the current git repository instead of your home directory.
- <code class="cli-flag">&#x2D;&#x2D;force</code> — Remove a skill that avr did not install.
