#!/bin/sh
set -eu

dir=$(ls -dt /backup/*/ | head -n 1)
test -n "$dir"
(cd "$dir" && sha256sum -c SHA256SUMS >/dev/null)
id="$(date -u +%s)-$$"
net="hhub-restore-$id"
pg="hhub-restore-pg-$id"
issuer="hhub-restore-issuer-$id"
holder="hhub-restore-keys-$id"
keyvol="hhub-restore-keys-$id"
cleanup() {
  docker rm -f "$issuer" "$holder" "$pg" >/dev/null 2>&1 || true
  docker volume rm "$keyvol" >/dev/null 2>&1 || true
  docker network rm "$net" >/dev/null 2>&1 || true
}
trap cleanup EXIT

pg_image=$(docker inspect -f '{{.Config.Image}}' postgres)
issuer_image=${RESTORE_ISSUER_IMAGE:-$(docker inspect -f '{{.Config.Image}}' identity-hub)}
docker network create "$net" >/dev/null
docker run -d --rm --name "$pg" --network "$net" --network-alias postgres \
  -e POSTGRES_HOST_AUTH_METHOD=trust "$pg_image" >/dev/null
ready=0
for n in $(seq 1 30); do
  if docker exec "$pg" pg_isready -U postgres >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
test "$ready" -eq 1

while IFS= read -r db; do
  case "$db" in
    ''|*[!a-zA-Z0-9_]* ) echo "invalid database name: $db" >&2; exit 1 ;;
  esac
  if test "$db" != postgres; then
    docker exec "$pg" psql -U postgres -v ON_ERROR_STOP=1 -c "CREATE ROLE $db LOGIN" >/dev/null
    docker exec "$pg" createdb -U postgres -O "$db" "$db"
    role="$db"
  else
    role=postgres
  fi
  docker exec -i "$pg" pg_restore -U postgres --role="$role" --no-owner --no-acl -d "$db" \
    < "$dir/db/$db.dump" >/dev/null
done < "$dir/databases"

go run ./cmd/keydist --conf "$dir/conf" --keys "$dir/keys" --descriptor /src/deploy/descriptor.yaml
docker volume create "$keyvol" >/dev/null
docker create --name "$holder" -v "$keyvol:/etc/hhub" --entrypoint /bin/true "$issuer_image" >/dev/null
docker cp "$dir/keys/keys-identity-hub/." "$holder:/etc/hhub/"
docker rm "$holder" >/dev/null
docker create --name "$issuer" --network "$net" --network-alias identity-hub \
  -e HHUB_SUB=identity-hub -e 'HHUB_DSN=postgres://identity_hub@postgres:5432/identity_hub?sslmode=disable' \
  -e HHUB_KEYS=/etc/hhub -e HHUB_KEK_PATH=/tmp/seal-kek \
  -v "$keyvol:/etc/hhub:ro" "$issuer_image" >/dev/null
docker cp "$dir/seal-kek" "$issuer:/tmp/seal-kek"
docker start "$issuer" >/dev/null
ready=0
for n in $(seq 1 30); do
  if docker run --rm --network "$net" --entrypoint /bin/sh "$issuer_image" \
      -c 'wget -qO- http://identity-hub:9000/healthz' 2>/dev/null | grep -q '"ok"'; then
    ready=1
    break
  fi
  sleep 1
done
if test "$ready" -ne 1; then docker logs "$issuer" >&2; exit 1; fi
printf 'restore drill: %s databases restored; identity-hub started with backed-up KEK\n' "$(wc -l < "$dir/databases")"
