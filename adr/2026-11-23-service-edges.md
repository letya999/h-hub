# service-edges: token auth on declared descriptor edges only

Status: accepted (owner decision, 2026-11-23).

## Context

Three new services talk to each other in phase 3. Calls without a
declared identity are how v0 grew ambient trust between components.

## Decision

Every inter-service call carries an Ed25519 service token: the caller
proves its key to identity-hub (`IssueServiceToken`), receives a token
scoped to `(sub, op)`, and the callee verifies the signature against
the distributed public key and the edge table rendered from
`deploy/descriptor.yaml`. An operation between services starts as an
`edge` line in the descriptor; `keydist` renders the table into each
service's keys volume. No edge, no call.
The operator signs a short delegation request; identity-hub checks its
owner, space and target, audits `ActAs`, then issues a scoped owner
token. Operator operations verify that token and its declared edge.

## Rejected

- A shared internal token header trusted on arrival: the verifier is
  then a config file anyone can edit, and identity comes from headers —
  banned by rule 9.
- mTLS inside the compose network: the same edges still need declaring,
  and the CA adds a secrets path this phase does not need.

## Invariants

- adr.service-edges.forged-rejected — `TestForgedToken` (e2e)
- adr.service-edges.edge-required — `TestServiceEdge` (e2e)
- adr.service-edges.rendered-table — `TestRenderEdges` (keydist)
- adr.service-edges.operator-audited — `TestActAsBindsAndAudits` (DB)
