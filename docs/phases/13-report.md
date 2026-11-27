# Phase 13 · Skills

Release `v0.0.13`.

## Implementation

| Item | Implementation and evidence |
| --- | --- |
| Read-only showcase | Spawn-controller compiles `workspace.skills` from the platform and user layers. The parent keeps its names. The adapter requires the rendered showcase at startup. [Layer compiler test](../../services/spawn-controller/internal/svc/skills_test.go) · [Startup test](../../services/harness-runtime/internal/config/config_test.go) |
| Own skills | The `files` toolset offers skill mutations under `persona.edit_own`. Agent skills are Markdown files under `skills/`. The controller mounts this root read-only and writes through the workspace boundary. [File boundary tests](../../services/spawn-controller/internal/svc/skills_test.go) |
| Content scanner | One deterministic scanner checks layer documents and the complete bodies produced by file writes and edits. It reports a file, line and rule for secret-like text, hidden controls or instruction overrides. A refused edit leaves the original unchanged. [Scanner tests](../../services/spawn-controller/internal/svc/skills_test.go) |
| Catalog and guide | Tool-hub pins the catalog in Postgres to the task-engine conversation key. Its system prompt budget is 4,096 bytes. Overflow retains all names and adds a search hint. The guide uses capability summaries and the current tool projection. [Budget and guide tests](../../services/tool-hub/internal/svc/skills_test.go) |

Select `files` and `skills` in the tools passport to expose their
tools. Own skill edits require `persona.edit_own`. Catalog search and
reading require `workspace.read`. Hermes's startup tool probe remains
the boundary for native tools. The container test used the mock channel,
scripted model and upstream Hermes `v0.23.4`. [Hermes image pin](../../deploy/hermes-image.ref).

## Proof matrix

| ID | Scenario and status |
| --- | --- |
| `cap.persona.edit_own` | A grant permitted an own skill edit. Revocation removed the skill mutation tools and denied a direct invocation. |
| `sys.skill-showcase` | The runtime mount was read-only. Removing the showcase caused startup to fail. |
| `sys.skill-scan` | A secret-like replacement failed with finding metadata and left the file intact. |
| `sys.skill-catalog` | Overflow preserved all names. The existing conversation kept its catalog while a new one saw the saved skill. |

[Mandatory skill scenario](../../test/e2e/channel/skills_test.go)

## Measurements and release

The focused unit tests ran in the pinned development container. The
first container run caught scanner finding coordinates disappearing when
the adapter received a localized HTTP error. The adapter now passes
only the file, line and rule to the model. The regression fails with
the previous relay and passes with the fix.

| Local run | Time |
| --- | ---: |
| Focused skills scenario | 66.31 s |
| Focused scenario in the full check | 50.81 s |
| Mandatory e2e | 394 s |
| Warm `just check` in the pinned development container | 492.99 s |

The full check passed compilation, lint, vet, unit and database tests,
mandatory e2e, executed trace proofs, secret scanning and descriptor
validation. The skills scenario emitted all four proof IDs and their
refusal markers. The Phase 12 report records the work remaining against
the five-minute check target.

## Owner questions

Phase 14 needs a choice between direct HTTP over a Unix socket with
Docker's `none` network, or direct TCP on a dedicated internal network.
The worker will keep its isolation policy and generation token on either
transport. [Docker's none network driver](https://docs.docker.com/engine/network/drivers/none/).
