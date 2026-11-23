#!/bin/sh
# OAuth sign-in for the Devin account behind the "devin" route. The
# session files land in the cliproxy-auth volume; nothing else sees
# them. Run interactively on the host: sh tools/cliproxy/login.sh
set -eu
export MSYS_NO_PATHCONV=1

img=${CLIPROXY_IMAGE:-docker.io/eceasy/cli-proxy-api:v8.1.1@sha256:e2f9c436ff992ef5830227ccc9fb67d40246bb1420708fbcd18e1de675e88c98}

exec docker run --rm -it --name cliproxy-login \
  --network hhub-egress \
  -p 1455:1455 \
  -v hhub_cliproxy-auth:/root/.cli-proxy-api \
  "$img" ./CLIProxyAPI --config /root/.cli-proxy-api/config.yaml --devin-login --no-browser --oauth-callback-port 1455
