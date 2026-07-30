## ADDED Requirements

### Requirement: rule entity evaluation is gated by authoritative graph-state validation

The rule processor MUST validate the complete authoritative `ENTITY_STATES` watch before pattern-specific bootstrap
may evaluate. A pattern-watcher entry at revision `R` MUST NOT evaluate until the authoritative guard has processed
cleanly through at least `R`. A typed stored-state contract failure MUST latch reset-required and prevent every later
rule evaluation, metric, transition, cleanup, and action until operator wipe/restart/reseed. An unexpected guard or
pattern-watcher transport close MUST disable evaluation as degraded and MUST NOT be reported as stored-state poison.
Expected cancellation, retirement, and shutdown closes MUST NOT report either failure state.

#### Scenario: poison outside every configured pattern blocks all bootstrap action

- **GIVEN** a valid entity matches a configured rule pattern and malformed authoritative state matches none
- **WHEN** either state is observed first during bootstrap
- **THEN** no pattern entity is evaluated and no rule action or evaluation metric is produced
- **AND** the rule graph lane reports reset-required rather than ready

#### Scenario: a matching revision cannot overtake earlier poison

- **GIVEN** a pattern watcher receives valid revision `R` before the authoritative guard reaches `R`
- **AND** the guard observes malformed state at an earlier revision
- **WHEN** the guard latches reset-required
- **THEN** revision `R` never evaluates or produces a rule side effect

### Requirement: dynamic watcher authority is atomic and generation-scoped

Each managed `(bucket, pattern)` watcher MUST have an exact generation identity. A dynamic replacement MUST validate
the requested set and prepare every added transport before publishing any configuration or authority change. If any
preparation fails, every prepared addition MUST stop and the previous desired configuration and watcher authority
MUST remain unchanged. Commit MUST register additions and retire removals atomically under the dispatch gate. A
retired generation MUST lose authority before physical Stop and MUST remain unauthorized even if Stop fails or the
same pattern is later added with a new generation.

#### Scenario: failed preparation leaves the old watcher set authoritative

- **GIVEN** a replacement needs several new watchers
- **WHEN** preparing any addition fails
- **THEN** prepared additions are stopped without callbacks
- **AND** the previous configuration and exact watcher generations remain authoritative

#### Scenario: stale decoded and queued work cannot cross generations

- **GIVEN** generation 1 queued or decoded work before its pattern was removed
- **AND** the same pattern is added as generation 2
- **WHEN** the old callback reaches dispatch or debounce fires
- **THEN** generation-1 work is rejected before current-state fetch, evaluation, metric, transition, or action
- **AND** valid generation-2 work remains eligible

### Requirement: coalesced entity work preserves provenance and one current evaluation

Managed debounce work MUST retain the exact watcher key and generation that authorized it. When several still-active
patterns queue the same entity in one coalescing window, the callback MUST authorize the entity if at least one exact
provenance remains active, then fetch current state and evaluate at most once for that batch. Bootstrap entries MUST
bypass live debounce and preserve `Bootstrap=true`. A delete MUST pass the per-entity revision fence before removing
pending work. An admitted delete MUST remove every pending legacy and provenance-bearing work item for the entity
before evaluation; a stale delete MUST leave newer queued work intact.

#### Scenario: overlapping active patterns evaluate one current state

- **GIVEN** two active watcher generations observe the same entity in one live coalescing window
- **WHEN** the batch fires
- **THEN** current ENTITY_STATES is fetched once and matching rules are evaluated once for that entity

### Requirement: one bounded per-entity fence orders revisions and deletion

Queued or in-flight entity work MUST retain a per-entity fence. Current-state fetch, evaluation, delete transition,
and rule-state cleanup MUST be serialized beneath that fence. A revisioned snapshot MUST evaluate only when its
revision is greater than the last completed revision. One delete revision delivered by overlapping watchers MUST
perform its on-exit transition and cleanup at most once. A concurrent delete MUST NOT overtake a fetch already holding
the fence, and an older fetched snapshot MUST NOT evaluate after a newer delete completed first.

Active fence entries MUST NOT be evicted. After the final queued or in-flight reference leaves, the watermark MUST be
bounded by a 15-minute idle TTL and a 65,536-entry LRU capacity. These limits define an internal dedupe horizon, not
operator retention or authoritative history. Shutdown MUST drain queued work without callbacks, release all retained
references, clear idle entries, and report an error if active references remain.

#### Scenario: duplicate overlapping deletes fire on-exit once

- **GIVEN** a stateful entity is currently matching
- **WHEN** overlapping watchers concurrently deliver the same delete revision
- **THEN** on-exit and rule-state cleanup run once
- **AND** both deliveries release their fence references

#### Scenario: a stale overlapping delete cannot erase a recreation

- **GIVEN** a fast watcher has advanced the entity fence and another active watcher retains queued work for a newer put
- **WHEN** a lagging overlapping watcher delivers an older delete revision
- **THEN** the delete fails the revision fence before queue mutation or evaluation
- **AND** the newer pending put remains queued and retains its fence reference

#### Scenario: shutdown drains retained work

- **GIVEN** entities are queued inside a coalescing window
- **WHEN** the processor shuts down
- **THEN** the queue is drained without evaluation, every queued fence reference is released, and idle watermarks are
  cleared
- **AND** cleanup fails visibly rather than silently succeeding if an active reference remains
