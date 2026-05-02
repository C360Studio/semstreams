# Migration Guide: beta.35 → beta.36

## Summary

Beta.36 closes a brittle interaction between rule prompt templates and
the `read_loop_result` tool that semspec tracked through a research-chain
churn against beta.35: rule templates substitute `$entity.id` (the full
6-part federated string) into the LLM's prompt, the LLM faithfully copies
that into a `read_loop_result(loop_id=...)` call, and the lookup misses
because the AGENT_LOOPS bucket keys on the bare-UUID `instance` segment
alone (`COMPLETE_<bare-uuid>`).

Two complementary fixes ship together.

| Surface | Status |
|---|---|
| New `$entity.org` / `$entity.platform` / `$entity.domain` / `$entity.system` / `$entity.type` / `$entity.instance` substitutions | **Additive** |
| Mirror `$related.<part>` substitutions for pair rules | **Additive** |
| `read_loop_result` tolerates a full 6-part entity ID for `loop_id` (strips to trailing segment) | **Behavioural — backward compatible** |
| Existing `$entity.id` substitution | **Unchanged** |
| Existing `read_loop_result` call shape with bare loop_id | **Unchanged** |

**The simplest beta.35 → beta.36 upgrade is to do nothing.** Existing
rule templates and tool calls behave identically. The new substitution
tokens are opt-in; the tool-side strip is a defensive guard that activates
only when a multi-segment loop_id arrives.

## What's new

### Per-segment entity substitution tokens

The 6-part federated entity ID format
(`org.platform.domain.system.type.instance`) is now exposed as discrete
substitution tokens. Each segment is independently addressable:

| Token | Renders | Use when |
|---|---|---|
| `$entity.org` | First segment (e.g. `c360`) | Multi-tenant routing, audit prefixes |
| `$entity.platform` | Second segment (e.g. `osh-demo-001`) | Per-deployment scoping |
| `$entity.domain` | Third segment (e.g. `agent`) | Domain-shape branching |
| `$entity.system` | Fourth segment (e.g. `agentic-loop`) | System/component identification |
| `$entity.type` | Fifth segment (e.g. `execution`) | Type-shape branching |
| `$entity.instance` | Sixth segment (e.g. the bare UUID) | Tool args that key on the bare ID — the canonical case |

Mirror `$related.<part>` tokens are available for pair rules.

Resolution requires a valid 6-part entity ID per `message.IsValidEntityID`.
Tokens against a non-conforming ID survive substitution and trip the
existing unresolved-template warning, surfacing author error or unexpected
ID shape rather than silently rendering empty.

### `read_loop_result` accepts the full entity ID

If `loop_id` contains one or more dots, the executor takes the trailing
segment after the last dot as the bucket key. Bare-UUID inputs (no dots)
are unchanged. UUIDs themselves contain no dots, so the strip can never
lose part of a real loop ID.

Two-line fix in the executor; preserves every existing call site.

## Recommended migration for rule authors

For prompt templates that pass a loop_id to `read_loop_result`:

```jsonc
// Before (works, but brittle — relies on the LLM copying the full
// federated string and the read_loop_result strip catching it):
{
  "prompt": "Researcher loop id: $entity.id. Call read_loop_result with that loop id...",
  "properties": { "researcher_loop_id": "$entity.id" }
}

// After (preferred — the substitution layer hands the LLM the bare UUID
// directly; no parsing or stripping required at any layer):
{
  "prompt": "Researcher loop id: $entity.instance. Call read_loop_result with that loop id...",
  "properties": { "researcher_loop_id": "$entity.instance" }
}
```

Both forms work in beta.36. The former relies on the executor-side strip;
the latter renders the bare UUID at the substitution layer so the LLM
never sees the full federated string. Prefer the latter going forward —
it's the lower-friction shape and it doesn't depend on tool-side
defensive parsing.

## Backward compatibility

- Existing rule templates using `$entity.id`: unchanged behaviour.
- Existing `read_loop_result` callers passing bare loop UUIDs: unchanged
  behaviour (the strip is a no-op for inputs without dots).
- Existing rule templates that referenced unrelated tokens like
  `$entity.foo` (where `foo` isn't a valid segment name): unchanged —
  the unresolvedTemplateVarRe regex caught those before and still does.

## Why both fixes

The substitution-layer fix (`$entity.instance`) is the right primitive
long-term: it's reusable across any tool or template that wants the bare
UUID, doesn't bake parsing logic into a tool executor, and surfaces ID
shape errors via the existing unresolved-template warning.

The tool-side strip is defensive insurance: it lets existing rule
configurations keep working without an immediate config update, and it
absorbs the case where an LLM hallucinates a multi-segment loop_id even
when the template handed it the bare UUID.

Together they eliminate the brittle path that produced semspec's
research-chain churn against beta.35.

## Cross-references

- `processor/rule/entity_substitution.go` — new substitution layer
- `processor/rule/execution_context.go` — `SubstituteVariables` doc
  comment lists every supported token
- `processor/agentic-tools/loop_result.go:normalizeLoopID` —
  defensive strip
- `processor/rule/entity_substitution_test.go` —
  `TestSubstituteVariables_EntityParts_FullPipeline`,
  `TestApplyEntityPartsSubstitutions_InvalidIDLeavesTokens`
- `processor/agentic-tools/loop_result_test.go` —
  `TestReadLoopResultExecutor_FullEntityIDLoopArg`,
  `TestNormalizeLoopID`
- semspec post-mortem against beta.35 (the empirical case study)
