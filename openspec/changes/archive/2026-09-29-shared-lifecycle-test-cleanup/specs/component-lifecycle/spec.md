# Accepted delta: component-lifecycle

Independent design acceptance: `../../review/design-review.md`.

## ADDED Requirements

### Requirement: Shared lifecycle tests own returned instances

The shared standard lifecycle suite, error-injection suite and lifecycle benchmarks SHALL take terminal ownership
of every nonnil component returned by their factory. They SHALL install lexical finalization before Initialize,
Start or assertions can exit the owning case or iteration. Factory resources not transferred by a returned
component SHALL remain the factory's responsibility. The exported factory and suite signatures SHALL remain stable.

#### Scenario: An assertion exits a lifecycle case

- **GIVEN** a factory returned an owned component
- **WHEN** an assertion terminates the case before its explicit terminal operation
- **THEN** lexical finalization SHALL call that component's Stop with a fresh finite cleanup context
- **AND** finalization SHALL complete its synchronous attempt before the case reaches substrate cleanup.

#### Scenario: Initialize or Start fails

- **GIVEN** a factory returned a component and its initialization or Start reports an error
- **WHEN** the suite leaves that case or iteration
- **THEN** it SHALL preserve the operation error and separately report terminal cleanup failures
- **AND** it SHALL attempt Stop when no concrete terminal attempt has yet occurred.

#### Scenario: Factory does not transfer an instance

- **GIVEN** a factory returns nil
- **WHEN** the suite handles that result
- **THEN** it SHALL report an acquisition failure without invoking lifecycle methods on nil
- **AND** it SHALL NOT claim cleanup authority over undisclosed pre-return resources.

### Requirement: Shared cleanup preserves authority and terminal results

Shared test support SHALL derive accepted work authority from its current testing context with a finite work budget.
Its lexical owner SHALL invoke controlled Stop with a separate fresh finite cleanup context before canceling accepted
Start authority. It SHALL finish this attempt before the Go testing runner cancels the test context and invokes
registered cleanup callbacks. If work authority has already ended, support SHALL report that state and preserve
accurate abort cleanup results rather than claim controlled success.

Controlled cleanup SHALL check returned errors and expired caller context. The explicit abort contract case SHALL
permit accurate native errors and require preservation of an expired Stop context's error. A concrete terminal
attempt SHALL suppress implicit repeat attempts, including after nonnil return; the existing explicit completed
repeated-Stop contract test SHALL remain permitted. Failed-Start cleanup retry SHALL retain its existing owner-specific
meaning and SHALL NOT establish running-generation rejoin. Finite context supply SHALL NOT mean Stop interruption,
guaranteed wall-clock return or complete joining after the caller bound wins.

#### Scenario: Controlled cleanup follows an early assertion

- **GIVEN** the accepted Start authority remains live when an assertion exits
- **WHEN** the lexical finalizer runs
- **THEN** Stop SHALL receive a fresh finite context while the accepted Start authority is still live
- **AND** cancellation of that Start authority SHALL follow the returned Stop attempt.

#### Scenario: Running Stop returns an error

- **GIVEN** a running component's concrete Stop has been attempted
- **WHEN** Stop returns a nonnil error
- **THEN** support SHALL preserve the result under the case's controlled or explicit-abort expectation
- **AND** fallback finalization SHALL NOT invoke Stop again as an assumed rejoin mechanism.

#### Scenario: An implementation ignores its Stop context

- **GIVEN** support invokes Stop synchronously with a finite context
- **WHEN** the implementation fails to observe cancellation
- **THEN** support SHALL NOT claim its context can interrupt that invocation or prove complete cleanup.

### Requirement: Shared iterations stop admitting work after failure

Parallel iterations, resource-leak cycles and lifecycle benchmarks SHALL stop admitting further work after an
unexpected lifecycle or cleanup failure. Parallel workers SHALL report errors without invoking FailNow or Fatal
and SHALL finish cleanup of already-owned instances before the parent returns. Their stop-admission signal SHALL
NOT cancel accepted Start authority of already-running instances. Benchmarks SHALL check operation results and
finalize each iteration, preserving which operation is timed.

#### Scenario: A parallel worker reports an initialization failure

- **GIVEN** workers own multiple fresh instances
- **WHEN** one worker reports an unexpected initialization failure
- **THEN** support SHALL report its phase and stop admitting new iterations
- **AND** each already-owned instance SHALL reach its terminal decision and every worker SHALL join.

#### Scenario: A resource-leak or benchmark cycle fails

- **GIVEN** the current iteration reports an unexpected operation or cleanup error
- **WHEN** its lexical owner finishes
- **THEN** the failure SHALL remain observable and no later iteration SHALL be admitted by that serial loop.

### Requirement: Error injection preserves base cleanup ownership

The error-injection suite SHALL own the underlying component independently of its injection wrapper. It SHALL
check prerequisite operations, preserve live Start authority through controlled operations, and use finite Stop
contexts. An injected wrapper Stop error SHALL NOT count as a concrete base terminal attempt. Finalization SHALL
invoke the underlying component directly and check its result without changing the wrapper's exported semantics.

#### Scenario: Injected Stop refuses before forwarding

- **GIVEN** a started base component is wrapped with an injected Stop error
- **WHEN** the suite verifies that injected error
- **THEN** lexical finalization SHALL still invoke the base component's Stop under fresh finite authority
- **AND** it SHALL report base cleanup failure independently of the expected injected error.
