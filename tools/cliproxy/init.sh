#!/bin/sh
# Generates the cliproxy client key and its config.yaml. The key is
# created inside the throwaway container and written straight into the
# two volumes that hold it: the gateway's route keys and cliproxy's
# auth directory. It never appears in argv, env, inspect or a dump.
set -eu

docker run --rm --entrypoint sh \
  -v hhub_cliproxy-auth:/auth \
  -v hhub_keys-model-gateway:/k \
  hhub-hubctl:local -c '
set -eu
key=$(head -c 24 /dev/urandom | od -An -tx1 | tr -d " \n")
mkdir -p /k/routes /auth
printf %s "$key" > /k/routes/cliproxy.key
chmod 600 /k/routes/cliproxy.key
{
  printf "config-version: 8\n"
  printf "server:\n  host: \"\"\n  port: 8317\n"
  printf "access:\n  api-keys:\n    - \"%s\"\n" "$key"
  printf "oauth:\n  auth-dir: \"/root/.cli-proxy-api\"\n"
} > /auth/config.yaml
chmod 600 /auth/config.yaml
'
echo "cliproxy key + config materialized into volumes"
