# identity-hub

## Purpose

Phase-3 issuer: it signs run, service, and owner-scoped operator tokens
for the task->answer path. Real user identity arrives in phase 4.

## Zone and neighbors

Core zone. task-engine calls `MintRunToken` and proof-signed
`IssueServiceToken`. The operator calls `ActAs` with a token signed by
its installed key; the issued owner token is bound to one owner and
callee ([TestActAsBindsAndAudits](internal/svc/act_as_db_test.go)).

## Owns

The Ed25519 issuer key pair (`keys-identity-hub` volume), public-key
registry, and its own database for issuance nonces and the `ActAs`
audit ([TestActAsBindsAndAudits](internal/svc/act_as_db_test.go)).

## Contracts

Implements `contracts/identity/v1` (`MintRunToken`,
`IssueServiceToken`, `ActAs`). The contract fake lives at
`contracts/identity/v1/fake.go` and passes the conformance suite via
real HTTP.

## Does not do

Does not authenticate users, store subjects, or issue grants — real
identity and rights arrive in phase 4. Does not verify service tokens
on other services' behalf; each callee verifies with the public key.

## Machines

None. Nonces and operator audit rows are durable domain records.

## Config

`HHUB_SUB` (service identity), `HHUB_DSN`, `HHUB_KEYS` (key directory);
loaded once in `internal/config`.

## Runbook

Starts with the stack (`just stack`). Healthy = `/healthz` answers and
`IssueServiceToken` accepts a proof-signed request. If token
verification fails elsewhere, first check the `issuer.pub` distributed
by `keydist` matches `service.pub`.
