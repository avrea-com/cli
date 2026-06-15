---
title: CLI Reference
description: "Reference for the avr command-line client (v0.1.5)."
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

- [`avr status`](./status/) — Show recent runs, performance stats, and cache health.
- [`avr run`](./run/) — View and manage GitHub workflow runs.
- [`avr job`](./job/) — Inspect Avrea job VMs (SSH, metrics, logs).
- [`avr workflow`](./workflow/) — List and view workflow definitions.
- [`avr cache`](./cache/) — Inspect and manage the Avrea build cache.
- [`avr log`](./log/) — Search across runner execution logs.

### Setup & Config

- [`avr auth`](./auth/) — Authenticate and manage credentials.
- [`avr config`](./config/) — View and manage CLI configuration.
- [`avr settings`](./settings/) — View and toggle cache and runner settings.
- [`avr firewall`](./firewall/) — Manage the egress firewall rule list for orgs and repositories.
- [`avr billing`](./billing/) — Manage billing, invoices, and payment methods.
- [`avr audit-events`](./audit-events/) — View audit events for organization writes.

### Additional Commands

- [`avr repo`](./repo/) — List repositories connected to Avrea.
- [`avr org`](./org/) — Manage organizations and installations.
- [`avr health`](./health/) — Check Avrea platform status.

## Aliases

- `jobs` → [`job`](./job/)
- `logs` → [`log`](./log/)
- `orgs` → [`org`](./org/)
- `repos` → [`repo`](./repo/)
- `workflows` → [`workflow`](./workflow/)
