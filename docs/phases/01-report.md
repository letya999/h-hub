# Phase 1 report — repository and gates (v0.0.1)

## Implemented

- Repo layout per brief: `cmd/hubctl{,/install}`, `shared/store/admin`,
  `internal/descriptor`, `tools/{archtest,descriptor,gates,meta,proof,devenv}`,
  `deploy`, `adr`, `docs/phases`, `testdata/broken`; `services/` and
  `contracts/` are empty `.gitkeep` placeholders.
- `deploy/dev.Dockerfile` builds `hhub-dev` (golang:1.27-alpine pinned by
  digest; golangci-lint v2.15.0, gitleaks v8.30.1, just 1.59.0 with sha256
  verification of every download). Recorded image id in
  `deploy/dev-image.ref`; recipes reference `hhub-dev@sha256:...`.
- `just check` on Windows (pwsh, `windows-shell`) only runs the container
  with the read-only `/src` mount and named cache volumes
  (`hhub-gocache`, `hhub-cache`); inside Linux it runs `_check`.
- `.golangci.yml` with all enforcement thresholds (see below), depguard
  rules (one binary/db/contract owner per layer, hubctl thin client,
  install-only Postgres access, banned v0 module) and forbidigo rules
  (`os.Getenv` outside install config, `exec.Command`, default http
  helpers, `time.Sleep`, `t.Skip*`, `panic`, `fmt.Print*` outside main).
- `deploy/descriptor.yaml` is the single service inventory:
  schema-validated (zone, digest-pinned image, mandatory mem/cpu/pids
  ceilings, env <= 10 HHUB_* vars, volumes, edges); `tools/descriptor`
  renders `deploy/compose.yaml`; `just check` asserts byte-exact
  regeneration.
- `shared/store/admin` + `cmd/hubctl/install` + `cmd/hubctl`: `init`
  provisions install-id, KEK, Ed25519 service/operator keys, db
  passwords, watchdog_ro + per-service roles/databases/fsm schema with
  SELECT-only watchdog grants, and `hub.install_id`; `doctor` is
  read-only and checks all of it.
- `tools/gates`: json-state, banned-words, windows-paths, msys-dirs,
  env-declared, docs (AGENTS length, test links, README template),
  trace (req: id -> adr/cap/sys), miglint (no down, destructive ->
  contract-of tag, state columns only in fsm), module-ban, blob-size,
  fix-needs-test, cron-alert.
- `tools/archtest`: AST ceilings (20 methods/type, 20 fields/struct,
  8000 prod lines/service).
- `tools/meta`: meta-test asserting every required linter is enabled and
  every threshold matches enforcement.
- `tools/proof`: runs external tools (golangci-lint, gitleaks) on their
  broken fixtures and asserts failure.
- `.github/workflows/ci.yml`: one job `check` = same `just check` in the
  same container; CODEOWNERS on adr/, gate configs, descriptor, tools;
  grouped dependabot; `.gitattributes` eol=lf; `.githooks/pre-commit` =
  `just check-fast`.

## Gate thresholds

funlen 60/40 lines/statements; gocognit 15; cyclop 12 func / 5 pkg-avg;
nestif 4; maintidx < 20; dupl 100; interfacebloat 5; revive arg-limit 5,
result-limit 3, file-length 500 (comments/blanks excluded), control
nesting 4; file 500 lines; type 20 methods; struct 20 fields; service
8000 prod lines; env <= 10/service; AGENTS.md <= 150 lines; git blob
< 256 KiB; docker build cache budget 10 GiB (tools/devenv).

## just check timing

Two clean runs on the Windows workstation (Docker Desktop, WSL2):
**18.6 s** and **20.8 s** — far under the 5-minute signal threshold.
Pre-commit `check-fast` ~10 s.

## Proven by broken examples

- gates (tools/gates): 12 gates x 15 fixtures under `testdata/broken/`
  (json-state, banned-words, windows-paths, msys-dirs `x;C`,
  env-declared, docs x2, trace unknown id, miglint-drop, miglint-state,
  module-ban, blob-size, fix-needs-test, cron-alert) — `TestBroken`.
- descriptor: `descriptor-noceil` (service without ceilings) and
  `descriptor-env11` (11 env entries) — `internal/descriptor TestBroken`.
- archtest: `archtest-methods` (21 methods), `archtest-fields` —
  `tools/archtest TestBroken`.
- golangci fixture module: 501-line file, 61-line funlen, 6-param
  revive, `os.Getenv`/`exec.Command`/`time.Sleep`/`t.Skip` forbidigo —
  `tools/proof TestGolangciBroken`.
- depguard banned import — `TestDepguardBroken`.
- gitleaks planted key — `TestGitleaksBroken`; repo history clean —
  `TestGitleaksRepo`.
- meta-test asserts every linter/threshold above is really configured.

## hubctl init / doctor evidence

- `just up` -> compose postgres healthy; `just hubctl-build` -> binary.
- `just init` twice: same install id `dd69c78ca1197ff1af1c65bc98dc94f6`,
  11 services provisioned; second run created nothing new.
- `just doctor`: 40 checks, all `ok`.
- Deleted `keys/operator.key` -> doctor FAIL `config files`; `init`
  regenerated the key (new pair, others untouched) -> doctor green.
- `DROP OWNED` + `DROP ROLE watchdog_ro` -> doctor FAIL on the role and
  all 8 grant checks; `init` recreated role + grants -> doctor green.
- Tampered `install-id` file -> doctor FAIL install-id mismatch on all 8
  databases; restored -> green.

## Did not fit / findings

- CI workflow is written but unproven: there is no remote yet, so the
  `check` job has never run on GitHub. First push will show whether the
  cache/host paths need adjustment.
- Stack-dependent checks (`hubctl init/doctor`) are verified here but
  are not part of `just check`, which must stay offline-fast. A smoke
  recipe could join up+init+doctor later; owner decision needed on
  whether it belongs in check or stays a manual smoke.
- gitleaks: the AWS documentation example key (`AKIA...EXAMPLE`) is in
  gitleaks' own allowlist — the fixture uses a non-documented key so the
  proof is real. `testdata/broken/` is allowlisted in `.gitleaks.toml`.
- Forbidden-word and windows-path gates are text scans and needed two
  heuristics: char classes inside the rule regex itself, and
  uppercase-drive + non-word-boundary for `VAR:/mount` and `%s:\n`
  printf formats. A lowercase `c:\` path literal would slip through —
  acceptable for a heuristic gate, noted for the record.
- `POSTGRES_HOST_AUTH_METHOD: trust` in the generated compose is a
  dev-only convenience for the single-host stack; revisit when
  credentials are wired (blueprint secret zone).
- Descriptor ceilings (mem/cpu/pids) are placeholder values pending
  first measured builds — the file itself notes this.
- Depguard per-service rules (`svc-*`) exist in `.golangci.yml` for the
  future service tree; `services/` is empty in phase 1.

## Owner questions

1. Should `init`/`doctor` smoke (up + init + doctor) join `just check`
   or stay a manual `just` recipe? Currently check is fully offline.
2. Dev compose trusts local postgres; confirm phase-1 scope is allowed
   to keep `trust` auth until credential-broker exists.
3. `hhub-conf` is a named volume for install state; acceptable for the
   single-host model, or should install state be a host bind-mount path
   from the start?

## Redesign-signal measurements (phase 1 baseline)

- `just check` local: ~19-21 s (threshold 5 min) — green.
- Mandatory e2e: none yet (threshold 10 min) — n/a.
- One change touching > 3 services: no services exist — n/a.
- Same service-pair contract changed twice in a row: no contracts — n/a.
- Service at 70% line budget in introduction phase: 0 lines — n/a.
- Contract scaffolding share of phase lines: contracts/ empty — n/a.
- MCP reference set: phase 17 — n/a.
