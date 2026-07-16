# Entity and Predicate Contract Recovery

## Status

This branch is frozen for containment review. Do not push or open a pull request from
`codex/entity-id-contract-completion` in its current form. The local safety branch
`codex/entity-id-contract-pre-recovery` preserves committed HEAD `1c8d595d`.

The branch contains 24 commits across 334 files relative to `origin/main`. It combines the completion of two existing
OpenSpec changes with later graph-event identity, rule-watcher hardening, lineage, rule-pack identity, fixture-audit,
and release-documentation work. Those changes are related, but they are not one reviewable implementation unit. The
existing OpenSpec changes remain the requirements and evidence owners; recovery splits their implementation into
independently reviewable pull-request slices rather than inventing replacement specifications.

## Confirmed Process Failure

`docs/operations/28-entity-id-source-corpus.json` was generated before the source auditor and most fixture migrations
were committed. It reports 446 findings and is not release evidence. The report command also permits a report with
findings to be written and verified. A diagnostic snapshot was therefore left at the final documentation path and
later appeared authoritative.

The entity surface-disposition layer is also not an effective gate:

- 1,855 checked surface groups exist;
- 1,448 groups are classified as unrelated;
- the generic KV, match-name, string-builder, and `strings.Split` rules produce 1,512 groups, of which 1,355 are
  unrelated;
- a disposition records a human classification but does not prove that a relevant implementation delegates to the
  canonical entity-ID APIs.

The blocking disposition manifest will be removed from the contract gate. The useful audit boundary is the concrete
value corpus: typed/static entity IDs, patterns, prefixes, known configuration fields, triple subjects, typed `@id`
references, semantic test fixture calls, and exact intentional-negative annotations. Any future dynamic bypass audit
must be a separate type-aware advisory analyzer, not a repository-wide name heuristic.

The checked corpus report is removed. The audit runs directly against the tracked source set and fails on every
unclassified concrete violation. Diagnostic JSON remains available on demand without creating a final documentation
artifact that can become stale or appear authoritative.

At the recovery freeze, after removing the surface manifest but before correcting the positive fixtures, the audit
extracted 1,089 concrete candidates and reported 172 unresolved values. That snapshot identified the real corpus
obligation: correct canonical positive fixtures and bind actual negative tests, pre-substitution templates, and
documented empty sentinels to exact, reason-matched classifications. After adding string-keyed Go map extraction and
enforcing exact authoritative-reason matching, the live audit passes with 1,139 candidates: 1,030 valid, 109 exactly
classified, and zero unresolved findings.

## Recovery Boundaries

The current stack will be reconstructed into independently reviewable changes. This is a content split, not a claim
that the later work lacks value.

### 1. Entity-contract completion

Keep only the original contract enforcement and required compile/correctness fallout:

- remaining configuration, schema, and shared-validator boundaries;
- authoritative replay/direct-NATS poison rejection;
- concrete entity value-corpus migration;
- canonical test helpers and required positive-fixture corrections;
- entity pattern/prefix routing that is directly required by the declared contract.

The local merge gate is zero concrete corpus findings, focused and full tests, schema no-drift, real-NATS contract
proof, clean wipe/restart/reseed proof, and affected framework e2e. Owned-product migration remains a v1 release and
archive gate, not part of this local framework pull request.

### 2. Predicate lineage

Keep `agent.lineage.<role-key>`, the trusted namespace delegation, producer validation, and related agentic changes
under `predicate-contract-enforcement`. This is a predicate authoring contract and does not ride with entity IDs,
watcher mechanics, or rule-pack identity.

### 3. Rule-watcher hardening

Move generalized watcher-generation, provenance, evaluation-fence, and coalescing-set redesign out of the first
entity-contract pull request. Retain only the minimum pattern validation and configuration fallout in the core slice.

### 4. Canonical graph-event and rule-pack identities

Move the `(*Event, error)` constructor break, hashed alert/trigger identities, property ownership rules, atomic
publisher preflight, universal/static PackID contract, and duplicate-pack composition rejection to the dedicated
`rule-event-identity` change, gated by ADR-076 review. The full production manifest includes graph events, rulepack
composition, service binding, rule config/schema, command wiring, shipped configs, and fixtures.

### 5. Release and owned-product work

Defer product-facing cutover material until the local contracts and exact release evidence are final. The OpenSpec
changes must distinguish local merge prerequisites from coordinated v1 release/archive obligations.

## Recovery Rules

1. No current branch push or pull request.
2. No stale or nonzero-finding report may be committed as release evidence.
3. No task may be checked merely because adjacent code exists; each completion needs direct test, audit, or runbook
   evidence.
4. No new production behavior enters this recovery unless it is required to make an existing declared contract
   compile or behave correctly.
5. Mixed commits are reconstructed by content and reviewed again; they are not merged intact for convenience.
6. The graph-index reconciliation does not resume until the recovered local entity/predicate dependency gate is
   explicit and green.

## Next Actions

1. Remove the blocking surface-disposition inventory and stale checked report.
2. Make the concrete source audit the only entity-corpus gate.
3. Rebaseline the OpenSpec task lists around the four implementation slices and separate release obligations above.
4. Reconstruct the local core change, then run focused and full trust gates.
5. Obtain independent review before any push or pull request.
