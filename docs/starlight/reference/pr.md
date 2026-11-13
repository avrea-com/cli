---
title: avr pr
description: "View and comment on pull requests."
---

View and comment on pull requests.

```sh
avr pr [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr pr comment`

Comment on a pull request.

```sh
avr pr comment [OPTIONS] NUMBER
```

Avrea stores the comment and, when GitHub sync is enabled for the
organization, publishes it to GitHub asynchronously as the Avrea GitHub App or
your linked GitHub account, with attribution.

Without --path, the comment applies to the whole pull request. With --path and
--line, it starts an inline review thread on the pull request's current head
commit. --start-line extends it to a range of lines. Line numbers refer to the
new file (--side right) or the old file (--side left).

Requires a signed-in account (scoped tokens cannot comment), pull request
comments enabled for the organization, and an Avrea Git mirror of the
repository.

```sh
Examples:
    avr pr comment 42 --body "Looks good to me."
    avr pr comment 42 --body-file review.md
    go test ./... 2>&1 | tail -n 20 | avr pr comment 42 --body-file -
    avr pr comment 42 --path src/app.go --line 18 --body "Handle this error."
    avr pr comment 42 --path src/app.go --start-line 10 --line 14 --side left --body "Why?"
    avr pr comment 42 --body "Ship it" --json id,publication
```

```sh
JSON FIELDS
    base_sha, body, commit_sha, created_at, end_line, id, path, publication,
    pull_number, repository_id, side, start_line, thread_id
```

**Arguments**

- <code class="cli-arg">NUMBER</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;body</code> <code class="cli-value">&lt;TEXT&gt;</code> — Comment text in Markdown.
- <code class="cli-flag">&#x2D;&#x2D;body-file</code> <code class="cli-value">&lt;FILENAME&gt;</code> — Read the comment text from a file, or - for stdin.
- <code class="cli-flag">&#x2D;&#x2D;path</code> <code class="cli-value">&lt;TEXT&gt;</code> — Comment inline on this file, relative to the repository root. Requires --line.
- <code class="cli-flag">&#x2D;&#x2D;line</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Line to comment on, or the last line of a --start-line range.
- <code class="cli-flag">&#x2D;&#x2D;start-line</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — First line of a multi-line inline comment. Defaults to --line.
- <code class="cli-flag">&#x2D;&#x2D;side</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Diff side the lines refer to: right for the new file, left for the old file. _(choices: `right`, `left` · default: `right`)_
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository (org/repo or rep-xxx). Auto-detected from git remote if omitted.
- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr pr list`

List pull requests across repositories.

```sh
avr pr list [OPTIONS]
```

```sh
Examples:
    avr pr list
    avr pr list --scope authored
    avr pr list --repo acme/widgets --state merged
    avr pr list --json number,title,mergeability
    avr pr list --json '?'           # list available fields
    avr pr list --json '*'           # all fields
```

```sh
JSON FIELDS
    author_login, base_ref, base_sha, check_status, comment_count, created_at,
    draft, head_ref, head_sha, mergeability, merged, number, repository_full_name,
    repository_id, state, title, unresolved_thread_count, updated_at
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;repo</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter by repository (org/repo or rep-xxx). Pass --repo more than once to filter multiple repositories. Auto-detected from git remote if omitted. _(repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;scope</code> <code class="cli-value">&lt;CHOICE&gt;</code> — List all, authored, involved, rest, or attention PRs. Attention requires open state. _(choices: `all`, `authored`, `involved`, `rest`, `attention` · default: `all`)_
- <code class="cli-flag">&#x2D;&#x2D;state</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Filter by pull request state. 'all' removes the state filter. _(choices: `open`, `closed`, `merged`, `all` · default: `open`)_
- <code class="cli-flag">-L, &#x2D;&#x2D;limit</code> <code class="cli-value">&lt;INTEGER RANGE&gt;</code> — Max PRs to return. _(default: `20`)_
- <code class="cli-flag">&#x2D;&#x2D;cursor</code> <code class="cli-value">&lt;TEXT&gt;</code> — Pagination cursor from a previous response.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.
