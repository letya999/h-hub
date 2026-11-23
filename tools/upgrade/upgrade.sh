#!/bin/sh
# Live-installation upgrade: drain active runtimes, activate the new
# service images (compose up --wait), smoke the stack, and roll back to
# the recorded previous pins on any failure. Services migrate their own
# schemas forward on boot; rollback is a redeploy of the saved pins.
set -eu
export MSYS_NO_PATHCONV=1

services="identity-hub task-engine channel-hub spawn-controller model-gateway credential-broker"
prev="$(mktemp)"
trap 'rm -f "$prev"' EXIT
for s in $services; do
  printf '%s %s\n' "$s" "$(docker inspect -f '{{.Image}}' "$s" 2>/dev/null || echo missing)"
done >"$prev"

# drain: every space runtime goes down before the swap
for row in $(docker exec postgres psql -U postgres -d spawn_controller -At \
  -c "SELECT owner||':'||id FROM spaces"); do
  o=${row%%:*}; sp=${row#*:}
  docker run --rm --network hhub -v hhub-conf:/etc/hhub:ro \
    hhub-hubctl:local user runtime-drain "$o" "$sp" >/dev/null || true
done

docker compose -f deploy/compose.yaml up -d --wait

devimg="hhub-dev@$(cat deploy/dev-image.ref)"
probe="upgrade-probe-$$"
hctl() {
  docker run --rm --network hhub -v hhub-conf:/etc/hhub:ro \
    hhub-hubctl:local "$@"
}
smoke() {
  for s in $services; do
    docker run --rm --network hhub "$devimg" \
      wget -qO- "http://$s:9000/healthz" \
      | grep -q '"ok"' || return 1
  done
  row="$(hctl user add "$probe")" || return 1
  code="$(printf '%s' "$row" | awk '{print $3}')"
  test -n "$code" || return 1
  hctl chat "$probe" "/start $code" >/dev/null || return 1
  hctl chat "$probe" 'upgrade smoke' | grep -q 'answer: upgrade smoke'
}

if smoke; then
  echo "upgrade: new images active, smoke green"
  exit 0
fi

echo "upgrade: smoke failed; rolling back to previous pins" >&2
while read -r s img; do
  test "$img" = missing && continue
  docker tag "$img" "hhub-$s:local" || continue
done <"$prev"
docker compose -f deploy/compose.yaml up -d --wait
echo "upgrade: rolled back" >&2
exit 1
