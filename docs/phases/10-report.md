# Phase 10 report — v0.0.10

Goal of the phase: the single owner-request mechanism — one contract,
one machine, one delivery path — through which services and agents ask
the human: questions, numbered choices and approvals, with single-use
answers, expiry that never hangs a job, and a conversation that stays
free while the owner decides.

## Scope delivered

### Contract and module

- `contracts/ownerreq/v1`: kinds question/choice/approval, canonical
  answers (number -> option text), a conformance suite every
  implementation passes. `CreateRequest`/`GetRequest` accept run or
  service tokens; `AnswerRequest` is service-token-only. Approval is
  unreachable for run callers — the model can ask and offer choice,
  never approve its own action.
- `services/tool-hub/internal/ownerreq` — 701 production lines against
  the pinned 800 budget, enforced by the new archtest module-line
  budget with a broken example. Machine `pending -> answered|denied|
  expired`; single-use application through the machine; `GetRequest`
  fires an overdue deadline so no reader sees stale pending.
- The toolset exposes `ask`/`choose` under `tools.use`; the tool
  result is `{request_id, state, deadline}` — no links, no nonces, no
  channel internals.

### Delivery and answers

- channel-hub: `DeliverRequest`/`RetractRequest`, the `request_links`
  table binding one open request per target, plain-text answers routed
  before any command dispatch, telegram inline buttons for choices and
  approvals. The newest request supersedes older open links.
- Delivery failures the channel reports as permanent (not-found,
  invalid, forbidden) retire the effect instead of retrying forever.

### Passport leaf

- `config.approvals.mode` — choice/live leaf, domain ask|auto|bypass,
  platform default ask. Auto and bypass resolve approvals at once with
  canonical `yes` and actor `mode:<mode>`; questions and choices still
  wait for the human. A projection test pins that bypass never widens
  grants: without the capability the tools are absent.

### task-engine waiting_owner

- The run journals an `owner-request` event the moment an ownerreq
  tool comes back pending; the job machine moves to `waiting_owner`
  carrying the request id, deadline re-armed to the request's own.
  The turn's final settles the run and frees the conversation lane —
  proven by `TestWaitingLaneIsFree`: the next job claims while the
  first still waits.
- The sweep polls tool-hub: an answer resumes the job into the queue
  with `[owner request answered: X]` composed into the continuation
  input; denial or expiry fails it with a clear reason — never a hang.

## Invariant matrix

| ID | Status | Proof |
| --- | --- | --- |
| sys.owner-request-single-use | proved | TestOwnerAnswerSingleUse |
| sys.owner-request-timeout | proved | TestOwnerRequestExpiry |
| sys.owner-request-self-answer | proved | TestOwnerRequestSelfAnswer |
| sys.seam-onesided | deferred:18 | owner-request part proved: the channel side cannot settle the requester side (single-use/self-answer refusals cross only the contract) |
| cap.tools.use | proved | TestCapabilityTriples (unchanged) |

## Measurements

- ownerreq module: 701 production lines (budget 800, archtest-enforced)
- e2e round trips: question 14.6s, choice 17.1s on the live stack
- proof e2e: approval outcomes 2.8s, self-answer 1.2s, single-use 1.3s,
  expiry 6.3s

## What did not work at first

- OpenAI tool-call arguments arrive as a JSON *string* carrying the
  object; the runner captured raw bytes and every invoke died as
  invalid-arg. Probe never noticed because echo does not parse an
  object. Fixed in harness-runtime with a regression test.
- The compose file is generated; adding the tool edge without
  regenerating it crashed every service on the missing env. The stale
  generated file is a recurrent trap — a gate candidate.
- IssueServiceToken nonces collide when parallel tests mint for the
  same service pair; test nonces now carry the test mark.
- keydist renders edges into the per-service volumes at install time;
  new descriptor edges need a keydist rerun plus a service restart —
  documented here because it cost a debugging loop.

## Owner questions

1. The first turn's final text ("asking the owner") is dropped when
   the job waits — the settle path skips result storage for waiting
   jobs. Should the user see it as an intermediate message?
2. A request whose report is lost (network failure mid-turn) leaves
   the job succeeded while the request expires unanswered — orphan
   requests have no sweeper yet. Phase 18 seam work or earlier?
3. Auto/bypass modes are proven at service level only; a full e2e
   needs passport mutation machinery that phase 11's config tools will
   make natural. Worth a scenario then?
