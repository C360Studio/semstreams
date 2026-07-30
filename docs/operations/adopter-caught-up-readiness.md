# Adopter note — asking SemStreams whether it is caught up

**Audience:** any repo consuming a SemStreams graph — semdragon (gh#712),
SemMachina (gh#732), semsource, semboids.

**Status:** additive, NOT breaking. Nothing you have today stops working. Adopt when
you have a place that currently sleeps, polls a count, or assumes an ordering.

This note states **the rule**, not the diff. See ADR-088 for the decision and
`openspec/specs/graph-index-readiness/` for the mechanics.

## The rule

1. **Ask `GRAPH_STATUS` for caught-up.** Readiness is watched KV state, one key per
   producer.
2. **One key per producer you depend on.** `graph-ingest`, `graph-index`, `rule`,
   `graph-embedding`. Declare the ones you actually depend on — no more.
3. **Fold client-side.** There is no published aggregate and there never will be
   (ADR-088). Use `graph/readiness.Set` if you are in Go; otherwise read the keys
   and conjoin them yourself.
4. **Absent = unknown = fail closed.** A key nobody published is not "ready" and is
   not "empty". It is unknown, and unknown defers.
5. **`bootstrap_complete && bootstrap_scope == 0` means there was authoritatively
   nothing to do** — as opposed to "I might be asking too early". This is the
   distinction gh#732 asked for.

## What each producer's envelope means

| Producer | `bootstrap_complete` means | `lag` unit |
|---|---|---|
| `graph-ingest` | the boot entity sweep drained AND the backlog present at bind was worked off | **messages** |
| `rule` | every currently-authoritative watcher generation finished its replay | always 0 (no queue) |
| `graph-index` | the initial index build finished (process-lifetime latch) | ENTITY_STATES revisions |
| `graph-embedding` | as graph-index | ENTITY_STATES revisions |

`lag` carries **different units per producer**. Do not compare it across keys or sum
it. Compare it to zero.

## Two things caught-up does NOT mean

**It is not a completeness claim.** `lag == 0` means no outstanding work, not that
every message was applied. A message that exhausts its delivery limit is parked and
leaves the counters entirely, so it is invisible here. Caught-up licenses **no
absence claim** — that rule (ADR-084) is unchanged. If you need "was X ingested",
ask for X.

**It is not a read gate.** Lag is a property of the answer, not a fault (ADR-085).
Gating your read paths on coverage makes a healthy system flap unavailable under
ordinary write load. Coverage is for deciding **when to take a snapshot**, which is
exactly gh#712's case.

## Migrating a wait

Replace *"sleep, then assume"* or *"poll until the count looks right"* with *"wait
until every declared producer reports caught-up"*.

The e2e entity stage in this repo is the worked example
(`test/e2e/scenarios/stages/entities.go`). Its old poll declared success the moment
enough entities happened to be present — true long before ingest finished — and
could equally declare failure on a slow-but-healthy stack. Both readings were about
timing, not truth. It now waits on the producers' own signal and still asserts the
counts afterwards, so a wrong-entities failure stays a failure but is attributable
instead of confounded with "we did not wait long enough".

**Do not assert an ordering against `Start` returning.** gh#732 measured that
interval as not even consistently positive. There is no constant to sleep on.

## Verify the key list against your deployment

Declaring a key whose producer your deployment does not run means waiting out your
full timeout on a key nobody publishes. The producer set is deployment-dependent:
measured 2026-07-30, `graph-ingest` appears in 18 shipped component instances,
`graph-index` in 8, `rule` in 8, `graph-embedding` in 4 — and **9 of the 18
`graph-ingest` instances bind no JetStream consumer at all** (their only input port
is core NATS request/reply), which is honestly caught-up with nothing to do.

This repo pins its own list with a config-drift test
(`test/e2e/scenarios/stages/readiness_keys_test.go`). Consider the same.

## Problems

File an issue. Problems you hit adopting this become new issues rather than blocking
the change on this side.
