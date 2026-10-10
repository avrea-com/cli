---
title: avr token
description: "Create and manage scoped access tokens."
---

Create and manage scoped access tokens.

```sh
avr token [OPTIONS] COMMAND [ARGS]...
```

A scoped access token is a credential bound to one organization that
expires within seven days. It reaches only the repositories and VMs it
names, and it cannot create further tokens. To use one, set AVR_TOKEN to
the credential and AVR_ORG to the organization ID.

## Subcommands

### `avr token create`

Create a scoped token and print its credential.

```sh
avr token create [OPTIONS]
```

The credential is shown once and cannot be retrieved later. A token needs
at least one of --repo, --vm or --allow-vm-create. Repository grants are
read-only and VM grants are admin; an optional level suffix
(--repo acme/api:read, --vm cvm-abc123:admin) must name that level.

```sh
Examples:
    avr token create --name ci-read --repo acme/api --repo acme/web
    avr token create --name agent --vm cvm-abc123 --ttl 30m
    avr token create --name sandbox --allow-vm-create --vm-create-limit 3
    avr token create --name ci-read --repo acme/api --json token --jq .token
```

```sh
JSON FIELDS
    allow_vm_create, created_at, expires_at, grants, id, last_used_at,
    name, organization_id, revoked_at, revoked_reason, token, user_id,
    vm_create_count, vm_create_limit
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;name</code> <code class="cli-value">&lt;TEXT&gt;</code> — Token name (1-100 characters). _(required)_
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Grant read access to a repository (org/repo or rep-xxx). Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;vm</code> <code class="cli-value">&lt;TEXT&gt;</code> — Grant admin access to a VM, by VM ID. Repeatable. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;allow-vm-create</code> — Let the token create VMs.
- <code class="cli-flag">&#x2D;&#x2D;vm-create-limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max VMs the token may create; needs --allow-vm-create. The server default is 1.
- <code class="cli-flag">&#x2D;&#x2D;ttl</code> <code class="cli-value">&lt;TEXT&gt;</code> — Lifetime: e.g. 30m, 8h, 7d, or a number of seconds. 60 seconds to 7 days; the server default is 8 hours.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr token list`

List live tokens, newest first.

```sh
avr token list [OPTIONS]
```

Shows your own tokens, or every member's when you are an organization
admin. Revoked and expired tokens are not listed; `avr token view` still
reads them by ID.

```sh
Examples:
    avr token list
    avr token list --json id,name,expires_at
```

```sh
JSON FIELDS
    allow_vm_create, created_at, expires_at, grants, id, last_used_at,
    name, organization_id, revoked_at, revoked_reason, user_id,
    vm_create_count, vm_create_limit
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max tokens to return. _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr token revoke`

Revoke a token.

```sh
avr token revoke [OPTIONS] TOKEN_ID
```

Requests made with the token are refused from then on. Revoking a token
that is already revoked succeeds.

```sh
Examples:
    avr token revoke <token-id>
    avr token revoke <token-id> --yes
```

**Arguments**

- <code class="cli-arg">TOKEN_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip the confirmation prompt.

### `avr token view`

Show a token's details and grants, including a revoked or expired one.

```sh
avr token view [OPTIONS] TOKEN_ID
```

The credential itself is never shown again after `avr token create`.

```sh
Examples:
    avr token view <token-id>
    avr token view <token-id> --json grants
```

```sh
JSON FIELDS
    allow_vm_create, created_at, expires_at, grants, id, last_used_at,
    name, organization_id, revoked_at, revoked_reason, user_id,
    vm_create_count, vm_create_limit
```

**Arguments**

- <code class="cli-arg">TOKEN_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
