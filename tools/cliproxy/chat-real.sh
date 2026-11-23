#!/bin/sh
# One manual real-LLM conversation through hubctl chat: links a fresh
# user, switches its space to the devin route, asks one question and
# prints the answer plus the recorded spend. Run on the host with the
# stack and cliproxy up: sh tools/cliproxy/chat-real.sh [user] [msg]
set -eu
export MSYS_NO_PATHCONV=1

# The host may be Git Bash on Windows: with path conversion off, the
# volume source needs the Windows form; /command args pass through.
src=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && (pwd -W 2>/dev/null || pwd))
data="$src/../h-hub-data"
user=${1:-manual}
msg=${2:-say hello in one short sentence}
hubctl() {
  docker run --rm --network hhub \
    -e 'HHUB_ADMIN_DSN=postgres://postgres@postgres:5432/postgres?sslmode=disable' \
    -e HHUB_CONFIG_DIR=/etc/hhub -e HHUB_DESCRIPTOR=/src/deploy/descriptor.yaml \
    -e HHUB_KEK_DIR=/run/hhub-secrets \
    -v hhub-conf:/etc/hhub -v "$src:/src:ro" \
    -v "$data/secrets:/run/hhub-secrets" \
    hhub-hubctl:local "$@"
}

registration=$(hubctl user add "$user")
set -- $registration
code=$3
echo "== link"
hubctl chat "$user" "/start $code"
echo "== route -> devin"
hubctl chat "$user" "/model devin"
echo "== question: $msg"
hubctl chat "$user" "$msg"
echo "== spend"
hubctl chat "$user" "/usage"
