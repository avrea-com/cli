---
title: avr vm
description: "Manage long-running VMs (SSH/RDP/VNC)."
---

Manage long-running VMs (SSH/RDP/VNC).

```sh
avr vm [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr vm create`

Create a long-running VM.

```sh
avr vm create [OPTIONS]
```

```sh
Provisioning is asynchronous: poll `avr vm show <id>` until the state is
RUNNING and endpoints are populated. The response carries a one-time
password for the VM's local account; save it now, it is never stored.
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;name</code> <code class="cli-value">&lt;TEXT&gt;</code> — Human-readable VM name. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;os</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Guest operating system. _(choices: `linux`, `macos`, `windows` · required)_
- <code class="cli-flag">&#x2D;&#x2D;os-version</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Guest OS version (e.g. ubuntu-22.04). Defaults to the latest version for the chosen --os. _(choices: `ubuntu-22.04`, `ubuntu-24.04`, `ubuntu-26.04`, `macos-26`, `windows-2025`)_
- <code class="cli-flag">&#x2D;&#x2D;size</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Hardware tier. Availability is OS-specific: linux 1-32 vCPU, macos 8/16, windows 2-16. _(choices: `1-vcpu`, `2-vcpu`, `4-vcpu`, `8-vcpu`, `16-vcpu`, `32-vcpu` · required)_
- <code class="cli-flag">&#x2D;&#x2D;ssh-key</code> <code class="cli-value">&lt;TEXT&gt;</code> — SSH public key, or @path to read one from a file. Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;remote-desktop / &#x2D;&#x2D;no-remote-desktop</code> — Enable a remote desktop: RDP (Windows, Linux) or VNC (macOS Screen Sharing). Availability depends on OS version; the server validates.
- <code class="cli-flag">&#x2D;&#x2D;ttl</code> <code class="cli-value">&lt;TEXT&gt;</code> — Auto-stop the VM after this long (e.g. 8h, 7d, 1800s). Default 8h, max 7d.
- <code class="cli-flag">&#x2D;&#x2D;egress-rules</code> <code class="cli-value">&lt;TEXT&gt;</code> — Per-VM egress firewall rules as a JSON array, or @path to a JSON file.
- <code class="cli-flag">&#x2D;&#x2D;ephemeral</code> — Required: acknowledge that the VM's disk is ephemeral (discarded on stop).
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit the raw API response (VM plus one-time password) as JSON.

### `avr vm delete`

Delete a VM.

```sh
avr vm delete [OPTIONS] CUSTOMER_VM_ID
```

Delete a VM. Asynchronous while live: shows DELETING until the node confirms the stop.

**Arguments**

- <code class="cli-arg">CUSTOMER_VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip the confirmation prompt.
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit the raw API response as JSON.

### `avr vm list`

List the organization's VMs, newest first (deleted ones excluded).

```sh
avr vm list [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;state</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by lifecycle state (e.g. RUNNING, STOPPED, PENDING).
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max VMs to return. _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Pagination cursor from a previous response.
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit the VM list as JSON.

### `avr vm show`

Show a VM's details, including connection endpoints and egress rules.

```sh
avr vm show [OPTIONS] CUSTOMER_VM_ID
```

**Arguments**

- <code class="cli-arg">CUSTOMER_VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit the full VM record (with egress rules) as JSON.

### `avr vm ssh`

Open an SSH session to a RUNNING VM (or print the command with --print).

```sh
avr vm ssh [OPTIONS] CUSTOMER_VM_ID [SSH_ARGS]...
```

Resolves the VM's SSH endpoint and replaces this process with `ssh`.
Extra options are passed through to ssh and placed before the destination,
so port-forwarding and similar flags work. Use `--` to stop avr from
interpreting them, e.g.:

    avr vm ssh cvm-abc123 -- -L 8080:localhost:80

**Arguments**

- <code class="cli-arg">CUSTOMER_VM_ID</code>
- <code class="cli-arg">[SSH_ARGS...]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-i, &#x2D;&#x2D;identity</code> <code class="cli-value">&lt;PATH&gt;</code> — Private key file to pass to ssh as -i.
- <code class="cli-flag">&#x2D;&#x2D;print</code> — Print the ssh command instead of running it.

### `avr vm start`

Start a stopped VM.

```sh
avr vm start [OPTIONS] CUSTOMER_VM_ID
```

Start a stopped VM. Boots a fresh disk and returns a one-time password.

**Arguments**

- <code class="cli-arg">CUSTOMER_VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit the raw API response as JSON.

### `avr vm stop`

Stop a running VM.

```sh
avr vm stop [OPTIONS] CUSTOMER_VM_ID
```

Stop a running VM. The ephemeral disk is discarded.

**Arguments**

- <code class="cli-arg">CUSTOMER_VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit the raw API response as JSON.

### `avr vm update`

Update a VM's name, TTL, or SSH keys, or rotate its password.

```sh
avr vm update [OPTIONS] CUSTOMER_VM_ID
```

Power state is controlled separately with `avr vm start` / `avr vm stop`.

**Arguments**

- <code class="cli-arg">CUSTOMER_VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;name</code> <code class="cli-value">&lt;TEXT&gt;</code> — New display name.
- <code class="cli-flag">&#x2D;&#x2D;ttl</code> <code class="cli-value">&lt;TEXT&gt;</code> — Extend the auto-stop window from now (e.g. 8h, 7d). Max 7d.
- <code class="cli-flag">&#x2D;&#x2D;ssh-key</code> <code class="cli-value">&lt;TEXT&gt;</code> — Replace stored SSH public keys (literal or @path). Repeatable. Applies live on a RUNNING VM, otherwise at next start. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;rotate-password</code> — Provision a fresh one-time password (returned in the response).
- <code class="cli-flag">&#x2D;&#x2D;egress-rules</code> <code class="cli-value">&lt;TEXT&gt;</code> — Replace the per-VM egress rules with this JSON array (or @path to a file).
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit the raw API response as JSON.

### `avr vm usage`

Show usage metering (runtime / vCPU / memory seconds) per VM.

```sh
avr vm usage [OPTIONS]
```

Each power-on cycle's window is clipped to the requested period and summed.
Deleted VMs are included: usage survives deletion.

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;start</code> <code class="cli-value">&lt;DATETIME&gt;</code> — Inclusive period start (default: 30 days ago).
- <code class="cli-flag">&#x2D;&#x2D;end</code> <code class="cli-value">&lt;DATETIME&gt;</code> — Exclusive period end (default: now).
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit the usage report as JSON.
