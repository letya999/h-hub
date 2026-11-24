# Platform data ownership

Platform data is YAML shipped inside a service binary via `embed`; it is
not configuration and has no runtime reader outside its owner.

## spawn-controller — `services/spawn-controller/internal/svc/platform/`

- `capabilities.yaml` — the capability catalog: the single vocabulary of
  grant names. Read by spawn-controller when it compiles an effective spec
  (unknown grant refuses activation), by the `capabilities` gate, by the
  trace gate to resolve `cap.*` ids, and by the capability e2e to build
  the grant set.
- `registry.yaml` — the leaf schema: every passport leaf with its class
  (live, narrow, respawn, recreate). Read at compile and effect time.
- `layer/*.yaml` — the platform layer documents, the base of every layer
  chain. `grants.yaml` holds the platform allow list; `tools.yaml`
  selects the toolsets every spec sees.
- `templates/*/` — the `assistant` and `restricted` passport templates a
  space starts from.

## tool-hub — `services/tool-hub/internal/svc/platform/`

- `toolsets.yaml` — the toolset registry: toolset -> executor ->
  provider, each tool naming exactly one catalog capability. Embedded in
  the tool-hub binary; read at admission (`AdmitCall`, `Projection`,
  `Invoke`).

## The link between them

No service reads another service's platform data at runtime. The one
place the two files can disagree — a tool citing a capability the
catalog does not have — is caught at build time by the `capabilities`
gate (`tools/gates/capabilities.go`), which cross-checks toolsets.yaml
against capabilities.yaml. Layer documents and templates that name
capabilities are checked the same way.
