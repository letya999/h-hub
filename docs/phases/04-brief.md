# Phase 4 brief — mock channel and scripted conversations (v0.0.4)

Source: owner overnight brief; `h-hub-blueprint.md` §§2.2, 2.7, 6.1,
9; `enforcement.md` §§3–4; `owner-decisions.md`. The archive is
read-only. No hermes-hub code, packages, files or configuration may be
opened or copied.

Work branch: `phase-04` from `main`. Tag: `v0.0.4` after the local
merge. No push.

## Goal

Prove the full message → answer path for three equal users through a
private mock channel, real identity binding, task-engine and runtime,
without an LLM or Telegram.

## Scope

- `channel-hub` is its own service, database, role, contract and state
  machines. Only its mock adapter is active. Intake is durable and
  deduplicated by `(channel, update_id)`. Delivery uses
  `queued → sending → sent` with `failed`, `ambiguous` and
  `undeliverable`; an uncertain send is never blindly repeated.
  `mock_outbox` stores everything sent and is read through the private
  mock contract. All effects use transactional outbox rows.
- Render model Markdown to neutral blocks before channel formatting;
  model HTML is never executed. Send at most five text parts and make
  the remainder available as a file, with visible truncation.
- `/start <code>`, `/help`, `/new`, `/status` and `/stop` are thin
  clients of the owning contracts. Unknown commands receive help
  without calling the model. Channel-hub owns no copy of identity,
  job or run state.
- Identity-hub owns principals, channel links and one-time link codes
  with 24-hour TTL. It issues run, link and service tokens and remains
  the sole issuer. Signing seeds are sealed with `shared/seal`; the
  KEK stays outside volumes and backups. `hubctl user add <id>
  [--template <name>]` idempotently creates the principal, space and
  code; `hubctl user disable` deactivates the principal. A retry
  completes an interrupted add. `hubctl doctor` reports split pairs.
- The scripted model accepts hand-written YAML `expect → respond`
  steps, tool calls, provider errors and hangs. An unmatched request
  or unused step fails the scenario. Matching is exact.
- `hubctl chat` injects messages and reads output from the private
  mock adapter for a human operator.

## Readiness

- E2e message → answer for three users created by `hubctl user add`,
  linked through `/start <code>`, with separate conversation and
  delivery data.
- Forged tokens are refused; `sys.service-edge` and
  `sys.tenant-isolation` cover the channel and identity boundaries.
- A long answer visibly truncates after five parts and stores its
  remainder as a file. Unknown commands do not invoke the model.
- `just check` green locally and in CI, under five minutes locally;
  mandatory e2e under ten minutes. Every new gate has a broken fixture.

## Out of scope

Telegram, credential-broker, user secrets, full-form passports,
tool-hub and real tool execution, memory, proactivity and workers.

## Process

One end-to-end path first. Add contract edges before service calls;
use verified tokens for tenant scope. Expand migrations first and
defer drops to the next release. Atomic Conventional Commits in
English, no AI attribution or push. Finish with
`docs/phases/04-report.md`, redesign-signal measurements, a local
merge into `main` and tag `v0.0.4`.
