# Phase 8 brief: full-form passports (v0.0.8)

## Goal

Replace the minimal phase-3 passport with the full-form layer
mechanism: six documents per layer, an ordered role chain in data, a
schema registry with consumers and effect classes, set-wise compile
with cross-validation, a sha256 digest, atomic CAS activation,
preview/confirm, rollback, and stored explain.

## Scope

- Six documents per layer: `stack`, `config`, `models`, `workspace`,
  `tools`, `grants`. One document format (JSON object with leaf
  paths), one inheritance chain. No second mechanism.
- Two roles in the chain — `platform` then `user` — where a lower
  layer may only narrow the effective result of the upper. The chain
  is an ordered list of roles in the registry data; adding `org` or
  `agent` later is a new role entry, not a schema or format change.
  `org`/`agent` themselves are not built in this phase.
- Layer templates ship as folders under `services/spawn-controller/
  platform/templates/`: `assistant` and `restricted`, each carrying
  the six documents plus workspace files (`soul.md`, `agents.md`).
  Grant ceilings from owner decisions: `assistant` gets `tools.use`,
  `memory.capture`, `memory.search`, `memory.propose`,
  `workspace.read`, `workspace.write`, `config.edit_own`,
  `persona.edit_own`, `models.select`; `restricted` gets `tools.use`,
  `memory.search`, `workspace.read`. `terminal.exec`, `ssh.exec`,
  `connectors.manage`, `mcp.remote.use`, `mandates.*` are never
  granted by default.
- Schema registry: one row per leaf — path, field class
  (`invariant`, `narrow`, `choice`, `user_data`, `keyset-narrow`),
  consumer, effect class (`live`, `respawn`, `recreate`). An
  unproven `live` is applied as `respawn`. A leaf without a consumer
  fails the `passport-schema` gate — that is a readiness criterion.
- Compile: whole layer set with cross-validation between documents;
  `digest = sha256(canonical spec)` without service metadata;
  activation is one transaction with a CAS on the space row;
  `preview` has no effects and `activate` applies exactly the shown
  candidate; `rollback` re-validates through the same path;
  `explain` is persisted and reports which layer supplied each leaf.
- Phase-3 minimal passports migrate to this mechanism: no second
  path, no old compiler left.

## Readiness

- Editing a leaf yields a new digest and exactly the declared effect
  class: `live` applies without restart, `respawn` restarts the
  runtime, `recreate` recreates the space world — each proven by a
  scenario.
- A leaf without a consumer fails the gate (broken fixture).
- A lower layer cannot widen the upper: the attempt is refused, a
  scenario exists.
- `confirm` after the candidate changed is rejected; `rollback`
  returns the previous digest and re-passes the same checks.
- `/model` from phase 6 keeps working through the new mechanism.
- Mandatory e2e under ten minutes; nightly green.

## Out of scope

Capabilities and tool-hub (9), owner-request flow (10), native tools
(11), broker forms and OAuth (18).
