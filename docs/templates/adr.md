# <slug>: <decision title>

## Context

What forces the decision. Keep it to the facts that matter.

## Decision

The single decision, stated so it can be checked.

## Rejected

- <alternative>: why it loses.

## Invariants

Each invariant gets an `adr.<slug>.<name>` ID and a named check —
a test, e2e, linter or gate that proves it. Tests reference the ID via
`testkit.Req`; the trace gate fails when a referenced ID is not
declared here.

- adr.<slug>.<invariant> — `TestCASRace` (a real check name: test,
  e2e, linter or gate; the docs gate requires it to exist)
