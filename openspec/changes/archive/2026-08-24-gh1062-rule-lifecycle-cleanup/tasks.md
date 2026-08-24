# Tasks

- [x] 1. Archive merged #1048's completed `align-standard-lifecycle-tests` change unchanged and establish its current
      `component-lifecycle` projection.
- [x] 2. Convert both Rule readiness tests to controlled LIFO cleanup with live Start authority, live NATS, bounded
      Stop, and strict nil result.
- [x] 3. Add a separate real-NATS accepted-parent abort proof under a fresh finite Stop context.
- [x] 4. Remove #1062 runtime-command-fence reinterpretation, terminal-watcher interpreter, normalization wiring, and
      dedicated normalization tests; restore direct Rule and ConfigManager cleanup behavior.
- [x] 5. Update portable lifecycle GoDoc, standard AcceptedStartParentCancellation proof, Rule abort observation, and
      component-lifecycle/runtime-context truth for controlled versus abort lanes.
- [x] 6. Rerun the exact focused unit/race, controlled real-NATS, abort count-20 observational, strict OpenSpec, and
      diff gates after correcting the reviewed abort proof's duplicate Stop ownership and deadline assertion.
- [x] 7. Landed via PR #1068 (`dc25bcc0`, merged 2026-08-24T00:10Z under the branch ruleset; the merge is the CI
      proof). Archived in PR #1071 under `openspec/README.md` rule 2 — the original wording asserted a post-merge
      fact and could not be ticked before merge.
