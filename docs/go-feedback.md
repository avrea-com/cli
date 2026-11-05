# Feedback in the Go CLI

Send feedback or report a bug after signing in with `avr auth login`:

```sh
avr feedback "Please support Lambda Cloud"
avr feedback --bug "logs -f stops after an hour"
avr feedback "Cache restores are slow" --org my-company
avr feedback --bug "Run logs are missing" --json '*'
avr feedback status fdb-019a4770720070008000000000000001
```

Messages may contain up to 10,000 Unicode characters after trimming surrounding
whitespace. Quote the message as one argument. Use `--` before a message beginning
with a dash or the literal word `status`.

Organization attribution is optional. `--org` accepts a slug or organization ID;
omitting it sends personal feedback even if you have a default organization.
Your account supplies contact details, and the CLI supplies its version and
platform in its user agent. Scoped tokens cannot use these endpoints.

The command prints an idempotency key to stderr before sending. If its response is
lost, unreadable, or the server asks you to retry, repeat the same message, bug
flag and organization with that key:

```sh
avr feedback "Please support Lambda Cloud" \
  --idempotency-key 019a4770720070008000000000000003
```

Reusing a key with different content returns a conflict. Resend the original
content with that key, or omit `--idempotency-key` for a new submission. Correct
authentication, permission, and validation errors before submitting again.
Each fresh invocation without `--idempotency-key` creates a new key. On a rate
limit, wait for the retry delay shown in the error and reuse the same key.

The response reports acceptance and the current delivery status. `queued` and
`sending` mean delivery is pending, `delivered` confirms delivery, `failed` means
delivery failed, and `needs_attention` requires the Avrea team to check the
outcome. `status` reads your own submissions and does not wait or resend.

Both commands support `--json FIELDS` and `--jq EXPR`. Use `--json '*'` for all
fields or `--json '?'` to discover them. Submission output also exposes the
`idempotency_key`; diagnostics stay on stderr. Feedback intake must be enabled
on the server before submissions are accepted.
