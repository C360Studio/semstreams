---
name: semstreams-dev
description: Guide SemStreams component, port, payload, and data-flow development through the shared contracts, existing patterns, and production-path proof. Use when adding or changing framework behavior.
---

# Develop SemStreams components

Read [Purpose and Product Boundary](../../../openspec/project.md) before scoping the change. SemStreams owns
knowledge-graph substrate and reusable primitives; rules trigger and components execute. State has one owner.

The [shared protocol](../../protocol.md) owns claims, worktrees, review, landing and closure. Establish the draft
PR claim before implementation. A skill invocation does not transfer another session's worktree or expand the
user's authorization. Shared state lives in GitHub and OpenSpec; private memory is optional historical context.

Use the project roles in [the agent guide](../../README.md): architecture and OpenSpec target state go through
the architect, nontrivial backend work through the developer, and nontrivial changes through the reviewer.
Read the applicable role's full contract; this entry point does not replace it. Durable documentation and task
truth stay with the technical writer, or the owner/root session where that profile is unavailable.

## Find the existing owner and pattern

Before adding a new surface, locate its current owner, production callers, and applicable spec/ADR. Also search
for the same problem shape on other planes; [Pattern Adoption](../../../docs/contributing/07-pattern-adoption.md)
has the worked example. Apply the developer contract's existing inventory and adopter-seam obligations.

| Work in this slice | Read when applicable |
| --- | --- |
| Facts vs requests; KV Watch vs JetStream | [kv-or-stream](../kv-or-stream/SKILL.md) |
| New durable state or a rule-readable fact | [entity-or-bucket](../entity-or-bucket/SKILL.md) |
| A new polymorphic payload | [new-payload](../new-payload/SKILL.md) |
| Rules, components or lifecycle orchestration | [orchestration-check](../orchestration-check/SKILL.md) |
| Graph reads or an embedded adapter | [query-pattern](../query-pattern/SKILL.md) |
| Component factory and port declaration | [framework composition](../../../openspec/specs/framework-composition/spec.md) and [discovery](../../../openspec/specs/component-discovery/spec.md) |
| Operator configuration or schema changes | [runtime configuration](../../../openspec/specs/component-runtime-config/spec.md) |
| Context ownership, concurrency and shutdown | [developer contract](../../contracts/semstreams-developer.md) |

Use the current component interfaces and port implementations as the source for signatures. A port descriptor
must agree with the factory's actual resources. Follow the applicable composition contract instead of copying
a historical constructor or a second hand-maintained interface list.

For payloads, the new-payload skill owns the recipe: explicit `RegisterPayloads`, payload-only alias
serialization, and registration from each applicable binary's composition root. No `init()` or blank-import
registration. Trace both production and E2E binaries through their shared registration calls; a text hit for
the package name alone does not establish that either binary registers the payload.

For context-taking operations, apply the full developer contract, including its bounded terminal-cleanup
exceptions. Production structs do not retain contexts; continuing work derives from `Start`/`Run` and joins
`Stop`. Do not use a copied constructor to invent a new lifetime or nil-context fallback.

## Challenge the behavior at its production seam

Apply the [testing discipline](../../../docs/contributing/01-testing.md#testing-discipline): identify the accepted
requirement, plausible violation, independent expectation, and distinguishing observation before implementation.
Preserve acceptance, rejection, forbidden-side-effect, and evidence-scope distinctions in the checks and handoff.
Record the [PBT decision](../../../docs/contributing/01-testing.md#when-to-use-property-based-testing) for applicable
invariants; use the [Rapid practice guide](../../../docs/contributing/09-property-testing.md) for implementation recipes.

Use the [testing policy](../../../docs/contributing/01-testing.md) and the developer/reviewer contracts to choose
the lowest sufficient tier. Observe the intended failing assertion before implementation. A constructor,
registry, codec or wire change needs proof through that production path; helper-only proof has a narrower claim.
Operator-reachable configuration uses the production JSON round trip and schema generation.

The [developer contract's test requirements](../../contracts/semstreams-developer.md#test-and-operational-fidelity)
already define native fuzz targets, accepted/rejected grammar seeds, spec-cited properties and reachable
boundaries. Derive the expected behavior from the cited clause, not the implementation's own algorithm. Start
from [the entity-ID properties](../../../pkg/types/entity_id_prop_test.go) and the cited clause they test when
that example fits. Check citations with `task spec:properties`; a resolving citation alone does not prove the
property's semantics.

When mutation evidence is used, identify the production call site whose removal or alteration the test must
detect. Confirm the mutation compiled, the intended assertion ran, and the failure was about that behavior.
Use the role contract's backup/checksum restoration procedure; preserve a useful shrunk counterexample.

## Verify and hand off

Use [semstreams-preflight](../semstreams-preflight/SKILL.md) to select existing local gates and report their
scope. Preserve exact-revision evidence and unresolved gates through
[semstreams-handoff](../semstreams-handoff/SKILL.md). Implementation findings go back through the relevant role;
the shared protocol controls issue filing, archive/spec sync, review and merge.
