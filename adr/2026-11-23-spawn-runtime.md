# spawn-runtime: intent-first Docker launch behind one spec digest

Status: accepted (owner decision, 2026-11-23).

## Context

Space containers must be traceable: what was asked, what was compiled,
what actually runs. v0 created containers ad hoc and later could not
say which spec a worker embodied.

## Decision

spawn-controller is the only Docker Engine API client. `CreateSpace`
compiles the minimal passports (stack, config, models) into an
effective spec whose digest pins the resolved runtime image
`name@sha256`. Before `docker create`, an `intents` row lands in the
spawn database; the Runtime machine then walks `absent -> starting ->
warm -> busy -> draining -> absent` (`failed`/`quarantined`/`missing`
are operator exits). Launch hardening: pinned digest, `cap-drop ALL`,
read-only rootfs, non-root uid, limits, tmpfs, network per descriptor
edges only. Space-zone containers get no Postgres route and no
connection env.
The current tuning values are warm TTL 15 minutes to reuse a started
container, busy deadline 11 minutes to bound a stuck turn, starting
deadline 90 seconds for image/start latency, and draining deadline 60
seconds for cleanup. These are adjustable policy values, not proof
thresholds; phase 3 carries the owner-ratified defaults in code.

## Rejected

- Storing the spec JSON next to the container as labels: the digest
  already identifies it; labels are mutable and unbounded.
- Synchronous create inside `EnsureRuntime`: a slow pull would hold the
  task-engine edge open; the poll-until-warm contract keeps it bounded.

## Invariants

- adr.spawn-runtime.intent-first — `TestRuntimeIntent` (e2e)
- adr.spawn-runtime.space-no-db — `TestSpaceNoDB` (e2e)
- adr.spawn-runtime.digest-pins-image — `TestSpecDigestBindsImage`
