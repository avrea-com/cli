---
title: avr secret
description: "Manage organization and repository secrets."
---

Manage organization and repository secrets.

```sh
avr secret [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr secret delete`

Delete one scoped value, or the whole name with --all.

```sh
avr secret delete [OPTIONS] NAME
```

**Arguments**

- <code class="cli-arg">NAME</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;all</code> — Delete the name and all of its scoped values.
- <code class="cli-flag">&#x2D;&#x2D;jq, -q</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter JSON with an embedded jq expression.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output comma-separated fields, * for all, or ? to list fields.
- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug; does not broaden availability.
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository full name or ID; defaults to the current checkout.
- <code class="cli-flag">&#x2D;&#x2D;variant</code> <code class="cli-value">&lt;TEXT&gt;</code> — Select an existing scoped value by label.
- <code class="cli-flag">&#x2D;&#x2D;variant-id</code> <code class="cli-value">&lt;TEXT&gt;</code> — Select an existing scoped value by ID.
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip confirmation.

### `avr secret edit`

Edit description, label, or availability without replacing a value.

```sh
avr secret edit [OPTIONS] NAME
```

**Arguments**

- <code class="cli-arg">NAME</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;branch</code> <code class="cli-value">&lt;TEXT&gt;</code> — Branch pattern; repeat for alternatives. _(default: `()` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;clear-branch</code> — Remove branch restrictions.
- <code class="cli-flag">&#x2D;&#x2D;clear-env</code> — Remove environment restrictions.
- <code class="cli-flag">&#x2D;&#x2D;clear-label</code> — Clear the scoped value label.
- <code class="cli-flag">&#x2D;&#x2D;clear-workflow</code> — Remove workflow restrictions.
- <code class="cli-flag">&#x2D;&#x2D;description</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization-wide description shared by all scoped values; an empty string clears it.
- <code class="cli-flag">&#x2D;&#x2D;env, -e</code> <code class="cli-value">&lt;TEXT&gt;</code> — Environment pattern; repeat for alternatives. _(default: `()` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;jq, -q</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter JSON with an embedded jq expression.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output comma-separated fields, * for all, or ? to list fields.
- <code class="cli-flag">&#x2D;&#x2D;label</code> <code class="cli-value">&lt;TEXT&gt;</code> — Label for the scoped value.
- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug; does not broaden availability.
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository full name or ID; defaults to the current checkout.
- <code class="cli-flag">&#x2D;&#x2D;repos</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization availability: repository names or IDs, comma-separated. _(default: `()` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;variant</code> <code class="cli-value">&lt;TEXT&gt;</code> — Select an existing scoped value by label.
- <code class="cli-flag">&#x2D;&#x2D;variant-id</code> <code class="cli-value">&lt;TEXT&gt;</code> — Select an existing scoped value by ID.
- <code class="cli-flag">&#x2D;&#x2D;visibility</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization availability: all, private, or selected (requires --repos).
- <code class="cli-flag">&#x2D;&#x2D;workflow</code> <code class="cli-value">&lt;TEXT&gt;</code> — Workflow path pattern; repeat for alternatives. _(default: `()` · repeatable)_

### `avr secret get`

Inspect a secret.

```sh
avr secret get [OPTIONS] NAME
```

**Arguments**

- <code class="cli-arg">NAME</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;jq, -q</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter JSON with an embedded jq expression.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output comma-separated fields, * for all, or ? to list fields.
- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug; does not broaden availability.
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository full name or ID; defaults to the current checkout.
- <code class="cli-flag">&#x2D;&#x2D;variant</code> <code class="cli-value">&lt;TEXT&gt;</code> — Select an existing scoped value by label.
- <code class="cli-flag">&#x2D;&#x2D;variant-id</code> <code class="cli-value">&lt;TEXT&gt;</code> — Select an existing scoped value by ID.
- <code class="cli-flag">&#x2D;&#x2D;version-id</code> <code class="cli-value">&lt;TEXT&gt;</code> — Inspect a specific submitted version without resubmitting it.

### `avr secret list`

List names, scoped-value counts, and update times.

```sh
avr secret list [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;jq, -q</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter JSON with an embedded jq expression.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output comma-separated fields, * for all, or ? to list fields.
- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug; does not broaden availability.
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository full name or ID; defaults to the current checkout.

### `avr secret set`

Create or replace a scoped value.

```sh
avr secret set [OPTIONS] [NAME]
```

**Arguments**

- <code class="cli-arg">[NAME]</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;branch</code> <code class="cli-value">&lt;TEXT&gt;</code> — Branch pattern; repeat for alternatives. _(default: `()` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;description</code> <code class="cli-value">&lt;TEXT&gt;</code> — Description for a newly created name; use edit to change an existing description.
- <code class="cli-flag">&#x2D;&#x2D;env, -e</code> <code class="cli-value">&lt;TEXT&gt;</code> — Environment pattern; repeat for alternatives. _(default: `()` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;env-file, -f</code> <code class="cli-value">&lt;TEXT&gt;</code> — Read literal NAME=value entries from a dotenv file; no expansion.
- <code class="cli-flag">&#x2D;&#x2D;jq, -q</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter JSON with an embedded jq expression.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output comma-separated fields, * for all, or ? to list fields.
- <code class="cli-flag">&#x2D;&#x2D;label</code> <code class="cli-value">&lt;TEXT&gt;</code> — Label for the scoped value.
- <code class="cli-flag">&#x2D;&#x2D;org, -o</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug; does not broaden availability.
- <code class="cli-flag">&#x2D;&#x2D;repo, -R</code> <code class="cli-value">&lt;TEXT&gt;</code> — Repository full name or ID; defaults to the current checkout.
- <code class="cli-flag">&#x2D;&#x2D;repos</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization availability: repository names or IDs, comma-separated. _(default: `()` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;type</code> <code class="cli-value">&lt;TEXT&gt;</code> — Secret type: env_var or file; chosen at creation. _(default: `env_var`)_
- <code class="cli-flag">&#x2D;&#x2D;variant</code> <code class="cli-value">&lt;TEXT&gt;</code> — Select an existing scoped value by label.
- <code class="cli-flag">&#x2D;&#x2D;variant-id</code> <code class="cli-value">&lt;TEXT&gt;</code> — Select an existing scoped value by ID.
- <code class="cli-flag">&#x2D;&#x2D;visibility</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization availability: all, private, or selected (requires --repos).
- <code class="cli-flag">&#x2D;&#x2D;wait</code> <code class="cli-value">&lt;TEXT&gt;</code> — Wait for write confirmation. With 0s, return the receipt immediately; unconfirmed writes exit nonzero. _(default: `30s`)_
- <code class="cli-flag">&#x2D;&#x2D;workflow</code> <code class="cli-value">&lt;TEXT&gt;</code> — Workflow path pattern; repeat for alternatives. _(default: `()` · repeatable)_
