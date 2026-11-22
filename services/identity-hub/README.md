# identity-hub

## Purpose

Phase-3 fake identity issuer: it signs run and service tokens so the
task->answer path can verify callers end to end, while the real
identity-hub is deferred to phase 4.

## Zone and neighbors

Core zone. task-engine calls `MintRunToken` to bind a run to its owner,
and `IssueServiceToken` (proof-signed by the caller's service key) to
obtain tokens for spawn-controller and harness-runtime edges. Nothing
calls back into task-engine.

## Owns

The Ed25519 issuer key pair (`keys-identity-hub` volume) and the
in-memory key registry used to verify service-token requests. No
database is attached in phase 3.

## Contracts

Implements `contracts/identity/v1` (`MintRunToken`,
`IssueServiceToken`). The contract fake lives at
`contracts/identity/v1/fake.go` and passes the conformance suite via
real HTTP.

## Does not do

Does not authenticate users, store subjects, or issue grants — real
identity and rights arrive in phase 4. Does not verify service tokens
on other services' behalf; each callee verifies with the public key.

## Machines

None — stateless issuer.

## Config

`HHUB_SUB` (service identity), `HHUB_DSN`, `HHUB_KEYS` (key directory);
loaded once in `internal/config`.

## Runbook

Starts with the stack (`just stack`). Healthy = `/healthz` answers and
`IssueServiceToken` accepts a proof-signed request. If token
verification fails elsewhere, first check the `issuer.pub` distributed
by `keydist` matches `service.pub`.
