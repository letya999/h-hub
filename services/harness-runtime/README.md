# harness-runtime

## Purpose

Drives the pinned Hermes process for turns admitted by task-engine. Hermes
owns the conversation on the space volume and reopens it after respawn.
[Process identity](../../test/e2e/lifecycle/hermes_test.go#L22) and
[session continuity](../../test/e2e/channel/hermes_session_test.go#L18).

## Zone and neighbors

Space zone. Task-engine admits and cancels runs. The adapter routes Hermes
model requests through model-gateway and h-hub tool calls through tool-hub.
The model receives the space instructions, the conversation and the tool
names offered on this turn, including an explicit empty list.
[Model request boundary](internal/svc/model_context_test.go).
When the effective projection includes skill search, the adapter
includes the conversation's budgeted catalog in the system prompt.
[Skill scenario](../../test/e2e/channel/skills_test.go).

## Owns

The in-memory admission table binds each run to its owner and context.
The controller renders the adapter configuration and mounts it read-only.
[Pinned runtime](../../test/e2e/lifecycle/hermes_test.go).

## Contracts

Implements `contracts/harness/v1` (`SubmitRun`, `CancelRun`); consumes
`task/v1` (`ReportRun`). Fake at `contracts/harness/v1/fake.go`.

## Does not do

Hermes starts with the tools selected by the effective passport. The adapter
refuses startup on an inventory mismatch and constrains each reply to the
tools offered on that request.
[Startup probe](../../test/e2e/lifecycle/hermes_test.go#L69) and
[nested requests](internal/svc/nested_tools_test.go).
Startup requires the layer skill showcase on the read-only render mount.
[Startup config test](internal/config/config_test.go).

## Machines

None of its own; run lifecycle lives in task-engine's Run machine and
the container lifecycle in spawn-controller's Runtime machine.

## Config

`HHUB_SUB`, `HHUB_CONFIG`; loaded once in `internal/config`. The env
allowlist is exact — anything else is absent in the container.

## Runbook

Runs as `hhub-rt-*` containers launched by spawn-controller. Healthy =
config file present and admit table empty or actively streaming. If a
run never settles, check `ReportRun` connectivity to task-engine and
the scripted-model route; a run that dies mid-turn surfaces as
`job.uncertain`, which is observed, never retried.
