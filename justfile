# just check: same gate set locally and in CI (rule 23).
# On Windows this file only launches the pinned hhub-dev container
# (pwsh, never Git Bash); inside Linux it runs the gates directly.
set windows-shell := ["pwsh.exe", "-NoProfile", "-Command"]

img      := "hhub-dev@" + `cat deploy/dev-image.ref`
src      := justfile_directory()
run      := "docker run --rm -v " + src + ":/src:ro -v hhub-gocache:/go -v hhub-cache:/root/.cache "
runnet   := "docker run --rm --network hhub -e HHUB_TEST_DSN=" + dsn + \
            " -v " + src + ":/src:ro -v hhub-gocache:/go -v hhub-cache:/root/.cache "
confvol  := "hhub-conf"
dsn      := "postgres://postgres@postgres:5432/postgres?sslmode=disable"
hubctl   := "docker run --rm --network hhub -e HHUB_ADMIN_DSN=" + dsn + \
            " -e HHUB_CONFIG_DIR=/etc/hhub -e HHUB_DESCRIPTOR=/src/deploy/descriptor.yaml" + \
            " -v " + confvol + ":/etc/hhub -v " + src + ":/src:ro hhub-hubctl:local"

check_line := if os() == "windows" { "docker compose -f deploy/compose.yaml up -d --wait postgres && " + runnet + img + " just check" } else { "just _check" }
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
    go vet -tags hhubdb ./...
    go test -tags hhubdb ./...
    gitleaks git .
    go run ./tools/descriptor check

_check-fast:
    golangci-lint run ./...
    go test $(go list ./... | grep -v /tools/proof)
    gitleaks protect --staged

# --- stack operations (host side, docker commands work in both shells) ---

# build every service/runtime/test image the stack needs
images:
    docker build -f deploy/hubctl.Dockerfile -t hhub-hubctl:local .
    docker build -f deploy/svc.Dockerfile --build-arg SVC=identity-hub -t hhub-identity-hub:local .
    docker build -f deploy/svc.Dockerfile --build-arg SVC=task-engine -t hhub-task-engine:local .
    docker build -f deploy/svc.Dockerfile --build-arg SVC=spawn-controller -t hhub-spawn-controller:local .
    docker build -f deploy/svc.Dockerfile --build-arg SVC=harness-runtime -t hhub-harness-runtime:local .
    docker build -f test/scripted-model/Dockerfile -t hhub-scripted-model:local .

# bring the whole phase-3 stack up: postgres -> init -> services ->
# scripted model. Idempotent.
stack: images
    docker compose -f deploy/compose.yaml up -d --wait postgres
    just init
    docker compose -f deploy/compose.yaml up -d --wait
    docker rm -f scripted-model 2>/dev/null || true
    docker run -d --name scripted-model --network hhub --restart unless-stopped hhub-scripted-model:local

# run the acceptance suite against the live stack (needs just stack)
e2e:
    docker run --rm --network hhub \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e HHUB_TASK_URL=http://task-engine:9000 \
      -e HHUB_SPAWN_URL=http://spawn-controller:9000 \
      -e HHUB_ADMIN_DSN={{ dsn }} \
      -v {{ src }}:/src:ro -v hhub-gocache:/go -v hhub-cache:/root/.cache \
      {{ img }} go test -tags hhube2e -count=1 -timeout 10m ./test/e2e

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
