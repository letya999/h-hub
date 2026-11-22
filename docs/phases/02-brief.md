# Phase 2 brief — documents and shared (v0.0.2)

Source: h-hub-blueprint.md §9 (phase 2), §2.13; owner decisions 2026-11-22.
Work branch: `phase-02`. Tag: `v0.0.2`.

## Scope

- Documents: root README, `docs/glossary.md`, templates
  `docs/templates/service-README.md` and `docs/templates/adr.md`.
- `shared/*` complete: store (pgx + migrations), fsm (+outbox+sweeper),
  httpapi, errors, auth, log, locale (ru/en), clock, proctree,
  lifecycle, seal, testkit. No domain code.
- Tests required by the brief: machine table test, kill between
  transition and outbox, CAS race, Postgres temp schemas.
- ADRs only for shared: store, state machines, secrets at rest. Each
  <= ~60 lines: context, decision, rejected, invariants with named
  checks. Service ADRs stay drafts in their phase PRs.
- `shared/store/admin` moves from lib/pq to pgx (one driver decision).

## Readiness

- shared invariants proven in `trace` (adr.<slug>.<invariant> IDs
  resolve to real ADR invariants and pass).
- `just check` green locally and in CI, under five minutes locally.
- Every gate still proven by a broken example; new checks get new
  fixtures where a violation is possible.
- Commits atomic Conventional Commits; tag `v0.0.2`; merge to main;
  report `docs/phases/02-report.md` with signal measurements.

## Out of scope

Any domain/service code, contract packages, capability catalog
(`cap.*` IDs stay unresolved until it exists), watchdog binary,
spawn-controller lease issuing (fsm provides the lease primitive only).
