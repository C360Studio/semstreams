## ADDED Requirements

### Requirement: Persisted flow state describes desired activation only

Flowstore SHALL persist `desired_state` with values `absent`, `disabled`, or `enabled`. It SHALL NOT persist or expose
the current `runtime_state` field or label a desired configuration write `not_deployed`, `deployed_stopped`, or
`running`.

Deploy SHALL validate and persist desired component records and set desired state `disabled`. Start SHALL set desired
state `enabled`; Stop SHALL set desired state `disabled`; Undeploy SHALL remove desired component records and set
desired state `absent`. While a process is running, none of those operations SHALL claim to have changed runtime.

Persisted deployment, start, or stop timestamps and metrics SHALL describe desired-state mutation or be removed. They
SHALL NOT claim an unobserved runtime transition.

#### Scenario: Start changes desired state only

- **GIVEN** a running process and flow F with desired state `disabled`
- **WHEN** Start accepts F's desired mutation
- **THEN** flowstore records desired state `enabled`
- **AND** the current runtime remains unchanged
- **AND** the result reports restart required

#### Scenario: Dirty restart retains desired activation

- **GIVEN** desired state `enabled` committed before power loss
- **WHEN** a new process boots against retained durable state
- **THEN** boot consumes the enabled desired component set
- **AND** recovery does not depend on a prior runtime-state transition or shutdown hook

### Requirement: Flow observation separates desired from effective truth

Flow reads and monitoring SHALL return desired state, current desired provenance, independently observed effective
state, immutable boot-applied provenance, and `restart_required`. A successful boot SHALL assign a unique `boot_id`,
canonicalize the selected desired configuration, and seal a framework-owned digest for the boot snapshot and relevant
flow/component subsets. Effective observations SHALL name the `boot_id` and digest they actually applied.

`restart_required` SHALL compare current desired digest and membership with boot-applied digest and membership. It
SHALL NOT compare only desired/effective activation labels: both may say `enabled` while their structural configuration
differs. Runtime health SHALL be reported separately and SHALL NOT prove that desired configuration became effective.
Effective state and provenance SHALL NOT derive from flowstore or copy desired state.

If no authoritative runtime observer is available, effective state SHALL be `unknown`. A partial, failed, starting, or
stopping runtime SHALL be represented honestly rather than collapsed into desired `enabled` or `disabled`.

#### Scenario: Pending desired edit is visible

- **GIVEN** effective flow F is running from boot state C
- **WHEN** an operator stores desired state C' that changes F
- **THEN** observation returns C' as desired
- **AND** returns the unchanged effective state derived from C
- **AND** returns boot-applied provenance for C rather than C'
- **AND** `restart_required` is true

#### Scenario: Equal activation labels do not hide structural drift

- **GIVEN** boot-applied flow C is enabled
- **AND** current desired flow C' is enabled but changes component configuration or membership
- **WHEN** flow F is observed
- **THEN** both activation labels may be `enabled`
- **AND** their canonical digests differ
- **AND** `restart_required` is true independently of runtime health

#### Scenario: Missing observer does not fabricate running

- **GIVEN** flowstore desired state is `enabled`
- **AND** no authoritative process observer is available
- **WHEN** `monitor_flow` reads F
- **THEN** effective state is `unknown`
- **AND** boot-applied provenance is `unknown`
- **AND** it does not report runtime `running` from desired state

### Requirement: Flow mutation responses name persistence and activation separately

Deploy, Start, Stop, and Undeploy responses SHALL identify the committed desired transition, the unchanged effective
runtime observation when available, and whether restart is required. Operation names MAY remain authoring verbs, but
their response and documentation SHALL NOT call the desired write active, deployed, running, stopped, or removed in
the current process.

#### Scenario: Deploy response is honest

- **GIVEN** a running process
- **WHEN** Deploy commits valid disabled desired component records
- **THEN** the response reports desired state `disabled`
- **AND** it reports runtime unchanged and restart required
- **AND** no persisted or monitored field claims the flow deployed in the running process
