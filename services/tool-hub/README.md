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
and `contracts/spawn/v1` (`ResolveSpec`); the fake lives next to the
contract.

## Does not do

Does not own policy or grants (spawn-controller), does not ask the
owner for confirmation (a later phase), does not run side-effecting
tools — the only executor is the in-process `toolhub` echo.

## Machines

None. Receipts are append-only facts; the projection cache never
changes under a digest.

## Config

`HHUB_SUB`, `HHUB_DSN`, `HHUB_KEYS`, `HHUB_IDENTITY_URL`, `HHUB_SPAWN_URL`;
loaded once in `internal/config`.

## Runbook

Run `just stack`. `/healthz` checks the database. An empty
projection is refused with `err.tool-projection-empty`; a call
without the capability is refused with `err.tool-denied`
([TestDecideMatrix](internal/svc/toolsets_test.go)).
