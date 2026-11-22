# just check: same gate set locally and in CI (rule 23).
# On Windows this file only launches the pinned hhub-dev container
# (pwsh, never Git Bash); inside Linux it runs the gates directly.
set windows-shell := ["pwsh.exe", "-NoProfile", "-Command"]

img      := "hhub-dev@" + `cat deploy/dev-image.ref`
src      := justfile_directory()
run      := "docker run --rm -v " + src + ":/src:ro -v hhub-gocache:/go -v hhub-cache:/root/.cache "
confvol  := "hhub-conf"
dsn      := "postgres://postgres@postgres:5432/postgres?sslmode=disable"
hubctl   := "docker run --rm --network hhub -e HHUB_ADMIN_DSN=" + dsn + \
            " -e HHUB_CONFIG_DIR=/etc/hhub -e HHUB_DESCRIPTOR=/src/deploy/descriptor.yaml" + \
            " -v " + confvol + ":/etc/hhub -v " + src + ":/src:ro -v hhub-gocache:/go " + \
            img + " /go/bin/hubctl"

check_line := if os() == "windows" { run + img + " just check" } else { "just _check" }
fast_line  := if os() == "windows" { run + img + " just check-fast" } else { "just _check-fast" }

# every gate, same locally and in CI
check:
    {{ check_line }}

# fast part for pre-commit
check-fast:
    {{ fast_line }}

# build the pinned dev image and record its digest
dev-image:
    docker build -f deploy/dev.Dockerfile -t hhub-dev:build .
    docker images -q --no-trunc hhub-dev:build > deploy/dev-image.ref
    docker tag hhub-dev:build hhub-dev:0.0.1

# --- inner recipes, executed inside the container (Linux) ---

_check:
    go build ./...
    golangci-lint run ./...
    go test ./...
    gitleaks git .
    go run ./tools/descriptor check

_check-fast:
    golangci-lint run ./...
    go test $(go list ./... | grep -v /tools/proof)
    gitleaks protect --staged

# --- stack operations (host side, docker commands work in both shells) ---

# start postgres from the generated compose
up:
    docker compose -f deploy/compose.yaml up -d

down:
    docker compose -f deploy/compose.yaml down

# build hubctl into the go cache volume
hubctl-build:
    {{ run }}{{ img }} go build -o /go/bin/hubctl ./cmd/hubctl

init:
    {{ hubctl }} init

doctor:
    {{ hubctl }} doctor

# docker disk budget of the dev machine (tools/, not a second gate set)
devenv:
    {{ run }}-v /var/run/docker.sock:/var/run/docker.sock {{ img }} go run ./tools/devenv
