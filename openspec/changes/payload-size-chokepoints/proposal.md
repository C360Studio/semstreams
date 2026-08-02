# Proposal — payload-size-chokepoints (gh#857, gh#855-adjacent)

## Why

A message or KV value that outgrows the NATS payload limit disappears instead of failing. The
2026-08-02 class sweep (ledger on gh#857) found `nats.ErrMaxPayload` handled at **exactly one
site in the tree** (the gh#837 clustering fix) while ten sites can silently lose data at the
limit — worst among them the agentic lanes: a loop's `COMPLETE_<loopID>` result write is a
void-returning log-and-drop (the loop reports complete while its result is unreadable forever
and a waiting parent stalls), and every `agent.request` inlines the loop's full message
history, which at the default 32KB per tool result crosses the 1MB wire limit around thirty
substantial iterations — precisely the loops that matter most. Oversized request/reply
responses fail as **timeouts** (the reply publish error is logged server-side and the caller
never learns "too large"), which is what living downstream of this class looks like to a
sister.

Two structural roots make it a class rather than twenty bugs: the 1MB value guard exists on
only one KV lane (`UpdateWithRetry*`) while `Put`/`Create`/`Update` and every publish are
unguarded, and `errs.Classify` defaults unknown errors to **transient**, so a permanently
oversized write retries forever as if it might succeed.

The owner's framing (binding constraints recorded on gh#857): SemStreams is a framework, and
size is a **substrate concern** — a new developer must get bounded-by-construction behavior
without discovering interfaces or sprinkling per-component byte knobs; payload/NATS size
knowledge must not float inside components.

## What Changes

- **Chokepoint guards at the natsclient seams** — one shared size check, limit derived from
  the connection's server-advertised `MaxPayload()` (never a hardcoded 1MB, so a deployment
  that raises the server limit is honored automatically), refusing with a stable
  `Invalid`-classified error naming bytes, limit, subject/key, and remedy. Applied at:
  `KVStore.Put`/`Create`/`Update` (joining the existing `UpdateWithRetry` guard, now derived
  not hardcoded), the publish seam (`Publish`/`PublishToStream*`), and the request-reply
  respond seam — where an oversized reply becomes a small classified "response too large"
  error reply, so callers get a typed failure instead of a timeout.
- **`nats.ErrMaxPayload` classifies permanent** (`errs.ErrorInvalid`) in `errs.Classify` —
  the one-line class fix that stops infinite retries against inputs that can never succeed.
- **Agentic lanes made loud and offloaded** (owner priority 1):
  - `COMPLETE_<loopID>` writes return their error, retry transient failures, mark the loop
    degraded on permanent ones, and offload bulky results to ObjectStore (`AGENT_CONTENT`)
    behind a ref-bearing KV value; the read side (`read_loop_result`) already pages and
    learns to follow the ref.
  - The `agent.request` lane gains behavior-neutral **hydration**: bulky historical message
    content rides as ObjectStore refs on the wire and `agentic-model` hydrates to full text
    when building the provider call — the same text reaches the model; only the wire shape
    is bounded. Until hydration lands, the chokepoint guard makes an over-limit request a
    loud terminal loop failure with a named reason, never a silent retry loop.
- **Existing knobs reclassified, not retired** (owner correction on gh#857):
  `tool_result_max_bytes` is an **ingestion bound** — it caps what a tool may inject into
  context from untrusted external sources, which is legitimate component-level policy — and
  is re-documented as such; its implied wire-size-defense job moves to the seams. The sweep
  in this change classifies every size-adjacent knob by **which limit it defends** before
  touching it.
- **Dead-guard deletion CANCELED** (amended 2026-08-02): between the audit read and this
  implementation, main gained real enforcement of `maxPrefixResponseBytes` (trim-until-fits
  byte budget + regression test), so there is nothing dead to delete — the respond-seam
  guard is now the backstop behind a live per-handler budget. See tasks.md 2.2.
- **GOVERNANCE_VERDICTS discard ruling** (owner decision task): an audit stream on
  `DiscardOld` silently evicts its oldest verdicts at the ceiling — options and a
  recommendation are in the design; the ruling is recorded before that task closes.

## Capabilities

### New Capabilities

- `payload-bounds`: the substrate size contract in one place — what every framework write,
  publish, and reply does at the payload limit; where the limit comes from; the offload
  contract for bulky payloads; and the ingestion-vs-wire knob taxonomy. One spec a new
  developer reads instead of twenty call sites.

### Modified Capabilities

- `agentic-loop`: loop completion results become durable-or-loud (never silently absent);
  the request lane is bounded by hydration; failure reasons are typed.

## Impact

- `natsclient` (kv.go, client.go, request.go), `pkg/errs` (one classification arm),
  `processor/agentic-loop` + `processor/agentic-tools` (completion offload + ref-following
  read), `processor/agentic-model` (hydration), `processor/graph-ingest/query.go` (dead
  constant), `config/streams.go` (governance ruling outcome only).
- Behavior changes, all in the honest direction: silent drops → typed errors; timeouts →
  fast classified "too large"; infinite retries → permanent refusal. Lockstep-relevant for
  sisters reading error classes; changelog entry required.
- Consumers: every sister (SemSource's live size workarounds were the field evidence);
  SemMachina's loops inherit the request-lane bound.
- The clustering site is untouched here: its drop contract is gh#855's scoped decision; this
  change closes the storage-gap half (`PutSummary` mis-classification falls to the
  chokepoint + classification fixes).

## Non-goals

- **Context windowing or summarization.** Hydration is behavior-neutral; deciding *which*
  messages a model sees is model-behavior policy and stays out of this change.
- **Provider-side context-window management** — already handled by the loop's truncation
  machinery; unrelated to wire limits.
- **Raising any limit.** The framework honors the server's advertised limit; choosing that
  limit remains the operator's deployment decision.
- **Retention redesign.** The governance-stream ruling here picks a discard posture; deep
  retention/archival is ADR-068's lane.
