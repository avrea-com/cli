---
title: avr sbom
description: "View, generate, and download repository SBOMs."
---

View, generate, and download repository SBOMs.

```sh
avr sbom [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr sbom download`

Download an SBOM artifact (CycloneDX, SPDX, or dependency inventory).

```sh
avr sbom download [OPTIONS] [COMMIT]
```

COMMIT is the commit of a recorded SBOM, as a full SHA or a unique prefix
of at least 7 characters such as the one `avr sbom list` shows. Defaults to
the latest SBOM.

```sh
Examples:
    avr sbom download --repo acme/api
    avr sbom download --repo acme/api --format spdx --out api.spdx.json
    avr sbom download 3f2c9a1b7d04 --repo acme/api --out - | jq .components
```

**Arguments**

- <code class="cli-arg">[COMMIT]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;commit</code> <code class="cli-value">&lt;TEXT&gt;</code> — Same as the COMMIT argument.
- <code class="cli-flag">&#x2D;&#x2D;format</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Artifact to download. _(choices: `cyclonedx`, `inventory`, `spdx` · default: `cyclonedx`)_
- <code class="cli-flag">&#x2D;&#x2D;out</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output file path, or "-" for stdout. Defaults to the artifact filename in the current directory.

### `avr sbom generate`

Start SBOM generation for a repository.

```sh
avr sbom generate [OPTIONS] [REF]
```

REF is the branch, tag, or commit SHA to analyse; an abbreviated SHA such
as the one `avr sbom list` shows works too. Defaults to the default-branch
tip. Without --wait, commit_sha is the full SHA of the commit the run
analyses when REF names one.

One analysis runs per repository at a time. A request for the same ref as
the analysis already running joins it; a request for a different ref is
rejected (HTTP 409) until that analysis finishes. Once an SBOM has been
recorded for the repository, a new run is refused (HTTP 429) for a
cooldown period, and the error says how many seconds remain.

With --wait and a REF that names a commit, success means a new SBOM for
that commit is downloadable, whatever the analysis task's own outcome
(task_status); commit_sha and recorded_at identify it. A failed analysis
can still deliver its SBOM later, so such a wait runs to --wait-timeout
before reporting failure. For a branch, tag, or the default branch the API
does not say which commit the run resolved, so --wait reports the analysis
task's outcome only. Transient errors while waiting are retried until
--wait-timeout; a wait that times out leaves the analysis running.

```sh
Examples:
    avr sbom generate --repo acme/api
    avr sbom generate v1.4.0 --repo acme/api
    avr sbom generate 3f2c9a1b7d04 --repo acme/api --wait
```

```sh
JSON FIELDS
    ai_task_id, commit_sha, error_code, recorded_at, status, task_status
```

**Arguments**

- <code class="cli-arg">[REF]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;ref</code> <code class="cli-value">&lt;TEXT&gt;</code> — Same as the REF argument.
- <code class="cli-flag">&#x2D;&#x2D;wait</code> — Wait until generation finishes before returning.
- <code class="cli-flag">&#x2D;&#x2D;wait-timeout</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Seconds to wait when --wait is set. _(default: `900`)_
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr sbom list`

List the repository's recorded SBOMs, newest first.

```sh
avr sbom list [OPTIONS]
```

Counts shown as "?" were not recorded by the SBOM's schema version.

```sh
Examples:
    avr sbom list --repo acme/api
    avr sbom list --repo acme/api --json commit_sha,copyleft_count
```

```sh
JSON FIELDS
    artifacts, branch, commit_sha, copyleft_count, generated_at,
    is_release, recorded_at, schema_version, total_dependencies,
    unresolved
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max SBOMs to return. _(default: `50`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Opaque cursor from a previous response's next_cursor.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr sbom view`

Show an SBOM's summary: dependency counts, licences, and artifacts.

```sh
avr sbom view [OPTIONS] [COMMIT]
```

COMMIT is the commit of a recorded SBOM, as a full SHA or a unique prefix
of at least 7 characters such as the one `avr sbom list` shows. Defaults to
the latest SBOM.

```sh
Examples:
    avr sbom view --repo acme/api
    avr sbom view 3f2c9a1b7d04 --repo acme/api
    avr sbom view --repo acme/api --json summary --jq .summary.licenses
```

```sh
JSON FIELDS
    artifacts, branch, commit_sha, generated_at, is_release, recorded_at,
    schema_version, summary
```

**Arguments**

- <code class="cli-arg">[COMMIT]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;commit</code> <code class="cli-value">&lt;TEXT&gt;</code> — Same as the COMMIT argument.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
