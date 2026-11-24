# Phase 8 report — v0.0.8

Goal of the phase: replace the minimal phase-3 passports with the
full-form layer mechanism — six documents per layer, one ordered role
chain, a schema registry with consumers and effect classes, set-wise
compile with cross-validation, one sha256 digest, CAS activation,
preview/confirm, rollback, and stored explain.

Phase-7 leftovers closed in the same branch: `env-no-secrets` gate,
the e2e package split, and the pinned SQLCipher base image.

## Scope delivered

### Layer documents and chain

- Six documents per layer — `stack`, `config`, `models`, `workspace`,
  `tools`, `grants` — one YAML format, one `map[path]value` leaf
  shape (`services/spawn-controller/internal/svc/docs.go`).
- The chain is data, not code: `roles: [platform, user]` lives in
  `internal/svc/platform/registry.yaml`; adding `org` or `agent` is a
  new role entry plus layer revisions — no schema or format change.
  A lower layer may only narrow the upper layer's effective result;
  widening is refused at submit and at compile.
- Templates are platform folders under
  `internal/svc/platform/templates/`: `assistant` and `restricted`,
  each carrying the six documents plus workspace files (`soul.md`,
  `agents.md`). Grant ceilings follow owner decisions;
  `terminal.exec`, `ssh.exec`, `connectors.manage`,
  `mcp.remote.use`, `mandates.*` are absent from every template.

### Schema registry

- `internal/svc/platform/registry.yaml`: one row per leaf — path,
  field class (`invariant`, `narrow`, `choice`, `user_data`,
  `keyset-narrow`), consumer, effect class (`live`, `respawn`,
  `recreate`). An unproven `live` entry is applied as `respawn`.
- The `passport-schema` gate (`tools/gates/passport.go`) parses the
  registry, validates classes/effects and fails on a leaf without a
  consumer. Broken example: `testdata/broken/passport-schema/`
  (proved in `tools/gates` tests).

### Compile, digest, activate

- `internal/svc/compile.go` compiles the whole layer set in chain
  order with cross-document validation (route `key_ref` resolves,
  workspace paths stay inside the space, model names exist). Digest
  = sha256 of the canonical effective spec without service metadata
  (`spec.go` `compile` unchanged, reused as the one canonicalizer).
- Immutable layer revisions live in spawn-controller's own database
  (`migrations/0002_layers.sql`): `layer_revisions`,
  `revision_sets`, `effective_specs`, `activations`, `explains`.
- `preview` compiles and stores a candidate — no effects.
  `activate` applies exactly the shown candidate inside one
  transaction guarded by a CAS on the space's active set row; a
  confirm against a changed base or candidate is refused.
  `rollback` re-submits a stored revision set through the same
  compile path and re-validates. `explain` persists per-leaf layer
  attribution.
- Effect classes route through existing FSM transitions and outbox
  rows (`internal/svc/layers.go`): `live` mutates the effective spec
  in place (model-gateway already reads it per call), `respawn`
  drains and restarts the runtime, `recreate` additionally emits a
  `render-reset` effect that rebuilds the rendered workspace.
- Phase-3 minimal passports migrate on space load into `platform` +
  `user` revisions; `defaultPassports` is deleted — the old compiler
  no longer exists.

### /model through the new mechanism

- `SetModel` now submits a `user`-layer revision narrowing
  `models.selected` and activates it (`set_model.go`); the channel
  command path is unchanged for users.

## Phase-7 closures in this branch

- `env-no-secrets` gate (`tools/gates/env.go`): a `KEY=value` env
  var that looks like a credential, or any secret passed to a
  service other than by `cred://` ref, fails the gate; broken
  examples in `testdata/broken/env-secrets/`.
- `test/e2e` split: `channel` (mock channel + model route/spend)
  and `lifecycle` (spaces, seal, upgrade, passport) run as two
  packages inside one `go test` call — both still parallel. Shared
  helpers moved to `test/e2e/harness`.
- `test/e2e/disrupt` holds `TestStackRestart` alone: it restarts
  postgres and all services, so it must not overlap in-flight
  channel traffic. `tools/tracecheck/run.sh` and `just e2e` run it
  sequentially after the parallel pair. Splitting it out removed a
  real race where an in-flight job went `uncertain` (by design,
  never auto-retried) when the restart landed mid-answer.
- SQLCipher: `deploy/sqlcipher.Dockerfile` builds the pinned
  v4.6.1 base; CI (`sqlcipher.yml`) publishes it to GHCR by digest;
  `broker.Dockerfile` and `dev.Dockerfile` consume
  `deploy/sqlcipher-base.ref` via `SQLCIPHER_BASE` build-arg — no
  local SQLCipher compile anywhere in the check path.

## sys.* matrix

| ID | Phase-8 status | Proof |
| --- | --- | --- |
| `sys.passport-narrow` | proved | TestPassportNarrowRefusal — user layer widening `config.limits` refused |
| `sys.passport-activate` | proved | TestPassportConfirmStaleRefused — confirm after a second submit refused |
| `sys.passport-live` | proved | TestPassportLiveAppliesWithoutRestart — model leaf applies, runtime keeps running |
| `sys.passport-respawn` | proved | TestPassportRespawnRestartsRuntime — config leaf restarts runtime |
| `sys.passport-recreate` | proved | TestPassportRecreateRebuildsWorld — workspace leaf rebuilds rendered files |
| `sys.passport-rollback` | proved | TestPassportRollbackRestoresDigest — previous digest restored through the same checks |
| `sys.passport-explain` | proved | persisted explain attribution asserted in live test |
| `sys.model-route` | proved (unchanged) | TestModelChannelCommands — `/model` via the new layer path |
| `sys.schema-version` | proved (unchanged) | TestSpecSchemaVersion |
| `sys.stack-restart` | proved (moved) | TestStackRestart in `test/e2e/disrupt` |
| `sys.secret-leak` | proved, extended | env-no-secrets gate + fixtures; broker canary re-run on live secrets, 0 hits |
| all other `sys.*` | unchanged | see 07-report matrix |

## Signals

- Warm `just check` (all layers cached, stack up): **3m55s** measured
  on the finished tree (235 s) — the 5-minute target is met; the
  6-minute floor is not needed.
- Cold: not re-measured end to end; the dominant cold term from
  phase 7 (SQLCipher compile ~5 min in two images) is gone — the base
  image is pulled by digest like every other image.
- E2E breakdown (parallel pair + disrupt tail):
  `channel` 67.8s, `lifecycle` 151.1s, `disrupt` 15.9s — wall clock
  ≈ 2m50s, well under the 10-minute budget.
- The e2e split plus sequential disrupt tail removed the stack-restart
  race that previously put an in-flight job into `uncertain`.
  `TestSweepLagOperatorAlert`'s delivery poll was raised 10 s → 30 s:
  under the parallel pair it must wait until its own job becomes the
  oldest pending deadline before the alert carries its key; the 30 s
  product threshold is unchanged.

## Live Telegram

No new live checks ran in this phase beyond what 07-report records:
adapter activation on lease, one real linked conversation and the
leak canary are proved; three-user traffic, photo/vision, `KEY=value`
deletion and edit cadence remain deferred — they need real humans on
the bot.

## Out of scope (per brief)

Capability and tool-hub layers (9), owner request flow (10), native
tools (11), broker forms and OAuth (18), `org`/`agent` roles.
