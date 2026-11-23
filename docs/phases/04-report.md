# Phase 4 report — mock channel and real identity (v0.0.4)

## Delivered

- `channel-hub` has its own database, contract, durable deduplicated intake,
  delivery and thread machines, mock outbox, command routing and bounded
  Markdown rendering. The response limit is five text parts plus an explicit
  remainder file ([TestInjectDedupAndIsolation](../../services/channel-hub/internal/svc/intake_db_test.go),
  [TestScriptedLongAnswer](../../test/e2e/channel/scripted_channel_test.go)).
- Identity-hub stores principals, single-use 24-hour link codes and channel
  bindings. It issues service, run, operator and linked-channel tokens through
  one issuer implementation. The production service no longer constructs the
  contract fake ([TestPrincipalLinkLifecycle](../../services/identity-hub/internal/svc/people_db_test.go)).
- The issuer signing key is sealed in identity-hub's database. The KEK is a
  host bind outside Docker volumes; the one-time sealed bootstrap is removed
  after import. Old plaintext signing keys migrate without changing the public
  key ([TestIssuerMigration](../../cmd/hubctl/install/issuer_test.go),
  [TestIssuerSealedImport](../../services/identity-hub/internal/svc/issuer_db_test.go)).
- `hubctl user add` retries across space, principal and link creation;
  `disable` deactivates a principal. `hubctl doctor` checks split home-space
  pairs; `hubctl chat` uses the private mock channel. A manual chat exercised
  `/start`, ordinary response, `/new`, `/status` and `/stop`.
- The scripted model loads exact YAML scenarios with response, tool-call,
  provider-error and hang actions. Verification fails on unused or mismatched
  steps ([TestScenarioExactAndComplete](../../test/scripted-model/scripts_test.go)).

## Acceptance evidence

- `just check`: green locally, including pinned Linux build, lint, vet,
  database tests, secret scan and descriptor gate. Final timed value below.
- `just stack`: healthy. `just doctor`: all checks green, including sealed
  issuer import, bootstrap removal and principal-space pairs.
- `just e2e`: **60.132 s**. Three users created through `hubctl user add`
  linked through `/start`, each received an answer. Tenant-scoped outbox
  reads, forged tokens and wrong-operation edges were refused. An unknown
  command returned help without creating a job
  ([TestMockChannelThreeUsers](../../test/e2e/channel/channel_test.go)).
- The long scripted answer escaped model HTML, visibly marked truncation,
  emitted five text parts and one remainder file, and consumed the full
  scenario ([TestScriptedLongAnswer](../../test/e2e/channel/scripted_channel_test.go)).

## Redesign signals

- Full local `just check`: **96.2 s**; threshold 5 minutes.
- Mandatory e2e: **60.132 s**; threshold 10 minutes.
- New channel-hub service: **976 Go lines**; identity-hub total:
  **1,273 Go lines**. Neither has a phase budget in the owner brief.
- The slice touched channel-hub, identity-hub and task-engine. No change
  required modifying more than three product services.
- Channel→identity and channel→task edges are new this phase. No existing
  service-pair operation was changed in two successive phases.
- No owner threshold, baseline exception or external module changed.

## Limits and follow-up

- The remote CI run is unverified because this local sequence does not push.
- Existing spaces pin a resolved runtime image. Removing an old image after
  rebuilding the local tag can leave such a space unable to warm until it is
  recreated; a new user provisioned after rebuild worked end to end.
- The phase-3 `conversations.active_run` column remains unused. Its deferred
  drop was not part of the phase-4 brief; phase 5 can remove it after the
  N-1 schema read check.
- The mock adapter intentionally has no Telegram transport or real tool
  execution. Those remain outside this phase.
