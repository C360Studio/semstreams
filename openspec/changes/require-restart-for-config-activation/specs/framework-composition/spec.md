## MODIFIED Requirements

### Requirement: Component start failures fail boot closed and surface in health

A lifecycle component whose `Start` returns an error during `Manager.StartAll` SHALL fail composition-root boot. It
SHALL be reported unhealthy while the failed boot is observable, never silently absorbed.

`ComponentManager.Start` SHALL be a component-start barrier: it may launch component `Start` calls concurrently but
SHALL return only after every launched call has returned, and SHALL return the joined errors of all failures. The
process SHALL NOT bring up its HTTP surface or report healthy while boot-time component starts are outstanding or
failed.

ComponentManager SHALL capture one validated desired-config snapshot before admission and construction. That snapshot
defines the complete boot transaction. A desired change committed after the snapshot, including while component
Starts are in flight, SHALL be next-boot state and SHALL NOT join, drain into, or mutate the current boot generation.
There SHALL be no post-boot dynamic component Start or restart path.

#### Scenario: Desired change after the boot snapshot waits for next boot

- **GIVEN** ComponentManager captured desired snapshot revision R and began boot composition
- **WHEN** desired component or model-registry revision R+1 commits before Start returns
- **THEN** the running process finishes composition from R
- **AND** R+1 reports restart required
- **AND** no reconcile drain creates, edits, removes, or restarts a component in the current process

#### Scenario: A boot-time component start failure fails StartAll

- **GIVEN** a registered lifecycle component whose `Start` returns an error
- **WHEN** `Manager.StartAll` runs
- **THEN** `ComponentManager.Start` returns an error naming the failed component
- **AND** `StartAll` fails, the HTTP surface is never brought up, and the process exits non-zero

#### Scenario: StartAll waits for every component start before proceeding

- **GIVEN** lifecycle components whose `Start` calls are launched concurrently
- **WHEN** `ComponentManager.Start` returns
- **THEN** every launched component `Start` call has already returned
- **AND** post-start boot steps observe the final boot-time component state, never a mid-start race

#### Scenario: Multiple boot-time failures are all reported

- **GIVEN** two or more components whose `Start` calls return errors in the same boot
- **WHEN** `ComponentManager.Start` joins the results
- **THEN** the returned error names each failed component and its error, not only the first

#### Scenario: No post-boot start failure state exists

- **GIVEN** a fully booted process
- **WHEN** desired component configuration changes
- **THEN** no component Start or restart occurs in that process
- **AND** health continues to describe only the sealed boot generation

#### Scenario: No unconsumed error hook survives

- **GIVEN** the component error hook registration surface
- **WHEN** no production caller consumes it after boot-time propagation lands
- **THEN** the hook is deleted rather than left as a dead exported surface
