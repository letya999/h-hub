# Phase 12 · Workspace tools and reviewed configuration

Release: `v0.0.12`. The phase closes only after `just check` and mandatory
container e2e pass in CI, the PR is merged into protected `main`, and the
release is tagged.

## Owner decisions

- The target is to disable every native Hermes tool and deliver tools through
  tool-hub and the other hub services. A native executor remains a temporary,
  explicitly priced exception. Phase 12 enables none.
- Only the `tools` passport chooses an executor: `toolhub`, `harness`, `mcp`,
  `cli`, or `remote`. The `config` passport contains no `harness.native` list.
- The file toolset belongs to tool-hub. The project has no `todo`, `clarify`,
  or `session_search` toolset.
- The startup probe continues to compare Hermes's actual active tool list
  with the allowed list. Any extra tool prevents readiness.

## Part A · Phase 11 follow-ups

1. Back up every upstream session and transcript volume with checksums.
   Restore a space from the backed-up volume and prove that its third message
   uses the first two.
2. Bind an approval answer to its request ID and delivery receipt. Two
   consecutive requests must not let an answer to the first affect the second.
3. Select and claim an overdue deadline in one transaction so a deadline
   cannot move between selection and handling.
4. Reject an unknown capability in any layer during recompilation. The error
   identifies the capability and space; report affected spaces without
   silently changing their rights.
5. Translate the phase 11 report into English.

## Part B · Phase 12

1. Remove `harness.native` from the config schema and code. Verify all layer
   templates. Keep executor selection in `tools`.
2. Classify every tool in the pinned Hermes inventory as `nothing`,
   `workspace`, `network`, `process`, or `external-service`. Bind the reviewed
   classifications to `inventory_sha256`; a new image inventory requires a
   fresh review. Report ambiguous tools classified conservatively.
3. Reject a `harness` executor for a `network`, `process`, or
   `external-service` tool unless the owner explicitly accepts delayed
   revocation and missing per-call audit. `nothing` and `workspace` need no
   acknowledgement. Test a refused example.
4. Make `workspace.layout.nodes[]` (`path`, `mode`, `note`) and `writable[]`
   the single description for mounts, path checks, and the explain tree.
   Default to read-only. `/config`, inbox, and vault are immutable read-only
   roots. A layout change has the `respawn` effect.
5. Add the `files` toolset with executor `toolhub`: `list`, `read`, `write`,
   `edit`, `mkdir`, `search`, and `remove`. Read operations require
   `workspace.read`; mutations require `workspace.write`. Confine paths with
   `os.Root`; refuse traversal, absolute paths, symlink escape, read-only
   roots, and paths outside the layout with a named reason. Bound file size,
   entry count, and depth. Write atomically and roll back on failure.
6. Use one reviewed table of configuration keys, validators, and passport
   leaves for both config compilation and `config.edit_own`. Exclude security
   leaves and test that a retired key is refused.
7. Prove that revoking `workspace.write` rejects the next tool-hub call
   without a restart. A tool selection change requests a restart from
   spawn-controller; it applies when idle and preserves conversation context.
   Measure Hermes respawn time.
8. Prove approval modes through a real file mutation: `ask` prompts before
   writing, `auto` proceeds without a prompt, and `bypass` cannot exceed
   grants.

## Readiness

- An agent writes within an allowed directory. Traversal, symlink escape,
  read-only roots, and paths outside layout fail with specific reasons.
- A file survives runtime respawn. The startup probe confirms that no native
  Hermes tool is active.
- Revocation acts on the next file call without respawn; tool selection
  changes restart only in idle and retain the conversation.
- Agent config edits use the same reviewed leaf table as compilation.
- `ask`, `auto`, and `bypass` pass container e2e. The executor gate refuses
  its broken example.
- `just check` is green and mandatory e2e completes within ten minutes.

## Out of scope

Skills (13), workers and terminal (14), MCP and CLI (15), memory (17), and
internet or egress (18) remain in their own phases. Do not copy code from
hermes-hub, enable a native Hermes tool, add a second config leaf list, or
weaken a gate. Record blocked work and its reason in the phase report.
