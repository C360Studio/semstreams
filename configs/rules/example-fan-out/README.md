# Example: parallel fan-out with rule-side join

This reference pack demonstrates the ADR-046 Phase 1 fan-out pattern
end-to-end: a coordinator decides `fan_out` over N subtopics; the
framework spawns N investigators in parallel via `for_each`; each
investigator's completion stamps a counter triple on the parent loop
entity via the #147 `subject` override; a join rule fires the
synthesizer when the counter length matches the source list.

**This pack exists to be copy-pasted.** Real consumers fork the role
names, predicates, and prompts to match their flow; the *structure*
(spawn → per-child stamp → length_eq join) is the pattern semstreams
recommends for the no-deps case. The DAG-edges case ships separately
as ADR-046 Phase 2 (`fan_out_gated`, GH #139).

## Files

- `01-fan-out-subtopics.json` — coordinator decides fan_out →
  framework spawns N investigators in parallel via `for_each` over
  `coordinator.decision.subtopics`. Each investigator's task carries
  `$subtopic` substituted into its prompt; the spawned loop entity
  itself carries `agent.loop.parent` (pointing back at the
  coordinator) and `agent.loop.task` (a unique TaskID) — both
  stamped natively by the agentic-loop.
- `02-stamp-completion-on-parent.json` — investigator completes →
  rule stamps `gather.child.completed` on the **parent loop entity**
  via the new `subject` override (substitution-resolved to
  `$entity.triple.agent.loop.parent`). The triple's Object is the
  child's TaskID (`$entity.triple.agent.loop.task` — distinct per
  investigator) so the predicate-set accumulates one triple per
  distinct child. The synthesizer recovers per-subtopic context by
  walking children via `agent.loop.parent` and calling
  `read_loop_result` on each — rules carry references, not payloads
  (ADR-028).
- `03-synthesize-when-all-complete.json` — fires on the parent loop
  entity when `gather.child.completed` has length equal to the
  source list length. The synthesizer's prompt is composed with
  the sibling loop IDs inlined as a JSON array (via
  `$entity.triple.gather.child.completed.triples`, ADR-048 PR 4).
  The persona prose teaches the synthesizer to parse the inline
  JSON and iterate — no graph-query tool needed, and the
  chain-agents-no-graph discipline (per ADR-041 addendum) stays
  intact. Future improvement: thread the JSON array via
  `publish_agent.properties` → `TaskMessage.Metadata` once that
  wire lands (separate framework follow-up; today only the
  `publish` action threads Properties to the published envelope,
  `publish_agent` does not).

## Wire-shape summary

```
coordinator.decide(action="fan_out", subtopics=["a","b","c"])
  ↓
graph: coordinator.decision.subtopics = ["a","b","c"]  (JSON-encoded triple, decide tool)
  ↓
rule 01: for_each subtopics → spawn investigator_loop_A, investigator_loop_B, investigator_loop_C
         (each spawned loop natively carries agent.loop.parent=coordinator + agent.loop.task=<unique TaskID>)
  ↓ (parallel)
rule 02 fires 3× as each investigator completes:
  add_triple subject=coordinator predicate=gather.child.completed object=<unique TaskID>
  ↓
coordinator entity accumulates 3 gather.child.completed triples (one per distinct TaskID)
  ↓
rule 03 matches when length_eq(gather.child.completed, $entity.triple.coordinator.decision.subtopics.length) → spawn synthesizer
  (the .length suffix — #149 — resolves to the integer count of the source subtopics list,
   so the pack works for any decomposer fan-out width without per-width forking)
synthesizer prompt has inlined JSON array of sibling loop IDs → parses JSON → read_loop_result per ID
```

The third triple's stamp wakes rule 03 since graph-ingest's per-Subject
CAS makes the read-after-write linearisable on the coordinator entity.
The first two stamps see length=1 and length=2 respectively and rule
03's condition is false; only the third makes it true.

## Counter dedup

`graph-ingest` treats triples as a set keyed by `(subject, predicate, object)`.
The counter pattern relies on this: if rule 02 fires twice for the
same investigator (rare but possible — e.g. transient redelivery on
the agent.complete subject), the second `add_triple` is idempotent
on the parent entity and the counter doesn't double-count.

The pattern is **NOT** safe for "number of times event X happened" —
that needs distinct Objects per event. For sibling-completion
counting, the natural keying is `object = <child TaskID>` so the
predicate-set semantics give you natural dedup — every investigator
has a unique TaskID stamped as `agent.loop.task` at spawn time
(`processor/agentic-loop/graph_writer.go:533`).

## What this pack doesn't cover

- **DAG edges between subtasks.** No depends_on, no priority order.
  See [ADR-046 Phase 2](../../docs/adr/046-parallel-fan-out-and-gated-dag-dispatch.md)
  / GH #139 for the gated-DAG dispatch pattern.
- **Bounded concurrency.** All N publish operations fan out immediately.
  `agentic-loop` owns `agent.task` acknowledgement admission at 1 and rejects
  a nonzero port override. Provider concurrency remains component-owned;
  configurable framework-side concurrency caps are Phase 2.
- **Partial failure recovery.** If one investigator fails (`outcome=failed`),
  rule 02's condition (`outcome=success`) doesn't match, the counter
  never reaches the expected length, and rule 03 never fires. Add a
  separate timeout rule that fires `synthesize` on partial-completion
  if your flow needs to recover.
