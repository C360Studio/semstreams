# Shared KV abort ownership

## Why

PR #1435's required hosted Test job failed in the predicate-layout smoke harness: a filtered KV listing returned a
deadline error and subsequent client drain exhausted fifteen seconds. A single independently reviewed experiment
then observed the same native watcher callback blocked on Updates after the real NewKVStore listing returned and
after Client.Close completed. Its diagnostic logged this defect but incorrectly returned PASS; that limitation is
retained explicitly and is not regression evidence. The initial hosted stall remains unattributed.

## What Changes

Repair the full-capability KVStore filtered-listing path by consuming WatchFiltered's native Updates directly.
Finalize each returned watcher through synchronous Stop and a finite post-Stop drain. Successful completion requires
snapshot completion and delivery closure; failures preserve typed primary errors and any cleanup failure. Preserve
the existing operation timeout and filter semantics, with no new public methods or caller tuning knobs.

Update the owner-load phase observer and directly reached test adapters to follow the production watcher. Add lean
ordinary regressions through NewKVStore, including a failing terminal-ownership assertion before implementation,
and verify sensitivity by removing the production completion obligation. Expiry cases use the virtual test clock.
After source review, run one focused native validation before broad required gates.

The original experiment's inventory.md and design.md remain historical evidence. The accepted production target is
repair-inventory.md, repair-design.md, repair-decision.md and the spec deltas. Exact experiment source and logs remain
under evidence/. The previous archived observer change remains unchanged; this active change reconciles the final
implementation and normative observer contract under draft PR #1435.

## Scope and authority

The coordinating session selects the independently reviewed private repair under the user's instruction to continue
this reliability work. No issue closure, historical-cause ruling or merge waiver is inferred. The earlier #1432 waiver
does not cover #1435. The completed experiment is not rerun. A failing post-repair native validation must be retained
and understood before further expensive testing; no timeout or workload relaxation is authorized.

## Impact

natsclient filtered KVStore implementation and focused tests, graph-index observer and reached full-bucket test
adapters, plus these specs and evidence. Minimal FilteredKeys/CatalogReader, unfiltered Keys, long-lived Watch,
SDK version, runner and latency assertions stay outside the repair. Contextless Stop is not preemptible; the private
five-second window bounds only the following drain, not total call duration. Whole-call measurements retain both.
#1286 retains separate budget/unit reconciliation; graph-query #1433/#1434 remains held at inventory.
