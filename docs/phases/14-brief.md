# Phase 14 · Workers and terminal

Release `v0.0.14` follows the Phase 13 merge, tag and verification.

## Worker ownership and lifecycle

Spawn-controller owns one Worker state machine and one Docker Engine
API launch path. The `terminal` profile supplies an image, resource
limits, the entry point and its mounts. The controller derives every
host path from the owned identifiers and the effective workspace layout.

A transaction records the worker intent before Docker creation. A live
claim carries a generation and a renewable lease. The state machine
owns deadlines and the reason for stopping. The controller confirms
removal by Inspect. An unresolved stop quarantines the key until
Inspect confirms absence. A janitor removes containers with the worker
ownership label and no registry entry.

## Execution boundary

The static cellinit process serves authenticated `/exec` and
`/healthz`. Spawn-controller installs the generation token in tmpfs
through Docker exec at launch. Tool-hub uses the token for a direct data
plane call. Cellinit listens on a fixed TCP loopback port under Docker's
`none` network. In a separate Unix socket exchange it authenticates
and transfers an already connected TCP socket to tool-hub. The HTTP
call travels directly on that connection.

Spawn-controller leases one transport directory per active generation.
Each worker mounts only its assigned directory. Tool-hub provisions
the directories under its dedicated transport volume at startup.
The controller releases a directory after Inspect confirms absence.

The isolation spec applies the effective workspace layout, read-only
configuration and skill roots, a read-only container filesystem,
no-exec tmpfs, a non-root UID, dropped capabilities and the effective
CPU, memory and PID budgets. The worker mounts only its own space.
Tool-hub has no Docker socket.

## Terminal

The `terminal_exec` tool requires `terminal.exec` and uses a separate
image with Node.js, Python, uv, Git and ripgrep. The request carries a
typed argument vector, bounded environment input and a timeout. Cellinit
bounds output and marks truncation. Losing an in-flight command yields
`uncertain`, with the generation retained until the controller confirms
its stop. Workspace writes persist after execution.

The descriptor owns the worker concurrency ceiling. A full ceiling
returns retryable `capacity`. Images are available before claim.
The cold-start target is a median of 1.5 seconds to readiness.

## Readiness

- Prove `sys.worker-intent`, `sys.worker-tenant`, `sys.worker-stop-confirmed`
  and `sys.space-no-db` against real containers.
- Kill a worker during a command and observe `uncertain`. Verify that a
  tool-hub restart preserves the live worker generation.
- Show that the janitor removes an unregistered worker, one tenant
  cannot access another tenant's data, and terminal writes persist.
- Prove the truncation flag, the capacity refusal and the startup budget.
- Pass `just check` and mandatory container e2e within ten minutes.
- Merge through green CI, tag `v0.0.14`, run nightly and record the
  proof matrix and timings in the phase report.

Hermes native execution tools remain disabled. MCP, CLI, recipes,
memory and network egress stay in their assigned phases.
