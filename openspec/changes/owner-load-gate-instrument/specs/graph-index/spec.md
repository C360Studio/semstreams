# graph-index — delta

> Delta for #1284. MODIFIES the owner-filter proof requirement so the CI activation guard stops collapsing an
> absolute ceiling and a latency contract onto one value, decides a breach by corroboration rather than by one
> shared-runner sample, and records what it measured on every run. Repairs a citation defect: the current text
> attributes the 3-second CI guard to ADR-065, which contracts no such budget — its stated absolute bound for this
> operation class is the 10-second handler timeout (`docs/adr/065-...:49`). The source of the 3-second figure is
> ADR-077 §8 condition 4 (`docs/adr/077-...:139`).
>
> **The corroboration rule and the amended reading of "each operation" are OWNER-GATED** (`design.md` § 9, Q1). The
> two existing scenarios are restated verbatim because a MODIFIED block restates every scenario.

## MODIFIED Requirements

### Requirement: Fixed-position owner filtering is proven before production reconciliation activates

PREDICATE, NAME, and source-owned INCOMING MUST be tested against real NATS using literal exact-arity
forward and owner filters constructed through the `nats-kv-keys` contract. The proof MUST cover filter-string
construction, malformed longer/shorter keys, matching correctness with no false positives, neighboring-owner and
reversed-axis controls, concurrent Put/Delete with exact-key deduplication, cancellation, empty buckets, restart,
and clean bucket recreation. Concurrent-mutation correctness MUST be evaluated only after mutations advance to a
declared final ENTITY_STATES revision and reconciliation reaches that watermark, with zero false matches,
omissions, stale survivors, or ownership violations.

Performance MUST be gated by absolute budgets, not comparison, and the two bounds MUST NOT be collapsed onto one
value. The **absolute ceiling** is the framework-enforced KV deadline that every `KeysByFilter` call already runs
under; the proof MUST observe it as a typed error from the operation itself and MUST NOT restate it as a separate
predicted budget. The **latency contract** is the ADR-077 §8 condition 4 CI guard (5,000 hot members, 20 spread
predicates, each operation under 3 seconds) together with one sustained-churn run on the 21,000-entity profile at the
configured worker shape and one stress shape, achieving p95 at most 3 seconds, p99 at most 5 seconds, no operation at
the 10-second handler bound, temporary consumers returning to baseline, and no unbounded queue growth. The selected
worker maximum MUST be enforced in validated configuration before activation.

A repetition at or above the CI guard's per-operation budget MUST NOT decide the guard on its own. It MUST be
recorded, and it MUST trigger exactly one corroborating measurement set of the same filter, taken after the breaching
set completes; the guard fails when the corroborating set also breaches, and MUST NOT re-measure a third time. Every
per-repetition budget assertion in the guard MUST follow this rule, not only the one that fires most often.

The measured distribution for every filter MUST be recorded on every run of the guard, whether or not it fires, and a
breach MUST be recorded even when the corroborating set passes. A run that records no distribution is not activation
evidence.

A store that fails correctness or budget MUST defer its cleanup authority to a separately specified bounded
replacement mechanism; that mechanism becomes a completion dependency of this change, and deferral MUST NOT waive
the required `[A] -> [B] -> []` result for any query-visible store.

#### Scenario: a source entity enumerates only its INCOMING assertions

- **GIVEN** INCOMING rows for multiple targets and sources
- **WHEN** the source-axis fixed-position filter is evaluated for one six-part source ID
- **THEN** every matching row is owned by that source assertion
- **AND** no row owned by another source is returned

#### Scenario: unit maxima do not replace real-NATS proof

- **GIVEN** canonical six-part entity IDs are bounded and every entity-bearing unit maximum fits shared budgets
- **WHEN** production activation is evaluated
- **THEN** unit arithmetic and representative data do not authorize activation
- **AND** activation waits for pinned real-NATS maximum key/filter exact-match conformance

#### Scenario: a single anomalous repetition does not decide the guard

- **GIVEN** a filter whose repetitions are all far inside the per-operation budget except one that breaches it
- **WHEN** the guard evaluates that filter
- **THEN** the breaching repetition is recorded with the whole measured distribution
- **AND** one corroborating measurement set of the same filter is taken
- **AND** the guard passes when every repetition of the corroborating set is inside the budget
- **AND** the recorded breach remains in the run output so its rate is countable across runs

#### Scenario: a sustained breach fails both measurement sets

- **GIVEN** a filter whose latency genuinely exceeds the per-operation budget
- **WHEN** the guard evaluates that filter
- **THEN** the first measurement set breaches
- **AND** the corroborating set breaches
- **AND** the guard fails, naming the filter, the breaching repetitions, and both distributions

#### Scenario: a corroborating set is taken exactly once

- **GIVEN** a corroborating measurement set that itself breaches the per-operation budget
- **WHEN** the guard evaluates the outcome
- **THEN** it fails immediately
- **AND** no third measurement set is taken, because a guard that re-measures until green records nothing

#### Scenario: the measured distribution is recorded on a passing run

- **GIVEN** a run in which every filter is inside every budget
- **WHEN** the guard completes
- **THEN** the per-filter repetition count, p50, p95, p99, and max are recorded for every filter
- **AND** the recorded run is admissible as ADR-077 §8 activation evidence against that revision

#### Scenario: an operation that reaches the framework bound fails as an error

- **GIVEN** a filtered key listing that reaches the framework-enforced KV deadline
- **WHEN** the guard's repetition completes
- **THEN** the operation returns a context-deadline error and the guard fails on that error
- **AND** the failure is not reported as a budget comparison, because the ceiling was observed rather than predicted
- **AND** no partial key set is accepted as a successful owner snapshot
