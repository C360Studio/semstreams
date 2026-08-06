# Todo Model Blocker Inventory

## Scope

This inventory is limited to the `write_todos` representation exposed by the GS-01 cutover. It exists because the
legacy representation relies on duplicate occurrence and slice ordering, while the canonical reconcile contract stores
a set of complete triples. It does not reopen graph mutation, recovery, ownership, or orchestration design.

## Existing write path

- `processor/agentic-tools/write_todos.go:189-201` sends the complete desired todo list through the built-in loop
  execution `todos` reconcile group with one request ID, source, and timestamp.
- `processor/agentic-tools/write_todos.go:332-366` encodes each item as five triples on the loop entity: ID, content,
  status, position, and updated-at.
- Items commonly share status and always share updated-at for one call. All five triples also share subject, source,
  timestamp, and request metadata for that call.
- The tool argument contract requires a stable unique ID, non-empty content, and one of three statuses. The public tool
  behavior is full-list replacement with caller-provided order.

## Canonical projection and storage behavior

- `pkg/projection/mutation_client.go:331-365` fills every empty triple `Context` from the mutation `RequestID`; an
  explicitly different context conflicts with the mutation metadata and is rejected.
- `processor/graph-ingest/canonical_mutations.go:255-319` performs one revision-fenced reconcile of the selected
  predicate group.
- `processor/graph-ingest/canonical_mutations.go:542-572` compares complete reconcile triples and removes exact
  duplicates before commit.
- Consequently, equal values such as two `pending` statuses or one call-wide updated-at become one stored triple. This
  is correct set behavior and must not be weakened to preserve the legacy todo encoding.

## Existing read path

- `processor/agentic-loop/todos.go:60-75` uses the canonical exact entity reader and treats a missing loop entity as an
  empty todo list.
- `processor/agentic-loop/todos.go:78-172` filters the five public predicates, slices them into positional groups of
  five, validates a fixed predicate order, and sorts decoded records by position.
- This reader assumes storage preserves repeated occurrences and insertion order. Deduplication can shear groups and
  either lose items or combine fields from different items.

## Public and framework surfaces

- `vocabulary/agentic/predicates.go:911-962` declares five public `agent.todo.*` predicates and documents structural
  rule access to all except rule-opaque content.
- `vocabulary/agentic/register.go:167-191` registers those five predicates; only content is rule-opaque.
- `internal/builtinprojection/contracts.go:11-49` exposes all five predicates as one built-in reconcile group.
- `docs/adr/036-agent-private-observable-state.md` and `docs/operations/15-agent-private-state.md` describe raw todo
  predicates as observable rule/query surfaces.
- A repository search found documentation assertions for raw todo predicate matching but no shipped rule or config that
  predicates on them. Therefore changing this surface breaks an advertised capability, but not a shipped runtime flow.
- `processor/agentic-tools/write_todos.go:215-239,369-397` exposes a success result containing count plus each item's
  ID, status, and position. Result metadata exposes loop entity ID, todo count, and the writer's pre-reconcile triple
  count. Changing from five triples per item therefore changes observable result metadata unless that implementation
  detail is deliberately removed.
- `processor/agentic-loop/todos.go:34-50` exports `TodoState` with all five fields and exports `TodoReader`, which returns
  `[]TodoState`. `BuildTodoStateMessage` consumes only ordered status and content, but external Go callers can consume
  the complete exported shape.

## Documentation contradictions

- `docs/adr/036-agent-private-observable-state.md:143-164` says the tool writes one triple per todo, then enumerates five
  predicates; production writes five triples. It also spells the updated-at predicate with an underscore while the
  registered constant uses a hyphen.
- `docs/operations/15-agent-private-state.md:118-124,164-167` repeats the nonexistent underscore spelling in public
  rule/query and observability guidance.
- These are existing contradictory claims, not compatibility requirements. The correction must replace them with one
  coherent representation and accurately distinguish supported raw single-predicate matching from unsupported
  record-correlated counts and conjunctions. The target surface remains decision question 1 below.

## Test fidelity

- `processor/agentic-loop/todos_integration_test.go:116-140` plants the legacy shape through direct `CreateEntity` and
  claims reconcile stores the desired slice verbatim. This bypasses production reconcile and its canonical deduplication.
- `processor/agentic-tools/write_todos_integration_test.go:61-75` separately requires two occurrences of every legacy
  predicate, including two identical updated-at values. That assertion directly conflicts with canonical set semantics.
- `processor/agentic-tools/write_todos_test.go:64-132` directly pins five desired triples per item and a separate
  position predicate in the unit-level writer contract.
- `processor/agentic-loop/todos_test.go:59-153` independently pins five-triple stride grouping, partial-stride
  dropping, and sensitivity to predicate order.
- Replacement coverage must exercise the real projection reconcile path and exact reader together with duplicate
  statuses, shared write timestamps, reordering, shortening, and empty-list clearing.

## Existing correlation idiom that does not transfer

- Scratchpad entries use a shared `Triple.Context` value to correlate a record's fields.
- The GS-01 projection client now reserves `Context` for the request ID and rejects a conflicting per-record context.
- Reusing todo ID as `Context` would restore grouping only by violating the canonical provenance contract; it is not a
  valid local fix.

## Adopter seam inventory

### Tool caller

- Must know: only the existing `write_todos` arguments and full-list replacement behavior.
- If they do nothing: existing callers continue submitting the same array; representation is framework-owned.
- Discovery: tool schema and tool result metadata. The current `triple_count` metadata is representation-shaped rather
  than a logical capability and must either receive an intentional new meaning or be removed as part of the clean break.
- Should know: nothing about triples, predicate groups, revisions, or correlation.

### Agent loop and prompt assembler

- Must know: how to decode the framework-owned current representation into ordered `TodoState` values.
- If they do nothing: stale positional decoding silently loses or shears items.
- Discovery: the narrow `TodoReader` implementation and its tests.
- Should know: one record boundary and malformed-record policy, not graph storage ordering. Exported `TodoState` and
  `TodoReader` mean any Go API shape change must be intentional even though no separate in-repository consumer exists.

### Rule, ops, and graph-query author

- Must know today: five raw predicates are advertised as separately matchable structural facts.
- If they do nothing after a clean break: no shipped rule changes, but custom consumers of those predicates must
  migrate.
- Discovery: vocabulary registry, ADR-036, and operations guidance.
- Should know: only an explicitly supported derived observation surface. They should not infer record correlation from
  repeated unkeyed triples.

### Operator

- Must know: todo state is current agent-private working memory on the loop entity.
- If they do nothing: no deployment or configuration change should be required.
- Discovery: agent private-state operations guide.
- Should know: no storage encoding details.

## Constraints for a bounded correction

- Preserve one canonical reconcile request for complete-list replacement.
- Preserve exact graph reads and eventual consistency behavior.
- Preserve tool input/output behavior unless a clean public break is explicitly documented.
- Do not introduce child entities, discovery indexes, ownership, retries, streams, or compatibility decoding.
- Do not use request provenance fields as record keys.
- Make record boundaries explicit and independent of triple order or duplicate occurrence.
- Reject or skip malformed records deterministically; never assemble fields from different records.
- Update vocabulary, built-in projection contract, tests, ADR-036, and operations guidance together.

## Decision questions

1. Is raw rule matching over each todo field a required framework capability, or can the current item be rule-opaque and
   expose future derived counters/status observations separately if a real consumer appears?
2. What is the smallest representation with an explicit record boundary that remains one reconcile group on the loop
   entity?
3. Should one malformed todo record be skipped while valid records remain available, or invalidate the complete list?
