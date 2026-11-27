# Phase 13 · Skills

Release `v0.0.13` follows the Phase 12 check timing work. Readiness requires
green local and CI checks, merge into protected `main`, a release tag, and
a manually dispatched nightly run.

## Read-only showcase

Spawn-controller assembles the platform and user layer skills from the
effective layer template. It renders the showcase under the existing
read-only configuration mount and refuses a runtime launch if the
showcase cannot be assembled or verified. Hermes offers no native skill
tools. The startup inventory probe verifies that boundary.

An agent's own skills are Markdown workspace files. The file tools for
skill mutations require `persona.edit_own`. The skill file root is
read-only in containers. The mutation goes through the workspace owner,
so an agent cannot bypass the scanner
with a direct write. Skill content is supplied as text. Executable
extension roots contain only reviewed, read-only artifacts.

## Validation and disclosure

One deterministic scanner checks all incoming layer skills and the
complete candidate file before write or edit. A refusal reports the
file, line and rule. In particular, secret-like text blocks saving and
the refusal never includes the matched text.

Tool-hub binds a catalog revision to a conversation. The model gets a
bounded list with descriptions. On overflow it gets the names and a
pointer to the tool-hub search tool. Search uses the same pinned
revision. Updates are available to new conversations.

Tool-hub generates the platform guide from the capability catalog and
the registry. It shows the capabilities of the platform, the tools
available to the current agent, and how to ask the user. The guide's
current tool list follows admission.

## Readiness

- A file-tool mutation of an own skill requires `persona.edit_own`.
- A secret-like skill is refused with the rule and location. The file
  contents remain intact.
- A runtime refuses to launch without its verified read-only showcase.
- Agent-writable storage is separate from executable extension roots.
- A catalog overflow gives names and search guidance. A session keeps
  its catalog revision while a new session can use the updated one.
- The startup probe and scripted model confirm Hermes's native skill
  tools are disabled.
- `just check` and mandatory container e2e pass. The Phase 13 report
  records the proof matrix, elapsed times and owner questions.

Worker execution, terminal, MCP, CLI and skill credential leasing stay
in their later phases.
