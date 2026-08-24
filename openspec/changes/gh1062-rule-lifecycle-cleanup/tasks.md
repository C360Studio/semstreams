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
- [ ] 7. Obtain independent SemStreams implementation review and hosted CI approval before integration.
