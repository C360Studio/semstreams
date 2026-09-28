## ADDED Requirements

### Requirement: A runtime configuration update MUST never block past a bounded Stop

A runtime configuration update (`ApplyConfigUpdate`, `UpdateWatchBuckets`) on a running rule processor MUST block
until the processor applies it on its runtime or that runtime ends, and MUST return the update's own result, a refusal
once Stop has closed admission, or the runtime's end error. The update carries no caller deadline, and it MUST NOT
block past a Stop bounded by its caller context: when that bound wins, Stop cancels the runtime and the update returns
its end error. A bounded Stop MUST return by its bound even when it closes admission at the instant the runtime ends.
The cron scheduler's `Stop(ctx)` MUST likewise be bounded by its caller context: it returns nil once the scheduler's
admitted fires have finished, or its context's error when the context ends first.

#### Scenario: An admitted update is released by a bounded Stop

- **GIVEN** a running rule processor executing a runtime configuration update that waits on its runtime
- **WHEN** Stop is called with a context whose deadline passes before the update finishes
- **THEN** Stop returns its deadline error, the runtime is canceled, and the update returns the cancellation

#### Scenario: Stop racing the runtime's end returns by its bound

- **GIVEN** a running rule processor whose runtime is ending
- **WHEN** Stop closes admission while the runtime is ending
- **THEN** Stop returns by its caller's bound and never waits on work the ended runtime will not run

#### Scenario: Cron scheduler Stop waits for an admitted fire under its caller context

- **GIVEN** a started cron scheduler with one fire admitted and running
- **WHEN** Stop is called with a caller context
- **THEN** Stop returns only after that fire finishes, returns nil, and a repeated Stop returns nil
