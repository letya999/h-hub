# Phase 2 report — documents and shared (v0.0.2)

## Implemented

- Documents: root `README.md` (purpose, architecture, layout, checks,
  workflow), `docs/glossary.md`, templates
  `docs/templates/service-README.md` (enforced headings) and
  `docs/templates/adr.md`.
- ADRs: `adr/2026-11-22-store.md`, `adr/2026-11-22-fsm.md`,
  `adr/2026-11-22-secrets-at-rest.md` — each declares full
  `adr.<slug>.<invariant>` identifiers; the trace gate resolves them.
- `shared/errors`: stable codes, retryability, message key + args,
  wrapping that keeps cause/class, HTTP status mapping.
- `shared/locale`: ru/en catalogs, key parity test, interpolation,
  missing key echoes the key.
- `shared/clock`: injectable `Clock`, fake with `Advance`.
- `shared/log`: structured JSON lines, correlation ids
  (tenant/run/job/request), secret redaction.
- `shared/seal`: AES-256-GCM, 32-byte KEK from file, nonce+ciphertext,
  AAD = purpose+kid; wrong purpose/kid/key fails; no plaintext leak.
- `shared/auth`: Ed25519 tokens `hhub_<purpose>.<payload>.<sig>`,
  signature over `purpose.payload`; parse/verify, malformed/forged
  rejected. Authorization policy stays service-owned.
- `shared/store`: pgx/v5 stdlib `database/sql` open; `migrate.go`
  applies `NNNN_name.sql` files: gap-free numbering from 1, sha256
  recorded, advisory lock inside the migration transaction, tampered or
  newer-than-files databases rejected.
- `shared/store/admin` ported lib/pq -> pgx/v5 (single driver decision);
  local identifier/literal quoting replaces `pq.Quote*`.
- `shared/fsm`: spec validation (initial/edges/final/long-lived,
  deadline required on non-final non-long-lived states); `Create`;
  `Transition` = optimistic CAS on (kind,id,state,version), `ErrStale`
  on lost race; state+history+outbox+hooks in one transaction;
  `DueRows`/`SweepRow`/`Sweep` re-read candidates and skip moved-on
  rows; sweeper `Lease` as atomic upsert; `EnsureSchema` under advisory
  lock (safe under parallel tests).
- `shared/httpapi`: `Handle` (auth check, bounded body, unknown fields
  rejected, shared error envelope), `Client.Call` (timeout, bearer
  token, envelope decode), in-process `Copy` parity, `Normalize`
  converts foreign errors to the shared form.
- `shared/lifecycle`: run group, cancellation propagation, HTTP drain.
- `shared/proctree`: process-group kill, orphan detection.
- `shared/testkit`: `Req` trace annotations, `DSN` from
  `HHUB_TEST_DSN`, `TempSchema` scratch schemas, goroutine leak check.
- Trace gate extended: scans `_test.go` and YAML for `req:`,
  `requires:`, `testkit.Req(...)`; `sys.*` resolves against the fixed
  catalog, `adr.<slug>.<invariant>` against invariants declared in ADR
  files, `cap.*` stays invalid until the capability catalog exists.
- `just check` (Windows -> pwsh -> container) now brings postgres up
  first and runs `go vet`/`go test -tags hhubdb` inside the `hhub`
  network; CI does the same.

## Gate thresholds

Unchanged from phase 1 (see 01-report). Adjustments:
depguard `shared-store` rule now allows `github.com/jackc/pgx/v5`
instead of `github.com/lib/pq`; forbidigo `os.Getenv` exception added
for `shared/testkit` (test config, same class as install config).

## just check timing

Windows workstation, full check incl. `docker compose up --wait
postgres` and the `hhubdb` test tag: **28.4 s** and **46.7 s** —
far under the 5-minute signal threshold.

## Proven by broken examples

- New fixture `testdata/broken/trace-invariant`: test references
  `adr.demo.missing-invariant` while the ADR declares only
  `adr.demo.real-invariant` -> gate fails (`TestBroken`).
- All phase-1 fixtures still pass; existing fixtures gofmt'ed in place
  (gate semantics unchanged).

## Shared evidence

- `go test -tags hhubdb ./...` green: 21 packages, DB tests in
  `shared/store` (migrations over temp schemas) and `shared/fsm`
  (transition writes all, forbidden writes nothing, rollback leaves
  nothing, CAS race, deadline sweep, sweep skips moved-on, concurrent
  transitions one wins, lease contention).
- `golangci-lint run`: 0 issues.
- `gitleaks git .`: clean.
- `tools/descriptor check`: compose byte-identical.
- `hubctl init` on pgx: same install id
  `dd69c78ca1197ff1af1c65bc98dc94f6`, 11 services; `doctor` 45 checks
  all ok.

## Did not fit / findings

- `ErrStale` races in `Sweep`/`SweepRow` are treated as "row moved on"
  skips, not errors — intentional, matches the re-verify-then-CAS
  design.
- `hhubdb`-tagged tests need the compose postgres; `just check` starts
  it. Running `go test -tags hhubdb` by hand without the stack fails
  fast on connect (testkit `DSN` requires `HHUB_TEST_DSN` or defaults
  to localhost).

## Redesign-signal measurements

- `just check` local: 28-47 s (threshold 5 min) — green.
- Mandatory e2e: none yet (threshold 10 min) — n/a.
- One change touching > 3 services: no services exist — n/a.
- Same service-pair contract changed twice in a row: no contracts — n/a.
- Service at 70% line budget in introduction phase: 0 lines — n/a.
- Contract scaffolding share of phase lines: contracts/ empty — n/a.
- MCP reference set: phase 17 — n/a.

## Closeout (phase-02-finish)

Owner decisions on the phase-2 questions, applied:

1. Postgres stays in `just check` — splitting checks is the
   two-quality-toolchains antipattern. `check-fast` remains the
   offline subset for pre-commit, unchanged.
2. `shared/store/admin` no longer opens connections. Install commands
   go through `store.Connect` with the install-role DSN as the
   parameter; the duplicated open+ping is deleted (rule 3).
3. Sweeper model v1.0: one sweeper per service, lease keyed by
   sweeper name, sharded leasing not designed. Declared as invariant
   `adr.fsm.single-sweeper` in `adr/2026-11-22-fsm.md`; check:
   `TestLeaseContention` (one owner per name; PK is `name`, no shard
   column).
4. "Kill between transition and outbox" is now a real process test:
   `TestKillBetweenCommitAndEffect` spawns a victim (`testkit.Spawn`,
   re-exec of the test binary) that commits a transition with an
   outbox intent, reports `COMMITTED`, and is SIGKILLed before the
   effect runs. The parent verifies committed state + pending intent
   survived, and the executor applies the effect exactly once via the
   outbox `seq` idempotency key (replay is a no-op).
5. fsm tests no longer share dev tables: `testkit.ScratchSchema` gives
   each test its own schema with the fsm tables under `Spec.Schema`;
   schema is dropped on cleanup — no `kind` collisions, no manual
   cleanup, CAS tests run on a real multi-connection pool.

Design notes:

- `Spec.Schema` binds the machine's tables to a schema (empty means
  `fsm`); validated as a safe identifier before SQL interpolation.
  `EnsureSchema`/`AcquireLease`/`ReleaseLease` take the schema
  explicitly.
- `testkit.TempSchema` keeps its single-connection + search_path
  semantics for migration tests and now returns the schema name;
  `ScratchSchema` serves pooled, name-qualifying tests.
- forbidigo gained one exception: `exec.Command` inside
  `shared/testkit` — the crash-test harness, same class as the
  tools/ exception.

New `just check` timing (Windows -> pwsh -> container, incl.
`compose up --wait postgres` and the subprocess kill test):
**36.7 s** — green, well under 5 minutes.
