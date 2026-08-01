## ADDED Requirements

### Requirement: Every e2e tier MUST declare what its failure rules out

Each e2e tier MUST carry a written **diagnostic contract**: the capabilities it
exercises, and — stated explicitly — what a red result eliminates as a cause. A
tier that only lists what it runs tells an operator what happened; a tier that
states what a failure rules out tells them where to look next, which is the
whole value of running it.

A tier MUST NOT gate on two capabilities it cannot distinguish in its output. A
single red light covering unrelated subsystems is not coverage — it is an
unattributable alarm, and the cost is measured in re-runs rather than in fixes.

#### Scenario: a tier fails and the contract narrows the search

- **GIVEN** a tier whose diagnostic contract names the capabilities it isolates
- **WHEN** the tier fails
- **THEN** the contract states which capabilities are eliminated as the cause
- **AND** an operator can act on the failure without first re-running it to characterise it

#### Scenario: a proposed tier bundles two unrelated capabilities

- **GIVEN** a tier that would gate on both retrieval and answer generation
- **WHEN** its diagnostic contract is written
- **THEN** it cannot state what a red result rules out
- **AND** the tier is split rather than shipped with an unattributable signal

### Requirement: The retrieval tier and the generation tier MUST be separately runnable

Embedding-backed retrieval and LLM answer synthesis MUST be runnable
independently, because they fail for unrelated reasons, have different cost
profiles, and answer different questions. Retrieval is deterministic and cheap;
answer synthesis is non-deterministic, quality-graded, and resource-bound.

The retrieval tier MUST be able to run without any generative LLM service. The
generation tier MAY depend on the retrieval tier's capabilities, since an answer
is synthesised over retrieved context.

#### Scenario: retrieval is verified without an LLM present

- **GIVEN** a deployment with an embedding service and no generative LLM service
- **WHEN** the retrieval tier runs
- **THEN** it completes and reports on embedding-backed retrieval
- **AND** it does not fail on the absence of a generative service

#### Scenario: answer quality regresses while retrieval is intact

- **GIVEN** a regression confined to answer synthesis
- **WHEN** both tiers run
- **THEN** the generation tier fails and the retrieval tier passes
- **AND** the pair localises the regression without further runs

### Requirement: A gating tier MUST be runnable on the resources of the gate that runs it

A tier wired into an automated gate MUST fit the resources of that gate's
executor, and the fit MUST be established by measurement rather than assumed. A
tier that over-subscribes its host does not fail honestly — it produces
intermittent failures whose cause is the harness, which are then charged to the
code under test.

Where a tier cannot fit, the coverage that is dropped from the automated gate
MUST be stated explicitly and retained somewhere that does run, rather than
being silently lost by omission.

#### Scenario: a tier saturates its executor

- **GIVEN** a tier whose services require more CPU than the gate's executor provides
- **WHEN** the tier runs on that executor
- **THEN** it is not wired into the gate on the strength of a passing run alone
- **AND** the resource fit is measured under the executor's constraints before wiring

#### Scenario: coverage is dropped to fit a gate

- **GIVEN** a capability removed from a tier so it fits an automated gate
- **WHEN** the tier is wired in
- **THEN** the dropped capability is named and assigned to a tier that still runs it
- **AND** the automated gate does not read as covering it

### Requirement: The inference tier ladder MUST be documented as retrieval, not generation

Documentation of the inference tiers MUST state that the ladder governs
**retrieval** — which entities are found to be related — and MUST state
explicitly that generative answer synthesis is not a tier on it. Conflating the
two is a demonstrated source of operator confusion and of unattributable test
failures.

The documented distinction between adjacent tiers MUST be expressed in terms of
what a user can now find, not in terms of the mechanism that finds it. A reader
deciding whether to pay for Tier 2 needs to know what queries start working, not
which algorithm runs.

#### Scenario: a reader decides whether Tier 2 is worth its cost

- **GIVEN** the tier documentation
- **WHEN** a reader compares Tier 1 with Tier 2
- **THEN** the difference is stated as retrieval across differing vocabulary — a search for one term also finding entities that use a different term for the same thing
- **AND** the external service Tier 2 requires is stated alongside it

#### Scenario: a reader asks whether a tier generates answers

- **GIVEN** the tier documentation
- **WHEN** a reader looks for where LLM answer synthesis sits on the ladder
- **THEN** the documentation states that it is not on the ladder
- **AND** it identifies retrieval and generation as separate axes

#### Scenario: a telemetry-only deployment considers a higher tier

- **GIVEN** a deployment whose entities carry no text
- **WHEN** its operator consults the tier documentation
- **THEN** it states that tier selection does not affect such entities
- **AND** the operator can conclude that a higher tier adds cost without capability
