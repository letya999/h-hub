# model-gateway

## Purpose

Owns the single relay for every model call: OpenAI-compatible relay,
spend accounting and the daily and per-run token ceilings.

## Zone and neighbors

Egress zone. The space runtime calls `Complete` with a run token;
channel-hub calls `Usage` and `Routes` with an owner token. The
gateway reads the caller's effective models passport from
spawn-controller per call and relays to the route it names — the
scripted model in checks, CLIProxyAPI for the real provider
([TestGatewayAccountsSpend](../../test/e2e/channel/model_test.go)).

## Owns

Its database holds per-day and per-run counters, open reservations and
the `spend_events` ledger. Route keys live only as files under
`<HHUB_KEYS>/routes/`; the gateway owns no copy elsewhere
([TestRouteKeyConfinedToRoutesDir](internal/svc/upstream_test.go)).

## Contracts

Implements `contracts/model/v1` (`Complete`, `Usage`, `Routes`) and
consumes `contracts/identity/v1` (token verify) and
`contracts/spawn/v1` (`ResolveSpec`); the fake lives next to the contract
([TestForgedRunTokenRefused](../../test/e2e/channel/model_test.go)).

## Does not do

Does not own model policy or passports (spawn-controller), does not
select routes in a UI, does not cache responses, does not manage the
CLIProxyAPI container or its OAuth session volume.

## Machines

Reservations are table rows with a deadline: settled with the request
or converted to estimated spend by the stale-reservation sweep
([TestEstimateChargedWhenUnmeasured](internal/svc/store_db_test.go)).

## Config

`HHUB_SUB`, `HHUB_DSN`, `HHUB_KEYS`, `HHUB_IDENTITY_URL`, `HHUB_SPAWN_URL`;
loaded once in `internal/config`.

## Runbook

Run `just stack`. `/healthz` checks the database; `Complete` answers
with the upstream body. A quota refusal surfaces as `err.quota`
([TestDailyQuotaRefusal](../../test/e2e/channel/model_test.go)). Ceiling
violations under concurrency are refused, never clamped
([TestParallelReservesCannotOverflow](internal/svc/store_db_test.go)).
Provider credentials are checked absent from logs, env, inspect and
dumps ([TestProviderSecretsAbsent](../../test/e2e/channel/model_test.go)).
