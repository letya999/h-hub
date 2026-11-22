# AGENTS.md - h-hub

h-hub is a security, control and reliability layer over AI assistants
(harness-agnostic, Hermes first) for several equal users. Channel 0.1 is
Telegram. Hermes thinks; h-hub makes it safe and reliable. One host, Linux,
Docker, Postgres, ru/en.

Every rule below is held by an automated check in `just check`. A rule
without a check is not a rule. Lowering a threshold, adding a baseline
exception, adding an external module or changing an acceptance scenario is
an owner-only change in its own commit (CODEOWNERS).

## Work in phases
- Work only inside the current phase brief (`docs/phases/NN-*.md`). Out of
  scope findings go to the phase report, not into code.
- A phase is done when its readiness criteria are green in CI, not when the
  code is written.
- Code is written from scratch. Never import, copy or port hermes-hub (v0)
  code, packages, files or configs; lessons give the design, not the code.

## Product
1. A feature not on the vision page does not exist. Widening the vision is
   an owner decision. A foreign product in the core (hosting, media, IdP) is
   forbidden.
2. One thin end-to-end path first (message -> answer with e2e), width only
   on top of a green core.

## One instance of everything
3. Every state, rule, mechanism and concept has exactly one owner and one
   implementation. Before writing code answer: where is the second copy and
   who reconciles them? If it exists, move it to the owner.
4. Policy is the effective passports with one digest. Services keep no
   policy copies with their own version.
5. Rights come only from `grants` in the layer chain: unconditional
   capabilities from the catalog. The agent neither sees nor edits its
   grants.
6. A new concept needs an owner decision and a glossary entry.

## Service boundaries
7. A service is its own binary, its own database and role in the shared
   Postgres (credential-broker: its own SQLCipher volume), its own contract,
   its own state machines. It imports only `shared/*`, `contracts/*` and its
   own `internal`.
8. `shared` is infrastructure only: store, fsm and outbox, http, errors,
   auth, log, locale, clock, proctree, lifecycle, seal, testkit. No domain
   code.
9. Between services: HTTP contracts with a token, a timeout and an
   idempotency key. No shared files, foreign databases, foreign mounts or
   identity from headers. The only exception is the SELECT-only
   `watchdog_ro` role on the `fsm` schema. Space-zone containers never reach
   a database.
10. `hubctl` is a thin contract client without domain logic; only
    `cmd/hubctl/install` (`init`, `doctor`) opens Postgres.
11. Only spawn-controller manages containers, through the Docker Engine
    API. Nobody else gets the Docker socket.

## Code
12. A type owns one state machine or one contract. Limits: function 60
    lines, file 500 lines, type 20 methods, struct 20 fields, 5 parameters.
13. Never drop an error silently. Missing input is a refusal, not a quiet
    mode.
14. Service config is a short declared env list (max 10 `HHUB_*`), read in
    one package and passed as a struct. Agent behaviour comes from passports.
15. A new path deletes the old one in the same change. The words `legacy`,
    `compat` and `fallback` do not appear in our identifiers or comments.
16. State changes only through `fsm.Transition` and lives in
    `fsm.machines`, never as a column of a domain table; a side effect is an
    outbox row in the same transaction.
17. Every non-final state has a deadline and a next actor. `uncertain` is
    never retried automatically.
18. User-visible strings only through the ru and en locale catalogs.

## Tests
19. Every capability, toolset and tool has a mandatory e2e on the mock
    channel with the scripted model (no LLM) and real containers. A red e2e
    blocks merge.
20. Tests check behaviour at a boundary: contract, state machine, rights
    decision. Tests are parallel, without `time.Sleep`, env or `t.Skip`.
21. A bug fix comes with a test that failed before the fix.
22. Coverage percentage is not a goal.

## Process
23. All checks run with one command, `just check`, locally and in CI. A
    green local run is required before a commit.
24. `//nolint` only with a specific linter and a reason.
25. One document per service: README from the template. A claim about
    behaviour links to an existing test.
26. The workstation is Windows; everything that runs is Linux. Checks and
    tests run in the pinned `hhub-dev` container (sources mounted read-only,
    caches in Docker volumes), the stack runs in Docker, data lives in
    volumes. No Windows code: no `*_windows.go`, no `.ps1`, no
    platform-specific test skips.
27. Upstream Hermes is not patched; a rare exception ships with an upstream
    issue and PR.
28. A decision is one ADR in `adr/` (date-slug, about 60 lines): context,
    decision, rejected options, critical invariants with proof level. No
    "proposed" status; only the owner accepts. A service ADR stays a draft
    in its PR until the service phase.
29. `trace` has three ID classes: `adr.<slug>.<invariant>`, `cap.<name>`,
    `sys.<name>`. The link goes test -> ID (`testkit.Req`, `requires:`).
    The gate fails on an ID without an executed check and on a link to an
    unknown ID. An ID is mandatory once its owning service is in the
    descriptor.
30. Migrations only expand the schema and stay readable by the previous
    release; drop and rename happen one release later; no down migrations.
31. A call between services carries a service token; a new operation
    between services starts with an edge in the descriptor.
32. Never import or copy hermes-hub code (module banned in `go.mod`).

## Git
- One protected `main`; every change via a green PR.
- Conventional Commits in English (`feat`, `fix`, `refactor`, `docs`,
  `test`, `chore`, `perf`). One logical change per commit.
- No AI attribution and no `Co-authored-by` lines for AI tools.
- No secrets, dumps or generated artifacts in git (size gate ~256 KB,
  gitleaks over history).

## Commands
- `just check` - every gate, the same locally and in CI; on Windows it only
  starts the `hhub-dev` container (pwsh shell, never Git Bash).
- `hubctl init` - create databases, roles, keys, KEK (idempotent).
- `hubctl doctor` - read-only installation check.
