# Post-Foundation-B remap inventory: independent review

**Verdict:** INVENTORY PASS.
**Repository baseline:** `9d530bf23c97054f38be5a6caf7c25ac20a07e1c`.
**Reviewed artifact:** `docs/proposals/post-foundation-b-remap-inventory.md`.
**Artifact identity:** 595 lines; 36,227 bytes;
SHA-256 `58e44190937c247a30ae5ce55621da27cddd113da6da858d64a2e9bc51bdd7fb`.
**Review mode:** independent and read-only.

## 1. Verdict and boundary

The inventory passes independent review. The four prior corrections are present and supported by the merged tree.
The reviewer found no new scope, interpretation, or adopter-seam defects.

This pass allows design work to begin from the reviewed inventory. The inventory itself approves no target state,
option, recommendation, implementation plan, or binding ruling.

The baseline advanced from #912 to #913 only through changes in `metric/`, the metrics service, and metrics lifecycle
tests. The focused recheck found no change to an inventoried surface.

## 2. Verified corrections

### 2.1 Generated port-schema projection

The projection is inventoried at `docs/proposals/post-foundation-b-remap-inventory.md:67-72`, `:149-166`,
`:429-446`, and `:450-463`.

- `component/schema_tags.go:359-424` recognizes `type:ports` and calls `GeneratePortFieldSchema`.
- `component/schema_tags.go:696-752` derives closed kind variants, direction sets, required fields, and
  `additionalProperties: false` from the canonical binding table.
- `component/registry.go:343-356`, `:411-428`, and `:455-470` retain and expose the resulting
  `Registration.Schema`.

The inventory records both evaluation time and retained form and includes the projection in its existing-surface,
modeled-fact, consumer, and collision inventories.

### 2.2 Issue #828 predicate-layout disposition

The disposition is recorded at `docs/proposals/post-foundation-b-remap-inventory.md:414-415`.

- Current authority at `openspec/specs/graph-index/spec.md:261-276` and
  `docs/adr/078-raw-canonical-predicate-membership-keys.md:20-36` requires raw predicate membership keys and no
  `PREDICATE_CATALOG`.
- Conflicting stale text remains at `openspec/specs/graph-index/spec.md:367-405`, especially `:375-391`, and
  `docs/adr/068-graph-retention-deletion-lifecycle.md:243-260`.

The inventory therefore keeps #828 open as current documentation-conflict evidence without selecting a correction.

### 2.3 Issues #881 and #888 changed trajectory premise

The changed-premise dispositions are recorded at
`docs/proposals/post-foundation-b-remap-inventory.md:396-402`.

- `processor/agentic-loop/trajectory_recorder.go:167-173` assigns captured evidence only to the trajectory fact.
- `agentic/trajectory_fact.go:143-173` defines that reference as `TrajectoryFactV1.Evidence` in the trajectory KV
  fact.
- The independent production search found no trajectory graph write or Graphable conversion.

The predicted trajectory graph population is absent. The inventory retains only the independently evidenced
non-trajectory observability and E2E questions.

### 2.4 Issue #690 changed mutation premise

The changed-premise disposition is recorded at
`docs/proposals/post-foundation-b-remap-inventory.md:403-405`.

- `graph/inference/applier.go:208-285` uses `graphmutation.NewClient`, `Append`, and typed mutation outcomes.
- `gateway/graph-gateway/component.go:872-879` still constructs the legacy relationship-stream producer.
- `graph/inference/applier.go:143-206` still defines the direct applier.
- Exact production searches found one `graph.events.relationship.create` producer and only the
  `NewDirectRelationshipApplier` constructor definition.

The inventory correctly replaces the former plural-production-path premise with those two narrower residuals.

## 3. Review conclusion

The reviewed artifact is an inventory checkpoint, not a design decision. Its evidence is sufficient for a separate
design phase to begin, subject to owner review and approval of whatever target state or options that phase produces.
