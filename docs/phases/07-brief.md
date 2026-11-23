# Phase 7 brief — live Telegram and installation secrets (v0.0.7)

Source: owner brief; `h-hub-blueprint.md` §2.7 channel-hub, §2.9
credential-broker, §9 phase 7; `enforcement.md` sections 3-4;
`owner-decisions.md` — when a threshold or decision is disputed, do not
invent. Work branch: `phase-07` from `main` after v0.0.6. Tag: `v0.0.7`.

## Goal

Live people work through Telegram, installation secrets live in
credential-broker, and no secrets remain in the environment.

## Scope

- `credential-broker`, a new service (budget 5000 lines): the whole
  database is SQLCipher, values are additionally sealed with the
  `shared/seal` envelope KEK outside the volume; static binary — the
  only CGO place in the project; `cipher_version` checked at startup,
  mismatch means refusal. The credential-type registry exists in code
  from day one: API key, OAuth, file. One builtin store — no store
  interface until a second store exists (owner decision). References
  look like `cred://builtin/platform/<name>`. Operations: `Acquire`
  (lease with TTL under two minutes, audited), `Revoke`,
  `ListConnections`; `SetSecret` for the operator path; `Snapshot`
  (`VACUUM INTO`) for backups; audit events in the broker database.
- `hubctl secret set <name>`: the broker operator surface, reachable
  only with the operator key held on the host (the same edge+key model
  as every operator op); value read from stdin or `--from-file`, never
  argv or env. Rotation and object versions behave like any credential.
- Secret migration: the Telegram bot token and the cliproxy key become
  broker objects. channel-hub takes the token as a lease at startup and
  on refresh and holds it in memory only; model-gateway takes the key
  as a lease per call. No env files with secrets remain anywhere —
  checked by a gate.
- Telegram adapter in channel-hub next to mock (mock stays for tests):
  polling with durable offset and the `update_id` idempotency key;
  chat topics give conversations keyed by (channel, peer, chat, topic);
  a status message about run progress edited at most once per 5
  seconds, carrying only the run phase, no content; a photo goes to
  the model as an attachment when the `models` route declares vision,
  otherwise a localized refusal; a `KEY=value`-looking message is
  intercepted, deleted at the provider and answered with the safe
  input path.
- Upgrade/rollback for a live installation: forward migrations, drain
  of active runs, smoke of the new image, pin activation, rollback to
  the previous pin; an unreadable pin refuses startup. Verified on the
  real installation.

## Owner decisions in scope

- `sys.seal-at-rest` was deferred to this phase and must now be proved
  in full: no secret values and no signing seeds in `pg_dump`, the
  broker volume dump, logs, env or inspect. Update `docs/invariants.md`.
- `just check` timing is measured on a warm run without image rebuilds;
  the report gives both numbers — cold with rebuild and warm. Warm over
  5 minutes is a triggered signal: describe cause and proposal.
- `CLIPROXY_IMAGE` is removed: the pinned digest lives in a ref-file
  next to the other images; image substitution through env is not
  allowed.

## Bot token dependency

Creating the bot in BotFather is the owner's task. Until the token
exists, everything is done on the mock channel and everything that can
be closed without Telegram is closed. When the live part is reached,
check whether the token is in the broker (the owner puts it there with
`hubctl secret set telegram-bot-token`); if absent, do not stub or
simulate the live run — record in the report what remains to be done
after the token appears, then ask the owner for the token and the bot
name.

## Readiness

- e2e on the mock channel green, including the secrets move to the
  broker;
- bot token and cliproxy key absent from logs, answers, env, inspect
  and dumps; canaries as in phase 6;
- `sys.seal-at-rest` proved;
- upgrade and rollback pass on the live installation; an unreadable
  pin refuses;
- live part: three users hold a conversation in Telegram (after the
  token appears);
- mandatory e2e within 10 minutes, nightly green.

## Out of scope

Full-form passports and layers (8), capabilities and tool-hub (9),
owner-request mechanism (10), broker forms and pairing (18),
egress-gateway (17). No secret form or OAuth recipes now — operator
input only.

## Process

Atomic Conventional Commits in English, one logical change each, no AI
attribution. Do not weaken gates or thresholds. `just check` green
locally before every commit.

## Finish

A PR with green CI, merge into `main`, tag `v0.0.7`, a manually
dispatched nightly with a link, and `docs/phases/07-report.md` with the
`sys.*` matrix, the signal measurements and a separate section on what
remains before live Telegram if there was no token. A short summary
back to the owner.
