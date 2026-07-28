# framework-composition — delta for reopen-framework-owned-bucket-guards

## ADDED Requirements

### Requirement: Component start failures fail boot closed and surface in health

A lifecycle component whose `Start` returns an error MUST fail composition-root boot when the
failure occurs during `Manager.StartAll`, and MUST be reported unhealthy — never silently
absorbed — when the failure occurs after boot. `ComponentManager.Start` is a
**component-start barrier**: it launches component `Start` calls concurrently but returns only
after every launched `Start` has returned, and returns the joined errors of all that failed.
There is no fire-and-forget component launch at boot, and no compatibility variant that
preserves one. A component-level fail-closed assertion (e.g. graph-ingest's create-time
retention refusal) is thereby a process-level refusal: the process MUST NOT bring up its HTTP
surface or report service health while boot-time component starts are outstanding or failed.

Post-boot component starts (dynamic configuration add or restart) MUST NOT crash the process;
they record the component as failed with its error, and the component manager's health check
MUST report a failed component by name with its last error until it recovers. Health MUST NOT
ignore the failed state.

#### Scenario: a boot-time component start failure fails StartAll

- **GIVEN** a registered lifecycle component whose `Start` returns an error
- **WHEN** `Manager.StartAll` runs
- **THEN** `ComponentManager.Start` returns an error naming the failed component, `StartAll`
  fails, the HTTP surface is never brought up, and the process exits non-zero

#### Scenario: StartAll waits for every component start before proceeding

- **GIVEN** lifecycle components whose `Start` calls are launched concurrently
- **WHEN** `ComponentManager.Start` returns
- **THEN** every launched component `Start` call has already returned (successfully or not),
  so post-start boot steps (owned-bucket coverage pass, HTTP setup) observe the final
  boot-time component state, never a mid-start race

#### Scenario: multiple boot-time failures are all reported

- **GIVEN** two or more components whose `Start` calls return errors in the same boot
- **WHEN** `ComponentManager.Start` joins the results
- **THEN** the returned error names each failed component and its error, not only the first

#### Scenario: a post-boot start failure is visible in health

- **GIVEN** a running process in which a dynamically added or restarted component's `Start`
  returns an error
- **WHEN** the component manager's health check runs
- **THEN** the health check returns an error naming the failed component and its last error,
  and the process's service health reflects the failure until the component recovers

#### Scenario: no unconsumed error hook survives

- **GIVEN** the component error hook registration surface
- **WHEN** no production caller consumes it after boot-time propagation lands
- **THEN** the hook is deleted rather than left as a dead exported surface (a signal read by
  nothing is not enforcement)
