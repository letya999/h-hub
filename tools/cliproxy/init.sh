#!/bin/sh
# Generates the cliproxy client key, its config.yaml and the broker
# object. The key is created inside the throwaway container, written
# into cliproxy's auth config and staged once on the host secrets dir
# just long enough for `hubctl secret set` to move it into the
# credential-broker — then the staging file is shredded. It never
# appears in argv, env, inspect or a dump.
set -eu
export MSYS_NO_PATHCONV=1

secrets="$(pwd)/../h-hub-data/secrets"
mkdir -p "$secrets"
staging="$secrets/cliproxy.key"
trap 'rm -f "$staging"' EXIT

docker run --rm --entrypoint sh \
  -v hhub_cliproxy-auth:/auth \
  -v "$secrets":/stage \
  hhub-hubctl:local -c '
set -eu
key=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d " \n")
mkdir -p /auth /stage
printf %s "$key" > /stage/cliproxy.key
chmod 600 /stage/cliproxy.key
{
  printf "config-version: 8\n"
  printf "server:\n  host: \"\"\n  port: 8317\n"
  printf "access:\n  api-keys:\n    - \"%s\"\n" "$key"
  printf "oauth:\n  auth-dir: \"/root/.cli-proxy-api\"\n"
} > /auth/config.yaml
chmod 600 /auth/config.yaml
'

just secret-set cliproxy-key --from-file /run/hhub-secrets/cliproxy.key
rm -f "$staging"
echo "cliproxy key in credential-broker; config materialized into volume"
