#!/bin/sh
# OAuth sign-in for the Devin account behind the "devin" route. The
# session files land in the cliproxy-auth volume; nothing else sees
# them. Run interactively on the host: sh tools/cliproxy/login.sh
set -eu
export MSYS_NO_PATHCONV=1

img=$(cat deploy/cliproxy-image.ref) || { echo "image pin unreadable" >&2; exit 1; }

exec docker run --rm -it --name cliproxy-login \
  --network hhub-egress \
  -p 1455:1455 \
  -v hhub_cliproxy-auth:/root/.cli-proxy-api \
  "$img" ./CLIProxyAPI --config /root/.cli-proxy-api/config.yaml --devin-login --no-browser --oauth-callback-port 1455
