# spawn-runtime: intent-first Docker launch behind one spec digest

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

## Rejected

- Storing the spec JSON next to the container as labels: the digest
  already identifies it; labels are mutable and unbounded.
- Synchronous create inside `EnsureRuntime`: a slow pull would hold the
  task-engine edge open; the poll-until-warm contract keeps it bounded.

## Invariants

- adr.spawn-runtime.intent-first — `TestWorkerIntent` (e2e)
- adr.spawn-runtime.space-no-db — `TestSpaceNoDB` (e2e)
- adr.spawn-runtime.digest-pins-image — effective-spec digest over the
  resolved image sha (checked in `svc` compile path)
