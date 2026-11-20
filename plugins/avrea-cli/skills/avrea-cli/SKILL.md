---
name: avrea-cli
description: Works with GitHub Actions workflows on Avrea runners and with Avrea CI, through the Avrea CLI (`avr`). Finds and watches the run for a commit, diagnoses failed or slow workflow runs, reads job logs, test results and runner metrics, inspects caches, right-sizes runners, and creates on-demand Linux, macOS or Windows VMs. Use when the user mentions Avrea or `avr`, pastes a GitHub Actions or Avrea run URL, or asks why CI failed or is slow, and the repository's CI runs on Avrea runners or Avrea CI. Use it before `gh run view` or downloading raw logs. Not for writing workflow files from scratch.
---

# Avrea CLI

Run `avr skill show` and follow the guide it prints. The guide ships with the
installed `avr`, so it always matches that version's commands.

If `avr` is not installed, install it with one of
`brew install avrea-com/tap/avr`, `mise use -g github:avrea-com/cli@latest`,
`go install github.com/avrea-com/cli/cmd/avr@latest` or
`uv tool install avr-cli`, or see https://docs.avrea.com/cli.

If a command exits with status 4 and `AVR_TOKEN` is set, signing in does not
help, because the token takes precedence: report the error and stop.
Otherwise start `avr auth device-login --no-qr` in the background (it waits
for approval), read the link and code from its output, ask the user to open
the link and approve, then wait for the command to exit. When no user can
approve, as in a scheduled or cloud session, report that `AVR_TOKEN` must be
set to a scoped or service token in the environment.

<!-- Managed by `avr skill install`. Edits are overwritten; remove with `avr skill uninstall`. -->
