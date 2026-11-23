# Phase 6 brief — real model behind a gateway (v0.0.6)

Source: owner overnight brief; `h-hub-blueprint.md` §2.14 model-gateway,
§3 the `models` passport, §9 phase 6; `enforcement.md` sections 3-4;
`owner-decisions.md` — when a threshold or decision is disputed, do not
invent. Work branch: `phase-06` from `main` after v0.0.5. Tag: `v0.0.6`.

## Goal

The conversation goes through a real LLM, spend is counted and limited,
and provider secrets are visible nowhere.

## Scope

- `model-gateway`, a new service (budget 2000 lines): an
  OpenAI-compatible relay with its own `model` database. It verifies
  the run token; the route comes from the effective `models` passport
  (`default`, `routes[]`); a daily ceiling and a per-run ceiling apply
  per user; spend events are recorded and a `Usage` operation reports
  them; the contract is about three operations plus `/healthz`. It does
  not do: model catalogs, selection in a UI, team quotas, response
  caching.
- Atomic accounting under several replicas: before the call, reserve an
  estimate with one conditional UPDATE on the (user, day) counter;
  after the response, return the remainder against the measured value;
  a response without measured spend is charged at the estimate. Test:
  parallel calls cannot push the counter over the ceiling.
- CLIProxyAPI as the upstream for the real LLM: pinned digest, an
  internal network that only model-gateway enters. The Devin OAuth
  session lives in cliproxy's own volume; sign-in is `hubctl models
  login`. The cliproxy key becomes a credential-broker object in phase
  7 — there is no broker now.
- The scripted model moves behind the gateway as an ordinary route so
  e2e exercises the gateway itself.
- Channel commands: `/usage` (spend against the ceilings) and `/model`
  (pick one of the passport's `routes`). `/model` changes the user's
  `models.default` through the existing minimal passport mechanism of
  phase 3; the full layer form is phase 8 — do not build it.
- Network: the space zone has no direct outbound route. The runtime
  reaches only model-gateway through a declared descriptor edge;
  everything else is closed. The egress gateway for tools is phase 17.

## Money and CI

Mandatory checks and CI never call a real LLM — only the scripted
model behind the gateway. The real provider is enabled by a separate
`just` recipe for a manual run and for the owner's live work. Report
what your own manual run cost.

## Readiness

- e2e "message -> answer" through the gateway on the scripted model is
  green.
- The ceiling works: exceeding the daily limit produces a clear
  refusal, not a silent cut; parallel calls cannot push the counter
  over.
- `/usage` shows spend, `/model` switches the route and the new route
  applies without restarting the runtime.
- `sys.secret-leak` is extended to cliproxy: neither the token nor the
  OAuth session appears in logs, responses, env, inspect or a dump.
- A manual run on the real LLM: one conversation through `hubctl chat`,
  with the answer and the recorded spend shown.
- `just check` within five minutes, mandatory e2e within ten, nightly
  green.

Update `docs/invariants.md`: what this phase proves, what stays
deferred, and until which phase.

## Out of scope

Telegram and credential-broker (phase 7), full-form passports and
layers (8), capabilities and tool-hub (9), egress-gateway (17), memory,
proactivity.

## Order

First the contract and the service on the scripted model, then
accounting and ceilings, then cliproxy and the live run, channel
commands last. Atomic Conventional Commits in English, one logical
change each, no AI attribution. Do not weaken gates or thresholds.

## Finish

A PR with green CI, merge into `main`, tag `v0.0.6`, a manually
dispatched nightly with a link, and `docs/phases/06-report.md` with the
`sys.*` matrix, the redesign-signal measurements and the manual run's
cost. A short summary back to the owner.

## Acceptance note

"Several days of live work by three people" from the blueprint is the
owner's own acceptance — do not wait for it or simulate it. The task
here is to bring the phase to the state where such work is possible.
