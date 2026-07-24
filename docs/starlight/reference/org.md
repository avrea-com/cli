---
title: avr org
description: "Manage organizations and installations."
---

Manage organizations and installations.

```sh
avr org [OPTIONS] COMMAND [ARGS]...
```

## Subcommands

### `avr org create`

Create a new organization.

```sh
avr org create [OPTIONS] NAME
```

```sh
JSON FIELDS
    name, organization_id, role, slug
```

**Arguments**

- <code class="cli-arg">NAME</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr org email-domain`

Claim and verify organization email domains.

```sh
avr org email-domain [OPTIONS] COMMAND [ARGS]...
```

#### `avr org email-domain claim`

Claim a company domain using DNS ownership verification (admin only).

```sh
avr org email-domain claim [OPTIONS] DOMAIN
```

The domain does not need to match your GitHub or Avrea account email.
Publish the returned TXT record, then run ``email-domain verify``.

```sh
Examples:
    avr org email-domain claim example.com
    avr org email-domain claim corp.example.com --org acme
```

```sh
JSON FIELDS
    created_at, dns_record_name, dns_record_value, domain,
    organization_email_domain_id, verified, verified_at
```

**Arguments**

- <code class="cli-arg">DOMAIN</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr org email-domain list`

List claimed organization email domains (admin only).

```sh
avr org email-domain list [OPTIONS]
```

```sh
JSON FIELDS
    created_at, dns_record_name, dns_record_value, domain,
    organization_email_domain_id, verified, verified_at
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr org email-domain set`

Set email domains for automatic org membership (admin only).

```sh
avr org email-domain set [OPTIONS] DOMAINS...
```

Replaces all existing domains — a typo wipes the org's auto-membership
policy. Confirms before applying; pass --yes to skip the prompt (required
when stdout isn't a TTY, e.g. in CI).

```sh
Examples:
    avr org email-domain set example.com
    avr org email-domain set example.com corp.example.com --yes
```

**Arguments**

- <code class="cli-arg">DOMAINS...</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip confirmation prompt.

#### `avr org email-domain verify`

Check a claimed domain's DNS TXT record (admin only).

```sh
avr org email-domain verify [OPTIONS] DOMAIN
```

Each invocation performs a fresh DNS lookup. If DNS has not propagated,
wait and run the command again.

```sh
Examples:
    avr org email-domain verify example.com
    avr org email-domain verify corp.example.com --org acme
```

```sh
JSON FIELDS
    created_at, dns_record_name, dns_record_value, domain,
    organization_email_domain_id, verified, verified_at
```

**Arguments**

- <code class="cli-arg">DOMAIN</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr org install`

Manage GitHub App installations.

```sh
avr org install [OPTIONS] COMMAND [ARGS]...
```

#### `avr org install add`

Start the GitHub App installation flow.

```sh
avr org install add [OPTIONS]
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;no-browser</code> — Do not open browser automatically.
- <code class="cli-flag">&#x2D;&#x2D;wait-seconds</code> <code class="cli-value">&lt;INTEGER&gt;</code> — Seconds to wait for detection. _(default: `120`)_

#### `avr org install list`

List accessible installations across all your organizations.

```sh
avr org install list [OPTIONS]
```

```sh
JSON FIELDS
    created_at, platform_installation_id, installation_id, organization_name,
    organization_slug, state, target_name
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr org install remove`

Remove/suspend a GitHub installation.

```sh
avr org install remove [OPTIONS]
```

Confirms before suspending; pass --yes to skip the prompt (required when
stdout isn't a TTY, e.g. in CI).

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;installation-id</code> <code class="cli-value">&lt;TEXT&gt;</code> — Installation ID to remove (ins-xxx format) _(required)_
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip confirmation prompt.

### `avr org list`

List organizations you belong to.

```sh
avr org list [OPTIONS]
```

```sh
Examples:
    avr org list
    avr org list --json slug,role
    avr org list --json '*' -q '.[] | select(.role == "admin")'
```

```sh
JSON FIELDS
    name, organization_id, role, slug
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr org members`

List organization members (admin only).

```sh
avr org members [OPTIONS]
```

```sh
Examples:
    avr org members
    avr org members --org org-abc123
    avr org members --json name,role
```

```sh
JSON FIELDS
    joined_at, name, role, user_id
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

### `avr org saml`

Configure SAML single sign-on for an organization.

```sh
avr org saml [OPTIONS] COMMAND [ARGS]...
```

#### `avr org saml configure`

Create or replace SAML configuration from IdP metadata (admin only).

```sh
avr org saml configure [OPTIONS] METADATA
```

METADATA is an IdP metadata XML file; pass - to read it from stdin.
Reconfiguring requires the complete metadata document again.

```sh
Examples:
    avr org saml configure idp-metadata.xml
    cat idp-metadata.xml | avr org saml configure - --org acme
    avr org saml configure idp.xml --email-attribute mail \
        --given-name-attribute firstName --family-name-attribute lastName
```

```sh
JSON FIELDS
    allow_idp_initiated, attr_email, attr_family_name, attr_given_name,
    attr_groups, created_at, default_role, idp_entity_id, idp_slo_url,
    idp_sso_url, is_enforced, jit_provisioning, name_id_format,
    organization_id, organization_saml_config_id, updated_at
```

**Arguments**

- <code class="cli-arg">METADATA</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;email-attribute, &#x2D;&#x2D;attr-email</code> <code class="cli-value">&lt;TEXT&gt;</code> — IdP attribute carrying the member email. _(default: `email`)_
- <code class="cli-flag">&#x2D;&#x2D;given-name-attribute, &#x2D;&#x2D;attr-given-name</code> <code class="cli-value">&lt;TEXT&gt;</code> — IdP given-name attribute.
- <code class="cli-flag">&#x2D;&#x2D;family-name-attribute, &#x2D;&#x2D;attr-family-name</code> <code class="cli-value">&lt;TEXT&gt;</code> — IdP family-name attribute.
- <code class="cli-flag">&#x2D;&#x2D;groups-attribute, &#x2D;&#x2D;attr-groups</code> <code class="cli-value">&lt;TEXT&gt;</code> — IdP groups attribute.
- <code class="cli-flag">&#x2D;&#x2D;default-role</code> <code class="cli-value">&lt;CHOICE&gt;</code> — Role assigned to JIT-provisioned members. _(choices: `user`, `admin`, `billing_admin` · default: `user`)_
- <code class="cli-flag">&#x2D;&#x2D;jit-provisioning / &#x2D;&#x2D;no-jit-provisioning</code> — Allow SAML to provision new members. _(default: `True`)_
- <code class="cli-flag">&#x2D;&#x2D;allow-idp-initiated / &#x2D;&#x2D;no-allow-idp-initiated</code> — Allow sign-in initiated from the IdP.
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr org saml enforcement`

Enable or disable mandatory SAML sign-in (admin only).

```sh
avr org saml enforcement [OPTIONS] {on|off}
```

Enabling requires a configured SAML connection and at least one verified
company domain.

```sh
Examples:
    avr org saml enforcement on
    avr org saml enforcement off --org acme
```

**Arguments**

- <code class="cli-arg">STATE</code> _(choices: `on`, `off`)_

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr org saml remove`

Remove the organization's SAML configuration (admin only).

```sh
avr org saml remove [OPTIONS]
```

Pass --yes to skip the confirmation prompt (required when prompts are
disabled for automation).

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;yes, -y</code> — Skip confirmation prompt.

#### `avr org saml show`

Show the current SAML configuration (admin only).

```sh
avr org saml show [OPTIONS]
```

```sh
JSON FIELDS
    allow_idp_initiated, attr_email, attr_family_name, attr_given_name,
    attr_groups, created_at, default_role, idp_entity_id, idp_slo_url,
    idp_sso_url, is_enforced, jit_provisioning, name_id_format,
    organization_id, organization_saml_config_id, updated_at
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;json</code> <code class="cli-value">&lt;TEXT&gt;</code> — Output JSON. Pass comma-separated field names, "*" for all fields, or "?" to list available fields.
- <code class="cli-flag">-q, &#x2D;&#x2D;jq</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter --json output through a jq expression.

#### `avr org saml sp-metadata`

Print Avrea's SAML service-provider metadata XML.

```sh
avr org saml sp-metadata [OPTIONS]
```

Redirect stdout to a file for import into your identity provider.

```sh
Examples:
    avr org saml sp-metadata > avrea-sp.xml
    avr org saml sp-metadata --org acme
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).

#### `avr org saml test`

Test the SAML connection in a browser (admin only).

```sh
avr org saml test [OPTIONS]
```

The test performs a real IdP sign-in and displays the parsed assertion
without creating a new Avrea session.

```sh
Examples:
    avr org saml test
    avr org saml test --org acme --no-browser
```

**Options**

- <code class="cli-flag">&#x2D;&#x2D;org</code> <code class="cli-value">&lt;TEXT&gt;</code> — Organization ID or slug. Uses default org if not specified (see: avr config set org).
- <code class="cli-flag">&#x2D;&#x2D;no-browser</code> — Print the test URL without opening a browser.
