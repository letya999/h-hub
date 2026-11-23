#!/bin/sh
# Starts the pinned cliproxy upstream on the egress network, reachable
# only from model-gateway. Prerequisite: tools/cliproxy/init.sh and a
# completed tools/cliproxy/login.sh session.
set -eu
export MSYS_NO_PATHCONV=1

img=${CLIPROXY_IMAGE:-docker.io/eceasy/cli-proxy-api:v8.1.1@sha256:e2f9c436ff992ef5830227ccc9fb67d40246bb1420708fbcd18e1de675e88c98}

docker rm -f cliproxy >/dev/null 2>&1 || true
docker run -d --name cliproxy --restart unless-stopped \
  --network hhub-egress \
  -v hhub_cliproxy-auth:/root/.cli-proxy-api \
  "$img" ./CLIProxyAPI --config /root/.cli-proxy-api/config.yaml >/dev/null
echo "cliproxy up on hhub-egress (devin route upstream)"
