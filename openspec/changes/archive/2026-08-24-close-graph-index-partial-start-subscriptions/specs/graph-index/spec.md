## ADDED Requirements

### Requirement: Failed graph-index Start owns partial query responders

Graph-index MUST publish private failed-Start cleanup authority before any Start-owned resource can escape. If query
subscription acquisition fails after one or more prior subscriptions succeeded, Start MUST attempt one bounded
synchronous rollback derived from the Start parent through the canonical failed-Start helper. It MUST attempt native
Drain for every acquired query subscription while callback authority remains live, then cancel and join every
Start-owned child. It MUST clear exact handles only after complete rollback.

If rollback fails or expires, graph-index MUST retain every exact cleanup handle, reject another Start on that
instance, and permit later manager Stop to retry cleanup with the Stop caller context. A clean rollback MAY permit the
existing direct same-instance Start behavior, but no manager Start retry is introduced. Neither path may leave a
duplicate responder. No public lifecycle state, cleanup knob, subscription count, subject, schema, or configuration is
added.

#### Scenario: Second subscription failure rolls back the first responder

- **GIVEN** outgoing subscription acquisition succeeded with an admitted callback and incoming acquisition fails
- **WHEN** failed-Start rollback runs
- **THEN** outgoing Drain is attempted and its callback completes while Start callback authority is live
- **AND** only then are runtime children canceled and joined
- **AND** Start returns only after bounded rollback resolves or retains exact cleanup authority

#### Scenario: Incomplete rollback rejects reuse and later Stop completes cleanup

- **GIVEN** second-subscription failure and outgoing Drain cannot complete within the failed-Start budget
- **WHEN** Start returns and another Start is attempted
- **THEN** exact cleanup authority remains retained and the second Start is rejected
- **AND** later manager Stop retries with its caller context and clears authority only after cleanup succeeds

#### Scenario: Clean retry has no duplicate responders

- **GIVEN** partial query acquisition failed and bounded rollback completed successfully
- **WHEN** the existing direct caller starts the clean instance again
- **THEN** each canonical graph-index query subject has exactly one responder
- **AND** no responder from the failed attempt remains
