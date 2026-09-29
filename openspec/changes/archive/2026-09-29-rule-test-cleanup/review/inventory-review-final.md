# Rule test cleanup final inventory review

Mode: inventory review only. Verdict: **INVENTORY PASS**.

Reviewed checkpoint: `047be916111816f353d2998a06b40e24bcecb0e6`, branch
`codex/gh1428-rule-test-cleanup`, worktree
`/Users/coby/.codex/worktrees/gh1397-runner-tests/semstreams`.
Frozen source baseline: `caa98f5acae60efbc669ad1e1795ab6e903abd42`.

Exact reviewed artifacts:

- `openspec/changes/rule-test-cleanup/inventory.md`, SHA256
  `ed8e5ce8c3c49416b5a0e3e2fe6477389a640881bfb4342253cb84535666ad73`.
- `openspec/changes/rule-test-cleanup/review/ownership-ledger.md`, SHA256
  `1b74ac431316ec3d945780bbab0768b0b6a3072c856960609d931623effbb1b6`.
- The exact baseline, caller, supplemental-query, pin and evidence companions identified by those artifacts and
  `review/evidence/inventory-completion-manifest.json`.

## Findings and disposition

No blocking or high inventory findings remain. The prior bounded ownership-ledger blocker is closed.
No design option, implementation, production repair, cleanup-safety guarantee or merge readiness is approved here.

The ledger accounts for all 24 exact baseline identities and all 37 physical lifecycle-helper call sites.
The count is 20 direct-test roots plus four helper roots; the helper callers are 24 scheduler, six cron Processor,
five run-scope and two revision sites. These overlapping sets are not 61 independent resources.
Profiles retain concrete owner distinctions, and rows reconcile acquisition, fallible setup before protection,
Start/operation authority, cancellation owner, helper return, terminal attempt, result handling and substrate order.

The previously missing distinctions are now explicit and agree with independently inspected source:

- Cron restart rows H27–H30 distinguish two fresh Processors, their separate 30-second Start authorities,
  explicit checked phase Stops and unconditional later fallback callbacks, including failure-path second attempts.
- Hardening B12 records deferred Start cancellation before fallback testing cleanup, the flag set only after
  successful explicit Stop, and its specific post-Stop borrow/coalescer observations.
- Graph-ingest profiles G/V distinguish testing-context cancellation from callback order and identify setup exits
  through Start/Flush before terminal registration. Their revision-tracking Processor fields are unstarted support.
- Debounce rows retain parent/subtest substrate lifetimes. General-rule rows distinguish operation authority from
  Start authority. B21/B22 preserve redundant client termination and their actual lexical/callback ordering.
- Scheduler rows H05/H22 identify test-owned fire goroutine joins separately from production lifecycle completion.

The appended 25 retained constructor/substrate references cover adjacent callers already present in the collected
sets. Non-started cases and deliberate nil, repeat, concurrency, registration and gauge probes remain distinct from
this exact repair population. The five reviewed resolutions retain their full dependency boundaries and fingerprints;
none intersects the exact root/caller/acquisition-helper declarations. They are neither new debt roots nor automatic
permission to refresh approvals. Source-level ownership facts needed for the bounded design are sufficiently complete.

## Independent enumeration and attempted refutation

The strict blind sequence was followed in this review. Before opening inventory.md, any inventory companion, or the
prior review, I read the reviewer contract, project/protocol and the supplied problem boundary/baseline, then enumerated
rule test source. Initial file/status/diff-stat discovery exposed filenames only, not inventory conclusions.
I notified the coordinator when the blind phase was complete, before reading the submitted inventory.

Bounded independent source searches covered direct Start/Stop calls, cleanup registrations, helper declarations and
callers, context origins, goroutine launches, cache/coalescer owners and raw watcher/coordinator paths. A source-derived
helper call list independently produced the four physical counts above and constructor/tracker/metrics caller sets.
Additional raw watcher and support hits were examined as adjacency, not silently added to the 24-root batch.

Structural queries, each once successfully after cache recovery where needed:

- `gopls references processor/rule/processor.go:1191:22`.
- `gopls implementation processor/rule/processor.go:1191:22`.
- `gopls call_hierarchy processor/rule/lifecycle_runtime_test.go:153:6`.
- `gopls workspace_symbol 'rule Stop'` returned no symbols; that result was not used as evidence of absence.

The first attempts failed to load the workspace fully because the default Go cache was not writable. Their partial
results were not accepted as complete. Retrying with temporary GOCACHE/GOPLSCACHE succeeded. The successful structural
queries used default build selection; source enumeration separately included integration files. No successful default
query is represented as integration execution or exhaustive integration type coverage.

After the blind phase, I compared the inventory and prior review with the independent results, then read the ledger
and its final suffix at the coordinator's final checkpoint. Narrow source ranges refuted possible conflations of
cancel-before-Stop, callback LIFO, explicit attempt versus successful completion, repeated fallback, and shared versus
per-subtest substrate. Existing shared lifecycle and graph-ingest test owners were inspected as matching problem
shapes, without selecting an adoption design. Native Processor, CronScheduler and graph-ingest contracts remain
separate; contextless operations and post-deadline owner-lane receives remain explicit limitations.

## Artifact and gate evidence

- HEAD and both final artifact hashes match the identities above; the working tree is clean. Production/test sources
  and cleanup baseline remain unchanged from the frozen source. Only inventory/evidence and inventory task truth
  changed. No design/spec delta exists; task 1.1 is supported, and inventory review/design tasks remain gated.
- Baseline SHA256 is `909d254426e4b190d3fe10c86f936dcea742d8a6998b7b90a93bbe02ccab2615`.
  Original companion hashes, all six raw ZIP members, all six completion-manifest artifacts and the current-claims
  archive/member identities were independently verified.
- All annotated helper reference source hashes match current unchanged source. Exact root/caller line texts match
  after whitespace normalization. All 37 physical helper sites occur in the ledger; B/H row counts are 24/37.
- Raw integration query records 51–54 equal the default physical sets exactly: 16, eight, nine and 12 references.
  Supplemental annotations preserve those 45 build-selection records without adding physical callers.
- Canonical `scripts/inventory-verify.sh` independently passed: ledger pin companion **370/370**, inventory **93/93**;
  zero moved, ambiguous, drift, malformed or unparsed pins. Every one of the ledger's 370 distinct inline quoted
  source pins is represented in that companion. Pin validity was not substituted for semantic ownership review.
- Current cleanup/context/lifecycle specs and the active proposal/tasks were read. The original reviewer report was
  read only after blind enumeration. Its acknowledged method limitation does not carry into this fresh review.

No tests, Docker operations, broad repository census, source modifications or commits were performed by this reviewer.
Only the requested report and temporary read-only evidence/tool-cache outputs were written outside the repository.
No independent live GitHub query was made; live claim state remains attributed to the coordinator's durable records.
The separate #1421 required-job flake gate and #1404-only waiver do not become inventory/design scope or merge approval.
Actual executed/skipped coverage and mutation/PBT decisions belong to the later concrete design/implementation.

**INVENTORY PASS — the exact checkpoint above is sufficiently complete to begin design.**
This verdict is not design acceptance, implementation authorization, or a claim of native wall-clock interruption,
complete joining, leak freedom, test execution, or merge readiness.
