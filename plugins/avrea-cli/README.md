# Avrea CLI plugin for Claude Code, Codex, Copilot and Cursor

This plugin adds one skill, `avrea-cli`, that teaches Claude to work with
GitHub Actions workflows on [Avrea](https://avrea.com) runners and with Avrea
CI, through the Avrea CLI, `avr`. With it, Claude finds and watches the CI run
for a commit, diagnoses failed or slow workflow runs, reads job logs, test
results and runner metrics, inspects caches, suggests runner sizes, and creates
on-demand Linux, macOS or Windows VMs.

## Install

```sh
claude plugin marketplace add avrea-com/cli   # Claude Code
claude plugin install avrea-cli@avrea
codex plugin marketplace add avrea-com/cli    # Codex
codex plugin add avrea-cli@avrea
copilot plugin marketplace add avrea-com/cli  # GitHub Copilot CLI
copilot plugin install avrea-cli@avrea
```

The [CLI README](https://github.com/avrea-com/cli#for-coding-agents) says how
each one updates.

Other agents:

- Cursor: this folder is also a Cursor plugin (`.cursor-plugin/plugin.json`),
  listed by `.cursor-plugin/marketplace.json` at the repository root.
- Gemini CLI: `gemini skills install https://github.com/avrea-com/cli.git --path plugins/avrea-cli/skills`.
- OpenCode and other Agent Skills tools: `npx skills add avrea-com/cli`. In Codex, `agents/openai.yaml` gives the
  skill its name, icon and a starter prompt.

The skill needs the `avr` command. If it is missing, the skill tells Claude
to install it with Homebrew, mise, `go install` or `uv tool install`. See the [CLI documentation](https://docs.avrea.com/cli/).

Install the skill one way per machine. If `avr skill install` or
`npx skills add avrea-com/cli` already installed it, the agent loads it twice;
remove the other copy with `avr skill uninstall`.

## What it runs, sends and fetches

The plugin contains only the skill's instructions: no hooks, MCP servers or
scripts. When the skill is used, Claude runs `avr` commands in your shell,
with your normal permission prompts:

- `avr skill show` prints the rules every task follows and a list of topics,
  and `avr skill show <topic>` prints one of them. The guide ships with the
  installed `avr`, so it matches that version.
- Other `avr` commands call the Avrea API at api.avrea.com, or the host in
  `AVR_HOST` or the stored sign-in, to read runs, jobs, logs, tests, metrics
  and caches.
- To sign in, Claude starts `avr auth device-login`, which prints a link and a
  code for you to approve in a browser. `avr` stores the resulting credential
  itself; Claude never asks for a password or token.
- If `AVR_TOKEN` is set in the environment, `avr` uses it instead of a stored
  sign-in and sends it to the Avrea API. The plugin does not read it.

The guide tells Claude to read before it changes anything, and to make changes
that act on shared systems, such as rerunning or cancelling runs, dispatching
workflows or creating VMs, only when your task calls for them and after saying
so. A VM is billed compute.

## Support

- Documentation: https://docs.avrea.com/cli/
- Contact: https://avrea.com/contact
- Issues: https://github.com/avrea-com/cli/issues

Licensed under the Apache License 2.0.
