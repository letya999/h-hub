#!/bin/sh
set -eu

report=$(mktemp)
trap 'rm -f "$report"' EXIT

if ! go test -tags "hhubdb libsqlite3" -json -count=1 ./... > "$report"; then
  cat "$report"
  exit 1
fi
if ! sh /src/tools/tracecheck/e2e.sh -json >> "$report"; then
  cat "$report"
  exit 1
fi
go run ./tools/tracecheck "$report"
echo 'unit, database, e2e and executed trace proofs passed'
