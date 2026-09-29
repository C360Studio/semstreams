# Rule test cleanup inventory review

Mode: inventory review only. Verdict: **INVENTORY CHANGES REQUESTED**.

Reviewed checkpoint: `8a305ed1b773e1b54d930ac342fa6440e9ae5546`, branch
`codex/gh1428-rule-test-cleanup`, worktree
`/Users/coby/.codex/worktrees/gh1397-runner-tests/semstreams`.
Frozen source baseline: `caa98f5acae60efbc669ad1e1795ab6e903abd42`.
Reviewed artifact: `openspec/changes/rule-test-cleanup/inventory.md`, SHA256
`0a5d50dfedf10925deb730d8bbef40d2000f2b72c1e5632f22cf9df0bc82ae02`.
All source citations below refer to that frozen source; inventory citations refer to the artifact above.

The exact debt population and helper caller sets are sufficiently measured. Design remains blocked by one bounded
missing evidence product: the semantic fixture ownership ledger acknowledged at inventory line 171. This does not
require another repository-wide census, a production lifecycle redesign, runtime execution, or sister-repository
inventory. No target state or implementation recommendation is reviewed here.

## Blocking finding

**BLOCKING openspec/changes/rule-test-cleanup/inventory.md:171 — Ownership facts are not reconciled per fixture and caller**

- Mechanism: the 24 root records identify where terminal calls occur; the 119 annotated reference records identify
  calls and selected source lines. Neither establishes who owns each acquired fixture before fallible setup, which
  scope cancels its accepted Start authority, or whether an explicit terminal attempt leaves fallback cleanup armed.
  Those facts determine whether the proposed test-only scope can satisfy the current lexical-ownership requirement.
  Generic descriptions of three native Stop implementations cannot substitute for the caller-specific facts.
- Evidence: `startCronProcessorForTest` acquires at `cron_scheduler_integration_test.go:76`, initializes at 82,
  registers cancellation at 85, starts at 86, and registers Stop at 87. Its callers at 229/260 and 313/327 own two
  distinct processors across restart phases. Explicit calls at 235/315/329 coexist with unconditional fallback
  callbacks. The ledger must distinguish a concrete attempted Stop from a completed Stop and describe the existing
  second-attempt path when the explicit assertion fails. The current prose names these calls but does not reconcile
  their case ownership or phase transitions.
- Additional evidence: `entity_watcher_hardening_integration_test.go:38` defers cancellation; the cleanup callback at
  73–76 is guarded by `stopped`, which changes only after the explicit Stop assertion at 118–119. An earlier fatal
  exit reaches cancellation before testing cleanup; an explicit Stop error leaves fallback armed. Conversely, many
  direct-defer cases stop before their earlier cancellation defer. The ledger must preserve that distinction instead
  of classifying all cleanup under one order. `state_cleanup_integration_test.go:26–27` also registers test-client
  termination in addition to the callback already installed by `natsclient/test_client.go:863–867`; substrate
  ownership cannot be inferred solely from the component Stop line.
- Smallest correction: materialize the bounded ledger described below, citing existing raw source evidence where it
  is complete and reading only missing ranges. Reconcile it to the exact root/caller identities. Describe current
  facts and explicit unknowns; do not select helpers, budgets, signatures, or implementation options in this step.
- Attempted refutation: inspected the companion schemas, caller annotations and raw records 9, 14, 27, 40, 47–50,
  plus narrow current source ranges. Raw records contain much of the needed source, but their annotations and
  keyword-selected lines are not a complete semantic ledger. The inventory itself expressly disclaims one.
  Therefore this is missing synthesis of bounded evidence, not proof that more repository surfaces must be searched.
- Verification authority: `test-cleanup-policy` / “Lexical ownership of lifecycle test fixtures” requires ownership
  before fallible setup, provisional ownership through transfer, Start/terminal/substrate ordering, checked concrete
  results and bound expiry, and no implicit retry after a concrete terminal attempt. The inventory gate needs the
  current facts against which a later design can address those obligations.

### Bounded acceptance requirements for the ledger

1. Cover all 24 exact baseline identities: 20 direct-test roots and four cleanup-owning helper roots. Cover the 37
   physical caller sites of those four helpers (24 scheduler, six cron Processor, five run-scope, two revision).
   These are call-site counts, not 37 distinct test functions or 61 distinct owners. Factor common helper facts once
   and link every caller to them; separate owner instances where a case starts two processors.
2. For each relevant fixture instance or factored equivalent group, record the acquisition/constructor, concrete owner
   type, first fallible setup/assertion after acquisition, existing cleanup registration and any uncovered exit
   interval, helper return/transfer boundary, and caller finalization scope. Pin the enclosing test/subtest lifetime
   where parent and child scopes differ. Constructor failure before any returned owner must be distinguished from a
   failure after ownership exists.
3. Record accepted Start authority and operation authority separately: exact derivation, finite budget if any,
   cancellation owner, and observed lexical order relative to component cleanup. Record test-context cancellation
   where applicable. Do not infer live authority from a variable name or finite-context classification.
4. Record every explicit terminal phase call reached by these fixtures, its error handling, fallback state, and the
   operations that follow it. Include the cron restart calls and the hardening explicit Stop. State whether fallback
   can attempt Stop again after failure; keep deliberate lifecycle probes distinct from ordinary cleanup debt.
5. Name the substrate/support resources whose lifetime affects each fixture: NATS client/container, subscriptions,
   callback or goroutine release/join ownership, and relevant cache/tracker support. Cite registration plus actual
   execution order of defers/testing cleanups, including redundant termination. State the native completion
   observation actually present, or “none asserted”; an inventory need not manufacture a runtime join proof.
6. Use the existing constructor/tracker/metrics/substrate helper caller sets to establish the acquired-owner chain
   and classify adjacent non-started/deliberate-probe callers as retained adjacency. Do not expand the 24-root repair
   population just because a constructor has other callers. Preserve the five exposed reviewed resolutions and
   identify which dependencies a later design would touch; membership alone does not authorize refreshing them.
7. Close with exact coverage accounting and a short list of remaining unknowns. Each source-level ownership/order
   unknown needed to choose a test-fixture design must be resolved; native cancellation limitations and runtime
   execution evidence may remain explicitly qualified as described below.

## Disposition of the seven declared gaps

| Gap | Disposition for this inventory gate |
|---|---|
| 1. Independent re-derivation | Structural caller sets independently confirmed in this review; see method limitation below. No new census is required. |
| 2. Per-case ownership ledger | **Blocking**, with the bounded acceptance requirements above. |
| 3. Four raw-only helper queries | Nonblocking annotation parity. Raw integration records 51–54 contain 16, 8, 9 and 12 references respectively; each physical set exactly equals its already annotated default query (indexes 1, 2, 9, 10). Record that equality/build coverage or add the annotations. No missing physical caller was found. |
| 4. Native dependency limits | Honest limitation, not a demand for production fixes or exhaustive cancellation proof. Preserve contextless watcher/cache operations, owner-lane post-deadline receives, and owner-specific Stop/retry distinctions. The later design must not claim deadline supply guarantees wall-clock termination or joining. |
| 5. Claim-record incompleteness | Not a missing test-fixture owner. Retain retrieval failures/truncation as limitations; add the coordinator's narrow current claim/gate evidence if making live status assertions. No broad GitHub census is needed for this verdict. |
| 6. Source execution status | Runtime execution is properly deferred. Build selections and skip annotations are not execution proof. Inventory acceptance needs source lifetime facts, not tests/Docker runs. Implementation must later report actual executed/skipped coverage. |
| 7. External adopter enumeration | Untriggered for this test-only inventory with no new exported surface. Internal `rule_test` is not an external adopter. Re-enter that obligation only if a later design changes an outward-facing contract. |

The coordinator separately reports live verification of draft #1429, open #1421, and the #1404-only waiver. This review
has not independently queried GitHub. #1421 remains a future merge gate; it is not an inventory/design blocker and
creates no authorization to repair graph-index in this batch.

## Verified identity and bounded enumeration

- Repository status was clean at entry and exit. Reviewed production/test sources and the baseline file have no diff
  from `caa98f5a`; the checkpoint contains inventory/evidence additions only. Proposal/tasks remain inventory-gated;
  no task is falsely checked complete. There is no design or spec delta to review.
- Verified all three companion SHA256 hashes and every member hash in `inventory-raw-manifest.json` against the ZIP
  bytes. Verified the current baseline hash is
  `909d254426e4b190d3fe10c86f936dcea742d8a6998b7b90a93bbe02ccab2615`.
  The companion has 24 distinct identities in ten files; its helper companion has 11 queries/119 reference records.
- Ran bounded source search at the frozen baseline:
  `git grep -n -E 'Stop\(context.Background\(\)\)|Terminate\(|\.Close\(|\.Shutdown\(' caa98f5acae60efbc669ad1e1795ab6e903abd42 -- 'processor/rule/*test.go'`.
  It also exposes ordinary phase calls and deliberate probes, which are not automatically extra baseline roots.
- Ran `gopls workspace_symbol -matcher=fuzzy 'rule Stop'`, then one integration `gopls references` query each at
  `processor/rule/cron_scheduler_test.go:94:6`, `cron_scheduler_integration_test.go:60:6`,
  `actions_run_scope_integration_test.go:108:6`, and `triple_mutator_revision_integration_test.go:46:6`.
  Results exactly match the inventory's physical caller sets: 24, six, five, two. No whole-file pin rereads occurred.
- The first gopls attempt could not write the default build cache. Retried with temporary GOCACHE; subsequent
  reference queries used temporary GOCACHE and GOPLSCACHE and succeeded. Cache-write diagnostics on the symbol query
  were not treated as evidence of absence. No tests or Docker operations ran.
- Accepted the materialized `inventory-pin-check.log` recording 93/93 as mechanical pin evidence, and independently
  checked artifact/member identities. Mechanical pin validity does not establish semantic coverage.

Method limitation: my initial boundary read extended through inventory line 75 and exposed the root/helper tables
before independent enumeration. That did not satisfy the contract's strict blind-read ordering. The source search and
four structural queries were independently executed and reconciled rather than accepted from the tables, but this
report must not be represented as a fully blinded review. The substantive blocker above is demonstrated by the
submitted evidence and narrow source reads. A subsequent review of the completed ledger can retain these structural
results without claiming that the original blind-enumeration sequence occurred.

## Review boundary and conclusion

Read the reviewer contract, applicable AGENTS/protocol/project context, current cleanup/context/lifecycle specs, and
the active proposal/tasks. Reviewed evidence identities, source accounting, native limit descriptions and the relevant
caller ranges. No repository mutation, test run, commit, or design work was performed; only this requested report and
temporary tool caches were written.

**INVENTORY CHANGES REQUESTED — exact blocking list: the per-case fixture ownership ledger at inventory.md:171.**
The four raw-only query annotations are not a second blocker. Native cancellation limits, implementation-time runtime
validation, untriggered external adopters, and the separate #1421 merge gate must not become scope expansion.
