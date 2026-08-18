## 1. Binding truth reset

- [x] 1.1 Record the current-main surface inventory, historical PR #990 collision table, adopter seams, and blocking
  findings in `pr990-truth-reset-inventory.md`.
- [x] 1.2 Obtain owner disposition for the narrow boot-only reconstruction and preserve it in
  `pr990-boot-only-disposition.md`.
- [x] 1.3 Record the Flow authoring and boot-composition decision in ADR-096 without importing historical PR #990
  lifecycle or monitoring machinery.
- [ ] 1.4 Reconcile every binding ruling against the final implementation in
  `pr990-boot-only-implementation-conformance.md`; any deviation requires explicit owner signature.
- [x] 1.5 Validate this active OpenSpec change strictly after the target-state rewrite.

## 2. Fixed boot composition

- [ ] 2.1 Prove ComponentManager reads the existing configuration once during construction and composes only that
  enabled component set.
- [ ] 2.2 Prove ComponentManager has no component or model-registry configuration subscription and no generic runtime
  component-config write surface.
- [ ] 2.3 Prove post-construction component and model-registry writes leave running component identity and membership
  unchanged.
- [ ] 2.4 Prove Registry admits validated boot declarations, seals after composition, and exposes defensive values
  without live component handles.
- [ ] 2.5 Remove runtime replacement reservation, removal transition, and same-instance mutation protocols.
- [ ] 2.6 Verify Config Manager has no production diff beyond the owner-approved foreign-identity fatal Start and
  detached-path removal. Verify model watcher, validator/factories, lifecyclejoin, owner Start/Stop mechanics,
  CronScheduler, ACK ordering, NATS shutdown/recovery, and the E2E WebSocket client have no production diff.

## 3. Flow authoring and explicit publication

- [ ] 3.1 Preserve saved Flow create/read/update/delete, validation, and compilation behavior.
- [ ] 3.2 Prove saving or updating a Flow changes flowstore only and does not publish component configuration.
- [ ] 3.3 Add `POST /flows/{id}/publish-component-configs` using the existing validator/compiler and Config Manager
  component write operation.
- [ ] 3.4 Prove publication sorts instance names and performs deterministic sequential upserts.
- [ ] 3.5 Prove omission never deletes an existing component configuration.
- [ ] 3.6 Prove partial failure reports exact persisted names and the failed name, and retry safely converges.
- [ ] 3.7 Prove successful publication reports the persisted names, unchanged runtime, and required reboot.
- [ ] 3.8 Remove Flow runtime lifecycle state, operations, tools, metrics, timestamps, logs, and streams without aliases
  or a replacement monitor.
- [ ] 3.9 Prove retained name-keyed Flow observations do not claim Flow ownership of component lifecycle or activation.

## 4. Rule and deferred findings

- [ ] 4.1 Verify the reconstruction has no production diff in Rule code, Rule storage, Rule watchers, graph-index
  readiness, or Rule entity watching.
- [ ] 4.2 Record that separate Rule hot-reload target-state artifacts remain unadvanced and receive no completion
  credit from this change.
- [ ] 4.3 Keep multi-key configuration atomicity, partial watcher/arbitration behavior, and validator constructor
  effects as deferred findings rather than implementation prerequisites.

## 5. Migration and verification

- [ ] 5.1 Document exact removed APIs and the save/validate/publish/reboot sequence in a SemStreams-owned migration
  guide. Sister repositories remain read-only.
- [ ] 5.2 Run focused unit and integration tests, including race coverage for boot-only composition and publication.
- [ ] 5.3 Run `task lint`, `go test -race ./...`, contract tests, schema generation/no-drift, and strict OpenSpec
  validation.
- [ ] 5.4 Run relevant core and CRUD E2E before the breaking change lands.
- [ ] 5.5 Record the exact implementation commit and verification artifacts in the conformance ledger.
- [ ] 5.6 Obtain independent SemStreams reviewer approval of the final diff and every conformance-ledger row.

## 6. Explicitly uncredited work

- [ ] 6.1 Confirm this change claims no lifecycle migration, shutdown, controlled restart, dirty recovery,
  effect-before-ACK, release, archive, or tag-readiness completion.
