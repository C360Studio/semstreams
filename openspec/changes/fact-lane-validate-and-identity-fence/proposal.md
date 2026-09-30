# Change: The Graphable lane validates a decoded payload and fences identity panics

## Why

Issue #1112 (milestone `v1.0.0-beta.163`, owner triage 2026-08-30: "a live-path defect the wave itself created").
Since #1109 registered `LoopExecutionEntity` in the payload registry (ADR-103), the graph-ingest Graphable lane can
decode it, and its `EntityID()` and `Triples()` still route through the panicking `LoopExecutionEntityID`
constructor. The lane calls both without a `recover()` and never calls the payload's `Validate()`. The panic escapes
to natsclient's `safeHandleMessage` (`natsclient/stream.go:767-775`), which recovers it and Naks, so one malformed
arrival becomes a poison message that is redelivered without end and never counted on the lane. The producer half
already validates:
`BaseMessage.MarshalJSON` refuses a payload whose `Validate()` fails. The consumer half does not. File:line premises
are in `design.md` at `1b1accf4ea4ea878c26236b5a9e6cb83d2d89d7a`.

## What Changes

- **BREAKING (contract tightening on the Graphable lane):** `decodeEntity` calls `BaseMessage.Validate()` after
  decode and before any Graphable method runs. A payload whose `Validate()` fails is a poison message on the lane's
  existing path: counted, logged at WARN with the subject and reason, acked and dropped, never persisted. A producer
  that publishes through `BaseMessage.MarshalJSON` already passed this check; only hand-written wire JSON that fails
  its own type's `Validate()` changes behavior. Migration note: `docs/operations/migration-fact-lane-validate.md`.
- The whole lane entry is fenced: a panic raised by payload code on the lane (the registered type's `UnmarshalJSON`
  inside decode, its `Validate()`, or its `EntityID()`, `Triples()`, `StorageRef()` or `IndexingProfile()`) becomes a
  classified rejection naming the message type (the subject, before decode succeeds) and the panic value, on the same
  poison path: counted and acked, instead of Nak-redelivered.
- `LoopExecutionEntity` adopts the identity-failure shape its four sibling registered types already have:
  `EntityID()` returns `""` through `TryLoopExecutionEntityID` (graph-ingest rejects the empty ID); `Triples()`
  omits the `parent` and `reply_to` triples whose constructor fails, the way its `run` branch already does;
  `Validate()` rejects a malformed parent, reply-to or run reference and cannot panic.
- One sentence in the new-payload skill and the payload-registry concept doc: a registered Graphable's identity
  methods never panic on decoded input.

## Impact

- Packages: `processor/graph-ingest` (one function), `agentic` (one type), docs. No exported signature changes.
- Sisters: a producer that bypasses `BaseMessage.MarshalJSON` and emits a payload failing its own `Validate()` is now
  rejected at ingest instead of persisted. None measured in-tree; the migration note carries the check.
- Specs: `graph-ingest` gains a requirement for the lane's validate-then-fence order; `payload-registry` gains the
  identity-failure shape for registered Graphable types.
- Out of scope, by the issue's boundary: the exported panicking constructors keep their contract for producer-owned
  inputs (their eight callers in `processor/agentic-loop` and the e2e scenario own what they pass); `Decoder.Decode`
  in `message/` is unchanged (rejected alternative R1 in `design.md`).
