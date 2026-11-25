# tool-hub

## Purpose

Owns the single admission point for tool calls: `AdmitCall` decides,
`Projection` lists the tools a model may see for a spec digest, and
`Invoke` runs an admitted call through the toolset's executor.

## Zone and neighbors

Agent zone. The space runtime calls all three ops with a run token
naming tool-hub in its audience; the run token binds the caller to
one run and one spec digest. The hub reads the caller's effective
spec from spawn-controller per call — grants are never copied
([TestProjectFiltersUngranted](internal/svc/toolsets_test.go)).

## Owns

Its database holds the `receipts` ledger — every allow and deny with
the digest pair and the capabilities checked — and the
`projection_cache`, a pure function of one spec digest
([TestReceiptsAreAppended](internal/svc/store_db_test.go)). The tool
registry ships embedded as `platform/toolsets.yaml`: toolset, its
executor, the provider behind it ([TestRegistryLoads](internal/svc/toolsets_test.go)).

## Contracts

Implements `contracts/toolhub/v1` (`AdmitCall`, `Projection`,
`Invoke`) and consumes `contracts/identity/v1` (run-token verify)
and `contracts/spawn/v1` (`ResolveSpec`). It owns `contracts/ownerreq/v1`,
calls task-engine's service-only `GetRequestTurn` and delivers or retracts
questions through `contracts/channel/v1`.
[Question and choice round trips](../../test/e2e/channel/ownerreq_test.go)

## Does not do

Spawn-controller owns policy and grants. Channel-hub owns delivery and
routes replies to the request's addressee.
[Single-use answers](../../test/e2e/channel/ownerreq_proof_test.go#TestOwnerAnswerSingleUse)

## Machines

`owner_request` moves from `pending` to `answered`, `denied` or `expired`.
Tool-hub checks the originating job before delivery and answer. A durable
outbox check also retires unreported requests after a job finishes.
[Lost-report scenario](../../test/e2e/channel/ownerreq_proof_test.go#TestFinishedJobRetractsUnreportedRequests)

## Config

`HHUB_SUB`, `HHUB_DSN`, `HHUB_KEYS`, `HHUB_IDENTITY_URL`,
`HHUB_SPAWN_URL`, `HHUB_CHANNEL_URL` and `HHUB_TASK_URL`,
loaded once in `internal/config`.

## Runbook

Run `just stack`. `/healthz` checks the database. An empty
projection is refused with `err.tool-projection-empty`; a call
without the capability is refused with `err.tool-denied`
([TestDecideMatrix](internal/svc/toolsets_test.go)).
