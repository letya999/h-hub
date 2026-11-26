#!/bin/sh
set -eu

start=$(date +%s)
failed=0
# Each parallel case may cold-start several real Hermes processes.
go test -tags hhube2e -count=1 -parallel 2 -timeout 10m "$@" \
  ./test/e2e/channel ./test/e2e/lifecycle ./test/e2e/capability || failed=1
# This package restarts the shared stack.
go test -tags hhube2e -count=1 -timeout 10m "$@" ./test/e2e/disrupt || failed=1
elapsed=$(($(date +%s) - start))
printf 'mandatory e2e: %ss\n' "$elapsed" >&2
if [ "$elapsed" -gt 600 ]; then
  echo 'mandatory e2e exceeded 10 minutes' >&2
  exit 1
fi
exit "$failed"
