#!/bin/sh
set -eu

start=$(date +%s)
failed=0
reports=$(mktemp -d)
trap 'rm -rf "$reports"' EXIT

run_group() {
  name=$1
  shift
  began=$(date +%s)
  status=0
  go test -tags hhube2e -count=1 -parallel 2 -timeout 10m "$@" || status=$?
  printf 'e2e %s: %ss\n' "$name" "$(($(date +%s) - began))" >&2
  return "$status"
}

# All other cases may cold-start Hermes. The two filters cover the same
# packages without dropping or repeating cases.
control='^Test(ForgedRunTokenRefused|OwnerApprovalOutcomes|OwnerRequestSelfAnswer|OwnerAnswerSingleUse|OwnerRequestExpiry'
control="$control|ForgedToken|ServiceEdge|MissingConfigRefused|HermesSessionArchiveIncludesCommittedWAL|OperatorAlert|OrphanRuntimeRemoved|OrphanWorkerRemoved|WorkerClaimAndDestroy|WorkerStopQuarantine"
control="$control|PassportNarrowRefusal|PassportConfirmStaleRefused|PassportRollbackRestoresDigest"
control="$control|StoreMissingDatabaseRefusesStartup|StaleSpawnTickRefusesHealth|SweepLagOperatorAlert|CellinitBootInDocker)$"
run_group control "$@" -run "$control" \
  ./test/e2e/channel ./test/e2e/lifecycle ./services/spawn-controller/internal/backend > "$reports/control" &
control_pid=$!
run_group hermes "$@" -skip "$control" \
  ./test/e2e/channel ./test/e2e/lifecycle ./test/e2e/capability > "$reports/hermes" &
hermes_pid=$!

wait "$control_pid" || failed=1
wait "$hermes_pid" || failed=1
cat "$reports/control" "$reports/hermes"
if [ "$failed" -ne 0 ]; then
  awk '/^SwapTotal:/' /proc/meminfo >&2
  docker events --since "$start" --until "$(date +%s)" --filter label=hhub.managed=true \
    --filter event=oom --format 'Hermes container {{.Actor.ID}} reached its memory limit' >&2
  for cid in $(docker ps -aq --filter label=hhub.managed=true --filter status=exited); do
    docker logs --since "$start" --tail 32 "$cid" 2>&1 | awk '/"msg":"fatal","err":"start:/' >&2
  done
fi
# This package restarts the shared stack.
run_group disruption "$@" ./test/e2e/disrupt || failed=1
elapsed=$(($(date +%s) - start))
printf 'mandatory e2e: %ss\n' "$elapsed" >&2
if [ "$elapsed" -gt 600 ]; then
  echo 'mandatory e2e exceeded 10 minutes' >&2
  exit 1
fi
exit "$failed"
