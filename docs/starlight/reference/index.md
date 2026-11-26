---
title: CLI Reference
description: "Reference for the avr command-line client."
head:
  - tag: title
    content: "avr command reference | Avrea Docs"
  - tag: meta
    attrs:
      property: og:title
      content: "avr command reference"
---

`avr` is the Avrea command-line client. Avrea on the command line.

## Synopsis

```sh
avr [GLOBAL OPTIONS] COMMAND [ARGS]...
```

**Global options**

- <code class="cli-flag">&#x2D;&#x2D;version, -V</code> — Show the version and exit.
- <code class="cli-flag">&#x2D;&#x2D;no-color</code> — Disable colored output. Also honors NO_COLOR=1.
- <code class="cli-flag">&#x2D;&#x2D;verbose, -v</code> — Show debug information including HTTP requests.
- <code class="cli-flag">&#x2D;&#x2D;links / &#x2D;&#x2D;no-links</code> — Make IDs clickable via OSC 8 hyperlinks. Auto-disabled off-TTY. Also honors AVR_LINKS=0. _(default: `True` · env: `AVR_LINKS`)_

## Commands

### Core Commands

- [`avr artifact`](./artifact/) — Manage workflow artifacts.
- [`avr status`](./status/) — Show recent runs, performance stats, and cache health.
- [`avr run`](./run/) — View and manage CI workflow runs.
- [`avr pr`](./pr/) — View and comment on pull requests.
- [`avr job`](./job/) — Inspect Avrea job VMs (SSH, metrics, logs).
- [`avr vm`](./vm/) — Manage long-running VMs (SSH/RDP/VNC).
- [`avr build`](./build/) — Build container images remotely with docker buildx on Avrea builders.
- [`avr builder`](./builder/) — Manage remote builder sessions.
- [`avr workflow`](./workflow/) — List and view workflow definitions.
- [`avr cache`](./cache/) — Inspect and manage the Avrea build cache.
- [`avr tests`](./tests/) — Read recorded tests: a job's results, repository search and ranking, one test's history and one result.
- [`avr sbom`](./sbom/) — View, generate, and download repository SBOMs.
- [`avr log`](./log/) — Search across runner execution logs.

### Setup & Config

- [`avr auth`](./auth/) — Authenticate and manage credentials.
- [`avr token`](./token/) — Create and manage personal access tokens.
- [`avr service-token`](./service-token/) — Create and manage the organization's service tokens.
- [`avr group`](./group/) — Build and manage the organization's group tree.
- [`avr config`](./config/) — View and manage CLI configuration.
- [`avr settings`](./settings/) — View and toggle cache and runner settings.
- [`avr skill`](./skill/) — Teach coding agents to use avr.
- [`avr reference`](./reference/) — List avr's commands, options and JSON fields.
- [`avr firewall`](./firewall/) — Manage the egress firewall rule list for orgs and repositories.
- [`avr billing`](./billing/) — Manage billing, invoices, and payment methods.
- [`avr audit-events`](./audit-events/) — View audit events for organization writes.

### Additional Commands

- [`avr repo`](./repo/) — Manage repositories, git mirrors, and public mirrors.
- [`avr org`](./org/) — Manage organizations and installations.
- [`avr health`](./health/) — Check Avrea platform status.
- [`avr api`](./api/) — Call an Avrea API endpoint.
- [`avr feedback`](./feedback/) — Send feedback or report a bug.
- [`avr secret`](./secret/) — Manage organization and repository secrets.
- [`avr variable`](./variable/) — Manage organization and repository variables.

## Aliases

- `builders` → [`builder`](./builder/)
- `groups` → [`group`](./group/)
- `jobs` → [`job`](./job/)
- `logs` → [`log`](./log/)
- `orgs` → [`org`](./org/)
- `prs` → [`pr`](./pr/)
- `repos` → [`repo`](./repo/)
- `secrets` → [`secret`](./secret/)
- `service-tokens` → [`service-token`](./service-token/)
- `tokens` → [`token`](./token/)
- `variables` → [`variable`](./variable/)
- `vms` → [`vm`](./vm/)
- `workflows` → [`workflow`](./workflow/)
