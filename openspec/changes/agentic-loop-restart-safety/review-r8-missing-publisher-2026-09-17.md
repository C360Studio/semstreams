# R8 missing-publisher slice

Base: `68c14c8eb25c512e988f740cbf7ea14b6815976f`.
Independent INVENTORY PASS: `inventory-r8-missing-publisher-2026-09-17.md`, SHA-256
`bb8f5866fb5f89e082de1be2217cd61983d4889e8acf229e09f575e535ca6cd0`; 55/55 pins and five source fingerprints verified.
The architect confirmed materialization and the independent caller/test supplement.

## Architect task lowering — conformance review passed

Independent CONFORMANCE REVIEW PASS covers lowering SHA-256
`a849da8270a02f0e9d43cd94852ab00a5f5e810e9fe5e8c2a99f49a3ca9f577a`. No new owner decision or public surface.

This implements the existing absent-publisher clause in
`rule-agent-publishing / Publish-agent classification uses canonical wildcard coverage and durable publication`.
It introduces no design choice, exported symbol, dependency, storage or admission mechanism.

1. In `publishAgentOnce`, after existing `TaskMessage.Validate` succeeds and before run mint, anchor writes or
   publication, reject `e.publisher == nil`.
2. Use existing `errs.WrapInvalid` with a clear `publish_agent`/missing-publisher diagnostic. Preserve field,
   substitution, reserved-subject and task-validation error precedence.
3. Remove this action's obsolete optional-publication/no-op branch. Retain envelope construction, marshal errors,
   publisher error propagation and successful publication.
4. Remove the redundant `published` flag: reaching spawned-task handling means publication succeeded.
   Preserve foreign-entity and optional-mutator behavior.
5. Correct the dependency comment's blanket nil-success claim. Generic publish/approve and constructor signatures
   remain unchanged; executors used by other actions may still be constructed without a publisher.

## Bounded proof

Use public `ActionExecutor.Execute`; new behavior tests cite the existing requirement.

- Correct both existing nil-publisher tests to require the classified error; retain the no-spawned-triple assertion.
- With lifecycle/mutator installed and `run_scope=new`, prove zero run mint, anchor and spawned-task writes.
- Preserve invalid-data precedence, empty resolved for_each zero-dispatch success, successful configured publication
  and publisher-error propagation.
- Use the real evaluator fixture to observe its existing failure log/counter. Existing cron error-status coverage
  remains applicable; no telemetry or scheduler behavior is added.
- Run focused RED → minimum change → GREEN, rule package race, then independent implementation review.
  No native fixture is necessary for an absent dependency.

## Limits

This is only missing-publisher refusal, not subject coverage, registry admission, stream bounds, DiscardNew,
backpressure, four producer-to-loop paths, source settlement or whole R8. Preserve attempt accounting, #1311,
generic publisher fallback and frozen stack holds. Root owns the short migration note and conservative task truth.

Implementation is released for this bounded slice. No new whole-PR, push, E2E or merge proof is claimed.

## Implementation and focused evidence

The frozen three-file implementation adds 20 and removes 26 runtime lines. No new production symbol or dependency.
The five-line migration note identifies direct Go callers; factory wiring, empty fan-out and other actions are unchanged.

| Existing obligation | Current implementation/proof |
| --- | --- |
| Missing publisher fails without attempted-send effects | actions.go:1896; public Execute ordinary/fan-out/fallback tests |
| Existing invalid-input and zero-work behavior remains | six precedence controls and empty-fan-out control |
| Existing failure observability, no new owner | real evaluator log and failure-counter test; unchanged cron error control |

```text
449b7693c2f4cb99de12c28012cd4ea88e7cd42ab5bbce6e5acdcabe11644fcb  processor/rule/actions.go
81db95d7b6874a36812a7e316da12c7746d7c5529e8fe3de4367d15cb1ca5087  processor/rule/actions_test.go
5d64c6a3b010113add63309235aa868a5d430452c00a895ab80abe66b9641707  processor/rule/actions_missing_publisher_test.go
```

Focused RED compiled and reproduced nil success, run/anchor writes and missing evaluator failure telemetry (0.540s).
GREEN: five top-level tests and ten nested cases pass (1.574s). Full rule unit race: 638 top-level tests and 565 nested
cases pass, zero FAIL/SKIP records (5.126s). Tagged integration vet, pinned revive, formatting and diff checks pass.
These commands used GOCACHE=/private/tmp/semstreams-r7-test-cache, GOFLAGS=-mod=readonly, GOPROXY=off and GOSUMDB=off.
Exact selectors, exit statuses, log hashes, original backups and source patches are in developer handoff SHA-256
`9e9e97938ad538eeedb6a7202140e4ee5d4f859a0d7b02371540a81281f645bf`.
Durable evidence: `/Users/coby/Code/c360/semstreams-wt/gh1146-rescue-checkpoint.y3bWGc/r8-missing-publisher.5T05yh/`.
All three source hashes remained unchanged. Developer ownership is released; all test sessions are complete.

Root static checks: 387/387 tracked citations resolve; the three citations in the new untracked test were separately
checked against the active requirement. Strict OpenSpec: 55/55. Queue remains 13/22. No Docker/native or whole-repository
gate was run for this slice. Previous published full-gate/E2E evidence belongs to 68c14c8e, not this local source.
Independent final IMPLEMENTATION APPROVE: no findings. The reviewer verified the exact source, migration note,
RED/GREEN logs, complete rule race counts and vet/lint records. No commit, push, stack change or R8 completion.
