# Design — caught-up readiness producers

## D0. MEASURED 2026-07-30 — the ack floor is unusable; take the fallback

The graph-ingest half rested on one unverified assumption: that the JetStream consumer's ack floor is
contiguous and advances past terminal outcomes. The **ack-ordering** claim is solid and read from code
(`keyed_ingest.go:125-225`: write → guard stamp → Ack, with every failure path Nak/Term before ack).
NATS server behavior was not verified in this repo, so a throwaway testcontainer probe settled it
against **both deployed server versions** (2.10-alpine and 2.12-alpine both appear in `docker/compose/`).
Stream of 5 messages, poison at stream sequence 2, every other sequence acked, `MaxDeliver: 3`:

| server | terminal mode | `AckFloor.Stream` | idle +5s / +10s | after 3 more msgs published+acked | `NumPending` | `NumAckPending` |
|---|---|---|---|---|---|---|
| 2.10 **and** 2.12 | `MaxDeliver` exhausted (3× Nak) | **1 — did NOT advance** | still 1 | **8** | 0 | 0 |
| 2.10 **and** 2.12 | `Term()` | **5 — advanced** | 5 | 8 | 0 | 0 |

**Answers: (a) NO — MaxDeliver exhaustion does not advance the floor. (b) YES — `Term()` does.**
Identical on both server versions.

**The floor is worse than "stalls", and the failure is bidirectional.** The probe's follow-up
measurements found the stall is not permanent: with no traffic the floor sits at 1 indefinitely
(verified at +5s and +10s), but the moment any *later* message is acked it leaps to the new high-water
— **8, skipping sequence 2 entirely, which was never applied.** So:

- **Idle after a poison message** → `Ready = (floor == lastSeq)` reads permanently-not-caught-up. This
  is the dangerous direction D0 predicted.
- **Traffic after a poison message** → the floor asserts coverage of a message that was dropped. This
  is the *opposite* dangerous direction, which D0 did not predict and which no amount of prose would
  have surfaced.

Either way **`AckFloor.Stream` never means "everything at or below this sequence is durable in the
graph."** The "restart-surviving contiguous high-water" framing is dead. `proposal.md` has been
corrected rather than left asserting it.

**Take the fallback: outstanding work is `NumPending + NumAckPending`, with no ack-floor claim
anywhere.** The probe measured it 0 in all 12 observations (2 versions × 2 terminal modes × 3 sample
points), including both cases where the floor was wrong. It is the only number that was right every
time, and it already accounts for the in-process lane queue because those messages are
delivered-but-unacked. Nothing else in the design changes — `Lag` was always going to be this sum.

**Honesty boundary the spec MUST state:** `NumPending + NumAckPending == 0` means *no outstanding
work*, **not** *every message was applied*. A `MaxDeliver`-parked message disappears from both
counters (measured above: both 0 while sequence 2 was never applied). "Caught up" is therefore a
statement about backlog, never about completeness — it cannot license an absence claim, consistent
with the existing readiness rule. Operator visibility for parked messages is already filed as
**gh#742**; this change must not paper over it by implying coverage.

Probe deleted per task 1.2. Precedent held: a ~40-line real-NATS probe falsified the author's own
hypothesis in one run — twice here, since the first result ("stalls forever") was itself corrected by
the permanence follow-up.

## D1. What "caught up" means, per lane

| Lane | Shape | Covered? |
|---|---|---|
| JetStream input ports | one durable pull consumer per port, `graph-ingest-<sanitized-subject>` | **yes** — `NumPending + NumAckPending` |
| mutation request/reply (8 subjects) | core NATS, no backlog | **no, deliberately** |
| query request/reply (4 subjects) | read-only | n/a |
| boot ENTITY_STATES sweep | drained to sentinel then stopped; already latches `entityBootstrapComplete` (`component.go:663-665`) | composed into `BootstrapComplete` |

`Ready = Lag == 0 && BootstrapComplete`. `Lag` = total outstanding across bound consumers, **in
messages** — a new unit on that field, which the spec must state. `StalenessMs` = age of the oldest
outstanding message from `meta.Timestamp` (already read at `component.go:1541`); reported, never
gating. `State = degraded` when `consumer.Info()` fails, mirroring graph-index's precedent for a
failed target read (`processor/graph-index/watermark.go:70-79`).

**Rejected — reuse `pkg/revlag`.** Its correctness rests on "delivery is monotonic ascending in the
revision passed to `Observe`" (`pkg/revlag/watermark.go:32-39`), guaranteed by an `OrderedConsumer`
on a KV watch. With `MaxDeliver: 3` and five Nak paths, redelivered sequences arrive out of order;
`Observe` would re-enter a completed revision into `pending` and pull `Indexed()` back down.

**Rejected — scan `GRAPH_INGEST_APPLIED_SEQ`.** `guardKey = entityID + "/" + stream`
(`keyed_ingest.go:63-65`), value a bare 8-byte sequence, read as `work.seq <= last` for that one key
(`:255-277`); a missing key returns "not stale", so absence carries no watermark meaning. Even a full
scan answers only per-entity. It is a redelivery dedup stamp, not a watermark.

**Rejected — a new projection field on `IndexStatusInputs`.** Mutually-exclusive input fields make an
invalid state representable, and bending the shared projection risks byte-drift in graph-index's
output, which the current spec explicitly protects. Add `graph.ComputeBacklogStatus` as a **second
named projection beside** `ComputeIndexStatus`. (`ComputeIndexStatus` cannot be reused as-is: it
computes `Ready = target > 0 && indexed >= target`, false at 0/0.)

## D2. Rule: per-generation, and why it diverges from graph-index

Watchers are keyed `(bucket, pattern)` (`entity_watcher.go:184-186`), created from
`config.EntityWatchBuckets`. Generation identity already exists — `managedEntityWatcher{watcher,
generation}` (`:196-199`), authority checked at `:517-524`, enforced at five seams. The only runtime
recreation path is a component-config PUT carrying `entity_watch_buckets`
(`service/component_manager_http.go:772` → `entity_watcher.go:290-395`); rule hot-reload does not
touch watchers, and an unexpected watch close latches the lane degraded permanently rather than
recreating.

Recreation **re-runs replay**: `prepareEntityWatcher` calls `bucket.Watch` with no `UpdatesOnly`
(`:209`), so every current value is re-delivered with `bootstrap == true` and a fresh nil sentinel.

**`BootstrapComplete` = conjunction over currently-authoritative generations, and it goes FALSE again
when a new generation registers.** This deliberately diverges from graph-index, whose latch is
documented "Latching only — never cleared" (`processor/graph-index/watermark.go:120-127`). The
justification is that graph-index's enumeration target is snapshotted once at attach, so a
per-process latch is honest there; rule's watcher set is runtime-mutable, so a per-process latch
would report bootstrapped while a freshly-added pattern replayed. Each generation still latches
against a **fixed** target — its own sentinel — never a moving one.

**Correction to gh#732's framing:** the existing `bootstrap` value is a goroutine-local `bool`
(`entity_watcher.go:451`, flipped `:477`), consumed by `Evaluate` and discarded. It is not a field,
not atomic, not published, not in `Status` or `Health`. It is a clean place to hook, not existing
state to expose.

**`Start` is unrelated and weaker than gh#732 assumes.** `run()` closes `ready` at `processor.go:515`
*before* `watchEntityStates` at `:518`, and watcher creation can additionally block ~15s inside
`getEntityStatesBucket` (30 × 500ms) waiting for ENTITY_STATES. That fully explains the measured
non-determinism and honors the issue's "do not block Start" non-goal for free.

**`State` reuses the two existing sticky latches** — `graphStateGuardDegraded` (`:57-69`, already
answering gh#712's "not-ready again on watcher loss") and `graphStateResetRequired`. Do not invent
states.

**Empty-pattern case is real and already correct:** the nil sentinel arrives unconditionally even
with zero values (nats.go `jetstream/kv.go:1308-1319`, reachable because `:209` never passes
`UpdatesOnly`), so `bootstrap` flips immediately. Zero configured patterns ⇒ zero watchers ⇒ the
conjunction over an empty set is vacuously true, reported as complete with scope 0. Unit coverage
exists; **no integration test asserts the sentinel from real JetStream** — add one.

## D3. `bootstrap_scope`: a count, not a bool

`BootstrapComplete` deliberately folds in the authoritatively-empty 0/0 outcome
(`graph/index_status.go:41-59`, ADR-085) and is **right to** for the health question — a gate asking
"is this sound to read from" gets the same answer either way. gh#732 asks a different question, "did
work happen," which today is unrecoverable from the wire for every producer because `TargetRevision`
carries the *live* target, not the bootstrap target.

A count rather than a bool because a bool answers only the stated case, while a count also surfaces a
replay that covered 3 of an expected 30,000 — the failure a migration gate actually fears. Cost is one
`omitempty uint64`: bounded, no per-entity list, satisfies the existing "watched key stays compact"
requirement.

**`EvaluateReadinessGate` MUST NOT read it.** It is caller-specific exactly like `IndexedRevision` —
reported, never admission control. Stating this in the requirement is what stops a future contributor
turning it into a threshold, which would be `max_staleness` under a new name. It also licenses
nothing about absence; ADR-084 D3 forecloses "any future field" by name.

## D4. Aggregation: client-side fold, absent = unknown = fail closed

Absent keys already fail closed correctly with **zero new semantics**: `Watcher.Read()` returns
`Known=false, Fresh=false` when nothing arrived (`graph/readiness/watcher.go:271-283`), and
`EvaluateReadinessGate` short-circuits `!Fresh` → `DeferStatusUnknown` (`readiness_gate.go:143-145`).
So a fold that ANDs the canonical gate over a declared key list is correct by construction. The list
must be the **consumer's** — a consumer that declares `graph-index` in a deployment that has none
correctly never proceeds.

Add a thin `Set` in `graph/readiness` (N watchers, `Start`/`Stop`/`WaitForFirst`, fold returning
`(proceed, firstDeferKey, DeferReason)` in deterministic key order). The alternative is every consumer
hand-rolling the loop, which this repo already paid for once — four divergent gate semantics, gh#590.
**No new defer reasons. No "optional key" flag** (an optional key is just one you did not declare).

Plus **one separately-named coverage predicate** for snapshot callers: `proceed && every declared key
reports Lag == 0`. This brushes ADR-085's boundary, so state why it does not cross it: ADR-085 banned
coverage as **admission control for reads**; a caller asking about coverage for a non-read purpose is
explicitly deferred to "that consumer's evidence", and gh#712 is that evidence. It must be named so it
cannot be mistaken for the gate, and must not defer any read path.

**The internal consumer that licenses the surface:** `test/e2e/scenarios/stages/entities.go:72-89`
polls entity count plus critical-entity presence in a deadline loop — the same heuristic gh#712
describes externally. Migrating it onto the fold is what makes this "add a signal and delete its
workaround" rather than "add a signal".

## D5. Surfaces: almost everything already exists

`graph/readiness` and `graph` already export `Watcher`/`NewWatcher`/`Start`/`Stop`/`Read`/
`WaitForFirst`/`Reading`/`EnsureBucket`/`Publisher`/`Publish`/`Key`/the bucket+key constants, and
`IndexStatusResponse`/`ComputeIndexStatus`/`EvaluateReadinessGate`/`StatusReading`/`DeferReason`/
`AllIndexStates`/`AllDeferReasons`. **gh#712's "public Go API" ask is ~90% satisfied**; only the two
keys, the fold, and `bootstrap_scope` are missing.

Operator surface is genuinely zero: `/readyz` (`service_manager.go:1465-1485`) is component-liveness
only and never touches `graph/readiness`; `gateway/` has no non-test reference to
`IndexStatusResponse`, `readiness.`, or `GRAPH_STATUS`. Add **one read-only dump** of the watched
keys plus per-key `known`/`fresh`/`age` — not a verdict, because a verdict bakes the key list into the
framework, which D4 rejects.

## D6. Restart semantics

| Field | graph-ingest | rule |
|---|---|---|
| `BootstrapComplete` | (boot sweep complete) AND (boot-backlog target reached); resets on restart | conjunction over authoritative generations; resets on restart AND on new generation |
| `BootstrapScope` | boot-backlog message count | values replayed across bootstrap generations |
| `Ready`/`Lag`/`StalenessMs` | never latch — recomputed each tick, so new backlog ⇒ not ready | same |
| `State=degraded` | `consumer.Info()` failure | `graphStateGuardDegraded`, sticky by design |

**"Cannot report settled before inferred writes are durable" is satisfied by construction** — ack is
the last statement in the success path, after the CAS, hierarchy containers/edges,
`ensureRelationshipTargetsExist` (`wg.Wait()` at `component.go:2730`), `routeForeignEdges`, and the
guard stamp.

**Pin it with a test — but NOT on the ack floor.** The original wording here ("force a write failure
and assert the ack floor does not advance") was written before §D0's measurement and is now the wrong
assertion twice over: nothing in this change reads `AckFloor`, so it tests NATS rather than us; and it
would **pass for the wrong reason** once the failure repeats to `MaxDeliver` exhaustion — the floor
does not advance there either, while the message is dropped. Assert instead on the quantity production
actually consults: force a write failure and assert **`NumPending + NumAckPending` stays > 0** (so
`Ready` stays false) while the write is failing, plus ack-is-terminal on the success path.

## D7. Capability home

Extend `openspec/specs/graph-index-readiness/`. Despite the name it is **already multi-producer** —
two of its requirements read "Every ADR-066 envelope producer (graph-index, graph-embedding)
SHALL…". And `openspec/specs/graph-ingest/spec.md`'s Purpose already disclaims readiness explicitly
("Readiness and coverage of the derived indexes belong to `graph-index-readiness`"), so splitting
would break a disclaimer that is current truth. Keeping `EvaluateReadinessGate`'s spec single is what
prevents the four-semantics failure reappearing at the spec layer.

**Rejected — rename to `graph-readiness`.** `openspec` has no rename primitive; the Purpose paragraph
fixes the naming confusion for free. File it if it still itches.

**HOLD status, verified against main at `87d7e0fc`:** four changes in flight —
`predicate-contract-enforcement` (42/44), `predicate-raw-key-representation` (10/14),
`graph-index-replacement-semantics` (15/19), `poison-response-scoping` (complete, tool-blocked).
**None touches `graph-index-readiness`.** `rule-entity-watcher-hardening` archived 2026-07-30 and
`openspec/specs/rule-entity-watching/` now exists, so the generation-scoped authority this design
relies on **is** spec'd truth — an earlier scoping pass flagged a HOLD here from a pre-archive
checkout; it is dissolved. Re-run `openspec list` at implementation time regardless.
