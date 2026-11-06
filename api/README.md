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
The schema is best-effort inspection input. Builds, tests and packaging do not
need it, and it does not generate runtime code.
