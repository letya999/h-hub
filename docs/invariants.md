# Invariant proof register

`proved` names one scenario that must run and reject the forbidden path.
`deferred:N` means the full claim waits for phase N; `deferred, consumer not
planned yet` means no phase builds the consumer. A partial test does not
make a deferred claim proved. Scenario links point to tests; the trace gate
checks their `testkit.Req` and `testkit.Deny` markers against executed tests.

| ID | Status | Owner | Scenario | Forbidden path that must be rejected |
| --- | --- | --- | --- | --- |
| `sys.harness-pinned` | proved | spawn-controller | [TestHermesPinnedProcess](../test/e2e/lifecycle/hermes_test.go#TestHermesPinnedProcess) | A tag without a digest or any substituted digest must be refused before launch. |
| `sys.harness-tool-list` | proved | harness-runtime | [TestHermesPassportTools](../test/e2e/lifecycle/hermes_test.go#TestHermesPassportTools) | A runtime with a missing or additional active tool must refuse to become ready. |
| `sys.tenant-isolation` | proved | channel-hub | [TestMockChannelThreeUsers](../test/e2e/channel/channel_test.go#TestMockChannelThreeUsers) | Owner A reads owner B's outbox. |
| `sys.forged-token` | proved | identity-hub | [TestMockChannelThreeUsers](../test/e2e/channel/channel_test.go#TestMockChannelThreeUsers) | A token signed by a foreign key reads an outbox. |
| `sys.secret-leak` | proved | identity-hub | [TestSecretsAbsentFromDumpLogsInspectAndAnswer](../test/e2e/lifecycle/seal_test.go#TestSecretsAbsentFromDumpLogsInspectAndAnswer), [TestTelegramSecretBodyNotStored](../services/channel-hub/internal/svc/secret_db_test.go#TestTelegramSecretBodyNotStored) | A sealed key opened for the wrong purpose must fail; an intercepted secret must not persist in intake. |
| `sys.kill-uncertain` | proved | task-engine | [TestKillUncertain](../test/e2e/lifecycle/e2e_test.go#TestKillUncertain) | Killed work must refuse blind retry. |
| `sys.store-missing` | proved | task-engine | [TestStoreMissingDatabaseRefusesStartup](../test/e2e/lifecycle/store_missing_test.go#TestStoreMissingDatabaseRefusesStartup) | A service with a missing database must refuse startup. |
| `sys.stack-restart` | proved | channel-hub | [TestStackRestart](../test/e2e/disrupt/stack_restart_test.go#TestStackRestart) | A settled job must refuse cancellation after restart. |
| `sys.late-receipt` | proved | task-engine | [TestLateReceiptRecordedWithoutReopening](../services/task-engine/internal/svc/late_receipt_db_test.go#TestLateReceiptRecordedWithoutReopening) | A late receipt must not reopen settled work. |
| `sys.owner-request-single-use` | proved | tool-hub | [TestOwnerAnswerSingleUse](../test/e2e/channel/ownerreq_proof_test.go#TestOwnerAnswerSingleUse) | A second answer to an already answered request must be refused. |
| `sys.owner-request-timeout` | proved | tool-hub | [TestOwnerRequestExpiry](../test/e2e/channel/ownerreq_proof_test.go#TestOwnerRequestExpiry) | An answer after the request expiry must be refused. |
| `sys.owner-request-self-answer` | proved | tool-hub | [TestOwnerRequestSelfAnswer](../test/e2e/channel/ownerreq_proof_test.go#TestOwnerRequestSelfAnswer) | A run token answering a request must be refused. |
| `sys.seam-onesided` | deferred:19 | tool-hub for owner requests | [TestFinishedJobRetractsUnreportedRequests](../test/e2e/channel/ownerreq_proof_test.go#TestFinishedJobRetractsUnreportedRequests), [TestOwnerAnswerSingleUse](../test/e2e/channel/ownerreq_proof_test.go#TestOwnerAnswerSingleUse) | An adapter must not implement both sides of a seam. |
| `sys.sweep-lag` | proved | task-engine | [TestSweepLagOperatorAlert](../test/e2e/lifecycle/sweep_lag_test.go#TestSweepLagOperatorAlert) | An operator token must be refused when forging a service alert. |
| `sys.wedge-unblock` | proved | task-engine and spawn-controller | [TestHubctlUnblocksJobAndRuntime](../test/e2e/lifecycle/wedge_unblock_test.go#TestHubctlUnblocksJobAndRuntime) | A foreign owner must be refused when draining another runtime. |
| `sys.schema-version` | proved | spawn-controller | [TestSpecSchemaVersion](../services/spawn-controller/internal/svc/spec_test.go#TestSpecSchemaVersion) | A future runtime spec version must be refused. |
| `sys.worker-intent` | deferred:14 | worker absent | [TestRuntimeIntent](../test/e2e/lifecycle/e2e_test.go#TestRuntimeIntent) | A worker without a recorded intent must not start. |
| `sys.space-no-db` | deferred:14 | worker absent | [TestSpaceNoDB](../test/e2e/lifecycle/e2e_test.go#TestSpaceNoDB) | A worker must not receive database credentials. |
| `sys.seal-at-rest` | proved | credential-broker | [TestSecretsAbsentFromDumpLogsInspectAndAnswer](../test/e2e/lifecycle/seal_test.go#TestSecretsAbsentFromDumpLogsInspectAndAnswer) | A broker-held secret value is recovered from its volume dump, service logs, inspect output or a Postgres dump. |
| `sys.watchdog-stale` | proved | spawn-controller and channel-hub | [TestStaleSpawnTickRefusesHealth](../test/e2e/lifecycle/watchdog_stale_test.go#TestStaleSpawnTickRefusesHealth) | A frozen tick must refuse health; an unauthenticated status read must fail. |
| `sys.service-edge` | proved | identity-hub | [TestMockChannelThreeUsers](../test/e2e/channel/channel_test.go#TestMockChannelThreeUsers) | A token for Inject must be refused at ReadOutbox. |
| `sys.worker-tenant` | deferred:14 | worker absent | — | A worker must refuse another tenant's context. |
| `sys.worker-stop-confirmed` | deferred:14 | worker absent | — | A stop request must not report success before worker exit. |
| `sys.package-build` | deferred:18 | package builder absent | — | An unapproved package must not build. |
| `sys.recipe-hosts` | deferred:18 | recipe host owner absent | — | A recipe must not access an undeclared host. |
| `sys.model-usage` | proved | model-gateway | [TestGatewayAccountsSpend](../test/e2e/channel/model_test.go#TestGatewayAccountsSpend) | A Complete call without a run token must be refused before any accounting. |
| `sys.model-quota` | proved | model-gateway | [TestDailyQuotaRefusal](../test/e2e/channel/model_test.go#TestDailyQuotaRefusal) | A request over the daily ceiling must be refused, not served. |
| `sys.model-route` | proved | model-gateway and spawn-controller | [TestModelChannelCommands](../test/e2e/channel/model_test.go#TestModelChannelCommands), [TestPlatformLayerHonestVision](../services/spawn-controller/internal/svc/spec_test.go#TestPlatformLayerHonestVision) | A route outside the passport's declared routes must be refused; a declared one applies without restart; a declared capability (vision) must hold at the upstream. |
| `sys.space-egress` | proved | spawn-controller | [TestSpaceReachesOnlyModelGateway](../test/e2e/channel/model_test.go#TestSpaceReachesOnlyModelGateway) | A space runtime reaching anything but declared edges must be refused. |
| `sys.passport-narrow` | proved | spawn-controller | [TestPassportNarrowRefusal](../test/e2e/lifecycle/passport_test.go#TestPassportNarrowRefusal) | A lower layer widening a leaf must be refused. |
| `sys.passport-activate` | proved | spawn-controller | [TestPassportConfirmStaleRefused](../test/e2e/lifecycle/passport_test.go#TestPassportConfirmStaleRefused) | Confirm on a changed candidate must be refused. |
| `sys.passport-live` | proved | spawn-controller and model-gateway | [TestPassportLiveAppliesWithoutRestart](../test/e2e/lifecycle/passport_test.go#TestPassportLiveAppliesWithoutRestart) | A live leaf must apply without restarting the runtime. |
| `sys.passport-respawn` | proved | spawn-controller | [TestPassportRespawnRestartsRuntime](../test/e2e/lifecycle/passport_test.go#TestPassportRespawnRestartsRuntime) | A respawn leaf must restart the runtime. |
| `sys.passport-recreate` | proved | spawn-controller | [TestPassportRecreateRebuildsWorld](../test/e2e/lifecycle/passport_test.go#TestPassportRecreateRebuildsWorld) | A recreate leaf must rebuild the space world. |
| `sys.passport-rollback` | proved | spawn-controller | [TestPassportRollbackRestoresDigest](../test/e2e/lifecycle/passport_test.go#TestPassportRollbackRestoresDigest) | Rollback to a digest that fails revalidation must be refused. |
| `sys.passport-explain` | proved | spawn-controller | [TestPassportLiveAppliesWithoutRestart](../test/e2e/lifecycle/passport_test.go#TestPassportLiveAppliesWithoutRestart) | A leaf without layer attribution must not appear in explain. |
| `sys.grant-revocation` | proved | tool-hub | [TestGrantRevocation](../test/e2e/capability/capability_test.go#TestGrantRevocation) | A call on a revoked grant must be refused on the next call. |
| `cap.tools.use` | proved | tool-hub | [TestCapabilityTriples](../test/e2e/capability/capability_test.go#TestCapabilityTriples) | A call without the tools.use grant must be refused and the tool absent from the projection. |
| `cap.config.edit_own` | proved | tool-hub | [TestPhase12Workspace](../test/e2e/channel/workspace_test.go#TestPhase12Workspace) | A call without the config.edit_own grant must be refused and the tool absent from the projection. |
| `cap.persona.edit_own` | proved | tool-hub | [TestPhase13Skills](../test/e2e/channel/skills_test.go#TestPhase13Skills) | A call without the persona.edit_own grant must be refused and the tool absent from the projection. |
| `cap.terminal.exec` | proved | tool-hub | [TestPhase14Terminal](../test/e2e/channel/terminal_test.go#TestPhase14Terminal) | Bounded execution requires the terminal.exec grant and the terminal toolset selection. |
| `sys.skill-showcase` | proved | spawn-controller | [TestPhase13Skills](../test/e2e/channel/skills_test.go#TestPhase13Skills) | A runtime refuses to start without its read-only showcase. |
| `sys.skill-scan` | proved | spawn-controller | [TestPhase13Skills](../test/e2e/channel/skills_test.go#TestPhase13Skills) | A skill write refused by the scanner must leave the prior content intact. |
| `sys.skill-catalog` | proved | tool-hub | [TestPhase13Skills](../test/e2e/channel/skills_test.go#TestPhase13Skills) | A session cannot read a skill outside its pinned catalog revision. |
| `cap.workspace.read` | proved | tool-hub | [TestPhase12Workspace](../test/e2e/channel/workspace_test.go#TestPhase12Workspace) | A call without the workspace.read grant must be refused and the tool absent from the projection. |
| `cap.workspace.write` | proved | tool-hub | [TestPhase12Workspace](../test/e2e/channel/workspace_test.go#TestPhase12Workspace) | A call without the workspace.write grant must be refused and the tool absent from the projection. |
| `cap.memory.capture` | deferred:17 | tool-hub | — | A call without the memory.capture grant must be refused and the tool absent from the projection. |
| `cap.memory.search` | deferred:17 | tool-hub | — | A call without the memory.search grant must be refused and the tool absent from the projection. |
| `cap.memory.propose` | deferred:17 | tool-hub | — | A call without the memory.propose grant must be refused and the tool absent from the projection. |
| `adr.fsm.cas-version` | proved | shared/fsm | [TestCASRace](../shared/fsm/fsm_db_test.go#TestCASRace) | A transition with a stale version must be refused. |
| `adr.fsm.history-same-tx` | proved | shared/fsm | [TestRollbackLeavesNothing](../shared/fsm/fsm_db_test.go#TestRollbackLeavesNothing) | A transition on a machine whose writes rolled back must be refused. |
| `adr.fsm.outbox-same-tx` | proved | shared/fsm | [TestRollbackLeavesNothing](../shared/fsm/fsm_db_test.go#TestRollbackLeavesNothing) | A transition on a machine whose writes rolled back must be refused. |
| `adr.fsm.deadline-nonfinal` | proved | shared/fsm | [TestDeadlineSweep](../shared/fsm/fsm_db_test.go#TestDeadlineSweep) | A transition on a machine that reached a final state must be refused. |
| `adr.fsm.sweep-reread` | proved | shared/fsm | [TestSweepSkipsMovedOn](../shared/fsm/fsm_db_test.go#TestSweepSkipsMovedOn) | A stale sweep candidate must be refused after state change. |
| `adr.fsm.lease-atomic` | proved | shared/fsm | [TestLeaseContention](../shared/fsm/fsm_db_test.go#TestLeaseContention) | A competing lease holder must be refused. |
| `adr.fsm.single-sweeper` | proved | shared/fsm | [TestLeaseContention](../shared/fsm/fsm_db_test.go#TestLeaseContention) | A second sweeper must not claim the same lease. |
| `adr.secrets-at-rest.gcm-aad` | proved | shared/seal | [TestWrongPurposeOrKidFails](../shared/seal/seal_test.go#TestWrongPurposeOrKidFails) | A ciphertext opened with wrong purpose or key ID must fail. |
| `adr.secrets-at-rest.no-plaintext` | proved | shared/seal | [TestNoPlaintextLeak](../shared/seal/seal_test.go#TestNoPlaintextLeak) | A sealed value must not reveal plaintext bytes. |
| `adr.secrets-at-rest.kek-file` | proved | shared/seal | [TestLoadRejectsBadLen](../shared/seal/seal_test.go#TestLoadRejectsBadLen) | A malformed KEK file must be refused. |
| `adr.store.single-driver` | deferred:6 | shared/store | — | A second SQL driver import must fail the dependency gate. |
| `adr.store.numbered-sql` | proved | shared/store | [TestReadMigrationsRejects](../shared/store/migrate_test.go#TestReadMigrationsRejects) | Duplicate or malformed migration numbers must be refused. |
| `adr.store.advisory-lock` | proved | shared/store | [TestMigrateApplyAndIdempotent](../shared/store/store_db_test.go#TestMigrateApplyAndIdempotent) | A second apply of an already-recorded migration must be refused. |
| `adr.store.n-minus-one` | deferred:5 | shared/store | [TestMigrateReadsNewerDatabase](../shared/store/store_db_test.go#TestMigrateReadsNewerDatabase) | The published previous binary must not reject an additive next schema. |
| `adr.harness-admit.unknown-run-refused` | proved | harness-runtime | [TestCancelUnknownRun](../services/harness-runtime/internal/svc/svc_test.go#TestCancelUnknownRun) | Cancellation of an unknown run must be refused. |
| `adr.harness-admit.config-readonly` | proved | harness-runtime | [TestSpaceNoDB](../test/e2e/lifecycle/e2e_test.go#TestSpaceNoDB) | A runtime must not write the mounted config. |
| `adr.service-edges.forged-rejected` | proved | identity-hub | [TestForgedToken](../test/e2e/lifecycle/e2e_test.go#TestForgedToken) | A forged token must be refused. |
| `adr.service-edges.edge-required` | proved | identity-hub | [TestServiceEdge](../test/e2e/lifecycle/e2e_test.go#TestServiceEdge) | An undeclared service operation must be refused. |
| `adr.service-edges.rendered-table` | proved | keydist | [TestRenderEdges](../cmd/keydist/main_test.go#TestRenderEdges) | An absent descriptor edge must not render a grant. |
| `adr.service-edges.operator-audited` | proved | identity-hub | [TestActAsBindsAndAudits](../services/identity-hub/internal/svc/act_as_db_test.go#TestActAsBindsAndAudits) | An operator acting as a foreign owner must be refused. |
| `adr.spawn-runtime.intent-first` | deferred:14 | spawn-controller | [TestRuntimeIntent](../test/e2e/lifecycle/e2e_test.go#TestRuntimeIntent) | A runtime without a registered intent must be refused at creation. |
| `adr.spawn-runtime.space-no-db` | proved | spawn-controller | [TestSpaceNoDB](../test/e2e/lifecycle/e2e_test.go#TestSpaceNoDB) | Runtime database access must be refused. |
| `adr.spawn-runtime.digest-pins-image` | proved | spawn-controller | [TestSpecDigestBindsImage](../services/spawn-controller/internal/svc/spec_test.go#TestSpecDigestBindsImage) | Runtime creation with a changed image must be refused. |
| `adr.task-jobs.kill-to-uncertain` | proved | task-engine | [TestKillUncertain](../test/e2e/lifecycle/e2e_test.go#TestKillUncertain) | Killed work must refuse automatic retry. |
| `adr.task-jobs.tenant-isolation` | proved | task-engine | [TestJobToAnswer](../test/e2e/lifecycle/e2e_test.go#TestJobToAnswer) | Owner A must be refused when reading owner B's job. |
| `adr.task-jobs.restart-durable` | proved | task-engine | [TestStackRestart](../test/e2e/disrupt/stack_restart_test.go#TestStackRestart) | A settled job must refuse a second cancellation after restart. |
| `adr.task-jobs.run-unique` | proved | task-engine | [TestRunClaimUnique](../services/task-engine/internal/svc/run_claim_db_test.go#TestRunClaimUnique) | A second claim of the same run must be refused. |
| `adr.model-gateway.reserve-conditional` | proved | model-gateway | [TestParallelReservesCannotOverflow](../services/model-gateway/internal/svc/store_db_test.go#TestParallelReservesCannotOverflow) | A reservation over a ceiling, raced or not, must be refused. |
| `adr.model-gateway.measured-or-estimate` | proved | model-gateway | [TestEstimateChargedWhenUnmeasured](../services/model-gateway/internal/svc/store_db_test.go#TestEstimateChargedWhenUnmeasured) | A settle without a matching reservation must be refused. |
| `adr.model-gateway.route-key-lease` | proved | model-gateway | [TestRouteKeyLeasesFromBroker](../services/model-gateway/internal/svc/upstream_test.go#TestRouteKeyLeasesFromBroker) | A route credential comes only as a broker lease; a value in env, spec or files, or an unknown ref, must be refused. |
| `adr.model-gateway.spec-per-call` | proved | model-gateway | [TestModelChannelCommands](../test/e2e/channel/model_test.go#TestModelChannelCommands) | A route the effective passport does not declare must be refused before any upstream call. |
| `adr.model-gateway.run-token-bound` | proved | model-gateway | [TestForgedRunTokenRefused](../test/e2e/channel/model_test.go#TestForgedRunTokenRefused) | A Complete call without a valid run token must be refused. |
