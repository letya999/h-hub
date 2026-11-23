wait_postgres() {
  for n in $(seq 1 30); do
    if docker exec "$1" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  return 1
}
