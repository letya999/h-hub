#!/bin/sh
set -eu
umask 077

stamp=$(date -u +%Y%m%dT%H%M%SZ)
dest="/backup/.$stamp"
mkdir "$dest"
mkdir "$dest/db" "$dest/conf" "$dest/sessions"
. /src/tools/backup/sessions.sh

source_hubctl() {
  docker run --rm --network hhub -v hhub-conf:/etc/hhub:ro hhub-hubctl:local "$@"
}

seed_session_probe() {
  probe="backup-probe-$stamp"
  registration=$(source_hubctl user add "$probe")
  set -- $registration
  if test "$#" -ne 3 || test "$1" != "$probe" || ! valid_session_space "$2"; then
    echo 'invalid backup probe registration' >&2
    exit 1
  fi
  answer=$(source_hubctl chat "$probe" "/start $3")
  case "$answer" in *"Account linked"*) ;; *) echo 'backup probe could not link' >&2; exit 1 ;; esac
  for marker in "one-$probe" "two-$probe"; do
    answer=$(source_hubctl chat "$probe" "remember marker $marker")
    case "$answer" in *"answer: remember marker $marker"*) ;; *) echo 'backup probe turn failed' >&2; exit 1 ;; esac
  done
  source_hubctl user runtime-drain "$probe" "$2" >/dev/null
  printf '%s %s\n' "$probe" "$2" > "$dest/session-probe"
}

: > "$dest/session-probe"
case "${1:-plain}" in
  plain) ;;
  with-session) seed_session_probe ;;
  *) echo 'backup mode must be plain or with-session' >&2; exit 1 ;;
esac
cp /run/hhub-secrets/seal-kek "$dest/seal-kek"
cp /run/hhub-secrets/credential_broker-db-key "$dest/broker-db-key"
cp -a /etc/hhub/. "$dest/conf/"
docker exec postgres pg_dumpall -U postgres --globals-only > "$dest/globals.sql"
docker exec postgres psql -U postgres -Atq -c \
  'SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname' > "$dest/databases"

while IFS= read -r db; do
  case "$db" in
    ''|*[!a-zA-Z0-9_]* ) echo "invalid database name: $db" >&2; exit 1 ;;
  esac
  docker exec postgres pg_dump -U postgres -Fc "$db" > "$dest/db/$db.dump"
done < "$dest/databases"

source_hubctl secret snapshot
docker cp credential-broker:/data/broker-snapshot.db "$dest/broker-snapshot.db"
source_hubctl secret list > "$dest/broker-connections.txt"

network=hhub-space
volumes=$(docker volume ls --format '{{.Name}}')
for volume in $volumes; do
  case "$volume" in "$network"-session-*-main) ;; *) continue ;; esac
  space=${volume#"$network"-session-}
  space=${space%-main}
  valid_session_space "$space"
  session_archive capture "$volume" > "$dest/sessions/$space.tar.gz"
  printf '%s\n' "$space"
done > "$dest/session-volumes"
if test -s "$dest/session-probe"; then
  read -r probe space < "$dest/session-probe"
  if ! grep -Fxq "$space" "$dest/session-volumes"; then
    echo "backup probe has no archived session volume: $space" >&2
    exit 1
  fi
fi
(
  cd "$dest"
  sha256sum db/*.dump seal-kek broker-db-key globals.sql broker-snapshot.db broker-connections.txt session-volumes session-probe
  while IFS= read -r space; do sha256sum "sessions/$space.tar.gz"; done < session-volumes
) > "$dest/SHA256SUMS"
published="/backup/$stamp"
test ! -e "$published"
mv "$dest" "$published"
printf 'backup: %s (%s databases, %s session volumes)\n' \
  "$published" "$(wc -l < "$published/databases")" "$(wc -l < "$published/session-volumes")"
