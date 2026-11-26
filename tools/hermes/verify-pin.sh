#!/bin/sh
set -eu

patch=$(find deploy services/harness-runtime tools/hermes -type f \( -name '*.patch' -o -name '*.diff' \) -print -quit)
if [ -n "$patch" ]; then
    echo "Undeclared upstream patch: $patch. An exception needs an upstream issue, PR and a build check on the pinned revision." >&2
    exit 1
fi

ref=$(cat "${1:-deploy/hermes-image.ref}")
tag=${ref%@*}
digest=${ref##*@}
if ! printf '%s\n' "$ref" | grep -Exq 'docker\.io/nousresearch/hermes-agent:v[0-9]+\.[0-9]+\.[0-9]+@sha256:[0-9a-f]{64}' ||
   [ "$(printf '%s\n' "$ref" | wc -l)" -ne 1 ]; then
    echo 'Hermes requires an official release tag and a sha256 image digest' >&2
    exit 1
fi

# Docker accepts tag@digest even when the digest belongs to a different tag.
receipt=$(docker pull "$tag")
if ! printf '%s\n' "$receipt" | grep -Fxq "Digest: $digest"; then
    echo "Hermes release $tag does not match $digest" >&2
    exit 1
fi
docker image inspect --format '{{.Id}}' "$ref"
observed=$(mktemp)
trap 'rm -f "$observed"' EXIT
sh tools/hermes/inventory.sh > "$observed"
cmp services/spawn-controller/internal/svc/hermes-inventory.yaml "$observed"
