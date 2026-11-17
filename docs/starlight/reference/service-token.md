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
Organization admins manage service tokens while signed in with
`avr auth login`; a scoped or service token cannot. To use a service
token, set AVR_TOKEN to its secret.

## Subcommands

### `avr service-token create`

Create a service token and print its secret once.

```sh
avr service-token create [OPTIONS]
```

The token belongs to the group --group names, not to you, and keeps
working when you leave the organization. It needs at least one grant;
every grant is a read and must be within your own access now. The
secret is shown once and cannot be retrieved later: store it, for
example as a CI secret, and set it as AVR_TOKEN where the token is used.
Creating a service token needs a recent sign-in.

--group takes a group ID or a group's path as `avr group list` shows it,
such as Acme/Platform/CI. The end of a path, such as Platform/CI or a
group name like CI, works when it selects only one group.

With --json, the selection must include the token field, because the
secret cannot be read again. If a --jq filter fails or drops the
secret, the command prints the unfiltered JSON instead and exits
nonzero, so the secret is not lost.

```sh
Examples:
    avr service-token create --name ci --group Platform/CI --repo acme/api
    avr service-token create --name egress --group CI --all-firewall-rules --ttl 7d
    avr service-token create --name ci --group <group-id> --repo acme/api --json token --jq .token
```

```sh
JSON FIELDS
    blocked_by, created_at, created_by, expires_at, expiring_soon, grants,
    grants_version, group_id, id, last_used_at, name, organization_id,
    previous_valid_until, revoked_at, revoked_reason, rotated_at,
    rotation_version, status, token
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;name</code> <code class="cli-value">&lt;TEXT&gt;</code> — Token name (1-100 characters). _(required)_
- <code class="cli-flag">&#x2D;&#x2D;group</code> <code class="cli-value">&lt;TEXT&gt;</code> — The group that owns the token: its name, path or ID (grp-xxx). _(required)_
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Grant read access to a repository (org/repo or rep-xxx). Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;firewall-rule</code> <code class="cli-value">&lt;TEXT&gt;</code> — Grant read access to an egress firewall rule, by rule ID (efr-xxx). Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;all-firewall-rules</code> — Grant read access to every egress firewall rule, including rules added later.
- <code class="cli-flag">&#x2D;&#x2D;ttl</code> <code class="cli-value">&lt;TEXT&gt;</code> — Lifetime: e.g. 30m, 8h, 7d, or a number of seconds. 60 seconds to 90 days; the server default is 30 days.
- <code class="cli-flag">&#x2D;&#x2D;never-expires</code> — The secret never expires: anyone holding it keeps its access until it is revoked. Cannot be combined with --ttl.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
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
such as Acme/Platform/CI. The end of a path, such as Platform/CI or a
group name like CI, works when it selects only one group.

```sh
Examples:
    avr service-token list
    avr service-token list --status live --group Platform/CI
    avr service-token list --json id,name,expires_at
```

```sh
JSON FIELDS
    blocked_by, created_at, created_by, expires_at, expiring_soon, grants,
    grants_version, group_id, id, last_used_at, name, organization_id,
    previous_valid_until, revoked_at, revoked_reason, rotated_at,
    rotation_version, status
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
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
working on their next request, and its grants are removed. Revoking a
token that is already revoked succeeds.

```sh
Examples:
    avr service-token revoke <token-id>
    avr service-token revoke <token-id> --yes
```

**Arguments**

- <code class="cli-arg">TOKEN_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip the confirmation prompt.

### `avr service-token revoke-all`

Revoke every service token of the organization.

```sh
avr service-token revoke-all [OPTIONS]
```

For a suspected leak: every unrevoked service token of the organization,
expired ones included, stops working on its next request.

```sh
Examples:
    avr service-token revoke-all --org <org>
    avr service-token revoke-all --org <org> --yes
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip the confirmation prompt.

### `avr service-token rotate`

Issue a service token a new secret and print it once.

```sh
avr service-token rotate [OPTIONS] TOKEN_ID
```

The new secret gets its own lifetime. The old secret keeps working for
the overlap, never past the expiry of either secret, so pipelines can
switch without a gap. Rotating again inside the overlap retires the old
secret at once. Rotating needs a recent sign-in.

With --json, the selection must include the token field, because the
secret cannot be read again. If a --jq filter fails or drops the
secret, the command prints the unfiltered JSON instead and exits
nonzero, so the secret is not lost.

```sh
Examples:
    avr service-token rotate <token-id>
    avr service-token rotate <token-id> --ttl 30d --overlap 2h
    avr service-token rotate <token-id> --json token --jq .token
```

```sh
JSON FIELDS
    blocked_by, created_at, created_by, expires_at, expiring_soon, grants,
    grants_version, group_id, id, last_used_at, name, organization_id,
    previous_valid_until, revoked_at, revoked_reason, rotated_at,
    rotation_version, status, token
```

**Arguments**

- <code class="cli-arg">TOKEN_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;ttl</code> <code class="cli-value">&lt;TEXT&gt;</code> — Lifetime: e.g. 30m, 8h, 7d, or a number of seconds. 60 seconds to 90 days; the server default is 30 days.
- <code class="cli-flag">&#x2D;&#x2D;never-expires</code> — The secret never expires: anyone holding it keeps its access until it is revoked. Cannot be combined with --ttl.
- <code class="cli-flag">&#x2D;&#x2D;overlap</code> <code class="cli-value">&lt;TEXT&gt;</code> — How long the old secret keeps working: e.g. 2h, 3d, or a number of seconds. 1 hour to 7 days; the server default is 24 hours.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr service-token set-access`

Replace a service token's whole access selection.

```sh
avr service-token set-access [OPTIONS] TOKEN_ID
```

The grants given here replace everything the token reaches: a grant you
leave out is removed, so repeat the ones to keep. The command reads the
token first and the change fails if the token changes in between. A
grant that widens its access must be within your own access now, and
any change needs a recent sign-in. The change applies to the token's
next request.

```sh
Examples:
    avr service-token set-access <token-id> --repo acme/api --repo acme/web
    avr service-token set-access <token-id> --all-firewall-rules
```

**Arguments**

- <code class="cli-arg">TOKEN_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Grant read access to a repository (org/repo or rep-xxx). Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;firewall-rule</code> <code class="cli-value">&lt;TEXT&gt;</code> — Grant read access to an egress firewall rule, by rule ID (efr-xxx). Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;all-firewall-rules</code> — Grant read access to every egress firewall rule, including rules added later.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).

### `avr service-token view`

Show a service token's details, versions and grants.

```sh
avr service-token view [OPTIONS] TOKEN_ID
```

Reads a revoked or expired token as well. Its grants are removed when it
is revoked and 30 days after it expires. The secret itself is never
shown again after `avr service-token create` or `rotate`.

```sh
Examples:
    avr service-token view <token-id>
    avr service-token view <token-id> --json grants
```

```sh
JSON FIELDS
    blocked_by, created_at, created_by, expires_at, expiring_soon, grants,
    grants_version, group_id, id, last_used_at, name, organization_id,
    previous_valid_until, revoked_at, revoked_reason, rotated_at,
    rotation_version, status
```

**Arguments**

- <code class="cli-arg">TOKEN_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
