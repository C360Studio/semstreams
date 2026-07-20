# Proposal: Read-path readiness — retry contract + Fuse transient consistency

## Why

#592 asked whether the read path (fusion honesty envelope, reverse-index reads)
should serve bounded-stale instead of erroring `IndexNotReady` after a write
burst. The #592 architect research concluded **no — retry-the-transient is the
correct contract** (no consumer genuinely can't retry; readiness is sticky so it
converges fast; "marked-stale" already exists via the envelope's `Lag` /
`IndexedRevision`, the graph facet's `ViewRevision`, and graphview snapshots).
See #592 for the full rationale and the recorded reopen trigger.

But the research found the **actual** bug semsource hit, which is smaller and
different: `Engine.Fuse` handles the readiness transient **inconsistently**. Its
top gate degrades honestly (`!Ready` → empty envelope, no error,
`engine_lens.go:84`); `collectEdges` **swallows** a `Neighbors` transient
(`:197` `if err != nil { return }`); but `Fuse` **propagates** a `Resolve`
transient as a hard error (`:89-91`). So in the narrow race at first catch-up
under concurrent load — after the top `Ready` gate already passed — a `Resolve`
transient surfaces to the caller as a failed query instead of the empty-honest
degrade the same function returns everywhere else. This is semsource's
"passes 5/5 alone, fails under full-suite load."

## What Changes

- **Fuse degrades consistently on the readiness transient.** When an internal
  read (`Resolve`, `Entities`, `Neighbors`) returns the classified
  `ErrorCodeIndexNotReady` transient, `Fuse` returns the empty-honest envelope
  (`Ready=false`, current `IndexStatus`) — the same degrade as its top gate —
  rather than propagating a hard error. Genuine (non-transient) errors still
  propagate.
- **Document the read-path retry contract** as a `graph-index-readiness`
  requirement: reverse-index / byName handlers return the classified transient;
  consumers detect it via `errs.IsTransient` (never message text) and retry;
  readiness is sticky so bounded retry converges; the exact staleness is on the
  envelope (`Lag` / `IndexedRevision`) for a consumer that wants a self-serve
  bounded decision via `IndexedRevision >= myRev` (ADR-066's finer contract).

## Capabilities

### Modified Capabilities

- `graph-index-readiness`: adds the read-path retry contract and the fusion
  degrade-consistency requirement.

## Impact

- `pkg/fusion/engine_lens.go` — `Fuse` classifies internal-read errors and
  degrades on the readiness transient instead of propagating. Small, contained.
- No wire/`Ready` change; no new read mode. Resolves what semsource hit.
- Tags with #591 as the joint release; semboids/semsource soak against that tag.

## Non-goals

- NOT building a bounded-stale read variant (#592 CLOSE — retry is the contract).
- NOT touching `ComputeIndexStatus` / `Ready` / the status wire.
- NOT changing the reverse-index direct API (`GetIncomingEdges` /
  `CountIncomingEdges`) — an unmarked undercount there is rejected; erroring is
  the correct fail-safe and callers retry.
- The reopen trigger (a continuous-write deployment also serving exact point
  queries — does not exist today) is recorded on #592, not built.
