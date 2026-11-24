# Phase 7 report — v0.0.7

Goal of the phase: live people work through Telegram, installation
secrets live in credential-broker, no secrets remain in the environment.

## Scope delivered

### credential-broker (new service)

- `contracts/cred/v1`: `Acquire` (lease < 2 min, audited), `Revoke`,
  `ListConnections`, `SetSecret`, `Snapshot`. Conformance suite +
  in-memory `Fake` for consumers.
- `services/credential-broker`: single CGO binary, whole-database
  SQLCipher over a statically built, hash-pinned SQLCipher v4.6.1
  (SHA256
  `d8f9afcbc2f4b55e316ca4ada4425daf3d0b4aab25f45e11a802ae422b9f53a3`).
  The `database/sql` binding is the project's own minimal driver
  (`services/credential-broker/internal/svc/sqlitedrv.go`): the raw
  key pragma is applied before any other statement. `mattn/go-sqlite3`
  was dropped after proving in C that its `Open` runs
  `PRAGMA synchronous` (PragFlg_NeedSchema, reads page 1) before the
  key hook, so it can create an encrypted file but never reopen one;
  the boundary regression lives in `sqlitedrv_test.go`
  (create → close → reopen → read/write, wrong-key refusal).
  `PRAGMA cipher_version` is probed at startup; an unreadable or
  non-SQLCipher engine refuses startup (proven live: a stale plaintext
  volume crashed the broker until removed). Values are additionally
  envelope-sealed with `shared/seal`; the KEK lives outside volumes.
  Credential registry in code (`api-key`, `oauth`, `file`); one builtin
  store; `cred://builtin/platform/<name>` refs; versions + soft-delete
  rotation; audit events in the broker's own database.
- `hubctl secret set <name>`: operator edge, stdin or `--from-file`
  only — never argv, never env. `hubctl secret snapshot` writes a
  consistent `VACUUM INTO` copy inside the broker volume; `backup.sh`
  copies the sealed file out and checksums it.

### Secret migration

- `channel-hub` leases `cred://builtin/platform/telegram-bot-token` in
  memory only (~45 s refresh), on start and per provider call. Missing
  credential → adapter inactive, no placeholder, no env fallback.
- `model-gateway` leases `cred://builtin/platform/cliproxy-key` per
  provider call via `key_ref` in the passport (replaces the route-key
  file; ADR and invariant registry updated).
- `tools/cliproxy/init.sh` stages the generated key, pushes it through
  `just secret-set cliproxy-key --from-file`, and shreds the staging
  file. `CLIPROXY_IMAGE` env removed; the digest is pinned in
  `deploy/cliproxy-image.ref`.

### Telegram adapter (channel-hub)

- Polling with durable `update_id` offset in `tg_offsets`
  (transactional with intake admit).
- Conversations keyed `(channel, peer, chat, topic)`; forum threads
  `thr:<id>`, direct-message topics `dm:<id>` via `tg_threads`.
- Status message edited at most once per 5 s, carries the run phase
  only (`channel.phase.*` locale keys), final phase bypasses throttle.
- Photos become model attachments when the active route advertises
  `vision` in the passport (spawn-controller → model-gateway →
  `contracts/model/v1.RouteInfo.Vision`); otherwise a localized
  refusal. Raw photo cap 512 KiB so base64 stays under the 1 MiB HTTP
  body limit. Attachments flow `task/v1` → task-engine (`jobs.attachments`
  JSONB) → `harness/v1` → OpenAI-style image content parts.
- `KEY=value`-looking messages are intercepted, deleted provider-side
  (`tg-delete` effect), and answered with a localized hint to use
  `hubctl secret set`.

### Upgrade / rollback

- `tools/upgrade/upgrade.sh` on the live install: snapshot previous
  image pins → drain every space runtime (`hubctl user
  runtime-drain`) → `compose up -d --wait` (services self-migrate
  forward) → healthz on every service + a provisioned user
  (`user add` → `/start` link → `chat` → scripted `answer:`) → on
  failure re-tag the previous pins and redeploy (rollback path).
  Ran green on the live stack; a failed smoke during development
  exercised the rollback redeploy path end to end.
- Unreadable pin refuses start: `deploy/cliproxy-image.ref` must match
  `name@sha256:<64 hex>` (`tools/cliproxy/up.sh`); a stale caller pin
  at `EnsureRuntime` is refused `CodeStale`
  (`adr.spawn-runtime.digest-pins-image`, e2e `TestUpgradePin`).

## sys.* matrix

| ID | Phase-7 status | Proof |
| --- | --- | --- |
| `sys.tenant-isolation` | proved (unchanged) | TestMockChannelThreeUsers |
| `sys.forged-token` | proved (unchanged) | TestMockChannelThreeUsers |
| `sys.secret-leak` | proved, extended | canary through broker write+lease; Telegram `KEY=value` interception (unit) |
| `sys.kill-uncertain` | proved (unchanged) | TestKillUncertain |
| `sys.store-missing` | proved (unchanged) | TestStoreMissingDatabaseRefusesStartup |
| `sys.stack-restart` | proved (unchanged) | TestStackRestart |
| `sys.late-receipt` | proved (unchanged) | TestLateReceiptRecordedWithoutReopening |
| `sys.seam-onesided` | deferred:10,18 | — |
| `sys.sweep-lag` | proved (unchanged) | TestSweepLagOperatorAlert |
| `sys.wedge-unblock` | proved (unchanged) | TestHubctlUnblocksJobAndRuntime |
| `sys.schema-version` | proved (unchanged) | TestSpecSchemaVersion |
| `sys.seal-at-rest` | **proved (was deferred:7)** | broker canary absent from volume dump, logs, inspect, pg_dump |
| `sys.watchdog-stale` | proved (unchanged) | TestStaleSpawnTickRefusesHealth |
| `sys.service-edge` | proved (unchanged) | TestServiceEdge |
| `sys.model-usage` | proved (unchanged) | TestGatewayAccountsSpend |
| `sys.model-quota` | proved (unchanged) | TestDailyQuotaRefusal |
| `sys.model-route` | proved (unchanged) | TestModelChannelCommands |
| `sys.space-egress` | proved (unchanged) | TestSpaceReachesOnlyModelGateway |
| `sys.worker-*`, `sys.package-build`, `sys.recipe-hosts`, `sys.space-no-db` | deferred (phases 13/17) | — |

## Signals

- `just check` warm (all image layers cached): **6m04s** measured on
  the finished tree; the e2e package alone is ~2m30s of that.
- `just check` cold (fresh image build): not measured end to end;
  the uncached part observed this phase is the image rebuild, ~10-13
  min (SQLCipher compile ~5 min inside the broker and dev images, Go
  builds ~2 min, base pulls + `go mod download` the rest), so cold ≈
  16-19 min.
- Mandatory e2e is well under the 10-minute budget.
- **Signal tripped**: warm `just check` is over the 5-minute
  threshold. Breakdown: e2e ~2.5 min (real containers, scripted
  model), build+lint+vet ~1.5 min, stack bring-up + `hubctl init`
  ~1 min, trace/gitleaks/descriptor ~0.5 min. The dominant term is
  the e2e package; the suite already runs tests in parallel where
  the harness allows it, so the cost is container startup per test,
  not serialization. Proposal for the owner: split `test/e2e` into
  two packages (channel/model vs lifecycle/upgrade/seal) and run
  them concurrently inside tracecheck — saves roughly a minute —
  or accept the 6-minute floor and revisit when phase-9 e2e lands.

## Live Telegram — observed state and remaining work

The owner installed `telegram-bot-token` after the report was written
(broker `ListConnections` shows it alongside `cliproxy-key`). Verified
live during the phase-8 wrap-up:

1. **Adapter activation on lease — proved.** The running stack picked
   the token up without restart or code change: polling works, intake
   admitted, answers delivered.
2. **Live conversation — proved.** Three real Telegram users linked
   via `/start` + code and held real conversations on `devin`.
   Two simultaneous conversations ran truly in parallel: runs started
   16 ms apart, both succeeded (8–10 s), answers went to their own
   chats, spend split per owner (`spend_events` seq 366/367). The
   three-at-once variant was not exercised — the third user stayed
   silent in the window.
3. **Photo / vision route — proved, after a fix.** The declared
   `vision: true` on `devin` was false: `devin/swe-2-flash` refuses
   all image parts upstream (`invalid_argument`). A platform-layer
   revision removed the flag and added `devin-vision`
   (`devin/gemini-3-8-flash`, the same cliproxy/key_ref). Photo on
   `devin-vision` produced a correct image description and a real
   spend row (route `devin-vision`, charged 2070, measured).
4. **`KEY=value` deletion — proved, after a fix.** Canary
   `HHUB_LIVE_CHECK=canary-otter-77`: the message was deleted from the
   chat, the hint delivered, no job spawned, zero hits in all service
   logs. One defect found and fixed — the raw secret value was
   persisted in `inbound_updates.body` (channel-hub commits, merged
   via #16); the value now dies at normalize.
5. **Status-edit cadence — proved.** One `message_id` per job; bodies
   were only phase words (Working…/Done.). An ~80 s run with eight
   progress heartbeats produced zero intermediate edits (same-phase
   skip; the 5 s throttle is the backstop).
6. **Leak canary with live secrets — proved.** A probe leased both
   real credentials (`telegram-bot-token`, `cliproxy-key`) through the
   production service-token path and grepped 24 readable surfaces
   (service logs, inspect output, pg_dump of every database, rendered
   configs): **0 hits**. Values were never printed.

Defects found by the live cases and fixed (PRs #16, #17):

- Photo job creation sent an empty idempotency key — refused by the
  engine as missing-input (wedge: intake stuck in waiting).
- A policy refusal finalized the status message as "Failed." — new
  `channel.phase.refused` ("Refused."/"Отклонено.") on all five
  refusal paths.
- A failed run's raw error (`unavailable: err.unavailable`) was served
  to the user as the answer; non-succeeded states now get the
  localized `channel.job-failed` text.
- `inbound_updates.body` stored the intercepted secret verbatim.
- The platform layer's route table was declared twice (yaml +
  `modelDefaults()`); `normalize()` now decodes the same yaml.
- `config.timeout_ms` 120 s is one non-streamed model call; slow real
  generations exceeded it — raised to 300 s everywhere (layer + live
  revisions on the three spaces).

Operational notes for the record: one Telegram update was lost in a
channel-hub restart window (poller down, Telegram did not redeliver);
`tg_status.phase` stays "Working…" after the final edit (internal
cursor only); long answers split mid-word (cosmetic). The bot token
appeared in service logs before the secret-body fix — rotation via
@BotFather + `hubctl secret set` is the owner's call.

## Out of scope (per brief)

Form secrets and OAuth recipes, broker pairing/forms (phase 18),
egress-gateway (17), capability/tool layers (9, 10).
