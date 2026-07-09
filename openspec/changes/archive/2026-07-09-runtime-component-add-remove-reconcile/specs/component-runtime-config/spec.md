## ADDED Requirements

### Requirement: Runtime component add/remove via the engine write methods drives a reconcile

The Manager SHALL, on a runtime component add (`PutComponentToKV`) or remove
(`DeleteComponentFromKV`), apply the change to the in-memory config synchronously
AND notify subscribers, so the `ComponentManager` reconciles it — spawning the
added component and tearing down the removed one — without requiring the
heavyweight `PushToKV` path. This holds even when the add/remove is interleaved
with other engine writes that raise the engine high-water revision.

#### Scenario: a component added at runtime is spawned

- **GIVEN** a running system watching config, with no `components.doc-source-003`
- **WHEN** a caller invokes `PutComponentToKV("doc-source-003", cfg)`
- **THEN** `doc-source-003` is present in the Manager's in-memory config
- **AND** subscribers to `components.*` are notified
- **AND** the `ComponentManager` spawns `doc-source-003`

#### Scenario: a component removed at runtime is torn down

- **GIVEN** a running system with a spawned `components.doc-source-003`
- **WHEN** a caller invokes `DeleteComponentFromKV("doc-source-003")`
- **THEN** `doc-source-003` is absent from the Manager's in-memory config
- **AND** subscribers to `components.*` are notified
- **AND** the `ComponentManager` tears down `doc-source-003`

#### Scenario: a delete interleaved under the engine high-water still reconciles

- **GIVEN** a runtime `DeleteComponentFromKV("doc-source-003")` at KV revision N
- **AND** a subsequent engine write raises the high-water revision above N
- **WHEN** the watcher processes the delete event (now classified engine-owned)
- **THEN** subscribers are still notified and the removal reconciles (the event is
  not silently skipped)

### Requirement: The engine-owned-revision skip suppresses only the in-memory re-apply

The config watcher SHALL, for an engine-owned revision (`revision <=
engineHighWaterRev`), suppress only the redundant in-memory re-apply of the value
and still notify matching subscribers — for both engine-owned and external events.
An engine-owned revision MUST NOT cause the notification to be dropped.

#### Scenario: an engine-owned event notifies subscribers

- **GIVEN** the Manager has just written a component and bumped its high-water revision
- **WHEN** the watcher delivers that event (revision at/below the high-water)
- **THEN** the in-memory config is not re-applied from the event
- **AND** subscribers matching the event key are still notified
