# h-hub

License: **AGPL-3.0-only**. See [LICENSE](LICENSE) and
[THIRD_PARTY.md](THIRD_PARTY.md).

A security, control and reliability layer over AI assistants —
harness-agnostic, Hermes first. Hermes thinks; h-hub makes execution
safe and reliable for several equal users, initially through Telegram,
on one Linux host with Docker and Postgres.

## Layout

- `deploy/` — descriptor.yaml is the single inventory of services,
  zones, resources and edges; compose.yaml is generated from it.
- `cmd/hubctl` — install, doctor and operator CLI.
- `internal/` — repo-only internals (descriptor model).
- `shared/` — infrastructure packages only: store, fsm, httpapi,
  errors, auth, log, locale, clock, proctree, lifecycle, seal, testkit.
  No domain code, no service imports.
- `contracts/` — service contracts with fakes (later phases).
- `services/` — one directory per service (later phases).
- `tools/` — repository gates, meta-test, proof runner, archtest.
- `docs/` — glossary, templates, phase briefs and reports.
- `adr/` — accepted architecture decisions (owner-controlled).

## Checks

`just check` is the single local/CI gate set. On Windows it only wraps
PowerShell + Docker Desktop: the pinned `hhub-dev` container runs the
same recipe as CI. `just check-fast` is the pre-commit subset.

```powershell
just check       # full gates incl. Postgres-backed tests
just check-fast  # offline subset (pre-commit)
just up          # bring the compose stack up
just hubctl init
just hubctl doctor
```

## Rules

- Atomic English Conventional Commits; no AI attribution.
- One owner per state, rule, mechanism and concept.
- Every gate is proven by a minimal broken example under
  `testdata/broken/`.
- See `AGENTS.md` for the enforced contract and `docs/glossary.md`
  for vocabulary.
