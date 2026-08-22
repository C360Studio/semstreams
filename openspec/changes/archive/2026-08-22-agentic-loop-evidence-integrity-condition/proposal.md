## Why

**A loop whose trajectory evidence failed to record is indistinguishable, to every machine
consumer, from one whose evidence is intact.** Today that failure reaches an `ERROR` log line, a
`{stage,kind,reason}` counter, and a latched Health degradation — three surfaces, all of which
terminate in a human. No rule can branch on it, and no product can decline to cite a loop whose
audit trail is not there.

That matters more here than it would elsewhere. `CLAUDE.md` makes agent execution evidence a
first-class capability rather than trace exhaust, and ADR-068 establishes that evidence is **not
regenerable** — so a consumer that cites a loop as audited when its evidence write failed has made
an unrecoverable error, not a recoverable one.

The framework is the only party that can know this. It observed the write and watched it fail. A
product cannot compute it from outside, and the fact currently exists in no form a product can
consume.

### Why now, and why this one

This is the first instance of a class the framework has already built twice without naming: a
**reportable condition** — a classified, framework-exclusive fact about an addressable subject that
a machine branches on without inventing a threshold. `agent.loop.terminal-reason` (gh#569) is the
shipped exemplar, added for exactly this reason: *rules could not tell budget exhaustion from a
transient model error*. `GRAPH_STATUS` readiness and `STORAGE_REPORT` pressure are the same class
on the KV plane.

Evidence integrity is the cheapest proof of the pattern and the highest-value single instance. It
deliberately does **not** introduce a general condition framework, a new bucket, or a new plane —
it adds one predicate to a graph write that already happens.

### The tension this change resolves

`agentic-loop`'s current spec already governs audit failure, and it is deliberately restrictive:

> If encoding or immutable fact Create/verification fails, no durable fact or reconstructed gap
> claim is required. Logs, metrics, and Health remain the operational evidence.

and, in a scenario:

> no counter, seal, gap fact, repair record, or completeness claim is manufactured

That prohibition is aimed at **fabrication** — inventing a reconstruction of what was lost. Every
scenario says so: "no fabricated reference", "no invented durable gap".

An observed loop-level classification is a different thing. It reconstructs nothing, names nothing
about what is missing, and repairs nothing. It records that the component tried to write evidence
and watched it fail — the same category of fact as `agent.loop.terminal-reason`.

The spec does not currently draw that line, and "no completeness claim is manufactured" reads
broadly enough to cover it. So this change **modifies** the existing requirement to carve the
distinction explicitly, rather than adding a second requirement that sits in visible tension with
it.

## What Changes

- **`agent.loop.evidence-integrity`**, a new predicate stamped on the loop execution entity when the
  component observed that the loop's evidence is not there — either a trajectory audit failure
  observed while recording that loop, or a startup determination that it cannot record trajectory
  evidence at all, which marks every loop in the process. The second scope is not an extra: it is
  the severest loss and the only one that produces no per-loop failure to observe, so without it
  total evidence loss would be the one state indistinguishable from a healthy one.
- **Stamped only on incompleteness; absent otherwise** — mirroring `agent.loop.terminal-reason`
  ("Stamped only on failure; absent on success"). This is the load-bearing design decision: the
  framework never asserts evidence is *complete*, because it can only observe failures it saw. An
  absent triple means "no audit loss observed", which per ADR-084 licenses nothing.
- **Rides the existing terminal graph write** (`graph_writer.go`), alongside `agent.loop.outcome`
  and `agent.loop.terminal-reason`. No new write path — the failures most worth reporting
  (`evidence_put`, `fact_create`) are exactly when the substrate is unhealthy, so a dedicated
  write at failure time would be the least likely to land.
- **Carries no stage or reason.** A loop may fail at several stages; electing one would manufacture
  a claim about which mattered, which is the fabrication the existing requirement bans. The full
  set stays in the `ERROR` log and the `{stage,kind,reason}` counter.
- **A fourth sibling on the existing fan-out.** `reportTrajectoryAuditFailure` already fans one
  `trajectoryAuditFailure` value to a Health latch, a metric, and a log. The condition joins that
  set; it is never derived from the counter.

## Impact

- **Affected spec:** `agentic-loop` — one MODIFIED requirement, one ADDED requirement.
- **Affected code:** `vocabulary/agentic/` (predicate + registration),
  `processor/agentic-loop/` (per-loop marker, component-wide total-loss latch, terminal write, and
  a `Late` flag threaded through the recorder's emit chokepoint so a report arriving after its
  loop's terminal write still logs/counts/degrades Health but does not mark).
- **No breaking change.** Additive predicate; absence is the existing behaviour and remains
  meaningful.
- **Not in scope:** the other three candidate agentic-loop conditions (input fidelity, graph
  visibility, governance coverage); any governance condition (blocked — `Message`/`Violation` carry
  no entity subject); a general reportable-conditions capability spec or ADR (deliberately gated on
  a product naming a condition it will branch on).
