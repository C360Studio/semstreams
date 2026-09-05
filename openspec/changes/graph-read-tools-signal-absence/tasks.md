# Tasks — graph-read-tools-signal-absence

**Amend a task line when the work HAPPENS, not only when it succeeds.** A `[~]` is a recorded decision and MUST also
be noted in the spec delta. No task here asserts a post-merge fact; the merge gate owns CI.

Word discipline: `scripts/openspec-queue.sh` reads hold / blocked / blocking / halt / red / failed / failing in any
OPEN task line as a live caveat; use "pause seam", "barrier", "abort", "does not compile", "MUST fail".

Premises measured on `main@797d294a`: `processor/agentic-tools/executors/graph_query.go:442` (dead `type` compare),
`:531-540` (stub), `:591-617` (no `IsRelationship`), `:428-431` (silent continue), `graph/types.go:24-47`,
`natsclient/kv.go:522-547,558-598`, `graph/kvcatalog.go:261`, `pkg/types/entity_id.go:160-164`,
`release/tier1-packages.txt:79`, `test/e2e/scenarios/agentic/approval_signal.go:36-40,77-88`.

Sequencing: 1.6 governs when sections 3–6 start; the implementation's file set intersects none of the 176 unique
paths Codex's #759/#1146 stack (PRs #1156/#1159/#1141) holds, and the delta is ADDED-only.

## 1. Claim and design

- [x] 1.1 Draft PR #1262 opened with `Closes #1261` on `claude/gh1261-graph-read-tools`, own worktree; the OpenSpec
      change is its first commit.
- [x] 1.2 Architect verification pass over the explorer inventory — `inventory-verification.md` (2 strikes, 11 additions).
- [x] 1.3 Independent inventory review (`semstreams-reviewer` re-derivation) recorded on PR #1262 — INVENTORY CHANGES
      REQUESTED (2 blocking rows: `graph.query.summary` as the same-class owner of "IDs by type"; the neighbors budget vs
      spec `:467`). Architect amendment in progress; re-review follows.
- [x] 1.3a Explorer inventory materialized as `inventory.md` with a parseable `base:` line. `task inventory:verify` on it:
      119 pins, 15 ok, 5 moved, 34 drift, 65 malformed, 44 unparsed — the explorer's table/range format does not fit the
      verifier grammar (#1256), so the malformed/unparsed counts are grammar, not drift; the 5 MOVED rows in
      `message/triple.go` (`:56→58`, `:61→63`, `:70→74`) are real pin errors at the explorer's own base and confirm the
      reviewer's HIGH. Re-pin the rows the design rests on; do not treat the verifier's exit as a gate here.
- [x] 1.3b Architect amendment folding the review and the owner note (#1261, 2026-09-05): both BLOCKING rows closed
      (`graph.query.summary` as the same-class owner; the budget classified as a model-facing cap distinct from spec
      `:467`), pins fixed, six rows added; `design.md` gained § Budget, § Break classification and sequencing, and § Tool-preference premise;
      owner questions renumbered 1–11.
- [x] 1.3c Re-review of the amended inventory (`semstreams-reviewer`) recorded on PR #1262 — INVENTORY CHANGES
      REQUESTED (round 2): BLOCKING `graph.index.query.predicateList` as the same-class owner of "which predicates
      exist"; HIGH `hierarchyStats` as a second owner of "which types exist"; four pin corrections; the ADR-106 `:81`
      half-quote (RC-6); two premise pins inside Codex-held files. Round 1's six findings confirmed closed.
- [x] 1.3d Architect amendment round 2 folding 1.3c (owner rows added, ADR-036 case-against rewritten on the
      predicate-catalog owner, RC-6 walked path named, pins fixed) plus the external-evidence table from the Cekikj
      restatement (Part 2 § 2.3/§ 2.5, Part 3 § 3.3/§ 3.4/§ 3.8) in `design.md` § Tool-preference premise.
- [ ] 1.3e Re-review round 3 (`semstreams-reviewer`) recorded on PR #1262.
- [ ] 1.4 Owner INVENTORY PASS on the PR; owner rulings on `proposal.md` questions 1–11 recorded on #1261.
- [ ] 1.5 Owner places the milestone (recommendation: `v1.0.0-beta.165`).
- [ ] 1.6 HOLD — sections 3–6 do not start until (a) 1.4 is recorded and (b) either Codex's #759/#1146 stack
      (PRs #1156/#1159/#1141) has landed or owner question 11 relaxes this to archive-order coordination. Re-check
      the file list against the PAGINATED Codex file lists (`gh api repos/:owner/:repo/pulls/N/files --paginate`)
      before 3.1, and re-pin the two premises that live inside held files (`executors/httprequest.go:23`,
      `component.go:974-994`).

## 2. Spec delta

- [x] 2.1 Three ADDED requirements in `specs/agentic-tools/spec.md`; no MODIFIED block —
      `openspec/specs/agentic-tools/spec.md:435/:467/:487` are held by PR #1159's pending delta.
- [ ] 2.2 Delta reconciled against the owner rulings from 1.4.

## 3. Code

- [ ] 3.1 `graph_query.go`: `KVKeyLister`; type-segment grammar helper shared by `entity_type`/`filter_type`;
      `queryByType` served; `extractRelationships` typed over `EntityState` with `IsRelationship()`, dead branch
      deleted; `predicates_present`/`filter_registered`; neighbors budget, `unresolved`, hints; descriptions rewritten.
- [ ] 3.2 `register_graph_query.go`: adapter `KeysByPattern` via `natsclient.FilteredKeys`.

## 4. Tests

- [ ] 4.1 Unit tests named in `design.md` § Test plan; fixtures via `graph.MarshalEntityState`; `// spec:` citations.
- [ ] 4.2 Integration `TestIntegration_QueryByType_ListsFromEntityStates`.
- [ ] 4.3 Fails-without-fix for the `IsRelationship` filter and the segment match, run against the committed state.
- [ ] 4.4 `predicate_authority_contract_test.go` unchanged and green.

## 5. Docs

- [ ] 5.1 `docs/operations/migration-graph-read-tools.md`: result-shape changes for `query_by_type` and
      `query_relationships` (home per owner question 5).

## 6. Gates

- [ ] 6.1 `task lint`; `go test -race ./processor/agentic-tools/...`; `-tags=integration -p 2`;
      `openspec validate --strict`; `task spec:properties`; `task schema:generate` no drift;
      `task api:compat:report` unchanged from baseline; `go run ./cmd/entity-id-audit .` (not in `task lint`).
- [ ] 6.2 `task e2e:agentic` green (the approval walk executes the served `query_by_type`).
- [ ] 6.3 `semstreams-reviewer` implementation pass recorded in section 7.
- [ ] 6.4 Archive as the final content commit; narrow archive-sync check.
