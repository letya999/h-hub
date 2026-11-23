# Phase 5 brief — foundation fault matrix (v0.0.5)

Source: owner overnight brief; `h-hub-blueprint.md` §9 and
`enforcement.md` §§4, 6. Work branch: `phase-05` from the local
`v0.0.4` merge. No LLM, Telegram, or new product service.

## Goal

Prove that the foundation survives faults, rollback, restore and
three-user load before adding a real model.

## Scope

- Prove the invariants owned by the current stack: tenant-isolation,
  forged-token, secret-leak, kill-uncertain, store-missing,
  stack-restart, late-receipt, sweep-lag, wedge-unblock,
  schema-version, space-no-db, seal-at-rest for the issuer seed,
  watchdog-stale, service-edge, and the runtime part of worker-intent.
  Each proved claim needs a runnable scenario and a negative assertion.
- Defer the absent owners, with an explicit destination: seam-onesided
  to the channel adapter in phase 10 and tool seam in phase 18;
  worker-intent (worker part), worker-tenant and
  worker-stop-confirmed to the worker owner in phase 13;
  package-build and recipe-hosts to the package/recipe owner in phase
  17; seal-at-rest for secret values to the credential-broker in phase
  7. A partial proof does not turn the whole `sys.*` ID green.
- Inject faults at each implemented write seam: FSM transition versus
  effect, Docker intent versus create, and channel send versus
  acknowledgement. Verify durable state, idempotent replay and no
  blind retry of an uncertain external effect.
- In CI, apply 0.0.5 migrations and smoke the published 0.0.4.1 image
  against the expanded schema. Fail a merge if N-1 cannot read it.
  Owner amendment: the immutable 0.0.4 binary rejected any newer
  migration number, so the owner approved 0.0.4.1 as the rollback
  baseline. It contains the 0.0.4 code plus the forward-readable
  migrator, published from a green PR and pinned by digest.
- Back up every service database with `pg_dump` and the host KEK;
  restore to an isolated installation and prove the restored stack
  starts and can decrypt its issuer key. Keep artifacts out of git.
- Send several dozen messages in parallel from three provisioned users,
  record completion, failures and elapsed time.
- Add a separate nightly fault-matrix workflow with a visible failure
  alert. Keep `just check` as the single required gate command.

## Readiness

The matrix has passed once; mandatory e2e stays under ten minutes and
`just check` under five minutes. The owner accepts a week of live
nightly runs separately, so do not wait for that week here. End with a
full redesign-signal report and a recommendation on service count.

## Constraints

Use the existing services and their contracts. Preserve phase-4 data
and the v0.0.4 image. Use published v0.0.4.1 for rollback. Do not
weaken a gate or invent a passing result for an invariant whose owning
subsystem is absent.
An unmet current-owner criterion goes in `05-report.md` and prevents a
release tag; continue with independent work.
