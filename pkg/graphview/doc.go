// Package graphview provides a shared read-side fan-out primitive over a NATS
// KV bucket (ADR-081): ONE WatchAll feeding one validated in-memory
// current-state projection, coalesced to a view-rate tick and fanned out to N
// local subscribers with snapshot+delta consistency, per-subscriber
// at-most-once backpressure, and an honest degraded-path contract.
//
// It exists to retire the O(N x writeRate) per-client-watcher trap (gh#579):
// each write is serialized, decoded, and contract-validated exactly once
// regardless of subscriber count. The view is domain-agnostic — the bucket
// handle and a validating decode func are injected; the decode func receives
// the key, raw value, and authoritative entry metadata (revision + server
// write time) and decides what a servable record is ((T, keep, err):
// keep=false maps the key to absent, err poisons the key per ADR-079
// semantics).
//
// # Coherence: why fan-out is one critical section (G1, the tick seam)
//
// A materialized view IS a cache, so the read-after-write coherence class
// that bit the graph-ingest read-through cache (PR #583) applies here at two
// seams:
//
//   - Attach seam: {snapshot capture + subscriber registration} is atomic
//     with delta application at one view sequence S under the projection
//     mutex. The snapshot reflects every applied change at <= S; the
//     subscriber receives exactly the changes > S. Snapshot capture holds the
//     lock (bounded map copy); snapshot delivery happens after unlock.
//
//   - Tick seam: the prior-art coalescers fire their callbacks OUTSIDE their
//     lock, so a detached batch holding K@R5 can be enqueued to a subscriber
//     that attached with a snapshot already at K@R6 — stale delivery, the
//     PR #583 shape one seam over. This package closes that seam
//     STRUCTURALLY: batch detach, subscriber-set iteration, and per-subscriber
//     enqueue are ONE critical section under the projection mutex (fanOut).
//     A detached batch never exists outside the lock, so no apply and no
//     attach can interleave between capture and enqueue — the stale-delivery
//     interleaving is unrepresentable rather than guarded at each enqueue
//     site (a guard every future enqueue path would have to remember).
//
// The work under the lock is bounded map writes (changed keys x subscribers)
// — never a channel send, decode, or I/O. Actual delivery to subscriber
// channels happens in per-subscriber goroutines outside the lock, so a slow
// subscriber degrades to staleness (its pending deltas coalesce
// last-writer-wins per key, bounded by live changed-key cardinality) and
// never blocks the watcher, the projection apply, or its peers.
//
// # Degraded paths (G5/G6)
//
// Readiness is caught-up, not started: attach and point reads before the
// initial WatchAll replay completes fail with ErrNotReady. Loss of the shared
// watcher fails closed — every subscriber's delta channel closes with
// ErrWatcherLost surfaced via Err(), and the frozen projection is never
// served as live. Restart re-bootstraps and reconciles ghost keys (keys
// absent from the fresh replay are removed) before reporting caught-up.
// Decode/contract failures surface as typed per-key *PoisonError signals in
// the delta lane and on point reads; they never launder through as upserts
// and never halt delivery for unrelated keys.
//
// The view coexists with raw WatchAll: consumers needing independent
// filters, historical replay, per-revision delivery, or independent ack
// semantics keep opening real JetStream consumers.
package graphview
