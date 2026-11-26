#!/bin/sh
set -eu

start=$(date +%s)
failed=0
# Each parallel case may cold-start several real Hermes processes.
if ! go test -tags hhube2e -count=1 -parallel 2 -timeout 10m "$@" \
  ./test/e2e/channel ./test/e2e/lifecycle ./test/e2e/capability; then
  failed=1
  awk '/^SwapTotal:/' /proc/meminfo >&2
  docker events --since "$start" --until "$(date +%s)" --filter label=hhub.managed=true \
    --filter event=oom --format 'Hermes container {{.Actor.ID}} reached its memory limit' >&2
  for cid in $(docker ps -aq --filter label=hhub.managed=true --filter status=exited); do
    docker logs --since "$start" --tail 32 "$cid" 2>&1 | awk '/"msg":"fatal","err":"start:/' >&2
  done
fi
# This package restarts the shared stack.
go test -tags hhube2e -count=1 -timeout 10m "$@" ./test/e2e/disrupt || failed=1
elapsed=$(($(date +%s) - start))
printf 'mandatory e2e: %ss\n' "$elapsed" >&2
if [ "$elapsed" -gt 600 ]; then
  echo 'mandatory e2e exceeded 10 minutes' >&2
  exit 1
fi
exit "$failed"
