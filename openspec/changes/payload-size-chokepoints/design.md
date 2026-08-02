# Design — payload-size-chokepoints (gh#857)

## Context

See `proposal.md` and the gh#857 ledger (every claim there carries `file:line` at the audit
commit). Facts the approach leans on:

- `natsclient.KVStore` guards size only on `UpdateWithRetry*` (`kv.go:346-350`), hardcoded
  1MB; `Put`/`Create`/`Update` and `Client.Publish`/`PublishToStream*` are unguarded.
- `errs.Classify` defaults unknown errors to Transient (`pkg/errs/errs.go:264-281`); the
  `retry` package's `NonRetryableError` is a separate type invisible to `errs`.
- The NATS client exposes the server-advertised limit at `Conn.MaxPayload()` — a deployment
  that raises `max_payload` (the SemSource-style workaround) is visible to us at runtime.
- `msg.Respond` failure on an oversized reply is logged and dropped
  (`natsclient/request.go:391-395`); the caller times out.
- `COMPLETE_<loopID>` writes go through a raw `jetstream.KeyValue` handle
  (`processor/agentic-loop/component.go:56`), bypassing even the guarded lane; the four
  write sites are void-returning. The read side already pages
  (`processor/agentic-tools/loop_result.go:113`).
- `AGENT_CONTENT` ObjectStore + `ContentStorable` ref-triples is the designed offload,
  already used by the trajectory content path (`graph_writer.go:508-525`); ObjectStore
  chunks internally and is not subject to the 1MB message limit.
- `AgentRequest.Messages` inlines full history (`agentic/types.go:112`);
  `tool_result_max_bytes` caps each tool result at ingestion (default 32KB, 0 = unlimited).

Binding constraints from the owner (gh#857 comments, including the correction): size is a
substrate concern owned at the seams; components carry no wire-size knowledge; the paved
path is the default path; agentic lanes first; classify every existing knob by which limit
it defends — ingestion bounds are policy and stay, wire-size defenses move to the substrate.

## Goals / Non-Goals

**Goals:** every framework write, publish, and reply either fits, offloads, or fails loud
with a permanent classification — enforced at ≤5 chokepoints, not per call site; the
agentic flagship cannot silently die at depth; one spec (`payload-bounds`) a new developer
reads.

**Non-Goals:** context windowing/summarization (model-behavior policy); provider context
windows; raising limits; retention redesign (see proposal Non-goals).

## Decisions

### D1 — One guard, limit derived from the connection, refusal classified Invalid

A single `checkPayloadSize(bytes, limit, seam, target)` helper in `natsclient`, called from
`KVStore.Put`/`Create`/`Update` (and replacing `UpdateWithRetry*`'s hardcoded constant),
`Publish`/`PublishToStream*`, and the respond seam. The limit is `Conn.MaxPayload()` at call
time — never a compiled-in 1MB — so raising the server limit raises the framework's bound
with zero code or config. Refusal is `errs.WrapInvalid` carrying byte count, limit, the
subject or bucket/key, and the remedy ("offload via ContentStorable / narrow the query /
page"), satisfying the three-fact operator-message rule. Alternatives rejected: per-site
checks (the class exists because sites are many and drift); a config knob for the framework
limit (the server already owns that number; a second copy drifts — and per the owner
constraint, no new size knobs).

### D2 — `ErrMaxPayload` is permanent

`errs.Classify` maps `nats.ErrMaxPayload` → `ErrorInvalid`. A payload the server will never
accept is not transient by any retry. One arm, one test, closes the retry-forever pathology
everywhere the raw error escapes a seam the guard missed.

### D3 — The respond seam answers "too large" instead of going quiet

When a handler's reply exceeds the limit, the framework sends the standard classified error
reply (ADR-060 headers; error bodies are small by construction) naming reply size and limit.
Callers through `RequestClassified`/`ClassifyReply` get a typed permanent error in
milliseconds instead of a timeout in seconds. This is the single biggest sister-facing
honesty gain in the change. The handler's oversized reply itself is dropped (it cannot be
sent); the handler-side log remains for the operator.

### D4 — `COMPLETE_` values: loud, and ref-bearing above a derived threshold

The four completion/failure/cancellation write sites return their errors; transient
failures retry (bounded); permanent failures mark the loop degraded with a typed reason —
"completed but result not durably stored" becomes a *visible* loop state, never an implicit
one. Values whose result exceeds a threshold derived from the wire limit (fraction of
`MaxPayload()`, not a knob) store the result in `AGENT_CONTENT` and write a KV value
carrying `{storage_ref, preview, size}`. `read_loop_result` resolves the ref transparently
(its `max_bytes`/`offset` paging semantics are unchanged — it pages over the hydrated
content). Compatibility: readers that decode old inline values keep working (the ref fields
are additive); the writers move first, readers already shipped paging.

### D5 — Request-lane hydration is behavior-neutral and lives at the two ends of the wire

Bulky historical message content in `agent.request` rides as `AGENT_CONTENT` refs; the
loop-side builder offloads content above the derived threshold when composing the request,
and `agentic-model` hydrates refs back to full text when building the provider call. The
SAME text reaches the model — this is a wire-shape change only, which is what makes it
safe to do without touching model-behavior policy. The trajectory path already writes step
content to `AGENT_CONTENT`, so hydration largely re-reads content the loop already stored.
Ordering within the change: D1's guard lands first, making an over-limit request a **loud
terminal loop failure** with a typed reason (not a retry loop) — the honest interim — then
hydration lifts the ceiling. If hydration slips, the guard still ships; the deliberate
not-done would reach the delta per house rule.

### D6 — Knob taxonomy: ingestion bounds stay, wire defenses dissolve

Per the owner's correction: `tool_result_max_bytes` is an **ingestion bound** — it protects
the loop from unbounded untrusted external content at the point of entry (a tool must not
turn an unlimited download into unlimited context), which is policy, stays configurable,
and is re-documented as exactly that. It is explicitly NOT the mechanism that keeps
requests under the wire limit (it never could — accumulation defeats any per-item cap);
D1/D5 own that. `0 = unlimited` remains valid *because* the wire guard now backstops it.
The sweep task classifies every other size-adjacent knob the same way before touching it;
a knob that exists only to defend the wire is retired, a knob that bounds ingestion or
resource policy stays.

### D7 — GOVERNANCE_VERDICTS: recommend DiscardOld + fill-ratio visibility, rule before close

Options: (a) keep `DiscardOld`, add a fill-ratio metric + Warn threshold so eviction
proximity is visible before verdicts vanish; (b) `DiscardNew` — rejected before because it
breaks the verdict publish path at the ceiling, i.e. trades silent history loss for a
halted governance pipeline; (c) archival exemption (unbounded, external archival owns
truncation). **Recommendation: (a)** now — an audit stream that *tells you* it is nearing
eviction is honest within this change's scope — with (c) recorded as the durable answer in
ADR-068's retention lane. The owner's ruling is recorded on the task before it closes.

## Risks / Trade-offs

- **[Guard rejects previously-"working" writes]** deployments silently losing data today
  will start seeing loud failures. → That is the point; the error names the remedy, and the
  changelog carries the class explanation. Same posture as the gh#810 guard.
- **[Respond-seam behavior change]** callers that treated timeout as "no data" now get a
  fast typed error. → Strictly more honest; sisters notified via changelog + gh#857.
- **[Ref-bearing COMPLETE_ values]** a reader that hard-decodes the old shape and is not
  updated could misread a ref value. → Additive fields + the read-path helper shipping in
  the same change; the sweep enumerates readers from the owning component (house rule) and
  the known set is small (`read_loop_result`, `flow_monitor`, research-graph adapters).
- **[Hydration adds a read hop per model call]** → Model calls are seconds; an ObjectStore
  read is milliseconds, and only fires for content above threshold.
- **[Derived thresholds surprise operators who raised max_payload]** → Deriving is the
  feature: their raise propagates everywhere at once, replacing today's per-surface
  workarounds.

## Migration Plan

Additive wire shapes (ref fields), guard behavior change is fail-loud-instead-of-silent
(no data migration). Writers-before-readers ordering inside the change for COMPLETE_ refs.
Rollback = revert; inline values remain readable throughout. Changelog: behavior-change
entry for the error-surface honesty gains; sister notice on gh#857.

## Open Questions

- The exact derived-threshold fraction for offload (e.g. ½ of `MaxPayload()`) — settles in
  review with a fixture; does not change specs or tasks.
- Whether `flow_monitor`'s scan should surface preview-vs-ref distinction to operators —
  cosmetic, deferrable.

## Knob taxonomy (task 4.4 sweep, 2026-08-02)

Sweep method: grep the tree for size-adjacent json-tagged config/schema struct fields
(`MaxBytes|MaxSize|MaxValueSize|max_bytes|max_size|Truncat|BufferSize|MaxRequestSize|
MaxBodyBytes|ChunkSize`), classify each by WHICH limit it defends (D6). Result: exactly
ONE knob was wire defense (`KVOptions.MaxValueSize`, already dissolved into the seam by
group 1); everything else is ingestion or resource policy and stays. No knob met the
"unambiguously wire-defense AND unused" retirement bar beyond it.

| Knob | Location | Bounds | Class | Disposition |
|---|---|---|---|---|
| `tool_result_max_bytes` | `processor/agentic-loop/config.go:61` | Content one tool may inject into loop context from external sources | **Ingestion** | STAYS; re-documented per D6 (task 4.3); 0=unlimited safe because the seam guard backstops |
| `KVOptions.MaxValueSize` | `natsclient/kv.go:28` | KV value size per store | **Was wire defense** (hardcoded 1MB) | DISSOLVED (group 1): default 0 = derive from server `max_payload`; >0 is an explicit override (tests/special cases), not a restatement of the wire limit |
| `BucketConfig.MaxBytes` | `config/config.go:368` | Total KV bucket storage | Resource/retention | STAYS |
| `StreamConfig.MaxBytes` | `config/streams.go:32` | Total stream storage (paired with `Discard`) | Resource/retention | STAYS |
| `DispatchStreamMaxBytes` | `processor/gated-dag/config.go:114` | Dispatch stream storage ceiling (DiscardNew default) | Resource/retention | STAYS |
| `MaxRequestSize` | `gateway/types.go:102` | Inbound HTTP request size at the gateway edge | **Ingestion** | STAYS |
| `MaxBodyBytes` | `gateway/lifecycle-gateway/component.go:58` | Inbound HTTP POST body at the operator API edge | **Ingestion** | STAYS |
| `fusion Budget.MaxBytes/MaxNodes` | `pkg/fusion/contract.go:37-39` | Caller-declared response budget (honest `Truncated` marker) | Resource (result budget) | STAYS |
| `cache MaxSize` | `pkg/cache/config.go:38` | Entry COUNT (not bytes) | Resource | STAYS (not size-class at all) |
| `Read/WriteBufferSize`, `BufferSize`, `LogStreamBufferSize` | `input/websocket/config.go:50-51`, `output/file/file.go:31`, `service/flow_service.go:38` | I/O buffer tuning | Resource | STAYS |
| `MaxMemory` | `config/config.go:209` | Embedded-server memory ceiling | Resource | STAYS |
| `maxPrefixResponseBytes` | `processor/graph-ingest/query.go` | Declared, never read | Dead wire-defense comfort | Deleted by task 2.2 (group 2's scope) |

Deliberately NOT knobs (owner constraint: no new size knobs): the offload threshold
(`resultOffloadThreshold` = ½ of the live `ServerPayloadLimit()`,
`processor/agentic-loop/component.go`) and the inline preview bound
(`resultPreviewBytes` = 2048, same file) are derived/const, not configuration.
