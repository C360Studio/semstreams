## ADDED Requirements

### Requirement: Filtered KVStore listings finalize native watcher delivery

`KVStore.KeysByFilter` and its `KeysByPrefix` alias SHALL preserve the existing operation timeout and native filter
semantics. When construction returns no watcher, operation cancellation or deadline expiry SHALL take precedence
and return nil keys with the typed context error. With a live operation context, a direct
`jetstream.ErrNoKeysFound` constructor result SHALL retain the existing `(nil, nil)` compatibility behavior without
Stop or drain; this is the sole exception to snapshot-completion and Updates-closure requirements.

For a returned watcher, successful results SHALL require initial snapshot completion and native Updates closure
after terminal cleanup. Post-marker entries SHALL be discarded. Other constructor failures, a missing watcher
without the sentinel, premature closure, non-benign Stop errors, or failure to observe closure SHALL NOT produce
successful keys. Operation cancellation SHALL return nil keys with the typed context error. Cleanup failure SHALL
remain distinguishable without replacing the primary failure. Later cancellation during terminal cleanup SHALL NOT
retroactively relabel an already completed snapshot as a collection timeout.

SemStreams SHALL invoke Stop once synchronously for a returned watcher, then consume Updates through closure using
a separate five-second terminal window derived from the operation context without inheriting its cancellation.
This window SHALL bound only the post-Stop wait, not contextless Stop or total public-call duration. No asynchronous
finalizer, retry, hidden fallback or adopter-configured cleanup budget SHALL be introduced.

Updates closure SHALL establish a delivery boundary only, not native goroutine joining or successful consumer
deletion. Only `nats.ErrBadSubscription` and `nats.ErrConnectionClosed` MAY be accepted as already-terminal Stop
conditions, and only after Updates closure is observed. Other Stop errors SHALL remain visible even after closure.
Drain expiry SHALL report incomplete delivery cleanup without falsely classifying a completed snapshot as an
operation-context deadline failure.

This requirement applies to the full-capability KVStore filtered path. The minimal-reader `FilteredKeys` and
`CatalogReader` method sets remain unchanged.

#### Scenario: Cancellation with pending native delivery

- **GIVEN** a filtered KVStore listing has a returned watcher with pending Updates delivery
- **WHEN** the operation context is canceled
- **THEN** the result is nil keys with the typed cancellation error
- **AND** Stop is requested synchronously and Updates is consumed through closure
- **AND** inability to observe closure is retained as an additional cleanup failure.

#### Scenario: Completed snapshot with queued later updates

- **WHEN** the initial snapshot marker is received before operation cancellation
- **THEN** only snapshot keys are eligible for the result
- **AND** later updates are discarded during terminal cleanup
- **AND** successful keys require delivery closure and acceptable Stop completion.

#### Scenario: Terminal drain does not complete

- **WHEN** Updates does not close within five seconds after synchronous Stop returns
- **THEN** the result is nil keys and a delivery-cleanup failure
- **AND** the call makes no native-completion claim
- **AND** no SemStreams-created asynchronous finalizer remains running.

#### Scenario: Stop has not returned

- **WHEN** native contextless Stop remains in progress
- **THEN** the post-Stop drain window does not certify or preempt that call
- **AND** public-call duration includes its actual duration.

#### Scenario: No-match constructor result respects cancellation

- **GIVEN** construction returns no watcher and direct `jetstream.ErrNoKeysFound`
- **WHEN** the operation context is still live
- **THEN** the result is `(nil, nil)` without Stop or drain
- **BUT WHEN** the operation context is canceled or expired
- **THEN** the result is nil keys with the corresponding typed context error, without Stop or drain.
