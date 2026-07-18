## 1. Hardened validators (D1) + entity-ID drift fix (D3)

> Truth pass (2026-07-18, post-rebase): main independently landed the STRONGER canonical predicate
> contract (`vocabulary/predicate_contract.go` `ParsePredicate` — lower-kebab charset, byte bounds,
> typed reasons) and the authoritative persistence seam (`graph.MarshalEntityState` /
> `ValidateEntityStateContract`). Items below are restated against the CURRENT tree; see design.md
> "Reality found during apply — post-rebase".

- [x] 1.1 Harden `vocabulary.IsValidPredicate` (`vocabulary/predicates.go`). SHIPPED AS: a thin
  delegate to upstream's `vocabulary.ParsePredicate` (exactly 3 segments, each non-empty, lower-kebab
  ASCII `[a-z][a-z0-9]*(-[a-z0-9]+)*`, per-segment + total byte bounds, typed
  `PredicateValidationReason`) — strictly stronger than the "replace the bare dotCount==2 check"
  wording this task originally carried. `IsValidPredicate` remains the single boolean surface the
  ingest gate and the lints call; `ParsePredicate` returns the typed reason.
- [x] 1.2 Unit tests for `IsValidPredicate` (`vocabulary/predicates_test.go`): valid 3-part; 3-part
  with hyphens; underscore REJECTED (lower-kebab contract — stricter than originally specified);
  2-part; 4-part; empty segment (`sensor..celsius`); leading/trailing dot; empty string; only-dots;
  and `sensorml.capability.value` VALID (guards the gh#519 collision case). `ParsePredicate` edge
  cases live in upstream's `vocabulary/predicate_contract_test.go`.
- [x] 1.3 Fix `parseEntityID` drift (`graph/clustering/summarizer.go`): exactly-6 contract — the
  `len(parts) >= 6` + `strings.Join(parts[5:], ".")` dotted-instance leniency is removed; a
  non-6-part ID now yields empty `EntityParts` instead of a mis-split. (Kept as a `len(parts) == 6`
  split with a comment pointing at `message.IsValidEntityID` as the contract owner, rather than
  calling the validator — the function only splits, it does not accept/reject.)
- [x] 1.4 Grep every other predicate/entity-ID split-and-assume site and confirm each relies on the
  guarantee or guards explicitly. Residual guards verified 2026-07-18: `vocabulary/iris.go` guards
  part counts + TrimSpace before use; `test/e2e/client/nats.go:892` guards `len(parts) >= 3` before
  the band parse (`parts[1]`/`parts[2]`).

## 2. Handler-level PREDICATE gate — entity-ID gate already exists

> Reality (apply, updated post-rebase): graph-ingest validates entity IDs at the mutation boundary
> (`validateEntityID`, now a delegate to `pkg/types.ValidateEntityID`) and has
> `recordMutationRejection` (mutation_rejections{subject,reason} counter + WARN log). Upstream ALSO
> validates predicates on create_with_triples / update_with_triples / the Graphable lane via the
> authoritative contract seam (`validateMutationEntityState` / `validateMutationPredicates` /
> `prepareFactProjection`), which fires BEFORE this change's handler gate on those lanes. This
> phase's gate (`validateTriplePredicates`) is the FIRST predicate authority on `triple.add` /
> `triple.add_batch` and a defense-in-depth backstop elsewhere.

- [x] 2.1 Predicate structural validation (`vocabulary.IsValidPredicate`) wired at every
  triple-carrying write path: `handleTripleAdd`, `handleTripleAddBatch`,
  `handleEntityCreateWithTriples`, `handleEntityUpdateWithTriples` (AddTriples only — the gate
  skips RemoveTriples because they are deletions, not writes; malformed remove-predicates are NOT a
  cleanup lane — the upstream preflight `validateMutationPredicates` rejects them as synthetic
  triples with `invalid_request`, and the triple.remove lane validates via
  `vocabulary.ParsePredicate`; pre-v1 the remedy for a bad persisted predicate is wipe + reseed), and
  the Graphable ingest path (`ingestEntity`). SHIPPED CONFIG: none — the gate is unconditionally
  FAIL-CLOSED. An escape-hatch bool (`Config.AllowNonConformingPredicates`, replacing the
  originally-planned `StructuralPredicateEnforcement` enum) was prototyped and then REMOVED
  pre-release as provably inert: the authoritative persistence seam rejects unconditionally, so the
  hatch could only swap the caller-visible error code, never permit persistence. No bypass
  configuration exists (see design.md "Escape hatch REMOVED pre-release").
- [x] 2.2 On an invalid predicate the gate returns classified `ErrorCodeStructuralInvalid`; the
  rejection is metered exactly ONCE by the `meteredMutation` wrapper, which meters every classified
  handler error by its code (`mutation_rejections{subject,reason="structural_invalid"}` + loud WARN
  whose detail names token + entity). No new metric; the gate does NOT meter directly (reviewer
  finding: an earlier direct `recordMutationRejection` call double-counted the same rejection under
  a second reason label — removed). On the lanes where the upstream contract seam fires first, the
  specific reason is metered by upstream's `predicate_contract_rejections{lane,reason}` instead.
- [x] 2.3 Unit tests (`structural_predicate_gate_test.go`): the gate rejects a non-3-part predicate
  with classified `structural_invalid` (`TestValidateTriplePredicates_FailClosed_RejectsClassified`
  — the always-fail-closed contract); a conforming predicate is untouched
  (`TestValidateTriplePredicates_ValidPredicate_Untouched`). The two escape-hatch tests were removed
  with the hatch. NOTE: the original "still persists (behavior unchanged)" observe-mode wording is
  unattainable in the current tree — the authoritative seam is unconditionally fail-closed (see
  design.md post-rebase note).
- [x] 2.4 Entity-ID validator-unification cleanup: filed gh#531 → now CLOSED, resolved upstream —
  `message.IsValidEntityID` and graph-ingest `validateEntityID` both delegate to the single
  authoritative `pkg/types` validator (`semtypes.IsValidEntityID` / `ValidateEntityID`); the
  divergent `entityIDRegex` is gone.

## 3. Audit the corpus (gate the flip)

- [x] 3.1 Reference-config lint (`TestReferenceConfigs_AllTripleRefsAreThreePart` in
  `test/reference_configs_test.go`): every `$entity/$related.triple.<predicate>` reference in
  `configs/rules/**` must satisfy `vocabulary.IsValidPredicate`. Passing on the current tree.
- [ ] 3.2 e2e validation: `task e2e:agentic` (research-graph + deep-research tiers) MUST run green —
  the research-predicate rename touches the shipped research pipeline. (Requires Docker; run before
  merge — also the e2e prudence gate 6.2.)
- [x] 3.3 **AUDIT RESULT — NOT CLEAN.** The audit surfaced real violators (validating the
  maintainer's concern). Restated against the CURRENT tree (upstream landed the renames in
  lower-kebab, stronger than this branch's original snake_case picks):
  - **Shipped predicates — MIGRATED to canonical 3-part lower-kebab** (constants in
    `agentic/research/predicates.go`, stamped through `research_graph.go`,
    `configs/rules/{research-graph,deep-research}/**`, e2e scenarios): `research.requested→
    research.request.received`, `research.topic→research.request.topic`, `research.hint→
    research.request.hint`, `research.budget_tokens→research.request.budget-tokens`,
    `research.max_iterations→research.request.max-iterations`, `research.parent_loop→
    research.parent.loop`, `research.parent_role→research.parent.role`, `research.loop_id→
    research.loop.id`, `loop.role→agent.loop.role` (reuse canonical); example-fan-out
    `gather.completed_child→gather.child.completed`; mission harness `mission.phase→
    mission.state.phase` (`cmd/e2e-semstreams/mission/state.go`). Lockstep note: semteams (sister
    repo) consumes the research predicates — its lockstep update attaches to its **beta.149
    adoption** (the renames shipped upstream; see docs/operations/31 cutover checklist), not to
    this PR.
  - **`network.traffic.*` 4-part constants**: RESOLVED UPSTREAM by rename to 3-part kebab
    (`network.traffic.bytes-in/bytes-out/packets-in/packets-out` in `vocabulary/predicates.go`) —
    the earlier "dead 4-part constants, flag for removal" note is obsolete.
  - **Test fixtures**: the corpus is now machine-audited by upstream's
    `task predicate:test-audit` (`internal/predicateaudit` + `cmd/predicate-test-audit`): every
    predicate-shaped fixture must be canonical or carry an exact `predicate-audit:invalid/unrelated`
    classification. This branch's fixture surface is clean under it (see 5.1 for the repo-wide
    residue, which pre-exists on main).

## 4. Flip fail-closed + loud log (D2)

- [x] 4.1 Fail-closed SHIPPED unconditionally — no configuration knob: a structurally-invalid
  predicate rejects the mutation with classified `ErrorCodeStructuralInvalid` via `rejectInvalid` on
  the triple.add lanes (metered once by the `meteredMutation` wrapper as reason=`structural_invalid`
  — see 2.2), and with the authoritative seam's classified `invalid_request` on the
  create/update/ingest lanes. Loud Warn kept on all lanes. (Entity-ID rejection already fail-closed.
  The prototyped `AllowNonConformingPredicates` hatch was removed pre-release as inert — see 2.1 /
  design.md.)
- [x] 4.2 Tests (`structural_predicate_gate_test.go`, driving the production `meteredMutation` →
  handler chain against the mock KV store): invalid predicate → mutation rejected with a classified
  error, NOTHING persisted (store absence / entity-unchanged asserted), rejection metered exactly
  once by the wrapper (reason=`structural_invalid`; the gate's former direct-meter cell asserted
  untouched — no double-metering), loud Warn asserted (subject + token + reason) — covering
  handleTripleAdd (`structural_invalid`, fires before must-exist), handleTripleAddBatch (whole batch rejected,
  conforming sibling triple NOT persisted), handleEntityCreateWithTriples + 
  handleEntityUpdateWithTriples (seam-first: `invalid_request` + predicate_contract_rejections
  metric, entity unchanged), and the Graphable `ingestEntity` lane; regression: fully-conforming
  mutation persists with merge semantics intact (append + version bump + gh#519
  `.value`-final-segment predicate accepted).
- [x] 4.3 RPC error contract CONFIRMED on the production wire
  (`structural_gate_wire_integration_test.go`, real NATS request/reply): the rejection reaches
  callers as ADR-060 header-classified errors (X-Status: error, X-Error-Class: invalid,
  X-Error-Code: structural_invalid on triple.add / invalid_request on create_with_triples, body =
  `{message}` naming the predicate), reconstructed by `RequestClassified`/`ClassifyReply` as
  `*errs.ClassifiedError` — never silently decoded as success; nothing persisted. Caller audit
  (2026-07-18): every in-tree mutation-subject caller uses the classified request path —
  agentic/agentrun/nats_reader.go:64, graph/inference/applier.go:274 (covers graph-clustering's
  anomaly applier), pkg/lifecycle/graph_emit.go:139/184/231,
  processor/agentic-loop/graph_writer.go:112/143/195, processor/agentic-tools/decide.go:707/737/787,
  owned_fact_writer.go:149, write_todos.go:421/443, processor/gated-dag/claim.go:77/106,
  processor/research-graph-llmwrap/triplepub.go:98/120/175,
  processor/rule/triple_mutator.go:75/117/181 (all RequestWithRetryClassified);
  gateway/graph-gateway/component.go:1840 (RequestClassified); test/e2e client uses
  RequestClassified for the mutation lane. processor/agentic-memory/publisher.go publishes
  fire-and-forget to a port-resolved `graph.mutation.{loopID}` event subject — not a mutation-API
  request/reply caller. The legacy `error:` body prefix is retired (ADR-060 PR-D); headers are the
  contract.

## 5. Retire the 2-part fixture debt + lint (D5)

- [x] 5.1 Fixture migration + disposition (2026-07-18):
  - `processor/rule/expression/evaluator_test.go`: already fully migrated (entity-triple fixtures
    use `robotics.battery.level` / `test.battery.level` / `test.fixture.status` etc.).
  - `processor/rule/expression_factory_test.go`: entity-triple-bound `openspec.validated` (2-part)
    MIGRATED to `openspec.change.validated` (4 occurrences incl. the matching condition Field).
    JUSTIFIED EXEMPTION: the remaining `battery.level`-style `Field:` strings in this file are
    message-JSON-path fixtures (resolved against nested message data, e.g.
    `{"battery":{"level":15.0}}`) or factory parse-only fixtures — they are NOT graph-bound
    predicates, are correctly not flagged by the predicate audit, and renaming them to
    predicate-shaped tokens would misrepresent the rule Field grammar's message-path half.
  - Intentional-invalid fixtures (the gate tests' `agent.role`) stay, each carrying an exact
    `predicate-audit:invalid` classification.
  - Pre-existing residue NOT owned by this change (identical on main, 34 findings from
    `task predicate:test-audit`): `processor/graph-index/*` unresolved-surface findings and the
    `.value`-suffix embedded-substitution class in `processor/rule/triple_value_substitution_test.go`
    + `expression_factory_test.go:710` (introduced by PR #548). The audit task is not wired into
    CI/check:push; follow-up belongs to the audit's owner.
- [x] 5.2 Lint against regression: reference configs are covered by 3.1
  (`TestReferenceConfigs_AllTripleRefsAreThreePart`); TEST FIXTURES are covered by upstream's
  `task predicate:test-audit` corpus audit (`internal/predicateaudit`), which fails on any new
  non-canonical predicate fixture without an exact classification — a stronger mechanism than the
  originally-planned extension of the reference-config lint to fixtures.

## 6. Gates, review, PR

- [x] 6.1 Gates run 2026-07-18, all green: `gofmt -l` clean; `task lint` clean (revive, port guard,
  request guard); `go vet ./...` + `-tags=integration` + `-tags=live_llm` clean;
  `go test -race -count=1 ./...` all packages ok, 0 FAIL; `task schema:generate` — regen updates
  `schemas/graph-ingest.v1.json` by REMOVING the `allow_nonconforming_predicates` field (hatch
  removed pre-release; intentional, ships with the change); no go.sum churn.
- [ ] 6.2 **e2e gate — prudence, not a BREAKING mandate.** Reviewer-verified vs beta.149: NO
  previously-accepted write newly rejects — the fail-closed seam flip shipped upstream in beta.149.
  This change's true caller-visible surface is (a) the triple-lane error-code string
  (`structural_invalid` where the seam would say `invalid_request`), (b) the single-metric surface
  (`mutation_rejections{reason="structural_invalid"}`, metered once by the wrapper), and (c) the
  clustering `parseEntityID` display fix. Run `task e2e:structural` (+ `e2e:semantic` if touched)
  green with `--build` before merge as prudence. Judge from log markers, not task exit.
- [ ] 6.3 **semstreams-reviewer** pass (new write-boundary validation + RPC error contract + metric
  = silent-failure-class surface).
- [x] 6.4 `openspec validate enforce-structural-invariants --strict` — passing (2026-07-18, after the
  graph-ingest spec-delta rewrite to the unconditional fail-closed / no-bypass requirement).
- [ ] 6.5 Branch + PR + CI green; on merge, unblock gh#519 (switch its `.value` disambiguation to
  the arity/structural model now that 3-part is guaranteed).
