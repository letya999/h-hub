# Glossary

**actor** — the identity recorded on a machine transition: a user,
a service name, or the sweeper.

**ADR** — an accepted architecture decision file under `adr/`, one
decision per file, named `YYYY-MM-DD-<slug>.md`, with invariants each
mapped to a check.

**capability (`cap.<name>`)** — a named capability granted to a user or
worker; the capability catalog is a later phase, so `cap.*` trace IDs
do not resolve yet.

**CAS** — compare-and-swap on `fsm.machines.version`; the loser gets a
`stale` error, never a silent overwrite.

**check / gate** — a repository check under `tools/gates` proven by a
broken example under `testdata/broken/`; `just check` runs all of them.

**contract** — a versioned Go interface package under `contracts/`
shared between a service and its callers, shipped with a fake.

**deadline** — `fsm.machines.deadline_at`: every non-final state arms a
deadline naming the sweep event and actor, unless declared long-lived.

**effect** — an outbox intent committed in the same transaction as the
state change that caused it.

**fsm** — the shared state-machine mechanism: one `Transition`, CAS by
version, history and outbox in the same transaction.

**hubctl** — the operator CLI: `init` provisions install state,
`doctor` verifies it read-only.

**install id** — a random id written once into every service database
by `hubctl init`; proves all databases belong to one installation.

**installation network** — the Docker network assigned to one stack;
spawn-controller inspects and removes only containers attached to it.

**KEK** — the 32-byte key-encryption key stored outside Docker volumes;
an explicit installation backup includes it for restore.

**principal** — an active or disabled human owner stored by identity-hub;
it names one provisioned home space.

**channel link** — identity-hub's binding from a channel-specific
external id to an active principal, confirmed with a one-time code.

**mock outbox** — channel-hub's persisted messages for the private mock
adapter; `hubctl chat` reads them through a scoped contract.

**lease** — `fsm.sweeper_leases`: an atomic owner+expiry row that picks
which instance runs the sweeper.

**long-lived state** — a non-final state with no deadline (e.g.
`active`); it changes only through explicit events, never by sweep.

**machine** — one row of `fsm.machines(kind, id)` driven by a `Spec`
declared in code.

**outbox** — `fsm.outbox` rows carrying effect intents; written
atomically with the transition, delivered later with the same
identity.

**operator alert** — a system fault signal delivered to the operator
through channel-hub's normal durable delivery path, with a stable key
to prevent duplicate sends.

**runtime drain** — an operator request that moves one owned runtime to
`draining` and records Docker removal in the same FSM transaction;
the space remains available for a later runtime claim.

**phase** — a checkpointed slice of the blueprint ending in a tag
(`v0.0.x`) and a report under `docs/phases/`.

**seal** — AES-256-GCM encryption at rest via `shared/seal`; AAD binds
each value to `purpose + kid`.

**schema version** — a durable document's decoding version; new space
specs write version 1 and the reader accepts earlier unversioned specs.

**skill** — Markdown instructions. The skill showcase is the read-only
catalog compiled from platform and user layers. An agent can author
its own skills as workspace files through the file tool.

**shared** — infrastructure-only packages under `shared/`; may not
import services or domain code.

**sweeper** — the shared/fsm loop that reads narrow due-row
projections, re-verifies inside a write transaction, and fires the
deadline event with CAS.

**sweep lag** — the time from the oldest pending machine deadline to
the current sweep pass; task-engine alerts the operator above 30 seconds.

**tenant** — a user of the installation; tenant identifiers flow
through `shared/log` context.

**trace id** — `adr.<slug>.<invariant>`, `cap.<name>`, or `sys.<name>`
referenced by tests; the trace gate resolves each to a real
requirement.

**zone** — a network zone from the descriptor (edge, hub, data);
services sit in exactly one.
