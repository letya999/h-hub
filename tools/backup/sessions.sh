# Run the same SQLite library as the pinned Hermes runtime.
session_archive() (
  mode=$1
  volume=$2
  access=rw
  if test "$mode" = capture; then access=ro; fi
  docker run --rm -i --network none --read-only --cap-drop ALL \
    --security-opt no-new-privileges --user 65534:65534 -v /tmp \
    -v "$volume:/var/tmp:$access" --entrypoint /opt/hermes/.venv/bin/python \
    "$(cat /src/deploy/hermes-image.ref)" -c "$(cat /src/tools/backup/session-archive.py)" "$mode"
)

valid_session_space() {
  case "$1" in
    *[!a-zA-Z0-9-]* | space-) return 1 ;;
    space-*) return 0 ;;
    *) return 1 ;;
  esac
}
