#!/bin/sh
# Starts the pinned cliproxy upstream on the egress network, reachable
# only from model-gateway. Prerequisite: tools/cliproxy/init.sh and a
# completed tools/cliproxy/login.sh session.
set -eu
export MSYS_NO_PATHCONV=1

img=$(cat deploy/cliproxy-image.ref) || { echo "image pin unreadable" >&2; exit 1; }
case "$img" in
  *@sha256:????????????????????????????????????????????????????????????????) ;;
  *) echo "image pin malformed: expected name@sha256:<64 hex>" >&2; exit 1 ;;
esac

docker rm -f cliproxy >/dev/null 2>&1 || true
docker run -d --name cliproxy --restart unless-stopped \
  --network hhub-egress \
  -v hhub_cliproxy-auth:/root/.cli-proxy-api \
  "$img" ./CLIProxyAPI --config /root/.cli-proxy-api/config.yaml >/dev/null
echo "cliproxy up on hhub-egress (devin route upstream)"
