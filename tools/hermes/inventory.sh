#!/bin/sh
set -eu
ref=$(cat deploy/hermes-image.ref)
docker run --rm -i --network none --read-only --cap-drop ALL --user 65534:65534 \
    --tmpfs /tmp:rw,nosuid,nodev,size=32m --tmpfs /opt/data:rw,nosuid,nodev,uid=65534,gid=65534,mode=0700 \
    -e HERMES_HOME=/opt/data -e PYTHONDONTWRITEBYTECODE=1 \
    --entrypoint /opt/hermes/.venv/bin/python "$ref" - "$ref" < tools/hermes/inventory.py
