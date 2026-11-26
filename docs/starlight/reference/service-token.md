---
title: avr service-token
description: "Create and manage the organization's service tokens."
---

Create and manage the organization's service tokens.

```sh
avr service-token [OPTIONS] COMMAND [ARGS]...
```

A service token (avt_...) belongs to a group of one organization rather
than to a person, and keeps working when its creator leaves. It reads
only the repositories and egress firewall rules its access names.
Organization admins manage service tokens while signed in (avr auth
device-login or avr auth login); a personal access token or service
token cannot.

A service token reaches the VMs its grants name. With --max-owned-vms it
creates VMs, and it administers those VMs even after VM creation is
stopped. It runs avr vm create without --repo or --egress-rules, list,
show, ssh, ssh-config, metrics without --watch, update with --name or
--ttl, rotate-https-label, pause and delete; resume and fork are
refused. avr vm capabilities needs a VM grant or --max-owned-vms.
Revoking the token deletes the VMs it created.

To use a service token, set AVR_TOKEN to its secret. Its organization
applies automatically; set AVR_ORG to the org-... ID in loops to save a
request per command. By default, a service token has 60 requests per
minute, or 31 per VM it may own when that is more, up to 600. Commands
that need a person, such as avr feedback, refuse it, and avr auth logout
does not revoke it: unset AVR_TOKEN instead. After a suspected leak,
revoke-all revokes every service token of the organization.

## Subcommands

### `avr service-token create`

Create a service token and print its secret once.

```sh
avr service-token create [OPTIONS]
```

The token belongs to the group --group names, not to you, and keeps
working when you leave the organization. It needs at least one grant
or --max-owned-vms. Repository and firewall-rule grants are reads; a
VM grant is read or write. Each grant must be within your own access
now. With --max-owned-vms the token creates VMs and administers the
ones it created; a token of the root group cannot create VMs. The
secret is shown once and cannot be retrieved later: store it, for
example as a CI secret, and set it as AVR_TOKEN where the token is used.
The API may ask for a recent sign-in; the command then exits 4 and says
to sign in again.

--group takes a group ID or a group's path as `avr group list` shows it,
such as Organization/Platform/CI. Names compare without case. A value equal to
one group's whole path selects that group; otherwise the end of a path,
such as Platform/CI or a group name like CI, must select exactly one
group.

With --json, the selection must include the token field, because the
secret cannot be read again. If a --jq filter fails or drops the
secret, the command prints the unfiltered JSON instead and exits
nonzero, so the secret is not lost.

Examples:

```sh
avr service-token create --name ci --group Platform/CI --repo acme/api
avr service-token create --name egress --group CI --all-firewall-rules --ttl 7d
avr service-token create --name agents --group Platform/CI --repo acme/api --max-owned-vms 5
avr service-token create --name ci --group <group-id> --repo acme/api --json token --jq .token
```

JSON FIELDS

```sh
blocked_by, created_at, created_by, expires_at, expiring_soon, grants,
grants_version, group_id, id, last_used_at, name, organization_id,
previous_valid_until, revoked_at, revoked_reason, rotated_at,
rotation_version, status, vm_cap, vm_count, token
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;name</code> <code class="cli-value">&lt;TEXT&gt;</code> — Token name (1-100 characters). _(required)_
- <code class="cli-flag">&#x2D;&#x2D;group</code> <code class="cli-value">&lt;TEXT&gt;</code> — The group that owns the token: its name, path or ID (grp-xxx). _(required)_
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Grant read access to a repository (org/repo or rep-xxx). Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;firewall-rule</code> <code class="cli-value">&lt;TEXT&gt;</code> — Grant read access to an egress firewall rule, by rule ID (efr-xxx). Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;all-firewall-rules</code> — Grant read access to every egress firewall rule, including rules added later.
- <code class="cli-flag">&#x2D;&#x2D;vm</code> <code class="cli-value">&lt;TEXT&gt;</code> — Grant access to a VM, by VM ID (cvm-xxx): read, or write with cvm-xxx:write. Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;all-vms</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Grant read or write access to every VM of the organization, including VMs created later. _(choices: `read`, `write`)_
- <code class="cli-flag">&#x2D;&#x2D;max-owned-vms</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Let the token create VMs and own up to this many at once; deleting a VM frees a place. A token of the root group cannot create VMs.
- <code class="cli-flag">&#x2D;&#x2D;ttl</code> <code class="cli-value">&lt;TEXT&gt;</code> — Lifetime: e.g. 30m, 8h, 7d, or a number of seconds. 60 seconds to 90 days; the server default is 30 days.
- <code class="cli-flag">&#x2D;&#x2D;never-expires</code> — The secret never expires: anyone holding it keeps its access until it is revoked. Cannot be combined with --ttl.
- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr service-token list`

List the organization's service tokens, soonest expiry first.

```sh
avr service-token list [OPTIONS]
```

Lists live, expired and revoked tokens; tokens that never expire come
last. A live token whose requests are refused all the same, for example
while its group is suspended, shows as blocked with the reason.

--group takes a group ID or a group's path as `avr group list` shows it,
such as Organization/Platform/CI. Names compare without case. A value equal to
one group's whole path selects that group; otherwise the end of a path,
such as Platform/CI or a group name like CI, must select exactly one
group.

Examples:

```sh
avr service-token list
avr service-token list --status live --group Platform/CI
avr service-token list --json id,name,expires_at
```

JSON FIELDS

```sh
blocked_by, created_at, created_by, expires_at, expiring_soon, grants,
grants_version, group_id, id, last_used_at, name, organization_id,
previous_valid_until, revoked_at, revoked_reason, rotated_at,
rotation_version, status, vm_cap, vm_count
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;group</code> <code class="cli-value">&lt;TEXT&gt;</code> — Only the tokens this group owns, by group name, path or ID (grp-xxx).
- <code class="cli-flag">&#x2D;&#x2D;status</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Only tokens with this status. _(choices: `live`, `expired`, `revoked`)_
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max tokens to return. _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr service-token revoke`

Revoke a service token.

```sh
avr service-token revoke [OPTIONS] TOKEN_ID
```

Its current secret and any old secret still in its rotation overlap stop
working on their next request, its grants are removed, and the VMs it
created are deleted. Revoking a token that is already revoked succeeds.

Examples:

```sh
avr service-token revoke <token-id>
avr service-token revoke <token-id> --yes
```

**Arguments**

- <code class="cli-arg">TOKEN_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip the confirmation prompt.

### `avr service-token revoke-all`

Revoke every service token of the organization.

```sh
avr service-token revoke-all [OPTIONS]
```

For a suspected leak: every unrevoked service token of the organization,
expired ones included, stops working on its next request, and the VMs
those tokens created are deleted.

Examples:

```sh
avr service-token revoke-all --org <org>
avr service-token revoke-all --org <org> --yes
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip the confirmation prompt.

### `avr service-token rotate`

Issue a service token a new secret and print it once.

```sh
avr service-token rotate [OPTIONS] TOKEN_ID
```

The new secret gets its own lifetime. The old secret keeps working for
the overlap, never past the expiry of either secret, so pipelines can
switch without a gap. Rotating again inside the overlap retires the old
secret at once; run from a terminal, the command asks first unless --yes
is given. The API may ask for a recent sign-in; the command then exits 4
and says to sign in again.

With --json, the selection must include the token field, because the
secret cannot be read again. If a --jq filter fails or drops the
secret, the command prints the unfiltered JSON instead and exits
nonzero, so the secret is not lost.

Examples:

```sh
avr service-token rotate <token-id>
avr service-token rotate <token-id> --ttl 30d --overlap 2h
avr service-token rotate <token-id> --json token --jq .token
```

JSON FIELDS

```sh
blocked_by, created_at, created_by, expires_at, expiring_soon, grants,
grants_version, group_id, id, last_used_at, name, organization_id,
previous_valid_until, revoked_at, revoked_reason, rotated_at,
rotation_version, status, vm_cap, vm_count, token
```

**Arguments**

- <code class="cli-arg">TOKEN_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;ttl</code> <code class="cli-value">&lt;TEXT&gt;</code> — Lifetime: e.g. 30m, 8h, 7d, or a number of seconds. 60 seconds to 90 days; the server default is 30 days.
- <code class="cli-flag">&#x2D;&#x2D;never-expires</code> — The secret never expires: anyone holding it keeps its access until it is revoked. Cannot be combined with --ttl.
- <code class="cli-flag">&#x2D;&#x2D;overlap</code> <code class="cli-value">&lt;TEXT&gt;</code> — How long the old secret keeps working: e.g. 2h, 3d, or a number of seconds. 1 hour to 7 days; the server default is 24 hours.
- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip the confirmation prompt.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr service-token set-access`

Replace a service token's grants or change its VM creation.

```sh
avr service-token set-access [OPTIONS] TOKEN_ID
```

Grant options (--repo, --firewall-rule, --all-firewall-rules, --vm and
--all-vms) replace every grant the token holds: a grant you leave out
is removed, so repeat the ones to keep. Without grant options the
grants stay; --no-grants removes them all and leaves VM creation as
it is. The maximum of owned VMs stays unless --max-owned-vms or
--no-vm-create changes it. When the change removes or lowers a grant
or VM creation, the command lists what goes and asks first; without a
terminal it then refuses unless --yes is given. It reads the token first and the change fails if its access
changes in between. A grant that widens its access must be within your
own access now. The API may ask for a recent sign-in; the command then
exits 4 and says to sign in again. The change applies to the token's
next request.

Examples:

```sh
avr service-token set-access <token-id> --repo acme/api --repo acme/web
avr service-token set-access <token-id> --all-firewall-rules --yes
avr service-token set-access <token-id> --max-owned-vms 10
avr service-token set-access <token-id> --no-vm-create
```

**Arguments**

- <code class="cli-arg">TOKEN_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Grant read access to a repository (org/repo or rep-xxx). Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;firewall-rule</code> <code class="cli-value">&lt;TEXT&gt;</code> — Grant read access to an egress firewall rule, by rule ID (efr-xxx). Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;all-firewall-rules</code> — Grant read access to every egress firewall rule, including rules added later.
- <code class="cli-flag">&#x2D;&#x2D;vm</code> <code class="cli-value">&lt;TEXT&gt;</code> — Grant access to a VM, by VM ID (cvm-xxx): read, or write with cvm-xxx:write. Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;all-vms</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Grant read or write access to every VM of the organization, including VMs created later. _(choices: `read`, `write`)_
- <code class="cli-flag">&#x2D;&#x2D;max-owned-vms</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Let the token create VMs and own up to this many at once; deleting a VM frees a place. A token of the root group cannot create VMs. Lowering it keeps the VMs the token owns. Omit to keep the current maximum.
- <code class="cli-flag">&#x2D;&#x2D;no-grants</code> — Remove every grant; whether the token creates VMs stays as it is. Cannot be combined with grant options.
- <code class="cli-flag">&#x2D;&#x2D;no-vm-create</code> — Stop the token creating VMs; its grants stay unless grant options are given. It keeps administering the VMs it created.
- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Apply the change without asking, even when it removes or lowers access.

### `avr service-token view`

Show a service token's details, versions and grants.

```sh
avr service-token view [OPTIONS] TOKEN_ID
```

Reads a revoked or expired token as well. Its grants are removed when it
is revoked and 30 days after it expires. The secret itself is never
shown again after `avr service-token create` or `rotate`.

Examples:

```sh
avr service-token view <token-id>
avr service-token view <token-id> --json grants
```

JSON FIELDS

```sh
blocked_by, created_at, created_by, expires_at, expiring_soon, grants,
grants_version, group_id, id, last_used_at, name, organization_id,
previous_valid_until, revoked_at, revoked_reason, rotated_at,
rotation_version, status, vm_cap, vm_count
```

**Arguments**

- <code class="cli-arg">TOKEN_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
