# Ops Role Persona Fragments

This directory holds prompt fragments for the `ops` agent role. Fragments are loaded at
startup by `persona.LoadFromDirectory` (PR3 of the ADR-027 plan) and upserted into the
`PERSONAS` KV bucket, making them available to the agentic-loop assembler via the
ADR-029 step-3b wiring.

Fragments are file-loaded rather than inline-Go so that the ops role prompt is auditable
in source control and can be updated without recompiling the binary. Edits take effect on
the next process restart (Phase 1 is startup-only; no file watcher).

**ADR pointers:**

- ADR-027 (`docs/adr/027-ops-agent-meta-harness.md`) — ops agent design and Phase 1 scope.
- ADR-029 (`docs/adr/029-instance-type-patterns.md`) — Pattern-B persona manager wiring.

**Precedence reminder** (from the parent `configs/personas/fragments/README.md`):

```
DefaultFragments (code) < files on disk (startup) < tool writes (current process lifetime)
```

Runtime edits via `update_persona` are ephemeral and reset to the file content on restart.
