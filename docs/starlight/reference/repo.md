---
title: avr repo
description: "Manage repositories and public mirrors."
---

Manage repositories and public mirrors.

```sh
avr repo [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr repo list`

List repositories you can access in an organization.

```sh
avr repo list [OPTIONS]
```

```sh
Examples:
    avr repo list
    avr repo list --json full_name,repository_id
    avr repo list --json '*' -q '.[].full_name'
```

```sh
JSON FIELDS
    full_name, platform, platform_repository_id, repository_id
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max repositories to return. _(default: `100`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr repo public-mirror`

Request and browse mirrors of public GitHub repositories.

```sh
avr repo public-mirror [OPTIONS] COMMAND [ARGS]...
```

#### `avr repo public-mirror cancel`

Cancel one pending public-mirror request.

```sh
avr repo public-mirror cancel [OPTIONS] REQUEST_ID
```

An approved mirror is global and cannot be withdrawn by a requester.

```sh
Examples:
    avr repo public-mirror cancel pmr-0123456789abcdef0123456789abcdef
    avr repo public-mirror cancel pmr-0123456789abcdef0123456789abcdef --yes
```

```sh
JSON FIELDS
    approval_state, created_at, github_snapshot, public_access_expires_at,
    reason, repository_full_name, repository_id, request_id,
    requester_organization_id, requester_user_id, review_note, reviewed_at,
    reviewed_by_user_id, status, updated_at
```

**Arguments**

- <code class="cli-arg">REQUEST_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip the confirmation prompt.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr repo public-mirror check`

Check whether a public GitHub repository mirror is available.

```sh
avr repo public-mirror check [OPTIONS] FULL_NAME
```

FULL_NAME must be an owner/repository name. This performs an exact lookup;
the global public-mirror catalog cannot be listed.

```sh
Examples:
    avr repo public-mirror check rust-lang/rust
    avr repo public-mirror check rust-lang/rust --json '*'
    avr repo public-mirror check rust-lang/rust --json repository_id,default_branch
```

```sh
JSON FIELDS
    approval_state, default_branch, https_clone_url, installation_kind,
    is_archived, is_disabled, is_fork, mirror_enabled, platform_owner_id,
    platform_owner_login, platform_owner_type, platform_pushed_at,
    platform_repository_id, platform_size_kb, public_access_expires_at,
    public_metadata_verified_at, repository_full_name, repository_id
```

**Arguments**

- <code class="cli-arg">FULL_NAME</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr repo public-mirror request`

Request a mirror of a public GitHub repository.

```sh
avr repo public-mirror request [OPTIONS] FULL_NAME
```

FULL_NAME is the canonical GitHub owner/repository name. Avrea validates
the repository with GitHub before recording the request.

```sh
Examples:
    avr repo public-mirror request rust-lang/rust
    avr repo public-mirror request rust-lang/rust --reason "Build dependency"
    avr repo public-mirror request rust-lang/rust --org acme --json '*'
```

```sh
JSON FIELDS
    approval_state, created_at, github_snapshot, public_access_expires_at,
    reason, repository_full_name, repository_id, request_id,
    requester_organization_id, requester_user_id, review_note, reviewed_at,
    reviewed_by_user_id, status, updated_at
```

**Arguments**

- <code class="cli-arg">FULL_NAME</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;reason</code> <code class="cli-value">&lt;TEXT&gt;</code> — Why your organization needs this repository mirrored.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr repo public-mirror requests`

List your organization's public-mirror requests.

```sh
avr repo public-mirror requests [OPTIONS]
```

```sh
Examples:
    avr repo public-mirror requests
    avr repo public-mirror requests --org acme
    avr repo public-mirror requests --json request_id,status,repository_full_name
```

```sh
JSON FIELDS
    approval_state, created_at, github_snapshot, public_access_expires_at,
    reason, repository_full_name, repository_id, request_id,
    requester_organization_id, requester_user_id, review_note, reviewed_at,
    reviewed_by_user_id, status, updated_at
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr repo public-mirror view`

View one public-mirror request.

```sh
avr repo public-mirror view [OPTIONS] REQUEST_ID
```

```sh
Examples:
    avr repo public-mirror view pmr-0123456789abcdef0123456789abcdef
    avr repo public-mirror view pmr-0123456789abcdef0123456789abcdef --json '*'
```

```sh
JSON FIELDS
    approval_state, created_at, github_snapshot, public_access_expires_at,
    reason, repository_full_name, repository_id, request_id,
    requester_organization_id, requester_user_id, review_note, reviewed_at,
    reviewed_by_user_id, status, updated_at
```

**Arguments**

- <code class="cli-arg">REQUEST_ID</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
