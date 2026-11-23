# Phase 5 brief — foundation fault matrix (v0.0.5)

Source: owner overnight brief; `h-hub-blueprint.md` §9 and
`enforcement.md` §§4, 6. Work branch: `phase-05` from the local
`v0.0.4` merge. No LLM, Telegram, new product service or push.

## Goal

Prove that the foundation survives faults, rollback, restore and
three-user load before adding a real model.

## Scope

- Run real scenarios for every `sys.*` invariant in the enforcement
  list: tenant-isolation, forged-token, secret-leak, kill-uncertain,
  store-missing, stack-restart, late-receipt, seam-onesided,
  sweep-lag, wedge-unblock, schema-version, worker-intent,
  space-no-db, seal-at-rest, watchdog-stale, service-edge,
  worker-tenant, worker-stop-confirmed, package-build and
  recipe-hosts. Record an absent owning subsystem as unproved; do not
  claim a passing test for a future service.
- Inject faults at each implemented write seam: FSM transition versus
  effect, Docker intent versus create, and channel send versus
  acknowledgement. Verify durable state, idempotent replay and no
  blind retry of an uncertain external effect.
- In CI, apply 0.0.5 migrations and smoke the published 0.0.4 image
  against the expanded schema. Fail a merge if N-1 cannot read it.
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
and the v0.0.4 image for rollback. Do not weaken a gate or invent a
passing result for an invariant whose owning subsystem is absent.
An unmet criterion goes in `05-report.md` and prevents a release tag;
continue with independent work.
