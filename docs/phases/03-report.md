# Phase 3 report — launch core (v0.0.3)

## Implemented

- `contracts/{identity,task,spawn,harness}/v1`: types, HTTP clients
  (timeout, bearer, idempotency key), in-memory fakes, conformance
  suites run over real `httptest` handlers. `TestConformance*`.
- `shared/edges`: descriptor-edge table `(caller sub, op, callee)`
  parsed from `deploy/edges.json`; `httpapi` verifiers for service
  tokens (Ed25519) and run tokens (audience + job/space binding).
- `shared/fsm`: `Touch` (deadline re-arm for heartbeats), watchdog
  `Tick` rows with age exposed for `/healthz`, outbox effect executor
  with `seq` idempotency, `fsm.machines` readers for e2e.
- `shared/testkit`: Docker Engine client over the unix socket
  (inspect, kill, restart) for the e2e suite.
- `descriptor`: `clients` section, per-service `deploy` flag, env
  declaration lists, space-zone rules (no DB route, no `HHUB_*_DSN`),
  compose generation for the whole stack, `deploy/edges.json` output.
- `identity-hub` (fake): Ed25519 service/run token issuance,
  single-use nonces persisted in its own DB, edge-gated ops.
  `TestIssueAndVerify*`.
- `task-engine`: Job machine (queued -> admitted -> running ->
  settled; uncertain/cancelled; failed+quarantined long-lived for
  operator retry/unquarantine) and Run machine (dispatched ->
  streaming -> settled), FIFO conversation claim with durable cursor,
  partial unique index "one non-final run per conversation", admission
  ceilings (32 live jobs/owner, 64 cluster-wide), uncertain policy
  (15 min observation), holds, cancellation, run-event journal,
  dispatch chain resolve-spec -> ensure-runtime -> mint-run-token ->
  submit-run, outbox for release/cancel effects.
- `spawn-controller`: spaces (idempotent on `(owner,name)`), minimal
  passport compile into canonical effective spec, spec digest binds
  the resolved immutable image ID; Runtime machine (absent ->
  starting -> warm -> busy -> draining -> absent + failed/quarantined/
  missing); Docker Engine API backend only (`internal/backend`) with
  hardening — non-root 65534, ro-rootfs, cap-drop ALL,
  no-new-privileges, tmpfs /tmp, mem/cpu/pids limits, render volume
  ro, `hhub` network only; worker intent row before `docker create`;
  reconcile of live containers; watchdog tick age in `/healthz`;
  host-side render of read-only harness config + issuer pubkey.
- `harness-runtime`: strict in-memory admission (SubmitRun creates
  the only entry; unknown run id is a refusal), run-token validation
  (audience, job, space), caller headers stripped, tools disabled,
  env allowlist only, no database; turn = one OpenAI-compatible call
  to the scripted model; start/final reports to task-engine over a
  bounded detached context so final failure lands after CancelRun.
- `cmd/hubctl job` (`create`/`get`/`retry`/`cancel`/`unquarantine`,
  `-url` flag, default `http://task-engine:9000`) and `cmd/keydist`
  (service keys, pubs, edges table into per-service volumes).
- `test/scripted-model`: OpenAI-compatible container, one built-in
  "answer with text" scenario, deterministic delay for the kill test.
- `test/e2e` (`hhube2e` tag, real containers): `TestJobToAnswer` (3
  parallel owners, tenant isolation), `TestKillUncertain`,
  `TestForgedToken`, `TestServiceEdge`, `TestSpaceNoDB`,
  `TestRuntimeIntent`, `TestMissingConfigRefused`, `TestStackRestart`.
- Docs: 4 service READMEs on the enforced template; ADR drafts
  `2026-11-23-{service-edges,spawn-runtime,task-jobs,harness-admit}`.

## Gate thresholds

No threshold lowered. Three gate-semantics fixes (each with a test,
flagged for owner review):

- docs: README/template compare now skips the `# ` H1 — the template
  H1 is a placeholder title, comparing it verbatim forced every
  service README to repeat "Service README template". Level-2+
  headings still compared verbatim (`TestHeadingsOf`).
- env-declared: regex narrowed to `os.Getenv`/`os.LookupEnv` reads.
  Names a service *writes* into a child container's env (spawn's
  runtime allowlist) are not reads and no longer need declaring
  (`TestEnvRe`).
- archtest: type-method counts keyed by package-qualified name —
  bare `Client` collided across packages (`httpapi.Client`,
  `testkit` docker client), merging unrelated types into one bucket.
  Same thresholds; the gate now measures what it claims to.
- `.golangci.yml`: forbidigo `os.Getenv` exclusion scoped to service
  `internal/config` packages (rule 14's single env-reading package)
  and `deploy/` generators that emit env names.

## just check timing

Windows workstation, full check incl. postgres and `hhubdb` tests:
**49 s** — far under the 5-minute signal threshold.

## e2e evidence

`go test -tags hhube2e -count=1 -timeout 10m ./test/e2e` →
**ok 59.4 s**, all 8 tests green:

- job -> answer for three owners in parallel; one owner's data
  invisible to another (`sys.tenant-isolation`).
- `docker kill` of a busy runtime -> `job.uncertain` with the
  15-minute observation armed, no auto-retry (`sys.kill-uncertain`).
- full stack restart -> jobs/runtimes recovered from DBs, queued work
  resumes (`sys.stack-restart`).
- forged service token -> refusal (`sys.forged-token`); op outside
  the declared edge -> refusal (`sys.service-edge`).
- runtime container has no DB env/route (runtime half of
  `sys.space-no-db`); runtime intent row precedes `docker create`
  (`adr.spawn-runtime.intent-first`); missing config -> start refusal.
  The phase-5 audit found that this had not tested a missing database
  or the worker janitor ([phase 5 report](05-report.md)).

## Design notes (within-brief decisions)

- Job `failed`/`quarantined` are `LongLived`, not `Final`: both have
  outgoing edges (`retry`, `unquarantine`) and wait for an operator,
  so no deadline arms them. Same shape as spawn's failed/quarantined.
- `EnsureRuntime` is asynchronous — it returns `Ready:false` while
  the runtime starts; the dispatcher polls the contract until warm.
- Run `streaming` is a 45 s stall deadline re-armed by `start`/
  `progress` heartbeats via `fsm.Touch` (killed runs fail fast;
  legitimately slow turns stay alive). `uncertain` observes 15 min.
- `CreateSpace` is idempotent on `(owner,name)` — retries and replays
  return the existing space instead of a uniqueness error.
- Space CRUD is an open operator surface; only runtime ops
  (ensure/release/cancel) carry service-token edges. User identity
  arrives with real identity-hub in phase 4 — interim posture, owner
  to confirm.
- Spec digest includes the resolved immutable image ID at compile
  time: `HHUB_RUNTIME_IMAGE` (a tag) is resolved through the Engine
  API before hashing, so the digest pins content, not a name.
- `hubctl job` takes `-url` (default `http://task-engine:9000`) —
  CLI env reads are outside the config-package exemption.

## Did not fit / findings

- CRLF->LF normalization changed migration file *bytes*; the stored
  sha256 then failed `store.Migrate` verification on existing dev
  DBs. Dev volumes were recreated (phase data is regenerable). The
  byte-pinned checksum means any post-apply rewrite — even EOL —
  invalidates live DBs; correct strictness for prod, but note it for
  future file-touching cleanups.
- `fix-needs-test` inspects HEAD: amending a `fix:` commit in place
  is self-blocking (HEAD is the commit being replaced). Worked around
  with a soft reset; minor tooling friction, not a gate hole.
- `check-fast` inside the pre-commit hook runs against the read-only
  mount — `gofmt` failures there must be fixed on the host.
- Rebuild flow requires `docker compose up -d --force-recreate` after
  image rebuilds; the compose project prefix must match volume names
  pinned in the template (`hhub_hhub-conf` vs literal `hhub-conf`).

## Redesign-signal measurements

- `just check` local: **49 s** (threshold 5 min) — green.
- Mandatory e2e: **59.4 s** (threshold 10 min) — green.
- One change touching > 3 services: the task->answer path itself
  spans identity, task-engine, spawn-controller, harness-runtime —
  that is the designed thin slice across *new* services, not a
  retrofit into existing ones. No out-of-scope service was edited.
- Same service-pair contract changed two phases in a row: n/a — first
  contracts this phase.
- Service at 70% of line budget in its introduction phase:
  - task-engine 1621 / 4000 = **41%**
  - spawn-controller 1403 / 8000 = **18%**
  - harness-runtime 433 / 6000 = **7%**
  - identity-hub 288 (fake, no brief budget)
  All well under 70%.
- Contract scaffolding share of phase lines: contracts/** = 1697 of
  8170 added lines = **21%** (< 50%); fake+suite alone 795 (10%).
- MCP reference set: n/a — phase 17.

## Questions for the owner

1. Accept or amend the four service ADR drafts
   (`2026-11-23-*.md`)? They stay drafts until your decision (rule 28).
2. The three gate-semantics fixes above are flaggable as
   check-weakening-adjacent. Keep as `fix(gates)` in this phase, or
   prefer them reverted and recommitted as an owner-only change?
3. Space CRUD unauthenticated until real identity-hub (phase 4) —
   acceptable interim posture for the dev stack?
4. Ratify tuning constants introduced this phase: admission ceilings
   32 jobs/owner + 64 live cluster-wide; run stream stall 45 s;
   run dispatched 30 s; runtime warm TTL 15 min / busy 11 min /
   starting 90 s / draining 60 s.
5. Keep byte-pinned migration checksums as-is (EOL rewrites invalidate
   existing dev DBs), or normalize migrations at apply time?

## Closeout (2026-11-23)

The owner's answers above are applied. The four service ADRs are
accepted; the gate fixes and byte-pinned migration checksums remain.
The ratified admission and runtime timing values are recorded as
tunable policy values in the task and spawn ADRs.

- A1: `hubctl job` signs with the operator key. Identity-hub checks
  owner, space and target on `ActAs`, records an audit row, and issues
  a scoped token. Task and space operator operations require verified
  tokens; no public operation remains except `/healthz`.
  `ClaimNext` was removed from the external contract because the
  dispatcher already owns queue claiming. `TestJobToAnswer` now proves
  cross-owner reads and writes fail, including a token for Alice
  naming Boris's space, cross-space idempotency replay and listing.
  `TestForgedToken` covers the same authenticated surface.
- A2: `runs_one_claim_per_conversation` is a partial unique index on
  `(owner, space, conversation)` for unreleased claims. All run-final
  paths release the claim inside the FSM transaction; dispatch refusal
  waits for the run deadline instead of freeing it early.
  `TestRunClaimUnique` makes two parallel inserts and requires the
  index's `23505` error; `TestJobToAnswer` completes a second turn in
  the same conversation. Migration `0002_run_claim.sql` transferred
  existing claims in the live dev DB. `conversations.active_run` has
  no readers or writers and remains in the schema until the next
  release, per expand/contract rule 30.
- A3: the duplicate job-finality map was removed; the Job machine's
  `Final` and `LongLived` sets own that definition.
- A4: the env gate rejects `os.Environ()` outside declared config
  readers and install code; a broken fixture proves the rejection.
  SQL files are pinned to LF by `.gitattributes` and a broken fixture.

Final local checks: `just check` **green** (timed value below),
`just e2e` **89.8 s** after the ADR mount assertion, including three
users, kill and stack restart. A manual
`hubctl job list -owner alice -space ...`
returned Alice's job only. The dev DB had no non-final run carrying
a released claim after migration.

New timed `just check`: **96.6 s** (under 5 minutes). The only deferred
schema work is dropping the unused `conversations.active_run` column
in phase 4.
No owner question remains from this phase.
