# Phase 6 report — released as v0.0.6

## What passed

- `model-gateway` is a separate service with its own `model` database:
  an OpenAI-compatible relay (`Complete`), usage and route reads
  (`Usage`, `Routes`). A turn needs a run token; owner reads need an
  owner-scoped token. Contract: three ops plus `/healthz`, 754
  production lines against the 2,000 budget
  ([service](../../services/model-gateway),
  [contract](../../contracts/model/v1)).
- Spend accounting is concurrency-safe: a reservation lands as one
  conditional `UPDATE` on the daily counter before any upstream call;
  settle returns the unused reservation and records a `spend_events`
  row. Daily and per-run ceilings refuse rather than clamp — a refusal
  surfaces in the channel as "token budget exceeded", not a silent cut.
  32 parallel reservations against one owner cannot overflow the
  ceiling ([db test](../../services/model-gateway/internal/svc/store_db_test.go)).
  A response without a `usage` block is charged at the estimate;
  abandoned reservations are swept into spend at the estimate after
  the TTL.
- Routes come from the effective passport's `models` section — the
  gateway resolves the spec per call and holds no policy copy. Route
  model rewriting pins `model` to the passport value; callers cannot
  pick an arbitrary upstream model. Route keys load only from the
  `routes/` directory inside the service's keys volume — traversal and
  absolute paths are refused ([test](../../services/model-gateway/internal/svc/upstream_test.go)).
- The scripted model moved behind the same gateway route; every e2e
  turn now exercises the relay. The harness runtime calls only
  `model-gateway` with its run token — no caller headers are
  forwarded.
- Space-zone confinement: space runtimes sit on `hhub-space` and can
  reach only `model-gateway`; e2e probes from inside a live runtime
  reach the gateway and are refused at `postgres`, `identity-hub`,
  `channel-hub` and `cliproxy`
  ([test](../../test/e2e/channel/model_test.go)). CLIProxyAPI itself lives on
  `hhub-egress`, reachable from the gateway only.
- Channel commands: `/usage` reports spent/reserved against both
  ceilings; `/model` lists routes and `/model <route>` writes
  `models.default` through `SetModel` on spawn-controller — the new
  route answers on the same warm runtime with no restart
  ([e2e](../../test/e2e/channel/model_test.go)).
- `sys.secret-leak` now covers the provider secrets: canary bytes
  planted in the cliproxy client-key file and the OAuth session volume
  appear in no service log, env, `docker inspect` output, database
  dump or channel answer ([e2e](../../test/e2e/channel/model_test.go)).
- Operator tooling for the real provider is manual-only and never in
  CI: `just cliproxy-init` materializes the client key and config into
  the two volumes without argv/env/inspect exposure,
  `just models-login` runs the interactive Devin OAuth flow,
  `just cliproxy-up` starts the pinned image
  (`eceasy/cli-proxy-api:v8.1.1@sha256:e2f9c436…88c98`),
  `just chat-real` links a user, switches to the `devin` route and
  prints the recorded spend.

## Manual real-LLM run and its cost

One conversation was run on the real provider through `hubctl chat`
(route `devin`, upstream `cli-proxy-api` with the owner's OAuth
session):

```
== question: say hello in one short sentence
Hello!
== spend
Spent today: 497 of 1000000 tokens; 0 reserved; per-run ceiling 250000.
```

The `spend_events` row for the turn: `reserved 4096 → charged 497,
measured=true` — the gateway reserved the estimate, the upstream
reported real usage and the settle wrote the measured charge.

**Cost of the run: 497 tokens** for one turn (prompt + completion)
against the owner's Devin account. Devin bills the account, not the
deployment; the recorded and bounded unit in h-hub is tokens.

## Invariant matrix

| ID | Status | Evidence |
| --- | --- | --- |
| `sys.model-usage` | Proved | e2e measures a scripted turn end-to-end; Complete without a run token is refused. |
| `sys.model-quota` | Proved | daily ceiling e2e fails the next turn with a readable refusal; parallel db reservations cannot overflow. |
| `sys.model-route` | Proved | `/model` lists and switches routes; an undeclared route is refused; the switch needs no restart. |
| `sys.space-egress` | Proved | in-container probes reach only `model-gateway`; postgres, services and cliproxy refuse. |
| `sys.secret-leak` | Proved | provider-secret canaries absent from logs, env, inspect, dumps and answers. |
| `sys.tenant-isolation` | Proved (5) | Task/channel/spawn e2e refuses owner A reading B's data. |
| `sys.forged-token` | Proved | a forged run token signed by a foreign key is refused before any upstream call. |
| `sys.store-missing` | Proved (5) | Missing database refuses service startup. |
| `sys.stack-restart` | Proved (5) | Stack restart keeps the message-to-answer path. |
| `sys.kill-uncertain` | Proved (5) | Uncertain work is never retried automatically. |
| `sys.late-receipt` | Proved (5) | Settled job remains settled after a late receipt. |
| `sys.sweep-lag` | Proved (5) | Overdue job creates one durable operator alert. |
| `sys.wedge-unblock` | Proved (5) | CLI exits held jobs and live runtimes. |
| `sys.schema-version` | Proved (5) | Future spec version is refused. |
| `sys.service-edge` | Proved (5) | Wrong operation or absent token is refused. |
| `sys.watchdog-stale` | Proved (5) | Stale tick makes spawn-controller unhealthy; alert fires once. |
| `sys.space-no-db` | Proved | runtime has no DB env/mount and its postgres probe is refused on `hhub-space`. |
| `sys.worker-intent` | Deferred to phase 13 | Runtime orphan scenario passes; worker owner is absent. |
| `sys.worker-tenant` | Deferred to phase 13 | Worker owner is absent. |
| `sys.worker-stop-confirmed` | Deferred to phase 13 | Worker owner is absent. |
| `sys.seal-at-rest` | Deferred to phase 7 | Issuer seed is sealed; credential-broker is the phase-7 owner for provider keys at rest. |
| `sys.seam-onesided` | Deferred to phases 10/18 | Channel adapter and tool seam owners do not exist. |
| `sys.package-build` | Deferred to phase 17 | Package owner is absent. |
| `sys.recipe-hosts` | Deferred to phase 17 | Recipe owner is absent. |

Phase 6 additionally flipped previously deferred `deferred:6` ADR rows
to proved with executed refusals: fsm history/outbox atomicity,
deadline transitions on final states, sweep re-reads, the migration
advisory lock, the rendered service-edge table, runtime read-only
config, space-without-database, and the spec-digest image pin.

## Release validation

- `just check` on `phase-06` passed: image builds, lint, vet, unit,
  database and e2e suites, executed trace proofs, full-history
  gitleaks and the descriptor check. The mandatory e2e took
  **125.2 s** of the 10-minute budget; the whole gate took about
  **7 minutes** with a warm build cache.
- The [phase-6 PR](https://github.com/letya999/h-hub/pull/10) passed
  [CI](https://github.com/letya999/h-hub/actions/runs/45216626124):
  `just check` and the published-image N-1 smoke in 5m56s. The
  [release commit on main](https://github.com/letya999/h-hub/actions/runs/45217810381)
  passed again. [Tag v0.0.6](https://github.com/letya999/h-hub/tree/v0.0.6)
  points to that green commit.
- The [GHCR image run](https://github.com/letya999/h-hub/actions/runs/45217841848)
  published all seven `v0.0.6` service and CLI images, model-gateway
  included. The [manually dispatched nightly](https://github.com/letya999/h-hub/actions/runs/45218066562)
  passed `Check` and the fault-scenario matrix in **6m10s**; no alert
  issue was opened.

## Remaining limits

- Usage is accounted in **tokens** only; dollar cost lives on the
  provider side. The estimate (4096) is a flat per-call number until a
  phase adds per-route estimates.
- The gateway relays only non-streaming `/chat/completions`; streaming
  and tool-calling shapes are not yet routed (tool-hub lands in
  phase 9).
- `just check` took about 7 minutes on this workstation — over the
  5-minute readiness target. The overrun is image rebuilds plus the
  larger test suite; no gate was weakened to compensate.
- The cliproxy OAuth session lives in its own volume as designed; it
  becomes a credential-broker object in phase 7.
- `sys.model-usage`'s denial covers token purpose binding; a forged
  token refusal is the `sys.forged-token`/`run-token-bound` proof.

## Redesign signals

- Production Go lines excluding tests: model-gateway **754/2,000**,
  spawn-controller **1,791/8,000**, task-engine **1,718/4,000**,
  channel-hub **1,107/4,000**, identity-hub **867/3,000**,
  harness-runtime **427/6,000**. No size trigger fired.
- Mandatory e2e: **125.2 s** against the 10-minute limit, up from
  70.3 s in phase 5 — the new model tests add two real container
  lifecycles and a quota burn turn.
- One real Devin turn took roughly 20–25 s wall-clock end-to-end
  through channel, engine, runtime, gateway and cliproxy — an order
  above the scripted model; acceptable for a live assistant reply.
