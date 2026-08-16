---
title: avr vm
description: "Manage long-running VMs (SSH/RDP/VNC)."
---

Manage long-running VMs (SSH/RDP/VNC).

```sh
avr vm [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr vm bootstrap`

Set up a RUNNING VM with your dev essentials over SSH.

```sh
avr vm bootstrap [OPTIONS] VM_ID
```

```sh
Each selected step runs on the VM and streams its output; bootstrap stops at
the first failure. Secrets (GitHub token, forwarded env values, agent
credentials) ride SSH stdin, never argv. A forwarded CLAUDE_CODE_OAUTH_TOKEN
is a year-long, subscription-wide bearer, so it is only carried under the
explicit --forward-agent-creds opt-in. Disks are ephemeral, so re-run
bootstrap after every `avr vm start`. Example:
```

```sh
avr vm bootstrap cvm-abc123 --setup-github --install claude,codex \
  --repo https://github.com/me/project --env AWS_REGION=eu-north-1
```

**Arguments**

- <code class="cli-arg">VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-i, &#x2D;&#x2D;identity</code> <code class="cli-value">&lt;PATH&gt;</code> — Private key file to pass to ssh as -i.
- <code class="cli-flag">&#x2D;&#x2D;setup-github / &#x2D;&#x2D;no-setup-github</code> — Forward your local `gh auth token` into the VM (gh auth login --with-token + gh auth setup-git).
- <code class="cli-flag">&#x2D;&#x2D;install</code> <code class="cli-value">&lt;TEXT&gt;</code> — Install an agent CLI (repeatable or comma-separated): claude, codex. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;forward-agent-creds</code> — Forward the installed agents' credentials from your environment (ANTHROPIC_API_KEY / CLAUDE_CODE_OAUTH_TOKEN / OPENAI_API_KEY). If claude has none, offers to run `claude setup-token`.
- <code class="cli-flag">&#x2D;&#x2D;install-avr</code> — Install the avr CLI in the VM (pipx, else pip).
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Clone this git repo into the VM's home directory.
- <code class="cli-flag">&#x2D;&#x2D;ref</code> <code class="cli-value">&lt;TEXT&gt;</code> — Branch to check out after cloning (requires --repo). Tags, PR refs, and raw SHAs are unsupported.
- <code class="cli-flag">&#x2D;&#x2D;dotfiles</code> <code class="cli-value">&lt;TEXT&gt;</code> — Clone this dotfiles repo and run its installer.
- <code class="cli-flag">&#x2D;&#x2D;env</code> <code class="cli-value">&lt;TEXT&gt;</code> — Set an env var in the VM: KEY=VALUE, or a bare KEY to forward it from your environment. Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;run</code> <code class="cli-value">&lt;TEXT&gt;</code> — Run a custom script last: an inline script, or @path to a file.
- <code class="cli-flag">&#x2D;&#x2D;print</code> — Print the ordered plan (secrets redacted) without running.

### `avr vm create`

Create a long-running VM.

```sh
avr vm create [OPTIONS]
```

```sh
Provisioning is asynchronous: poll `avr vm show <id>` until the state is
RUNNING and endpoints are populated, or pass --wait to block until then and
print a ready-to-paste connect command with the password baked in. The
response carries a one-time password for the VM's local account; save it
now, it is never stored.
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;name</code> <code class="cli-value">&lt;TEXT&gt;</code> — Human-readable VM name. _(required)_
- <code class="cli-flag">&#x2D;&#x2D;os</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Guest operating system. _(choices: `linux`, `macos`, `windows` · required)_
- <code class="cli-flag">&#x2D;&#x2D;os-version</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Guest OS version (e.g. ubuntu-26.04). Defaults to the latest version for the chosen --os. _(choices: `ubuntu-22.04`, `ubuntu-24.04`, `ubuntu-26.04`, `macos-26`, `windows-2025`)_
- <code class="cli-flag">&#x2D;&#x2D;size</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Hardware tier. Availability is OS-specific: linux 1-32 vCPU, macos 8/16, windows 2-16. _(choices: `1-vcpu`, `2-vcpu`, `4-vcpu`, `8-vcpu`, `16-vcpu`, `32-vcpu` · required)_
- <code class="cli-flag">&#x2D;&#x2D;ssh-key</code> <code class="cli-value">&lt;TEXT&gt;</code> — SSH public key, or @path to read one from a file. Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;remote-desktop / &#x2D;&#x2D;no-remote-desktop</code> — Enable a remote desktop: RDP (Windows, Linux) or VNC (macOS Screen Sharing). Availability depends on OS version; the server validates.
- <code class="cli-flag">&#x2D;&#x2D;ttl</code> <code class="cli-value">&lt;TEXT&gt;</code> — Auto-stop the VM after this long (e.g. 8h, 7d, 1800s). Default 8h, max 7d.
- <code class="cli-flag">&#x2D;&#x2D;egress-rules</code> <code class="cli-value">&lt;TEXT&gt;</code> — Per-VM egress firewall rules as a JSON array, or @path to a JSON file.
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Git repository (owner/repo) to preload into the VM at boot. Best-effort; the checkout is warmed from Avrea's mirror when available.
- <code class="cli-flag">&#x2D;&#x2D;ref</code> <code class="cli-value">&lt;TEXT&gt;</code> — Branch to preload (default: the repository's default branch). Requires --repo. Tags, pull-request refs, and raw commit SHAs are not supported.
- <code class="cli-flag">&#x2D;&#x2D;disable-cache</code> <code class="cli-value">&lt;TEXT&gt;</code> — Disable a build/CI cache on this VM (repeatable, or comma-separated). Narrowing only: a VM can turn off an inherited cache but not turn one on. e.g. gha, packages, bazel, gradle, maven, turbo, nx, go-build (or a raw cache.&lt;name&gt;.enabled key). Repository-scoped caches require --repo. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;ephemeral</code> — Required: acknowledge that the VM's disk is ephemeral (discarded on stop).
- <code class="cli-flag">&#x2D;&#x2D;wait</code> — Wait until the VM is RUNNING, then print a ready-to-paste connect command with the password baked in.
- <code class="cli-flag">&#x2D;&#x2D;wait-timeout</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Seconds to wait when --wait is set. _(default: `300`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit the raw API response (VM plus one-time password) as JSON.

### `avr vm delete`

Delete a VM.

```sh
avr vm delete [OPTIONS] VM_ID
```

Delete a VM. Asynchronous while live: shows DELETING until the node confirms the stop.

**Arguments**

- <code class="cli-arg">VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip the confirmation prompt.
- <code class="cli-flag">&#x2D;&#x2D;wait</code> — Wait until the VM is fully deleted before returning.
- <code class="cli-flag">&#x2D;&#x2D;wait-timeout</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Seconds to wait when --wait is set. _(default: `300`)_
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

### `avr vm port-forward`

Forward one or more local ports to TCP ports on the VM over SSH.

```sh
avr vm port-forward [OPTIONS] VM_ID
```

The generic primitive behind `avr vm rdp` / `avr vm vnc`: opens
127.0.0.1:&lt;local&gt; -&gt; &lt;VM&gt;:&lt;guest&gt; through the VM's SSH endpoint for each
--port, and holds them open until Ctrl-C. Bring your own client.

```sh
avr vm port-forward cvm-abc123 --port 8080
avr vm port-forward cvm-abc123 --port 8080 --port 5432 --port 9000:3000
```

**Arguments**

- <code class="cli-arg">VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;port</code> <code class="cli-value">&lt;TEXT&gt;</code> — Guest TCP port to forward, optionally with a local bind port (e.g. 8080 or 9000:8080). Repeatable. _(repeatable · required)_
- <code class="cli-flag">&#x2D;&#x2D;local-port</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Local port to bind for a single bare --port (with multiple ports, use --port LOCAL:GUEST instead).
- <code class="cli-flag">-i, &#x2D;&#x2D;identity</code> <code class="cli-value">&lt;PATH&gt;</code> — Private key file to pass to ssh as -i.
- <code class="cli-flag">&#x2D;&#x2D;print</code> — Print the ssh command and exit, without opening the tunnel.

### `avr vm rdp`

Open an RDP desktop on a Windows or Linux VM over an SSH tunnel.

```sh
avr vm rdp [OPTIONS] VM_ID
```

Forwards a local port to the guest's RDP service (:3389) through the VM's
SSH endpoint, so the desktop is never exposed publicly. Holds the tunnel
open until Ctrl-C; pass --launch to also start a local RDP client.

**Arguments**

- <code class="cli-arg">VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;local-port</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Local port to bind (default: an unused port).
- <code class="cli-flag">-i, &#x2D;&#x2D;identity</code> <code class="cli-value">&lt;PATH&gt;</code> — Private key file to pass to ssh as -i.
- <code class="cli-flag">&#x2D;&#x2D;launch / &#x2D;&#x2D;no-launch</code> — Also start a local RDP client, instead of just printing the connect command.
- <code class="cli-flag">&#x2D;&#x2D;print</code> — Print the tunnel and client commands and exit, without opening the tunnel.

### `avr vm show`

Show a VM's details, including connection endpoints and egress rules.

```sh
avr vm show [OPTIONS] VM_ID
```

**Arguments**

- <code class="cli-arg">VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit the full VM record (with egress rules) as JSON.

### `avr vm ssh`

Open an SSH session to a RUNNING VM, or run a command on it.

```sh
avr vm ssh [OPTIONS] VM_ID [SSH_ARGS]...
```

With no extra arguments this opens an interactive session. Anything after
`--` is run as a remote command instead, e.g.:

    avr vm ssh cvm-abc123 -- uname -a

A one-off `-- <cmd>` runs in a non-login shell that sources no startup files,
so it won't see env forwarded by `avr vm bootstrap`. Pass `--login` to run it
in a login shell instead (e.g. so `claude` finds its subscription token):

    avr vm ssh cvm-abc123 --login -- claude -p 'summarize the repo'

Pass `--session <name>` to attach to (or create) a persistent tmux session,
so the shell and any long-running process in it survive a dropped
connection; reconnect with the same name to resume where you left off. A
`-- <cmd>` given with --session runs only when the session is first created;
reattaching to an existing session resumes it and does not rerun the command.

When the VM's endpoint publishes a host key it is pinned, so the first
connect neither prompts nor is spoofable. If the endpoint has no host key,
`avr` prints a warning and falls back to trust-on-first-use, so this
spoofing protection is conditional rather than guaranteed. For
port-forwarding use `avr vm port-forward`.

**Arguments**

- <code class="cli-arg">VM_ID</code>
- <code class="cli-arg">[SSH_ARGS...]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-i, &#x2D;&#x2D;identity</code> <code class="cli-value">&lt;PATH&gt;</code> — Private key file to pass to ssh as -i.
- <code class="cli-flag">&#x2D;&#x2D;session</code> <code class="cli-value">&lt;TEXT&gt;</code> — Attach to (or create) a persistent tmux session by this name, so the shell and any long-running process in it survive a dropped connection. Reconnect with the same --session. A `-- <cmd>` runs only when the session is created, not on reattach.
- <code class="cli-flag">&#x2D;&#x2D;login</code> — Run the `-- <cmd>` in a login shell (bash -lc) so it sees bootstrap-forwarded env like CLAUDE_CODE_OAUTH_TOKEN. Needed for a non-interactive command; a plain `ssh host cmd` shell sources nothing.
- <code class="cli-flag">&#x2D;&#x2D;print</code> — Print the ssh command instead of running it.

### `avr vm ssh-config`

Print (or --append) an ssh_config Host block for a RUNNING VM.

```sh
avr vm ssh-config [OPTIONS] VM_ID
```

Reach the VM with plain `ssh`, scp/rsync, and VS Code / Cursor Remote-SSH,
host key pinned, without wrapping each tool. Redirect it yourself:

    avr vm ssh-config cvm-abc123 >> ~/.ssh/config

or let --append manage the block for you (idempotent — re-run after a
restart to refresh the endpoint in place):

    avr vm ssh-config cvm-abc123 --append

The block references a dedicated known_hosts file that this command writes
the pinned host key into (one entry per VM). If the endpoint publishes no
host key, the block falls back to accept-new (trust-on-first-use) and a
warning is printed.

**Arguments**

- <code class="cli-arg">VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-i, &#x2D;&#x2D;identity</code> <code class="cli-value">&lt;PATH&gt;</code> — IdentityFile to write into the block.
- <code class="cli-flag">&#x2D;&#x2D;host-alias</code> <code class="cli-value">&lt;TEXT&gt;</code> — Host alias for the block (default: avr-&lt;vm-id&gt;).
- <code class="cli-flag">&#x2D;&#x2D;known-hosts-file</code> <code class="cli-value">&lt;PATH&gt;</code> — Where to pin the host key (default: ~/.ssh/avr_known_hosts).
- <code class="cli-flag">&#x2D;&#x2D;append</code> — Write the block into your SSH config (default: ~/.ssh/config) instead of printing it, replacing any prior block for the same alias in place.
- <code class="cli-flag">&#x2D;&#x2D;config-file</code> <code class="cli-value">&lt;PATH&gt;</code> — SSH config file for --append (default: ~/.ssh/config).

### `avr vm start`

Start a stopped VM.

```sh
avr vm start [OPTIONS] VM_ID
```

Start a stopped VM. Boots a fresh disk and returns a one-time password.

**Arguments**

- <code class="cli-arg">VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;wait</code> — Wait until RUNNING, then print a ready-to-paste connect command with the fresh password.
- <code class="cli-flag">&#x2D;&#x2D;wait-timeout</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Seconds to wait when --wait is set. _(default: `300`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit the raw API response as JSON.

### `avr vm stop`

Stop a running VM.

```sh
avr vm stop [OPTIONS] VM_ID
```

Stop a running VM. The ephemeral disk is discarded.

**Arguments**

- <code class="cli-arg">VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;wait</code> — Wait until the VM reaches STOPPED before returning.
- <code class="cli-flag">&#x2D;&#x2D;wait-timeout</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Seconds to wait when --wait is set. _(default: `300`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> — Emit the raw API response as JSON.

### `avr vm update`

Update a VM's name, TTL, or SSH keys, or rotate its password.

```sh
avr vm update [OPTIONS] VM_ID
```

Power state is controlled separately with avr vm start / avr vm stop.

**Arguments**

- <code class="cli-arg">VM_ID</code>

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

### `avr vm vnc`

Open a VNC desktop on a macOS VM (Screen Sharing) over an SSH tunnel.

```sh
avr vm vnc [OPTIONS] VM_ID
```

Forwards a local port to the guest's Screen Sharing service (:5900) through
the VM's SSH endpoint, so the desktop is never exposed publicly. Holds the
tunnel open until Ctrl-C; pass --launch to also open Screen Sharing.

**Arguments**

- <code class="cli-arg">VM_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;local-port</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Local port to bind (default: an unused port).
- <code class="cli-flag">-i, &#x2D;&#x2D;identity</code> <code class="cli-value">&lt;PATH&gt;</code> — Private key file to pass to ssh as -i.
- <code class="cli-flag">&#x2D;&#x2D;launch / &#x2D;&#x2D;no-launch</code> — Also start a local VNC client (macOS Screen Sharing), instead of just printing the connect command.
- <code class="cli-flag">&#x2D;&#x2D;print</code> — Print the tunnel and client commands and exit, without opening the tunnel.
