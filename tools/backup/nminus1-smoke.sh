#!/bin/sh
set -eu

: "${GHCR_TOKEN:?GHCR_TOKEN is required to pull the published image}"
image=$(cat /src/deploy/nminus1-image.ref)
printf '%s' "$GHCR_TOKEN" | docker login ghcr.io -u letya999 --password-stdin >/dev/null
docker pull "$image" >/dev/null
RESTORE_ISSUER_IMAGE="$image" REQUIRE_IDENTITY_MIGRATION=6 \
  sh /src/tools/backup/restore-drill.sh
