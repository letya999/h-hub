# Phase 11 · Hermes inside the space

## Fixes after v0.0.11

Hermes could restore the installation's system description from a saved session and send it to the model. Each model request now receives the space instructions from `soul.md` and `agents.md`, together with the tool list for that call. An empty list is `[]`. User messages, tool calls, and their results retain conversation context. [Model boundary test](../../services/harness-runtime/internal/svc/model_context_test.go), [pinned Hermes test](../../test/e2e/lifecycle/hermes_test.go).

On successful Telegram delivery, the completed answer replaces the progress message. If the answer is ready first, the channel sends it directly. Quotes, angle brackets, and ampersands are delivered as plain text. [Tests for both delivery orders and retries](../../services/channel-hub/internal/svc/telegram_reply_db_test.go).

During verification, `TestJobToAnswer` observed a `succeeded` job with an empty answer. The job finished between separate reads of its result and state. `GetJob` and `ListJobs` now fetch both in one PostgreSQL query. The regression completes a job from another connection during each contract call. [Answer snapshot test](../../services/task-engine/internal/svc/job_snapshot_db_test.go).

Mandatory e2e in local `just check` took 352 seconds after the context fix and 374 seconds after the job snapshot fix, below the 600-second limit. Both runs used the pinned Hermes image.

## v0.0.11 release

Hermes Agent produces the answers in `v0.0.11`. h-hub admits a turn under the passport, provides model and tool access, reads Hermes events, and returns the result through the channel. The phase started from `v0.0.10`. [Phase PR and CI results](https://github.com/letya999/h-hub/pull/23/checks).

## Image and process

The official NousResearch stable release `v0.23.4`, dated 24 November 2026, is pinned. It was the latest stable release when selected. The release tag and `stable` tag referred to the same OCI index. The build checks that Docker publishes the pinned digest for the release tag. [Hermes release](https://github.com/NousResearch/hermes-agent/releases/tag/v0.23.4), [image pin](../../deploy/hermes-image.ref), [build check](../../tools/hermes/verify-pin.sh).

| Identifier | Verified value |
| --- | --- |
| OCI index | `sha256:ef0650d523ee1b5ab32c779acabc446d9936869a9932591553688a62bcf0c42f` |
| Linux amd64 manifest | `sha256:3bc37fd8546e90fc41fe0d29511b4052840c281617cbcec6093bfe03d86a4ad9` |
| Upstream revision in the image | `8fc0374447313e276f6b9a2764bfc750190c966a` |

Inside the space container, `docker-init` starts the h-hub adapter, which starts `/opt/hermes/.venv/bin/python -P -m hermes_cli.main gateway run`, the upstream CLI entry point. In an early measurement at 1 CPU and 256 MiB with swap available, the native API opened after 13.61 and 13.62 seconds. The standard startup script with a separate bytecode directory took 16.74 and 17.59 seconds under the same limits. [Upstream entry point](https://github.com/NousResearch/hermes-agent/blob/8fc0374447313e276f6b9a2764bfc750190c966a/pyproject.toml#L594), [process test](../../test/e2e/lifecycle/hermes_test.go).

## Seven steps

| Step | Behavior | Evidence |
| --- | --- | --- |
| 1. Pin | The build rejects an image without a digest or with the digest of another existing release. | [Pin check](../../tools/hermes/verify-pin.sh) |
| 2. Launch | spawn-controller starts a runtime for a space, agent, and digest. The host renders configuration and mounts it read-only. The process has an explicit environment, UID `65534:65534`, `cap-drop ALL`, a read-only root filesystem, tmpfs, and memory, CPU, and PID limits. Only declared network edges are available. | [Container specification](../../services/spawn-controller/internal/backend/docker.go), [network boundary test](../../test/e2e/channel/model_test.go) |
| 3. Driver | `SubmitRun` starts a turn. The adapter reads Hermes `/v1/runs/{id}/events` and relays events to task-engine `ReportRun`. `GetRun` reads the journal. `CancelRun` stops the native turn. The `contracts/harness/v1` contract is unchanged. A Hermes refusal reaches the job with its reason. | [Driver](../../services/harness-runtime/internal/hermes/runs.go), [end-to-end refusal](../../test/e2e/channel/hermes_rejection_test.go) |
| 4. Sessions | Hermes history on the space volume keeps the conversation binding. After physical container replacement, a third answer used the first two messages. Two conversations for one owner and one for another held three simultaneous model reservations. | [Sessions, respawn, and concurrency](../../test/e2e/channel/hermes_session_test.go) |
| 5. Level B | Before opening the public port, the startup probe compares actual tool names in Hermes's model request with the effective passport. During a turn, proposed model calls are checked against the tools in that request. | [Startup probe](../../services/harness-runtime/internal/svc/inventory.go), [nested-session test](../../services/harness-runtime/internal/svc/nested_tools_test.go) |
| 6. Disabled by default | Passport changes produced `[] → [todo_list] → []` in Hermes. Conversation worked with an empty list. After a grant, the model invoked native `todo_list`, received a `tool` result, and returned an answer through the channel. | [Passport e2e](../../test/e2e/lifecycle/hermes_test.go) |
| 7. Upstream check | The official image is unchanged. The h-hub extension uses the normal plugin mechanism. The build rejects an undeclared patch or drift between the pinned image's inventory and the repository file. | [Build check](../../tools/hermes/verify-pin.sh), [inventory probe](../../tools/hermes/inventory.sh) |

A race appeared when a job completed concurrently with a new passport activation. Runtime release and stops caused by a passport, operator, or space deletion now coordinate on the FSM row. The regression reproduced a PostgreSQL `stale` error before the fix and passed for all three initiators afterward. [Concurrent stop test](../../services/spawn-controller/internal/svc/drain_runtime_db_test.go).

In step 4 CI, the main suite passed but the N−1 probe failed with `ReadOutbox: unavailable`. The pinned rollback build of `identity-hub` does not put the new conversation field in the token. Task-engine now confirms the already-bound conversation in the first `ReportRun` response. It derives the key from owner, space, and conversation after matching the token to the recorded run. No extra call or token version is needed, and the previous session key is retained on the volume. A local rollback test restored nine databases and obtained an answer from real Hermes in 105.61 seconds, including backup and temporary stack preparation. [Original CI failure](https://github.com/letya999/h-hub/actions/runs/45766352438), [journal receipt test](../../services/task-engine/internal/svc/conversation_receipt_db_test.go).

## Passports and trust boundary

Upstream's `tool_execution` handler can continue after an exception, so this Hermes version supports control level **B** here. Hermes executes the allowed native tools. h-hub tools pass through tool-hub. The Hermes API and internal bridge listen on loopback. Identity comes from the admitted-run table and a verified native `admission_id`. Caller headers do not set owner, digest, or rights. [Pinned middleware](https://github.com/NousResearch/hermes-agent/blob/8fc0374447313e276f6b9a2764bfc750190c966a/hermes_cli/middleware.py), [adapter bridge](../../services/harness-runtime/internal/svc/relay.go).

Hermes's static toolset list differs from schemas actually sent to the model. The host disables `tool_search`. The startup probe opens one service turn through the native API, compares its schemas, and deletes that service session. It spends no external provider tokens. A nested session may have fewer tools. For example, with parent rights to `delegate_task` and `todo_list`, a worker role requesting the `todo` set received only `todo_list`. An isolated schema check across both roles, all 60 toolsets, and inheritance under three passports found no extra tool in 366 lists. [Host configuration](../../services/spawn-controller/internal/svc/hermes_config.go), [upstream nested-role lists](https://github.com/NousResearch/hermes-agent/blob/8fc0374447313e276f6b9a2764bfc750190c966a/tools/delegate_tool_toolsets.py).

The inventory covers 73 tool names, 60 toolsets, and 22 platforms. Each name was probed separately in an isolated profile without external credentials. Of the requests, 35 returned exactly the requested schema and 38 returned an empty list, including `browser_cdp`, `discord`, `image_generate`, and `kanban_create`. No extra tools appeared. A runtime refuses startup if a passport requires a missing schema. [Release inventory](../../services/spawn-controller/internal/svc/hermes-inventory.yaml).

## Proofs and timing

The register contains 37 system invariants. Execution checks passed for all 30 marked `proved`. Seven remain `deferred`. `sys.space-no-db` already checks the space container, while its full worker scenario is deferred. The other six concern workers, package builds, recipe network rules, and preventing an adapter from implementing both contract sides. [Full register](../invariants.md).

| Invariant | Check | Evidence |
| --- | --- | --- |
| `sys.harness-pinned` | Missing and substituted digests are rejected during policy activation and launch. Docker confirms the image and a separate Hermes process. | `TestHermesPinnedProcess`, `Req`, and six executed `Deny` checks. [Test](../../test/e2e/lifecycle/hermes_test.go) |
| `sys.harness-tool-list` | An extra or missing schema stops the process before it accepts a user turn. A new process loses a tool after its grant is revoked. | `TestHermesPassportTools`, `Req`, and two executed `Deny` checks. [Test](../../test/e2e/lifecycle/hermes_test.go) |

Every commit passed `just check` in the pinned Linux container before publication. Total time includes build and stack preparation. Mandatory e2e had a separate 600-second limit.

| Change | Local `just check` |
| --- | ---: |
| 1. Image pin, cold build | 874.5 s |
| 2. Container launch | 386.5 s |
| Acceptance scenario text replacement, approved by owner | 361.68 s |
| 3. Native driver | 418.75 s |
| 4. Session and respawn | 468.35 s |
| 5. Level B and startup probe | 489.97 s |
| 6. Tool list | 579.26 s, including 400 s e2e |
| Base passport size | 533 s, including 342 s e2e |
| Compatibility with pinned rollback `identity-hub` | 567.89 s, including 377 s e2e |
| 7. Patch protection, final run | [PR check results](https://github.com/letya999/h-hub/pull/23/checks) |

In GitHub Actions, mandatory e2e took 214 seconds after the base limit changed. In the same run, the restored stack with the older `identity-hub` did not answer in time. The next commit fixed conversation binding and its GitHub e2e took 274 seconds. The rollback check restored nine databases, opened the broker snapshot, and answered through the mock channel. [Base-limit check](https://github.com/letya999/h-hub/actions/runs/45775588065), [binding and rollback check](https://github.com/letya999/h-hub/actions/runs/45776873369).

In release measurement `just load`, with a 384 MiB base limit, three users received all 36 distinct answers in 39.3 seconds, 12 each. All 36 jobs succeeded with answer text. Times below come from the task-engine journal. The model provider was scripted and the Hermes process was real. [Load scenario](../../test/e2e/channel/load_test.go).

| Interval | Median | p95 |
| --- | ---: | ---: |
| Turn start → first text | 0.183 s | 0.224 s |
| Turn start → successful result | 0.296 s | 0.422 s |
| Job queued → result, including queue and cold start | 32.792 s | 35.666 s |

In a separate e2e at 384 MiB, the first mock-channel answer arrived in 27.01 seconds. The second answer in the same conversation took 2.01 seconds. After physical replacement of the Hermes container, it recalled both markers from the earlier messages in 22.01 seconds. The model-gateway contract returns the entire accounted message. External LLM latency was outside these measurements. [Conversation test](../../test/e2e/channel/hermes_session_test.go).

## Owner decisions and open questions

The owner set the base passport memory limit to 384 MiB. On Linux without swap, four runs at 256 MiB were killed on the first turn with `OOMKilled=true` and exit code `137`. This happened with the standard configuration, two allocator settings, and 32 optional components disabled. At 384 MiB with no swap, standard Hermes answered twice and peak usage was 292.52 MiB. Upgrading from `v0.0.10` changes only the original base layer's default limit. Explicit owner values, including an explicit 256 MiB, remain. Tests covered existing spaces and a new container with Docker's actual 384 MiB limit. [Base-layer upgrade](../../services/spawn-controller/internal/svc/upgrade_image.go), [explicit-limit tests](../../services/spawn-controller/internal/svc/upgrade_image_db_test.go).

Hermes's built-in repetition guard stops an answer of 21,000 identical Cyrillic `Ж` characters with `repetition loop`. With owner approval, a separate commit replaced the old acceptance filler with 7,000 unique Cyrillic triples. The HTML prefix, 21,000 characters, 42,000 filler bytes, five-message checks, file check, and full scenario consumption remain. Separate tests cover the expected refusal of the original repetition and character-by-character delivery of the valid long answer. [Acceptance scenario](../../test/e2e/channel/scripted_channel_test.go), [repetition guard](../../test/e2e/channel/hermes_rejection_test.go), [complete delivery](../../test/e2e/channel/hermes_delivery_test.go).

The following questions were outside phase 11.

- **Backup.** Respawn preserves a conversation while the space volume exists. Whole-installation backup does not yet include Hermes history volumes. Their backup and restoration order needs a decision. [Backup contents](../../tools/backup/backup.sh)
- **Channel approvals.** In a separate run, a repeated answer in channel-hub attached to the next approval request. The same implementation exists in `v0.0.10`. Routing needs a separate fix. [Approval scenario](../../test/e2e/channel/ownerreq_proof_test.go)
- **Historical passports.** The local database contains `models.select`, a right absent from the `v0.0.10` catalog. The owner must decide the intended rights before those spaces are recompiled.
- **Overdue alert.** In one local full run, `TestSweepLagOperatorAlert` did not observe an alert within 30 seconds, although the job reached `quarantined`. Selecting the oldest deadline and sweeping overdue jobs use separate queries, allowing a race if the deadline moves between them. Three isolated reruns passed in 1.00, 0.68, and 0.68 seconds. The sweep order and test timeout were already present in `v0.0.10`. [Scenario](../../test/e2e/lifecycle/sweep_lag_test.go), [alert path](../../services/task-engine/internal/svc/alert.go).
