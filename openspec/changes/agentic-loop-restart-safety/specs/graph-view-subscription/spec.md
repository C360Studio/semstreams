## MODIFIED Requirements

### Requirement: View lifecycle and ownership

A graph view SHALL be explicitly constructed and owned (injected into its consumers; no ambient process-global
registry). Subscriber detach SHALL release that subscriber's buffered state. View shutdown SHALL terminate every
subscription with an explicit terminal signal, never a silent hang.

A graph view SHALL retain no context and no closure, provider, interface container, or other indirection that
recovers one. `Start` receives the owning lifecycle context as an operation argument. `Stop` joins every watcher,
subscriber, callback, and owned replacement task.

A failed view MAY be discarded and replaced by its lifecycle owner. `View.Restart` is not an exported recovery
contract. The owner SHALL stop the failed view, construct a fresh view, and start it with the active lifecycle context
held only on that goroutine's stack. A replacement SHALL not become visible until construction and `Start` succeed.

#### Scenario: Detach releases resources

- **GIVEN** a subscriber detaches or its context is cancelled
- **WHEN** subsequent ticks fire
- **THEN** the view retains no pending buffer for it

#### Scenario: Shutdown is observable

- **GIVEN** a view with attached subscribers is stopped
- **WHEN** shutdown completes
- **THEN** every subscriber observes explicit terminal close, not hang

#### Scenario: Owner replaces a failed view

- **GIVEN** a view has failed closed after watcher loss
- **WHEN** its lifecycle owner chooses recovery
- **THEN** the owner stops the failed view
- **AND** constructs and starts a replacement with the active lifecycle context
- **AND** publishes no replacement until construction and `Start` succeed

#### Scenario: Shutdown joins replacement work

- **WHEN** owner shutdown races view replacement
- **THEN** shutdown wins or joins the replacement
- **AND** no watcher, subscriber, callback, context, or restart provider survives `Stop`

#### Scenario: View retains no lifecycle context

- **WHEN** `Start` returns after launching owned work
- **THEN** the view retains no context or closure/provider that can recover one
- **AND** every owned task receives the active context directly and joins `Stop`
