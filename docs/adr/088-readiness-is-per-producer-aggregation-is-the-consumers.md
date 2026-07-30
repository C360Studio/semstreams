# ADR-088: Readiness Is Per-Producer; Aggregation Belongs to the Consumer

## Status

Accepted — 2026-07-30. Decision record for the `caught-up-readiness-producers`
change. Extends ADR-083 (readiness as watched KV state in `GRAPH_STATUS`) and
ADR-066 (the envelope) to two new producers, and keeps ADR-084's absence rule and
ADR-085's gate-on-health rule intact — neither is reopened here.

This is a **cross-repo contract**: semdragon (gh#712) and SemMachina (gh#732) both
consume it. Mechanics live in `openspec/specs/graph-index-readiness/`; this records
only the decision and why the obvious alternative was rejected.

## Context

Two independent consumers asked for the same missing primitive: a way to tell
**caught up** from **merely started**.

- **gh#712 (semdragon)** captured a parity snapshot while graph-ingest was still
  applying entities. `graphSummary.total_entities > 0` was true and health was
  green, so nonempty-and-healthy read as settled. It was not.
- **gh#732 (SemMachina)** had nothing signalling that the rule processor's
  bootstrap entity-watch replay had finished. `Start` returning is not that
  signal, and the interval was measured as not even consistently positive — so
  there was no constant to sleep on, and asserting either ordering asserts a race.

The substrate already existed. What was missing was two producer keys and a way for
a consumer that depends on several producers to ask one question.

## Decision

**1. Readiness is published per producer, one key per producer, and no aggregate is
published.**

An aggregate would be the one envelope in the system that is *derived* rather than
*observed*, so its staleness would report the aggregator's liveness rather than the
producers'. On any defer the consumer needs per-producer detail anyway. And the
producer set is deployment-dependent — measured 2026-07-30, graph-ingest appears in
18 shipped component instances, graph-index in 8, rule in 8, graph-embedding in 4 —
so a framework-published aggregate would have to guess its own membership.

**2. Aggregation is a client-side fold over a consumer-declared key list**
(`graph/readiness.Set`), delegating each key to the existing
`graph.EvaluateReadinessGate`. No new gate semantics, no new defer reasons, no
optional-key flag: an optional key is one you did not declare. Declaring a key you
do not depend on makes you defer on someone else's outage; omitting one you do
depend on is the bug this change exists to fix — and both are visible at your own
call site rather than buried in a framework default.

**3. There is no framework-declared mandatory key list.** Declaring a key constant
does not make it required. Half the shipped `graph-ingest` instances bind no
JetStream consumer at all (their only input port is core NATS request/reply), and a
mandatory list would make those deployments permanently unready.

**4. Caught-up is a claim about BACKLOG, never about COMPLETENESS.** Zero
outstanding work means no outstanding work — not that every message was applied. A
message that exhausts `MaxDeliver` is parked and leaves the counters entirely, so it
is invisible to the signal. This does not weaken ADR-084: caught-up licenses no
absence claim either. Operator visibility for parked messages is gh#742.

**5. Bootstrap completion is scoped to whatever the producer can actually vouch
for.** graph-index latches for the process lifetime, which is correct for it. The
rule processor tracks per WATCHER GENERATION, because its watcher set is
runtime-mutable via a component-config PUT and a recreated watcher re-runs its
replay — a process-lifetime latch there would report "bootstrapped" while a freshly
re-added pattern was mid-replay, which is gh#732's bug in a new costume.

**6. `bootstrap_scope` reports the size of the initial build in the producer's own
unit, and the gate MUST NOT read it.** It exists for one distinction that was not
recoverable from the wire for any producer: `complete && scope == 0` means
*authoritatively nothing to do*. The moment a verdict depends on a magnitude, this
becomes a threshold knob and readiness stops being a health question — which is why
ADR-085 deleted `max_staleness`. A test pins the gate's verdict as invariant across
the field.

## Consequences

- Consumers depending on several producers declare their own key list and fold it.
  An absent key fails closed with no new machinery: the watcher reports unknown and
  the existing gate short-circuits it.
- Coverage (`FullyCovered`) stays a **separate, named** predicate from the health
  gate. ADR-085 banned coverage as admission control **for reads** and explicitly
  deferred the non-read case to that consumer's evidence; gh#712 is that evidence.
  Snapshot callers may use it; read paths may not.
- Adding a producer is additive: a new key in an existing bucket, plus whichever
  consumers choose to declare it.

## Alternatives rejected

- **Publish an aggregate readiness key.** See decision 1 — derived staleness, no
  knowable membership, and consumers need per-producer detail on every defer.
- **Derive catch-up from the JetStream consumer ack floor.** MEASURED against both
  deployed server versions (2.10, 2.12) and rejected on evidence:
  `AckFloor.Stream` does not advance past a `MaxDeliver`-exhausted message, and
  then, on the next unrelated acknowledgement, jumps *past* the never-applied
  message. It therefore reads not-caught-up while idle and falsely-covered under
  traffic — wrong in both directions, and it never means "everything at or below
  this sequence is durable." Outstanding work is the pending plus
  delivered-but-unacknowledged total instead, which is invariant to which counter
  currently holds a message where neither half is.
- **A framework-level "wait for everything" helper.** It would have to know the
  deployment's producer set. See decision 3.
- **Fold readiness into `/readyz`.** That is the orchestrator's "can this process
  serve" contract; adding data-plane coverage makes a healthy binary flap unready
  under ordinary write load.
