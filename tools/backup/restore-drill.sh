#!/bin/sh
set -eu

dir=$(ls -dt /backup/*/ | head -n 1)
test -n "$dir"
(cd "$dir" && sha256sum -c SHA256SUMS >/dev/null)
id="$(date -u +%s)-$$"
net="hhub-restore-$id"
pg="hhub-restore-pg-$id"
issuer="hhub-restore-issuer-$id"
task="hhub-restore-task-$id"
channel="hhub-restore-channel-$id"
spawn="hhub-restore-spawn-$id"
render="hhub-restore-render-$id"
cleanup() {
  docker rm -f "$issuer" "$task" "$channel" "$spawn" "$pg" >/dev/null 2>&1 || true
  for service in identity-hub task-engine channel-hub spawn-controller; do
    docker rm -f "hhub-restore-keys-$service-$id" >/dev/null 2>&1 || true
    docker volume rm "hhub-restore-keys-$service-$id" >/dev/null 2>&1 || true
  done
  docker volume rm "$render" >/dev/null 2>&1 || true
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

if test -n "${REQUIRE_IDENTITY_MIGRATION:-}"; then
  version=$(docker exec "$pg" psql -U postgres -d identity_hub -tAc \
    'SELECT max(num) FROM schema_migrations')
  test "$version" = "$REQUIRE_IDENTITY_MIGRATION"
fi

go run ./cmd/keydist --conf "$dir/conf" --keys "$dir/keys" --descriptor /src/deploy/descriptor.yaml
for service in identity-hub task-engine channel-hub spawn-controller; do
  vol="hhub-restore-keys-$service-$id"
  holder="$vol"
  image="hhub-$service:local"
  if test "$service" = identity-hub; then image="$issuer_image"; fi
  docker volume create "$vol" >/dev/null
  docker create --name "$holder" -v "$vol:/etc/hhub" --entrypoint /bin/true "$image" >/dev/null
  docker cp "$dir/keys/keys-$service/." "$holder:/etc/hhub/"
  docker rm "$holder" >/dev/null
done
docker volume create "$render" >/dev/null
docker create --name "$issuer" --network "$net" --network-alias identity-hub \
  -e HHUB_SUB=identity-hub -e 'HHUB_DSN=postgres://identity_hub@postgres:5432/identity_hub?sslmode=disable' \
  -e HHUB_KEYS=/etc/hhub -e HHUB_KEK_PATH=/tmp/seal-kek \
  -v "hhub-restore-keys-identity-hub-$id:/etc/hhub:ro" "$issuer_image" >/dev/null
docker cp "$dir/seal-kek" "$issuer:/tmp/seal-kek"
docker start "$issuer" >/dev/null
docker run -d --rm --name "$task" --network "$net" --network-alias task-engine \
  -e HHUB_SUB=task-engine -e 'HHUB_DSN=postgres://task_engine@postgres:5432/task_engine?sslmode=disable' \
  -e HHUB_KEYS=/etc/hhub -e HHUB_IDENTITY_URL=http://identity-hub:9000 \
  -e HHUB_SPAWN_URL=http://spawn-controller:9000 \
  -e HHUB_CHANNEL_URL=http://channel-hub:9000 \
  -v "hhub-restore-keys-task-engine-$id:/etc/hhub:ro" hhub-task-engine:local >/dev/null
docker run -d --rm --name "$channel" --network "$net" --network-alias channel-hub \
  -e HHUB_SUB=channel-hub -e 'HHUB_DSN=postgres://channel_hub@postgres:5432/channel_hub?sslmode=disable' \
  -e HHUB_KEYS=/etc/hhub -e HHUB_IDENTITY_URL=http://identity-hub:9000 \
  -e HHUB_TASK_URL=http://task-engine:9000 \
  -v "hhub-restore-keys-channel-hub-$id:/etc/hhub:ro" hhub-channel-hub:local >/dev/null
# The drill must not control live host containers before cutover.
docker run -d --rm --name "$spawn" --network "$net" --network-alias spawn-controller \
  -e HHUB_SUB=spawn-controller -e 'HHUB_DSN=postgres://spawn_controller@postgres:5432/spawn_controller?sslmode=disable' \
  -e HHUB_KEYS=/etc/hhub -e HHUB_RUNTIME_IMAGE=hhub-harness-runtime:local \
  -e HHUB_DOCKER_SOCK=/tmp/disabled.sock -e HHUB_TASK_URL=http://task-engine:9000 \
  -e HHUB_MODEL_URL=http://scripted-model:9000 -e "HHUB_RENDER_VOL=$render" \
  -e HHUB_RENDER_DIR=/render -e "HHUB_NETWORK=$net" \
  -v "hhub-restore-keys-spawn-controller-$id:/etc/hhub:ro" -v "$render:/render" \
  hhub-spawn-controller:local >/dev/null
for pair in "identity-hub:$issuer" "task-engine:$task" "channel-hub:$channel" "spawn-controller:$spawn"; do
  host=${pair%%:*}
  container=${pair#*:}
  ready=0
  for n in $(seq 1 30); do
    if docker run --rm --network "$net" --entrypoint /bin/sh "$issuer_image" \
        -c "wget -qO- http://$host:9000/healthz" 2>/dev/null | grep -q '"ok"'; then
      ready=1
      break
    fi
    sleep 1
  done
  if test "$ready" -ne 1; then docker logs "$container" >&2; exit 1; fi
done
printf 'restore drill: %s databases restored; four services healthy with backed-up KEK\n' "$(wc -l < "$dir/databases")"
