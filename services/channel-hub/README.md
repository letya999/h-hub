# channel-hub

## Purpose

Owns durable mock-channel intake, command routing and answer delivery.

## Zone and neighbors

Edge zone. The operator injects mock updates and reads its mock outbox.
Channel-hub calls identity-hub for links and task-engine for jobs through
declared service edges ([TestMockChannelThreeUsers](../../test/e2e/channel/channel_test.go)).

## Owns

Its database holds deduplicated inbound updates, delivery records,
conversation pointers and `mock_outbox`; it owns no principal or job state
([TestInjectDedupAndIsolation](internal/svc/intake_db_test.go)).

## Contracts

Implements `contracts/channel/v1` (`Inject`, `ReadOutbox`) and consumes
`contracts/identity/v1`, `contracts/task/v1`, `contracts/model/v1`
(`Usage`, `Routes` for `/usage` and `/model`) and `contracts/spawn/v1`
(`SetModel`)
([TestModelChannelCommands](../../test/e2e/channel/model_test.go)).

## Does not do

Does not run a model, issue tokens, or execute tools. Telegram is outside
this phase.

## Machines

`intake`: queued → processing → waiting → completed or failed. `delivery`:
queued → sending → sent, failed, ambiguous or undeliverable. Both have
deadlines and are swept by channel-hub. `mock_thread` is long-lived and
changes on `/new` ([TestInjectDedupAndIsolation](internal/svc/intake_db_test.go)).

## Config

`HHUB_SUB`, `HHUB_DSN`, `HHUB_KEYS`, `HHUB_IDENTITY_URL`, `HHUB_TASK_URL`, `HHUB_SPAWN_URL`, `HHUB_MODEL_URL`;
loaded once in `internal/config`.

## Runbook

Run `just stack` and use `hubctl chat <user>`. `/healthz` checks the database.
Inspect the service log, FSM outbox and `mock_outbox` when a delivery stalls.
Service alerts use the same delivery path and accept only declared service
edges ([TestOperatorAlert](../../test/e2e/lifecycle/operator_alert_test.go)).
The channel checks the spawn watchdog and sends a direct operator alert
for each stale tick ([TestStaleSpawnTickRefusesHealth](../../test/e2e/lifecycle/watchdog_stale_test.go)).
Long answers become five text parts plus a remainder file
([TestScriptedLongAnswer](../../test/e2e/channel/scripted_channel_test.go)).
