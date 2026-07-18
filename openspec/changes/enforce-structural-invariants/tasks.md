## 1. Hardened validators (D1) + entity-ID drift fix (D3)

- [x] 1.1 Harden `vocabulary.IsValidPredicate` (`vocabulary/predicates.go`): exactly 3 dot-separated parts, each non-empty (replace the bare `dotCount == 2` check). Mirror `message.IsValidEntityID`'s rigor. Keep it structure-only (hex-encoding handles raw-KV-safety per gh#474) — document that in the func comment.
- [x] 1.2 Add unit tests for `IsValidPredicate`: valid 3-part; 2-part; 4-part; empty segment (`sensor..celsius`); leading/trailing dot; empty string. Include a `.value`-ending 3-part predicate (`sensorml.capability.value`) as VALID (guards the gh#519 collision case).
- [x] 1.3 Fix `parseEntityID` drift (`graph/clustering/summarizer.go`): the contract is exactly 6 parts — remove the `len(parts) >= 6` + `strings.Join(parts[5:], ".")` dotted-instance leniency; a non-6-part ID is invalid, not a dotted-instance ID. Route through `message.IsValidEntityID` where it makes sense.
- [x] 1.4 Grep every other predicate/entity-ID split-and-assume site (`vocabulary/iris.go` `parts[0]`, any `strings.Split(pred)` / `parts[2]`) and confirm each now relies on the guarantee or guards explicitly; note residual guards.

## 2. Observe-only PREDICATE gate (D4 dry-run) — entity-ID gate already exists

> Reality (apply): graph-ingest already validates entity IDs at the mutation boundary (`validateEntityID`/`entityIDRegex`) and already has `recordMutationRejection` (mutation_rejections{subject,reason} counter + WARN log). This phase adds the missing **predicate** arm through that existing mechanism.

- [x] 2.1 Add predicate structural validation (`vocabulary.IsValidPredicate`) to every triple-carrying write path: `handleTripleAdd` / `AddTriple`, `handleTripleAddBatch` / `AddTriples`, `create_with_triples`, `update_with_triples`, and the Graphable ingest path. Config-gated `StructuralPredicateEnforcement` (default **observe**).
- [x] 2.2 On an invalid predicate, call the EXISTING `recordMutationRejection(subject, "structural_predicate_invalid", detail)` (reuses the mutation_rejections counter + loud WARN naming token+subject+reason). No new metric — reuse the established one.
- [x] 2.3 Unit tests: observe-only mode records the rejection metric + log but still persists (behavior unchanged); a conforming predicate is untouched. (`mutation_rejection_metric_test.go` is the sibling pattern.)
- [x] 2.4 File the entity-ID **validator-unification** cleanup issue: 3 divergent definitions (`message.IsValidEntityID`, `pkg/types.EntityID.IsValid`, graph-ingest `entityIDRegex`). Out of scope here; link from this change. → filed gh#531.

## 3. Audit the corpus (gate the flip)

- [x] 3.1 Reference-config lint added (`TestReferenceConfigs_AllTripleRefsAreThreePart` in `test/reference_configs_test.go`): every `$entity/$related.triple.<predicate>` reference in `configs/rules/**` must be a valid 3-part predicate. Passes after the migration below.
- [ ] 3.2 e2e validation: `task e2e:agentic` (research-graph + deep-research tiers) MUST run green — the research-predicate rename touches the shipped research pipeline. (Requires Docker; run before merge — also the BREAKING e2e gate 6.2.)
- [x] 3.3 **AUDIT RESULT — NOT CLEAN.** The audit surfaced real violators (validating the maintainer's concern):
  - **Shipped predicates (10) — MIGRATED to 3-part:** research-graph pipeline (9) `research.{requested→request.received, topic→request.topic, hint→request.hint, budget_tokens→request.budget_tokens, max_iterations→request.max_iterations, parent_loop→parent.loop, parent_role→parent.role, loop_id→loop.id}` + `loop.role→agent.loop.role` (reuse canonical); plus example-fan-out `gather.completed_child→gather.child.completed`. Renamed across constants, `research_graph.go` stamping, `configs/rules/{research-graph,deep-research}/**`, e2e scenarios, and the mission harness `mission.phase→mission.state.phase`. **BREAKING: semteams (sister repo) consumes the research predicates and MUST update in lockstep — coordinate before tag.**
  - **Dead constants:** `network.traffic.{bytes,packets}.{in,out}` (4-part) in `vocabulary/predicates.go` — no non-test usage found; flag for removal (separate cleanup).
  - **Test fixtures (151 across 68 files) — MIGRATED** to `test.*` 3-part convention (task 5.1).

## 4. Flip fail-closed + loud log (D2)

- [x] 4.1 Flip `StructuralPredicateEnforcement` default to fail-closed: a structurally-invalid predicate rejects the mutation with a classified validation error (via `rejectInvalid` + `recordMutationRejection`); nothing persists. Keep the loud log. (Entity-ID rejection already fail-closed.)
- [ ] 4.2 Tests: invalid predicate → mutation rejected, classified error, nothing written, rejection metric + loud log asserted; fully-conforming mutation → persisted with existing merge semantics intact (regression).
- [ ] 4.3 Confirm the RPC error contract: callers of the mutation API receive the classified error via the natsclient `error:`-payload convention (audit the request path so it's not silently decoded as success).

## 5. Retire the 2-part fixture debt + lint (D5)

- [ ] 5.1 Migrate test fixtures using 2-part predicates (`agent.role`, `battery.level`, `source.value`, and any others surfaced by grep) to conforming 3-part predicates across `processor/rule/` and elsewhere.
- [x] 5.2 Extend the reference-config lint (3.1) to also fail on any non-3-part predicate reference in test fixtures, so the bad pattern cannot return.

## 6. Gates, review, PR

- [ ] 6.1 `gofmt`, `task lint`, `go vet ./...` + `-tags=integration` + `-tags=live_llm`, `go test -race ./...`, `task schema:generate` no-drift (a new config field for the gate mode carries a `schema:` tag → regen + commit `schemas/*.json`).
- [ ] 6.2 **BREAKING-change e2e gate** (the fail-closed flip changes ingest accept/reject behavior): run at least `task e2e:structural` (+ `e2e:semantic` if touched) green with `--build` BEFORE the flip lands on main. Judge from log markers, not task exit.
- [ ] 6.3 **semstreams-reviewer** pass (new write-boundary validation + RPC error contract + metric = silent-failure-class surface).
- [ ] 6.4 `openspec validate enforce-structural-invariants --strict`.
- [ ] 6.5 Branch + PR + CI green; on merge, unblock gh#519 (switch its `.value` disambiguation to the arity/structural model now that 3-part is guaranteed).
