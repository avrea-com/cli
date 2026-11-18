---
title: avr api
description: "Call an Avrea API endpoint."
---

Call an Avrea API endpoint.

```sh
avr api [OPTIONS] <endpoint>
```

Use GET by default, or POST when supplying fields or input. With GET, HEAD, or --input, fields become query parameters.

```sh
Examples:
  avr api users/me/organizations
  avr api /orgs/acme/workflow-runs -X GET -F limit=5
  avr api /logs/search --input search.json
  avr api users/me/organizations -i
```

**Arguments**

- <code class="cli-arg">ENDPOINT</code>

**Options**

- <code class="cli-flag">&#x2D;&#x2D;field, -F</code> <code class="cli-value">&lt;TEXT&gt;</code> — Add a typed field (key=value, @file or @-). _(default: `()` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;header, -H</code> <code class="cli-value">&lt;TEXT&gt;</code> — Set a request header (name: value). _(default: `()` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;include, -i</code> — Print response status and headers.
- <code class="cli-flag">&#x2D;&#x2D;input</code> <code class="cli-value">&lt;TEXT&gt;</code> — Read the body from a file or - for stdin.
- <code class="cli-flag">&#x2D;&#x2D;jq, -q</code> <code class="cli-value">&lt;TEXT&gt;</code> — Filter the JSON response.
- <code class="cli-flag">&#x2D;&#x2D;method, -X</code> <code class="cli-value">&lt;TEXT&gt;</code> — HTTP request method.
- <code class="cli-flag">&#x2D;&#x2D;raw-field, -f</code> <code class="cli-value">&lt;TEXT&gt;</code> — Add a literal string field (key=value). _(default: `()` · repeatable)_
- <code class="cli-flag">&#x2D;&#x2D;silent</code> — Suppress the response body.
