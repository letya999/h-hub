#!/bin/sh
set -eu

tmp=$(mktemp -d /tmp/hhub-nminus1.XXXXXX)
git archive v0.0.4 | tar -x -C "$tmp"
docker build -f "$tmp/deploy/svc.Dockerfile" --build-arg SVC=identity-hub \
  -t hhub-identity-hub:nminus1 "$tmp" >/dev/null
RESTORE_ISSUER_IMAGE=hhub-identity-hub:nminus1 sh /src/tools/backup/restore-drill.sh
