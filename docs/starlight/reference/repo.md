---
title: avr repo
description: "Manage repositories and mirrors."
---

Manage repositories and mirrors.

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

### `avr repo mirror`

Create and manage avrea-git mirrors of your repositories.

```sh
avr repo mirror [OPTIONS] COMMAND [ARGS]...
```

#### `avr repo mirror create`

Create an avrea-git mirror for a repository.

```sh
avr repo mirror create [OPTIONS]
```

Enables mirroring and places the repository in Avrea's git clusters, where
it is kept in sync automatically. Idempotent: re-running converges the
mirror configuration. Requires the organization admin role.

```sh
Examples:
    avr repo mirror create
    avr repo mirror create --repo acme/widgets
    avr repo mirror create --repo rep-abc123 --json '*'
```

```sh
JSON FIELDS
    enabled, full_name, placements, repository_id
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr repo mirror delete`

Disable a repository's avrea-git mirror.

```sh
avr repo mirror delete [OPTIONS]
```

Stops mirroring the repository. The mirror configuration is retained but
inert, so re-creating the mirror restores it. Requires the organization
admin role.

```sh
Examples:
    avr repo mirror delete --repo acme/widgets
    avr repo mirror delete --repo acme/widgets --yes
```

```sh
JSON FIELDS
    enabled, full_name, placements, repository_id
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip the confirmation prompt.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr repo mirror status`

Show a repository's avrea-git mirror status.

```sh
avr repo mirror status [OPTIONS]
```

Reports whether the repository is mirrored and, per cluster, when it last
synced.

```sh
Examples:
    avr repo mirror status
    avr repo mirror status --repo acme/widgets
    avr repo mirror status --repo acme/widgets --json enabled
    avr repo mirror status --json '*' -q '.placements[].cluster_id'
```

```sh
JSON FIELDS
    enabled, full_name, placements, repository_id
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
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

A repository that is not mirrored is an answer, not a failure: the command
reports ``Available: no`` (``available: false`` under --json) and exits 0.
Only a real failure — denied, malformed name, server error — exits non-zero.

```sh
Examples:
    avr repo public-mirror check rust-lang/rust
    avr repo public-mirror check rust-lang/rust --json '*'
    avr repo public-mirror check rust-lang/rust --json available,default_branch
```

```sh
JSON FIELDS
    approval_state, available, default_branch, https_clone_url,
    installation_kind, is_archived, is_disabled, is_fork, mirror_enabled,
    platform_owner_id, platform_owner_login, platform_owner_type,
    platform_pushed_at, platform_repository_id, platform_size_kb,
    public_access_expires_at, public_metadata_verified_at,
    repository_full_name, repository_id
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
