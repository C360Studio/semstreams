# graph-index — delta

> **Post-archive corrections (2026-09-12).** This is a design-time record and two classes of figure in it were
> corrected after it was archived. **(1)** Every cross-kind latency comparison — a ratio between two measurements
> that do not do the same work — is repudiated; the like-for-like figures are tabulated in `design.md` § 11.11.
> **(2)** The `60c79736` supervised record cited throughout predates the submission-order instrument and is
> superseded by `b10671ed`, whose worst p95/p99 are 580.383 ms / 591.050 ms, making the full profile's 3s/5s
> **5.2x/8.5x** rather than the 9.6x/15.6x recorded here (`design.md` § 11.12). Current truth for both lives in
> `docs/operations/32-predicate-layout-smoke-harness.md`, § "Owner-filter acceptance record". No ruling changes.


> Delta for #1284, written to the **owner ruling of 2026-09-11** (#1284 comment 5635299542). The CI owner-filter
> profile is demoted to a regression guard; ADR-077 condition 4's activation evidence rehomes onto a supervised run
> that owes a fresh measurement at the current server pin. The CI per-operation budget is **deleted**, not widened:
> the framework-enforced KV deadline is the ceiling, observed as the operation's own typed error.
>
> **Citation repair.** `openspec/specs/graph-index/spec.md:184` attributes the 3-second CI guard to ADR-065, which
> contracts no such budget; its stated absolute bound for this operation class is the 10-second handler timeout
> (`docs/adr/065-...:49`). The 3-second figure came from ADR-077 §8 condition 4 — but the #1284 amendment removed it
> from that condition, so the repaired text cites no ADR for a numeric budget and states none.
>
> **Q7(a) WAS applied** under a recorded session-level call (`tasks.md` § 4.2, `proposal.md`): `operationBudget` is
> gone from the `ownerLoadProfile` struct entirely, taking the full profile's unreachable `10 * time.Second` with it.
> After the per-repetition assertions were deleted nothing read the field, and the value it held could never be
> compared, because the framework's 5s KV deadline fails the operation first. The normative text below already
> licenses this: it forbids restating the ceiling as a predicted budget **at any value, above or below it**, and is
> not scoped to the CI profile.
>
> **Both questions are now RULED** (#1284 comment 5640631023, 2026-09-11). Q7(b) — the full profile keeps
> `p95Budget: 3s` / `p99Budget: 5s` despite sitting at 9.6x/15.6x over the measured 311.449 ms / 320.157 ms; the
> looseness is published in the acceptance record rather than tightened, and re-derivation is #1287's. Q6 — ADR-065
> needs no edit, confirmed. ADR-077 condition 5 carries the same "10-second handler bound" phrase and is NOT edited;
> the text below keeps that bound on the query-handler path.
>
> The two existing scenarios are restated verbatim because a MODIFIED block restates every scenario.

## MODIFIED Requirements

### Requirement: Fixed-position owner filtering is proven before production reconciliation activates

PREDICATE, NAME, and source-owned INCOMING MUST be tested against real NATS using literal exact-arity
forward and owner filters constructed through the `nats-kv-keys` contract. The proof MUST cover filter-string
construction, malformed longer/shorter keys, matching correctness with no false positives, neighboring-owner and
reversed-axis controls, concurrent Put/Delete with exact-key deduplication, cancellation, empty buckets, restart,
and clean bucket recreation. Concurrent-mutation correctness MUST be evaluated only after mutations advance to a
declared final ENTITY_STATES revision and reconciliation reaches that watermark, with zero false matches,
omissions, stale survivors, or ownership violations.

Performance MUST be gated by absolute budgets, not comparison, and exactly one absolute ceiling MUST apply to a
directly measured key listing: the framework-enforced KV deadline that every such call already runs under. The proof
MUST observe that ceiling as the operation's own typed error and MUST NOT restate it as a predicted per-operation
budget at any value, above or below it. A predicted budget below the enforced deadline fails on stalls the framework
itself tolerates; a predicted budget above it can never fire. The 10-second handler bound remains the ceiling on the
query-handler path and MUST NOT be carried as a per-operation budget on a directly measured key listing.

Activation evidence for the owner-filter workload MUST come from a supervised run recorded against the current
server and SDK pin, not from a shared-runner CI job. The recorded evidence MUST carry the repository revision, host
CPU and memory, container runtime version, server and SDK pin, run timestamp, and the complete per-filter
distribution. A supervised record taken under a superseded pin MUST NOT be cited as current evidence, and a run that
records no distribution is not activation evidence.

Every recorded latency value MUST carry its unit where the value appears, not only in a note elsewhere in the
document, and any budget derived from the record MUST state its basis in that same unit. A budget whose stated
justification cannot be checked against a recorded measurement in the same unit is not derived.

The continuously-running CI profile is a regression guard, not activation evidence. It MUST assert exact match-set
correctness for every owner and forward filter, exact convergence after churn, the typed-error ceiling, p95 and p99
latency budgets derived from the supervised record, a bounded dispatcher queue, temporary consumers returning to
every per-store baseline, released temporary subscriptions, zero slow consumers, and the server resident-set bound.
It MUST NOT assert a per-repetition wall-clock budget.

The selected worker maximum MUST be enforced in validated configuration before activation.

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

#### Scenario: an operation that reaches the framework bound fails as an error

- **GIVEN** a filtered key listing that reaches the framework-enforced KV deadline
- **WHEN** the guard's repetition completes
- **THEN** the operation returns a context-deadline error and the guard fails on that error
- **AND** the failure is not reported as a budget comparison, because the ceiling was observed rather than predicted
- **AND** no partial key set is accepted as a successful owner snapshot

#### Scenario: a runner stall below the framework deadline does not fail the regression guard

- **GIVEN** a CI run in which one repetition of one filter takes several seconds while the same run's other
  repetitions of the same filter complete in well under a second
- **WHEN** the guard evaluates that filter
- **THEN** no per-repetition wall-clock comparison rejects the run
- **AND** the guard fails only if the percentile budgets, the match sets, the convergence check, the queue bound, the
  consumer baselines, the subscription count, the slow-consumer count, or the resident-set bound fail
- **AND** the measured distribution is recorded so the excursion remains countable

#### Scenario: a layout regression still fails the regression guard

- **GIVEN** a change that makes an owner or forward filter over-match, rescan, or reconstruct a retired catalog
- **WHEN** the CI profile runs
- **THEN** the guard fails on the match-set assertion, the convergence assertion, or the percentile budgets
- **AND** an order-of-magnitude latency regression is visible in the recorded distribution even where a budget does
  not reject it

#### Scenario: the measured distribution is recorded on a passing run

- **GIVEN** a run in which every filter is inside every budget
- **WHEN** the guard completes
- **THEN** every filter's per-repetition durations are recorded in submission order, alongside its p50, p95, p99 and max
- **AND** a supervised run additionally records the revision, host, runtime, pin and timestamp of its measurement

#### Scenario: a derived budget states a basis that can be checked

- **GIVEN** a latency budget in the regression guard justified by the supervised record
- **WHEN** its basis is read
- **THEN** it names the measured value it is a multiple of, in the same unit that value is recorded in
- **AND** a reader can recompute the multiple from the published record without converting units

#### Scenario: a superseded pin does not carry forward as evidence

- **GIVEN** a recorded supervised run measured under a server or SDK pin that has since moved
- **WHEN** activation evidence is evaluated
- **THEN** its latency rows are historical and do not satisfy the activation condition
- **AND** activation waits for a supervised run recorded on the current pin
