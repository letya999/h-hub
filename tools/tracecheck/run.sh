#!/bin/sh
set -eu

report=$(mktemp)
trap 'rm -f "$report"' EXIT

if ! go test -tags "hhubdb libsqlite3" -json -count=1 ./... > "$report"; then
  cat "$report"
  exit 1
fi
if ! go test -tags hhube2e -json -count=1 -timeout 10m \
  ./test/e2e/channel ./test/e2e/lifecycle >> "$report"; then
  cat "$report"
  exit 1
fi
# The disrupt package restarts the shared stack — it runs alone
# after the parallel pair so no in-flight test is hit.
if ! go test -tags hhube2e -json -count=1 -timeout 10m \
  ./test/e2e/disrupt >> "$report"; then
  cat "$report"
  exit 1
fi
go run ./tools/tracecheck "$report"
echo 'unit, database, e2e and executed trace proofs passed'
