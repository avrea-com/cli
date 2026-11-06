#!/bin/sh
# Run inside a disposable Alpine container with the checkout mounted read-only.
set -eu
apk add --no-cache python3 curl ca-certificates
python3 -m unittest scripts.test_go_distribution -v
set -- "$1"/avr_*_linux_*.tar.gz
[ "$#" = 1 ] && [ -f "$1" ]
mkdir /tmp/avr-alpine
# The artifact was verified on its native runner before this smoke test.
tar -xzf "$1" -C /tmp/avr-alpine --strip-components=1
/tmp/avr-alpine/avr --version
/tmp/avr-alpine/avr repo --help
printf '{"status":"ok"}\n' > /tmp/avr-alpine/health
python3 -m http.server 8080 --bind 127.0.0.1 --directory /tmp/avr-alpine &
server=$!
trap 'kill "$server"; wait "$server" || true' EXIT
# Wait for the local fixture without depending on an external API's availability.
for attempt in 1 2 3 4 5; do
  if curl --fail --silent http://127.0.0.1:8080/health >/dev/null; then break; fi
  sleep 1
done
result=$(AVR_HOST=http://localhost:8080 /tmp/avr-alpine/avr health --json status --jq .status)
[ "$result" = ok ]
