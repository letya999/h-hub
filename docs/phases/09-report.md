# Phase 9 report — v0.0.9

Goal of the phase: tool-hub, the admission point for tool calls —
`AdmitCall`/`Projection`/`Invoke` driven by the effective spec's
`grants`, a platform capability catalog, a receipt ledger behind every
decision, and the runtime loop that asks before it calls.

The rework pass on top of the first implementation closed five defects
the owner listed: the probe toolset pretending to be production data,
twelve `cap.*` rows marked `proved` by fixture tools, the missing
toolset↔catalog gate, the revocation criterion without a name, and the
unwritten platform-data ownership.

## Scope delivered

### Capability catalog and gate

- `services/spawn-controller/internal/svc/platform/capabilities.yaml`
  is the platform dictionary of unconditional capabilities; compile
  refuses a `grants.allow` name that is not in it. Layer templates keep
  the grants they need — the catalog lists what *exists*, not what is
  proved.
- The `capabilities` gate (`tools/gates/capabilities.go`) also
  cross-checks `services/tool-hub/internal/svc/platform/toolsets.yaml`:
  every capability a tool declares must be in the catalog, a tool has
  exactly one capability, and the toolset executor stays inside
  `{toolhub, harness, mcp, cli, remote}`. Broken example:
  `testdata/broken/toolset-capability/` (proved in `tools/gates`).
  A tool naming a capability nobody can grant would be unissuable
  forever — the only place the two platform files meet is the gate.
- The trace gate resolves `cap.<name>` against the same catalog and the
  register requires a `cap.*` row per catalog name with status `proved`
  or `deferred:<phase>`.

### tool-hub service

- `contracts/toolhub/v1`: `AdmitCall`, `Projection`, `Invoke` with the
  fake and the conformance suite.
- The service keeps no policy copy (rules 3-4): every op resolves the
  caller's effective spec from spawn-controller per request, decides
  grant → toolset → tool, and writes a receipt row either way.
- `toolsets.yaml` ships exactly one toolset — `probe` with `probe_echo`
  (executor `toolhub`, capability `tools.use`). The earlier 13-tool
  `cap` fixture was removed from platform data: it shipped fake tools
  into every real install.
- Specs activated before tool calling existed render no `tool_url`;
  harness-runtime treats that as no admission edge and runs tool-less
  instead of refusing (`config_test.go` pins it).

### Runtime tool loop

- Each turn fetches `Projection` under the run's digest; the model
  sees the granted tools array; `tool_calls` are dispatched through
  `Invoke`; the run token audience now includes `tool-hub`.

### E2E and honest marking

- `test/e2e/capability`: `TestCapabilityTriples` proves grant →
  projection → invoke on `probe_echo`, and the deny leg (projection
  refused/absent, `Invoke` Forbidden) on the same warm runtime.
- `TestGrantRevocation` proves `sys.grant-revocation`: after a
  `live`-effect grant removal the next call is refused, the tool is
  gone from the projection, and the container id is unchanged — no
  restart.
- The `setGrants` helper retries activate on a moved candidate: the
  CAS stale refusal is by design, the runtime's own job can move the
  platform candidate between preview and confirm.
- `docs/platform-data.md` records which service owns each platform
  file and that the gate holds the link between them.

## sys.* / cap.* matrix

| ID | Phase-9 status | Proof |
| --- | --- | --- |
| `sys.grant-revocation` | proved | TestGrantRevocation — revoked grant refused on the next call, same container |
| `cap.tools.use` | proved | TestCapabilityTriples — real consumer `probe_echo` |
| `cap.config.edit_own` | deferred:11 | consumer lands with workspace/config tools |
| `cap.workspace.read` | deferred:11 | same |
| `cap.workspace.write` | deferred:11 | same |
| `cap.persona.edit_own` | deferred:12 | consumer lands with skills |
| `cap.memory.capture` | deferred:16 | consumer lands with memory |
| `cap.memory.search` | deferred:16 | same |
| `cap.memory.propose` | deferred:16 | same |
| `cap.tools.configure` | deferred, consumer not planned yet | no phase builds the tool |
| `cap.models.select` | deferred, consumer not planned yet | no phase builds the tool |
| `cap.agents.restart_self` | deferred, consumer not planned yet | no phase builds the tool |
| `cap.observability.read_own_logs` | deferred, consumer not planned yet | no phase builds the tool |
| `cap.grants.read_own` | deferred, consumer not planned yet | no phase builds the tool |
| all other `sys.*` | unchanged | see 08-report matrix |

## Signals

- Warm `just check` on the merged tree (all layers cached, stack up):
  **4m26s** — the 5-minute target is met.
- E2E: parallel packages — `channel` ≈104s, `lifecycle` ≈122s,
  `capability` ≈116s (triples 37.7s + revocation 78.1s) — plus the
  sequential `disrupt` tail. CI end-to-end run: 6m58s.
- Runtime starts: the capability package runs each test on one warm
  runtime; its meta-check enforces starts ≤ distinct projections, and
  the revocation test pins the same container id across the revoke.

## Failures and incidents

- **Stale dev database.** The first green-attempt `just check` failed
  every job: tool-hub crash-looped on `applied body changed` — the dev
  `tool_hub` DB carried a CRLF-era migration hash. Recreated the
  disposable dev DB; `checkApplied` behaved correctly.
- **N-1 smoke failed twice.**
  1. A restored spec can legitimately render no `tool_url`; `Load`
     required it, so every restored turn refused. `tool_url` is now
     optional with a config test (`65f6136`).
  2. The drill itself ran the stack without tool-hub, while a fresh
     backup's specs do render `tool_url` — the runtime dialled an
     absent host. The drill now starts tool-hub, restores or creates
     `tool_hub`, and waits on its healthz (`f86d342`), pinned by
     `TestRestoreDrillRunsToolHub`.
- **Sweeper deadline race** (`TestRuntimeDeadlineReapsContainer`,
  failed in CI twice): `busy` is never entered — nothing fires
  `acquire` — so a runtime serves jobs from `warm`, and every
  `ReleaseRuntime` on it is a bad transition that `Touch`es the
  15-minute idle deadline back up. The settle's `release-runtime`
  outbox effect could land after the test pinned `deadline_at` in the
  past, silently re-arming it; the runtime then correctly stayed warm
  ~85s. The test now drains the pending effect before pinning
  (`12c3a55`). Pre-existing race, surfaced by the heavier parallel
  suite — same class as the phase-7 sweeper-starvation defect.
- **`fix-needs-test` gate blind spot.** It inspects `git show HEAD` —
  during pre-commit that is the *parent* commit, and in CI it sees the
  PR merge SHA, so a `fix` commit without a test (`c17f2ab`) was never
  actually checked anywhere. Once discovered, it blocked every new
  commit locally until the commit was re-created with its test
  (`git reset --soft` + force-push, owner-approved in-session).
- Two commits aborted when `invariants.md` edits raced the hook's gate
  run mid-file; sequencing only.

## Owner questions

- Five capabilities sit at `deferred, consumer not planned yet`
  (`tools.configure`, `models.select`, `agents.restart_self`,
  `observability.read_own_logs`, `grants.read_own`): no build phase
  produces their tools. Keep them dictionary-only, or does one belong
  to a named phase?
- `fix-needs-test` checks the parent commit, not the one being
  created — worth an owner decision whether the gate should inspect
  the new commit's staged tree or all branch commits.
- `restore-drill.sh` runs a service subset: credential-broker is
  absent while channel-hub holds Acquire/Revoke edges to it. Intentional
  (the probe never exercises secrets) or a gap to close next time a
  service is added?
- Nightly on main after the tag:
  https://github.com/letya999/h-hub/actions/runs/45568371140

## Out of scope (per brief)

Native tools with side effects — network, files, processes — are
phases 11-15; the probe toolset is the only shipped tool. Owner
request flow (10), memory-hub (16), broker forms and OAuth (18).
