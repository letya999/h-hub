#!/bin/sh
set -eu
umask 077

stamp=$(date -u +%Y%m%dT%H%M%SZ)
dest="/backup/$stamp"
mkdir "$dest"
mkdir "$dest/db" "$dest/conf"
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

docker run --rm --network hhub -v hhub-conf:/etc/hhub:ro \
  hhub-hubctl:local secret snapshot
docker cp credential-broker:/data/broker-snapshot.db "$dest/broker-snapshot.db"
docker run --rm --network hhub -v hhub-conf:/etc/hhub:ro \
  hhub-hubctl:local secret list > "$dest/broker-connections.txt"

(cd "$dest" && sha256sum db/*.dump seal-kek broker-db-key globals.sql broker-snapshot.db broker-connections.txt > SHA256SUMS)
printf 'backup: %s (%s databases)\n' "$dest" "$(wc -l < "$dest/databases")"
