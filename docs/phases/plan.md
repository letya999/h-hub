# Build plan

One phase per release. A phase is closed when its readiness criteria are
green in CI, merged to `main` and tagged. Phase N ships `v0.0.N`; phase 21
ships `v0.1.0`.

Source of truth for scope: `h-hub-blueprint.md` section 9 in the design
archive. This file is the short index.

## Renumbering, 2026-11-26

A review found that `harness-runtime` had been a self-contained agent loop
since phase 3 instead of an adapter over pinned Hermes: no Hermes image and
no version pin existed in the repository. h-hub is a control layer over AI
assistants, not an assistant, so Hermes returns as its own phase.

- Phases 1-10 keep their numbers; their briefs and reports stand unchanged.
- The former phases 11-20 became 12-21.
- The new phase 11 is the Hermes integration.

Readiness criteria from now on name an external dependency by image, version
and digest — not by observed behaviour alone. A stub passes a behavioural
criterion exactly as well as the real component, which is how this gap
survived eight phases.

## Phases

| # | Version | Scope | State |
|---|---|---|---|
| 1 | v0.0.1 | repository, gates, descriptor, `hubctl init`/`doctor` | done |
| 2 | v0.0.2 | documents, `shared/*`, shared ADRs | done |
| 3 | v0.0.3 | spawn-controller, task-engine, harness-runtime, fake identity | done |
| 4 | v0.0.4 | mock channel, identity-hub, scripted model, `hubctl chat` | done |
| 5 | v0.0.5 | fault matrix without an LLM, backup and restore drill | done |
| 6 | v0.0.6 | model-gateway, CLIProxyAPI, spend ceilings | done |
| 7 | v0.0.7 | Telegram adapter, credential-broker, upgrade/rollback | done |
| 8 | v0.0.8 | full-form passports, layers, templates, effect classes | done |
| 9 | v0.0.9 | capability catalog, admission, projection, triples | done |
| 10 | v0.0.10 | owner request, approval modes, `job.waiting_owner` | done |
| 11 | v0.0.11 | **Hermes in the loop: adapter over the pinned image** | done |
| 12 | v0.0.12 | native harness tools, `workspace`, reviewed self-edit, [approval mode end-to-end tests](10-report.md#phase-12-acceptance) | planned |
| 13 | v0.0.13 | skills | planned |
| 14 | v0.0.14 | worker machine, terminal, cellinit, janitor | planned |
| 15 | v0.0.15 | MCP and CLI in a worker, `oci` recipes | planned |
| 16 | v0.0.16 | recipes, connectors, registry search | planned |
| 17 | v0.0.17 | memory-hub | planned |
| 18 | v0.0.18 | egress-gateway, internet, SSH, `package` recipes | planned |
| 19 | v0.0.19 | ingress-gateway, forms, user secrets, webhooks | planned |
| 20 | v0.0.20 | proactive-engine, mandates, attention budget | planned |
| 21 | v0.1.0 | hardening and acceptance on a published digest | planned |

Deferred invariants in `docs/invariants.md` name the phase that will prove
them; those numbers follow this table.
