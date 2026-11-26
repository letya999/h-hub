# Phase 12 · Workspace tools and reviewed configuration

## Part A · Phase 11 follow-ups

| Item | Change and proof |
| --- | --- |
| A1 | Backup now captures the persistent Hermes session database and transcripts, with checksums in the archive manifest. The isolated restore drill backed up nine Postgres databases and five Hermes session volumes, restored them, and continued a seeded conversation with a third message that used both earlier markers. A regression covers a session update still in the live WAL. [Restore drill](../../tools/backup/restore-drill.sh) · [Archive test](../../test/e2e/lifecycle/session_backup_test.go) |
| A2 | Mock replies use the outbox delivery receipt. Telegram replies use the prompt's message ID or the callback's request ID and matching delivery. The regression opens two prompts and replays a reply to the first while the second is open. [Owner request tests](../../test/e2e/channel/ownerreq_proof_test.go) |
| A3 | The overdue sweep locks, selects, and transitions the oldest due item in the transaction that records the alert. The regression forces the alert write to fail and checks that the transition also rolls back. [Deadline test](../../services/task-engine/internal/svc/alert_db_test.go) |
| A4 | Recompilation reports the space and unknown capability and refuses the passport. On the local installation, ten spaces had grants referring to `models.select` in their latest submitted layer revisions. No grants were changed. [Compiler](../../services/spawn-controller/internal/svc/compile.go) |
| A5 | The phase 11 report is available in English. [Phase 11 report](11-report.md) |

The affected local spaces in A4 were `space-275d514d66f70293`,
`space-75e1c27f36d074fc`, `space-8a68bfdb63f84075`,
`space-8facb09bf03fb4fa`, `space-afcc9f9d436ba61a`,
`space-befafb4847048b99`, `space-cd027ee04b28d3a8`,
`space-d64b7692e5dfd00c`, `space-e0a22968697499c2`, and
`space-f7a761fc6ab35349`.

## Part B · Phase 12

| Item | Change |
| --- | --- |
| B1 | The phase 11 schema and layer templates already lacked `config.harness.native`. Executor choices now live in `tools.executors`, and the retired config key has a regression test. |
| B2 | All 73 entries in the pinned Hermes inventory have a reviewed effect. The verifier binds the review to the image pin and inventory digest. [Inventory](../../services/spawn-controller/internal/svc/hermes-inventory.yaml) |
| B3 | The compiler requires `accept_native_limits` when `tools` selects `harness` for an effectful native tool. The toolset catalog no longer carries an executor field. The startup probe still requires the actual active list to match the allowed list exactly. Phase 12 selects zero native tools. [Executor test](../../services/spawn-controller/internal/svc/executor_test.go) |
| B4 | `workspace.layout.nodes` and `writable` determine mounts, access, and the explain tree. `/config`, `/inbox`, and `/vault` are always read-only. Layout roots cannot overlap. |
| B5 | The `files` toolset provides list, read, write, edit, mkdir, search, and remove. Tool-hub checks grants and logs the call. Spawn-controller opens only the workspace it owns, using `os.Root`. Workspace files persist across runtime replacement. Limits are 64 KiB per text file, 256 entries per listing or search, 16 path components, and 1,024 path bytes. Remove handles a file or an empty directory. [File boundary tests](../../services/spawn-controller/internal/svc/workspace_files_test.go) |
| B6 | Config compilation and `config_edit` share the validator for the reviewed `config.timeout_ms` leaf. The agent-facing tool rejects other keys, including the retired `harness.native`. [Reviewed config tests](../../services/spawn-controller/internal/svc/config_edit_test.go) |
| B7 | Tool-hub applies grants on each invocation. A tool selection change sends a digest-bound restart request. The active runtime stops accepting new turns once it is idle. The next turn claims a replacement using the same workspace and Hermes session volume. |
| B8 | An admitted file mutation in `ask` waits for the owner. A positive answer authorizes one identical action in that job. `auto` resolves a permitted action without prompting. `bypass` still runs the grant check. |

## Native inventory review

| Effect | Tools |
| --- | ---: |
| `nothing` | 1 |
| `workspace` | 0 |
| `network` | 2 |
| `process` | 11 |
| `external-service` | 59 |

`show_tip` is informational. `web_extract` and `web_search` use the network.
The generic filesystem schemas do not establish a reviewed workspace-only
scope. File writes and patches use `process`; `read_file` and `search_files`
use the conservative class. The same conservative class covers unclear
backend or persistence boundaries in `annotate_preview`, `browser_*`,
`clarify`, `desktop_*`, `drive_preview`, `gui_tour`, `manage_catalog`,
`manage_connections`, `memory`, `react_to_message`, `read_window_below`,
`session_search`, `set_theme`, `setup_beat`, `skill_*`, `skills_list`, and
`todo_list`. The per-tool classification is recorded in the pinned inventory.

## Proof matrix

| ID | Evidence |
| --- | --- |
| `sys.harness-pinned`, `sys.harness-tool-list` | The runtime checks the pin and exact startup tool list. The disposable runtime refuses readiness with an injected native tool or a missing projected tool. [Hermes scenarios](../../test/e2e/lifecycle/hermes_test.go) |
| `sys.owner-request-single-use` | Request-bound delivery receipt, replay on the first request, and independent answer on the second. [Owner request scenarios](../../test/e2e/channel/ownerreq_proof_test.go) |
| `sys.sweep-lag` | Operator alert boundary and transaction rollback regression. [Alert scenario](../../test/e2e/lifecycle/sweep_lag_test.go) · [Deadline test](../../services/task-engine/internal/svc/alert_db_test.go) |
| `sys.passport-live`, `sys.passport-respawn`, `sys.passport-explain` | Live and respawn effects, derived layout, and a tool selection change during an active turn. [Passport scenarios](../../test/e2e/lifecycle/passport_test.go) · [Workspace scenario](../../test/e2e/channel/workspace_test.go) |
| `sys.grant-revocation`, `cap.workspace.read`, `cap.workspace.write`, `cap.config.edit_own` | File operations, path refusals, config validation and projection, per-call grant refusal, and approval modes over a real mutation. [Workspace scenario](../../test/e2e/channel/workspace_test.go) |

## Measurements and open work

| Local measurement | Standalone scenario | Full check |
| --- | ---: | ---: |
| `just check` in the pinned development container | — | 492.86 s |
| Mandatory container e2e | — | 401 s |
| Workspace, approvals, revocation, and respawn scenario | 106.98 s | 126.59 s |
| Hermes respawn from the runtime claim to readiness | 19.16 s | 21.66 s |
| First user message to the answer from the replacement runtime | 25.76 s | 29.51 s |

Both runs used the pinned Hermes container, the mock channel, and the
scripted model on the Windows Docker workstation. The time to answer
includes runtime launch and delivery latency. The active turn finished
before the replacement and is outside this measurement.

The full check passed its unit, database, trace, secret scanning, and
descriptor gates. Its mandatory e2e completed within ten minutes. The
isolated restore drill completed and demonstrated conversation continuity.
The local development stack had applied an early, unpublished version of
the restart-receipt migration. After verifying that the index definition
was identical, the local migration record was reconciled with the final
file so checks could run against the existing installation.

There are no open owner questions for this phase.
