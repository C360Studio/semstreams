# #1222 pre-owner design review

Verdict: **DESIGN CHANGES REQUESTED**. Two bounded design corrections are required before owner review. This is not an implementation review or owner ruling.

## Reviewed identity

- Source base: `12ae633381b8b8b26c333efe5f5c8691cfa47fb4`.
- Design: `openspec/changes/e2e-required-check-evidence/design.md`, SHA256 `3b2dfe966e786b8284ae5415a81a3955df208d724bad31d7a34e0c5375f51231`.
- Design manifest SHA256 `36b58a0d5db5c8add8adbcfe13594f628b4ca8c3cd023b8776481273d9316083`.
- Frozen passed inventory manifest SHA256 `584fa0db8b0d3fb44940578ec8dccf53c5b0642816827241e7a4bcf0152ec34a`.
- Inventory PASS review SHA256 `03f19cf75207715b502161a824cc7af7eddc34672ca7611f66802fadb0789d3f`.

Read the complete 411-line draft. Ran design-manifest checksum verification: all three entries OK. The inventory identity is preserved; task checkboxes/HOLD are later documentary status and do not replace baseline evidence. No tests, Docker, paid operations, source/spec edits or GitHub mutations. Only this review file is written. Applied `.agents/skills/orchestration-check/SKILL.md` to the task/reporter responsibility boundary.

## HIGH design.md:246 — Whole-invocation evidence starts after existing prerequisite work unless its boundary is defined

Mechanism: the design requires an incomplete envelope before infrastructure and a terminal record on every controlled Setup/Execute/teardown return (246–251), and describes whole-invocation controlled-return coverage (228–231). But it also retains existing Task scheduling (182–184) and only expressly exempts build failure before reporter startup (212–213). Current `taskfiles/e2e/agentic.yml:20–23` and statistical task dependencies run check-ports and runner build before task commands. `taskfiles/e2e/common.yml:27` makes check-ports depend on e2e:clean; infrastructure teardown therefore occurs before a report initialized in the task's cmds. A preflight/cleanup dependency failure is a controlled failure that can prevent both initialization and finalization. This is not the draft's abrupt process/host-death exception.

Smallest correction: state the actual reporting/bootstrap boundary and dependency ordering for adopted direct/composite tasks, including prerequisite cleanup, preflight, runner-build failure and cleanup-finalizer ordering. Make the promised artifact/failure contract match that boundary. Preserve the existing cleanup-before-probe ordering and Task execution authority; no new process coordinator is requested. If pre-envelope prerequisite failures retain only outer status/logs, state that exception throughout the guarantees rather than describing them as whole-invocation terminal records.

Verification/refutation: read agentic.yml16–30, common.yml3–30, statistical task commands, Taskfile.yml165–207. Existing rc/defer blocks preserve child failures but do not execute a future cmd-level initializer when a dependency fails. The build exception in the draft addresses only one prerequisite class; abrupt-death limits do not cover ordinary failed dependencies. Add a deterministic preflight/dependency failure case to the planned wrapper proof so the chosen boundary is falsifiable.

## HIGH design.md:131 — The initial required core filter obligation has no defined expectation for the actual fixture

Mechanism: selection table and named catalog (131–146) admit `core-dataflow.output-count/content/filter`; representative repair at299–303 requires expected filter decisions and run-correlated output. Current `configs/protocol-flow.json:182` has `criteria: {}` and :215 has `mappings: []`. `core_dataflow.go:220` and :330 contain historical '>50' filtering comments, but the current fixture is pass-through; :331 merely counts values above50 and :350–358 never rejects a wrong filter decision. Turning that prose into a required check could either claim selective-filter proof from a pass-through fixture or silently add a selective fixture/config change that the draft's write scope does not name. The independent expected set is not yet concrete.

Smallest correction: define exactly which configured behavior the required ID proves and the independent expected output set for the controlled inputs. Either accurately name the existing pass-through contract or explicitly bound and inventory the selective fixture change, including relevant config/compose ownership and rejection witness. Reconcile the chosen output-count expectation and mutation target with that same fixture. This is a request for a precise proof obligation, not a directive to add filtering or change production semantics.

Verification/refutation: read protocol-flow criteria/mappings and core_dataflow input/content sites. The draft does not literally promise '>50' and could intend pass-through; that interpretation would be valid if explicit. Its current wording leaves the central behavioral oracle unresolved. Removing warning fallbacks and checking run identity alone does not prove a rejected input was actually rejected.

## Confirmed strengths and refuted concerns

- Graph roundtrip identity across inference variants is grounded: `tiered.go:269` is a common stage; the existing probe checks actual state/query identities. No new graph API is implied.
- Structural zero-ML proof is existing behavior: `tiered_structural.go:53/67` checks embedding/clustering execution; `tiered.go:140` sets ExpectedClusters=0. Its check ID must retain the meaning zero clustering runs/component absence, not be read as a claim that no community record exists. This clarification does not require a new feature.
- Extending Result/TestRun and retaining typed tier output as a finalized projection uses existing owners. Legacy reader distinction and success-only-save repair address actual inventoried gaps.
- The bounded adoption is explicit rather than an opt-in strict-mode loophole: default core, all five composite constituents and slow-consumer are adopted; unchanged CI jobs therefore cannot rely on unattested evidence. Other individual tasks remain execution-only/unattested and cannot satisfy a required named selection. The owner must affirm that bounded adoption choice; it does not waive their existing release gates.
- Genuine alternatives include do-nothing, local callback/count repair, existing report/Task seams, larger process coordinator, and generic engine. Costs are structural or labeled historical, not fabricated measurements.
- Reporter-only CLI, generated run identity, per-member immutable files, one finalizer and existing Task lifecycle avoid adding a production orchestration owner. The reporting seam still needs the concrete prerequisite boundary above.
- Required IDs, diagnostic distinction, refusal of missing/duplicate/foreign outcomes, and preservation of unrelated existing fatal checks are materially stronger than stage totals. TTFT and B0/B2 remain diagnostics.
- PBT uses independent set/outcome invariants; deterministic boundary examples and targeted mutation plans include actual omission/false-green paths. Runtime/CI timings and attribution experiments remain unrun and correctly labeled.
- Compatibility/provenance risks remain explicit: external report consumers unknown, remote execution may lack full proof identity, observed app identity must not be replaced by runner SHA. These are owner-visible limits, not evidence that current Docker artifacts already supply every requested field.
- #1117/#769/#1128/#1293 and throughput/research owners are retained. Refreshing landed #1402 and active #1403/#1404 before shared edits remains necessary. No source change is authorized by this review.

## Recheck boundary

Recheck only the two corrected obligations and their propagated guarantees/verification plan, plus the new exact manifest. No broad inventory restart is needed. Preserve the passed inventory and this review identity as the design review chain. No owner acceptance should be inferred until DESIGN REVIEW PASS and the owner's explicit decision.

Root coordination after review: correction direction is explicitly existing-config pass-through identity/content/count, without selective-filter fixture expansion. Bootstrap correction must preserve cleanup-before-probe and existing Task execution authority. This direction does not alter the reviewed draft or discharge either finding until the revision is materialized.
