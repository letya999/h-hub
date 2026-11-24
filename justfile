# just check: same gate set locally and in CI (rule 23).
# On Windows this file only launches the pinned hhub-dev container
# (pwsh, never Git Bash); inside Linux it runs the gates directly.
set windows-shell := ["pwsh.exe", "-NoProfile", "-Command"]

img      := "hhub-dev@" + `cat deploy/dev-image.ref`
sqlciph  := `cat deploy/sqlcipher-base.ref`
src      := justfile_directory()
data     := src + "/../h-hub-data"
run      := "docker run --rm -v " + src + ":/src:ro -v hhub-gocache:/go -v hhub-cache:/root/.cache "
runnet   := "docker run --rm --network hhub -e HHUB_TEST_DSN=" + dsn + \
            " -v " + src + ":/src:ro -v hhub-gocache:/go -v hhub-cache:/root/.cache "
confvol  := "hhub-conf"
dsn      := "postgres://postgres@postgres:5432/postgres?sslmode=disable"
hubctl   := "docker run --rm -i --network hhub -e HHUB_ADMIN_DSN=" + dsn + \
            " -e HHUB_CONFIG_DIR=/etc/hhub -e HHUB_DESCRIPTOR=/src/deploy/descriptor.yaml -e HHUB_KEK_DIR=/run/hhub-secrets" + \
            " -v " + confvol + ":/etc/hhub -v " + src + ":/src:ro -v " + data + "/secrets:/run/hhub-secrets hhub-hubctl:local"

checkrun := "docker run --rm --network hhub -v /var/run/docker.sock:/var/run/docker.sock " + \
            "-e HHUB_TEST_DSN=" + dsn + " -e HHUB_ADMIN_DSN=" + dsn + \
            " -e HHUB_TASK_URL=http://task-engine:9000 -e HHUB_SPAWN_URL=http://spawn-controller:9000" + \
            " -e HHUB_IDENTITY_URL=http://identity-hub:9000 -e HHUB_MODEL_URL=http://model-gateway:9000" + \
            " -e HHUB_BROKER_URL=http://credential-broker:9000" + \
            " -v hhub-conf:/etc/hhub:ro -v " + data + "/secrets:/run/hhub-secrets:ro" + \
            " -v " + src + ":/src:ro -v hhub-gocache:/go -v hhub-cache:/root/.cache "
check_line := if os() == "windows" { "just stack && " + checkrun + img + " just _check" } else { "just _check" }
fast_line  := if os() == "windows" { run + img + " just check-fast" } else { "just _check-fast" }

# every gate, same locally and in CI
check:
    {{ check_line }}

# fast part for pre-commit
check-fast:
    {{ fast_line }}

# build the pinned dev image and record its digest
dev-image:
    docker build -f deploy/dev.Dockerfile --build-arg SQLCIPHER_BASE={{ sqlciph }} -t hhub-dev:build .
    docker images -q --no-trunc hhub-dev:build > deploy/dev-image.ref
    docker tag hhub-dev:build hhub-dev:0.0.1

# --- inner recipes, executed inside the container (Linux) ---

_check:
    go build -tags libsqlite3 ./...
    golangci-lint run --build-tags libsqlite3 ./...
    go vet -tags "hhubdb libsqlite3" ./...
    sh /src/tools/tracecheck/run.sh
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
    docker build -f deploy/svc.Dockerfile --build-arg SVC=channel-hub -t hhub-channel-hub:local .
    docker build -f deploy/svc.Dockerfile --build-arg SVC=task-engine -t hhub-task-engine:local .
    docker build -f deploy/svc.Dockerfile --build-arg SVC=spawn-controller -t hhub-spawn-controller:local .
    docker build -f deploy/svc.Dockerfile --build-arg SVC=harness-runtime -t hhub-harness-runtime:local .
    docker build -f deploy/svc.Dockerfile --build-arg SVC=model-gateway -t hhub-model-gateway:local .
    docker build -f deploy/broker.Dockerfile --build-arg SQLCIPHER_BASE={{ sqlciph }} -t hhub-credential-broker:local .
    docker build -f test/scripted-model/Dockerfile -t hhub-scripted-model:local .

# bring the whole phase-3 stack up: postgres -> init -> services ->
# scripted model. Idempotent.
stack: images
    docker compose -f deploy/compose.yaml up -d --wait postgres
    just init
    docker compose -f deploy/compose.yaml up -d --wait
    -docker rm -f scripted-model
    docker run -d --name scripted-model --network hhub --restart unless-stopped hhub-scripted-model:local

# run the acceptance suite against the live stack (needs just stack)
e2e:
    docker run --rm --network hhub \
      -v /var/run/docker.sock:/var/run/docker.sock \
      -e HHUB_TASK_URL=http://task-engine:9000 \
      -e HHUB_SPAWN_URL=http://spawn-controller:9000 \
      -e HHUB_IDENTITY_URL=http://identity-hub:9000 \
      -e HHUB_MODEL_URL=http://model-gateway:9000 \
      -e HHUB_BROKER_URL=http://credential-broker:9000 \
      -e HHUB_ADMIN_DSN={{ dsn }} \
      -v hhub-conf:/etc/hhub:ro \
      -v {{ data }}/secrets:/run/hhub-secrets:ro \
      -v {{ src }}:/src:ro -v hhub-gocache:/go -v hhub-cache:/root/.cache \
      {{ img }} sh -c 'go test -tags hhube2e -count=1 -timeout 10m \
        ./test/e2e/channel ./test/e2e/lifecycle && \
        go test -tags hhube2e -count=1 -timeout 10m ./test/e2e/disrupt'

# optional three-user load measurement, separate from mandatory e2e
load:
    docker run --rm --network hhub \
      -e HHUB_TASK_URL=http://task-engine:9000 \
      -e HHUB_SPAWN_URL=http://spawn-controller:9000 \
      -e HHUB_IDENTITY_URL=http://identity-hub:9000 \
      -e HHUB_ADMIN_DSN={{ dsn }} \
      -v hhub-conf:/etc/hhub:ro \
      -v {{ src }}:/src:ro -v hhub-gocache:/go -v hhub-cache:/root/.cache \
      {{ img }} go test -tags hhube2e,hhubload -run '^TestThreeUserLoad$' -v -count=1 -timeout 10m ./test/e2e/channel

# capture all databases, installation config and host KEK outside git
backup:
    docker run --rm --network hhub -v /var/run/docker.sock:/var/run/docker.sock \
      -v {{ src }}:/src:ro -v {{ data }}/backups:/backup \
      -v {{ data }}/secrets:/run/hhub-secrets:ro -v hhub-conf:/etc/hhub:ro \
      -v hhub-gocache:/go -v hhub-cache:/root/.cache \
      {{ img }} sh /src/tools/backup/backup.sh

# restore the latest backup into fresh, isolated Docker resources
restore-drill:
    docker run --rm --network hhub -v /var/run/docker.sock:/var/run/docker.sock \
      -v {{ src }}:/src:ro -v {{ data }}/backups:/backup \
      -v hhub-gocache:/go -v hhub-cache:/root/.cache \
      {{ img }} sh /src/tools/backup/restore-drill.sh

# live-install upgrade: drain runtimes, activate new images, smoke,
# roll back to the previous pins on failure
upgrade:
    sh tools/upgrade/upgrade.sh

# --- real provider path (manual only; checks and CI never reach it) ---

# materialize the cliproxy client key + config into the two volumes
cliproxy-init:
    sh tools/cliproxy/init.sh

# OAuth sign-in for the Devin account (interactive, on the host)
models-login:
    sh tools/cliproxy/login.sh

# start the pinned cliproxy upstream on the egress-only network
cliproxy-up:
    sh tools/cliproxy/up.sh

# one real conversation through hubctl chat on the devin route
chat-real user='manual' msg='say hello in one short sentence':
    sh tools/cliproxy/chat-real.sh {{ user }} "{{ msg }}"

# locally smoke the frozen 0.0.4 identity image on the restored 0.0.5 backup
nminus1-smoke:
    docker run --rm -v /var/run/docker.sock:/var/run/docker.sock \
      -e GHCR_TOKEN \
      -v {{ src }}:/src:ro -v {{ data }}/backups:/backup \
      -v hhub-gocache:/go -v hhub-cache:/root/.cache \
      {{ img }} sh /src/tools/backup/nminus1-smoke.sh

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

# write an installation secret into the broker; value on stdin or --from-file
secret-set name *args:
    {{ hubctl }} secret set {{ name }} {{ args }}

# docker disk budget of the dev machine (tools/, not a second gate set)
devenv:
    {{ run }}-v /var/run/docker.sock:/var/run/docker.sock {{ img }} go run ./tools/devenv
