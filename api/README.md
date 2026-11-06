# API responses and schema inspection

The CLI decodes API responses into maps using `json.Decoder.UseNumber`.
Commands share those values for terminal rendering and `--json`/gojq output.
There is no generated model package or second typed copy of each response.

The client checks response envelopes and important field types in
`client/records.go`. Commands retain semantic checks where they act on a value:
resource selection, SSH endpoints/host keys, native dispatch receipts, download
integrity and user input. Unknown fields and opaque settings/summary payloads
remain untouched. These focused checks are not a complete OpenAPI validator.
Request maps explicitly distinguish omitted keys, null, empty values and false.

OpenAPI remains useful for inspecting or implementing API changes. Until the
public API serves its schema, fetch it from the local core checkout:

```sh
make go-fetch-schema AVREA_CORE=/path/to/avrea-core
```

This reads `origin/main:control/openapi/public.json` without modifying or fetching
the core checkout. To select a revision:

```sh
uv run python scripts/fetch_go_schema.py --core /path/to/avrea-core --core-ref HEAD
```

Once the API serves the schema:

```sh
make go-fetch-schema OPENAPI_URL=https://<public-api-host>/<schema-path>
```

The fetcher also accepts `AVR_OPENAPI_URL`. It validates the document and saves it
to ignored `build/go/openapi.json`, preserving the previous file on failure.
The schema is best-effort inspection input and does not generate runtime code.

## Request compatibility check

```sh
make go-api-check AVREA_CORE=/path/to/avrea-core
# Once the public schema endpoint exists:
make go-api-check OPENAPI_URL=https://<public-api-host>/<schema-path>
```

This fetches the current schema and checks real command-generated requests in
`commands/api_contract_test.go`. A mock transport supplies the PR feature flag
and rejects target requests, including writes; it never contacts an Avrea
environment. Cases assert that the intended endpoint is reached, then compare
the route, method, query names,
required queries, primitive types/bounds and enum values against OpenAPI.

Coverage is representative, not exhaustive: auth, organizations, repositories,
pull-request scopes, runs, jobs, cache usage/purge and VMs. Add cases as request
shapes change. Bodies, responses, path-value formats, streaming and full JSON
Schema validation remain outside this check; native tests cover behavior.
Some server routes parse queries outside OpenAPI, so these need separate tests.

Run this check before release and after API-facing changes. Ordinary CI runs
the checker's drift-detection tests but skips the external-schema cases until
given `AVR_TEST_OPENAPI`. It does not require access to private core, a versioned
API, or a committed schema. Normal builds, tests and packaging do not need it.
