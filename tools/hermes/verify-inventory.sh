#!/bin/sh
set -eu

declared=$1
observed=$2
sha=$(sed -n 's/^inventory_sha256: //p' "$observed")
reviewed=$(sed -n 's/^reviewed_inventory_sha256: //p' "$declared")
if test -z "$sha" || test "$reviewed" != "$sha"; then
  echo 'Pinned Hermes inventory needs a new effect review' >&2
  exit 1
fi
portable=$(mktemp)
trap 'rm -f "$portable"' EXIT
sed -E '
  /^reviewed_inventory_sha256: /d
  /^tools:$/,$ {
    s/^  - "/unclassified tool: "/
    s/^  - \{name: ("[^"]+"), effect: (nothing|workspace|network|process|external-service)\}$/  - \1/
  }' "$declared" > "$portable"
cmp "$portable" "$observed"
