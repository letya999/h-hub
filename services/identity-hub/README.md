# identity-hub

## Purpose

Owns human principals, mock-channel links and token issuance.

## Zone and neighbors

Core zone. The operator provisions principals; channel-hub confirms links and
requests owner tokens; task-engine requests run tokens. All calls use declared
edges ([TestPrincipalLinkLifecycle](internal/svc/people_db_test.go),
[TestMintNeedsEdge](internal/svc/svc_test.go)).

## Owns

Its database holds principals, channel links, one-time code hashes and sealed
values, issuance nonces, audit rows and the sealed Ed25519 signing key. The KEK
is a host file outside Docker volumes; the signing seed is imported from a
sealed bootstrap on first boot ([TestIssuerSealedImport](internal/svc/issuer_db_test.go)).

## Contracts

Implements `contracts/identity/v1`. The contract fake and conformance suite
exercise the same issuer code; production keeps identity state in Postgres
([TestPrincipalLinkLifecycle](internal/svc/people_db_test.go)).

## Does not do

Does not own spaces, jobs or channel delivery. Rights and grants are a later
phase.

## Machines

`principal`: active → disabled, operator transition. `link_code`: pending →
used or expired, with a 24-hour deadline swept by identity-hub
([TestPrincipalLinkLifecycle](internal/svc/people_db_test.go)).

## Config

`HHUB_SUB`, `HHUB_DSN`, `HHUB_KEYS`, `HHUB_KEK_PATH`; loaded once in
`internal/config`.

## Runbook

Run `just stack`, then `just doctor`. `/healthz` checks the database. If the
service cannot start, check the sealed issuer import, host KEK and public key;
`doctor` reports missing import and any remaining plaintext key
([TestIssuerSealedImport](internal/svc/issuer_db_test.go)).
