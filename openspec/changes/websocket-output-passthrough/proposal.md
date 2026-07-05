# output/websocket: pass-through mode for pre-validated JSON (gh#471)

## Why

`output/websocket` decodes **every** inbound NATS message into `map[string]any` and
re-marshals it before broadcast, solely to inject `timestamp`/`subject` when absent:

```go
// output/websocket/websocket.go — handleNATSMessageData (and the sibling handleNATSMessage)
var msgData map[string]any
if err := json.Unmarshal(data, &msgData); err != nil { /* wrap as raw_data */ }
else {
    if _, ok := msgData["timestamp"]; !ok { msgData["timestamp"] = ... }
    if _, ok := msgData["subject"];   !ok { msgData["subject"]   = subject }
}
jsonData, _ := json.Marshal(msgData)
```

For producers that already emit valid, envelope-complete JSON this is pure
per-message overhead — the most allocation-heavy decode shape Go offers (`map[string]any`)
plus a full re-encode — and it scales with message rate, independent of client count.

semboids' baseline CPU profile (200 boids @ 30Hz, `docs/perf/baseline-200boids-30hz.md`)
flags this decode/re-encode as the **largest in-process application cost** even at
30 msg/s (`encoding/json.appendCompact`, `strconv.fmtF` re-formatting float
positions, `decodeState.skip`). At the thousands-of-msg/s rates the same host
sustains on the graph-ingest side (now unblocked by gh#470), it becomes the
dominant producer-side cost of ws egress. Side effects beyond cost: JSON object key
order is not preserved and all numbers round-trip through `float64`, so producer
bytes are perturbed **even when nothing is injected**.

This is a **framework substrate** concern — the ws output component is SemStreams'
egress primitive, and the fix is an opt-in the component owns; no product semantics
are involved.

## What Changes

- **Add an opt-in `passthrough` config flag (default `false`).** When enabled, the
  component validates inbound bytes cheaply with `json.Valid(data)` and broadcasts
  the **original bytes unchanged** — no decode, no re-encode, no key-order or
  float-precision perturbation, no `timestamp`/`subject` injection. Non-JSON
  payloads still fall back to the existing `raw_data` wrapper (so the flag is safe
  even on a mixed subject).
- **Apply it on both inbound handler paths.** The component has two message
  entrypoints that both do the decode/re-encode — `handleNATSMessageData` (data +
  subject) and `handleNATSMessage` (`*nats.Msg`). Both honor `passthrough`.
- **Current behavior stays the default.** With `passthrough` unset/false the
  inject-when-absent path is byte-for-byte unchanged, so existing consumers that
  rely on injected `timestamp`/`subject` are unaffected.

The contract of opting in: **the producer guarantees its own envelope.** With
`passthrough: true` the component does NOT inject `timestamp`/`subject`; a producer
that needs them present must emit them. This is the explicit tradeoff (stated in
the spec) — pass-through is for producers that already ship envelope-complete JSON.

## Impact

- **Affected specs:** new capability `websocket-output` (seeded lazily by this
  change; distilled from `output/websocket/websocket.go` and verified against code).
- **Affected code:** `output/websocket/websocket.go` — `Config` gains a
  `Passthrough` field (schema tag), `ConstructorConfig`/`Output`/factory wire it,
  and both handler paths branch on it. Tests.
- **No breaking change.** Additive config field, default false = today's behavior.
  Schema regen picks up the new `type:bool` field (committed).
- **Gate relevance:** e2e:core's `core-dataflow` scenario drives the ws output path
  directly (envelope receipt over the ws stream), so it exercises the default
  (non-passthrough) path end-to-end.
