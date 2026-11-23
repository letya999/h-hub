# model-gateway: relay, ceilings and accounting

Status: draft (service phase 6).

## Context

Every model call must pass one owned seam so spend can be counted and
capped, provider credentials stay out of runtimes, and CI keeps a
deterministic upstream behind the same path a real provider uses.

## Decision

`model-gateway` is the single relay. The space runtime calls its
`Complete` op with a run token; the gateway resolves the caller's
effective `models` passport from spawn-controller per call — the
route table, the daily and per-run token ceilings and the reservation
estimate live there under one digest, never as a local copy. The
route pins the upstream URL, the model name and an optional key file
name; the gateway rewrites the body's `model` field, attaches a
`Bearer` read from `<keys>/routes/<file>` and relays the OpenAI
response verbatim.

Accounting is one conditional update per counter: a reservation of
`reserve` tokens lands on `(owner, day)` and `(run)` only while
`spent + reserved + reserve <= ceiling`; zero rows matched is the
quota refusal. After the upstream answer, the measured usage settles
— charged when reported, the estimate when absent — inside one
transaction that also appends the spend event. A sweep converts
reservations orphaned by a crash into charged spend at their estimate
after ten minutes. `Usage` and `Routes` are owner-scoped reads;
`SetModel` on spawn-controller rewrites `models.default` inside the
stored spec and recompiles the digest, so a route switch needs no
runtime restart.

CLIProxyAPI is the real upstream: pinned image on the egress network
(which only model-gateway joins), its client key materialized into
the gateway's keys volume, its OAuth session in cliproxy-auth. The
scripted model rides the same route shape for checks and CI.

## Rejected

- Pre-flight estimate in task-engine or the runtime: a second policy
  owner invites drift (rule 3).
- Ceiling check inside the caller's body `max_tokens`: callers choose
  it, so it cannot be a quota.
- Storing route keys in the spec or descriptor: policy documents are
  readable where secrets must not be.

## Invariants

- adr.model-gateway.reserve-conditional — `TestParallelReservesCannotOverflow`,
  `TestDailyCeilingRefuses`, `TestRunCeilingRefuses` (DB)
- adr.model-gateway.measured-or-estimate — `TestEstimateChargedWhenUnmeasured` (DB)
- adr.model-gateway.route-key-file — `TestRouteKeyConfinedToRoutesDir` (unit)
- adr.model-gateway.spec-per-call — `TestModelChannelCommands` (e2e)
- adr.model-gateway.run-token-bound — `TestForgedRunTokenRefused` (e2e)
