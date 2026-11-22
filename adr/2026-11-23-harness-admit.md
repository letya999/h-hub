# harness-admit: memory-only admission, host-rendered config

## Context

The runtime container sits in the space zone with no database. It must
still refuse runs task-engine never dispatched, and it must get its
Hermes config without any writeable state.

## Decision

harness-runtime keeps an in-memory admit table. Only `SubmitRun` — a
task-engine edge verified by run token — plants a row; a `ReportRun` or
model call for an unknown run-id is refused, and caller headers are
stripped before the model request. The Hermes configuration is
rendered host-side by spawn-controller into the `hhub-render` volume
and mounted read-only; the container env is an exact allowlist
(`HHUB_SUB`, `HHUB_CONFIG`). All toolsets are disabled; the single
scenario is a scripted "answer with text" model call.

## Rejected

- A small admission database in the container: space zone has no
  database route — the table would lie about durability anyway.
- The runtime rendering its own config from env: env is the declared
  surface; a mounted file carries the full passport without growing
  the env list.

## Invariants

- adr.harness-admit.unknown-run-refused — admit-table refusal covered
  by `contracts/harness/v1` conformance suite
- adr.harness-admit.config-readonly — missing rendered config fails
  startup (`TestStoreMissing` covers the missing-input refusal family)
