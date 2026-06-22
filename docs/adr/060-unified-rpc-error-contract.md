# ADR-060: Unified RPC Error Contract — One Typed, Wrappable Error Over the Wire

## Status

**Proposed (scope/decision) — 2026-06-22.** This ADR **names a responsibility and decides a
target shape**; it does **not** design the migration mechanism beyond reusing what already
exists. It reuses the gh#93 header-classification seam (`X-Status` / `X-Error-Class`),
`pkg/errs.ClassifiedError` and its three-value `ErrorClass` set {transient, invalid, fatal}, the
`graph.ErrorCode*` constants, and the `RequestClassified` / `RequestWithRetryClassified` /
`ClassifyReply` / `RespondError` caller surface. It invents **no new taxonomy, transport,
registry, or error class** — one small `RPCError` type and one standard error body, both built
from primitives already on main. Every current-behavior claim below is grounded in `file:line`
and was read against main on 2026-06-22. Pre-1.0, we own every consumer, so this ADR **takes the
break** (gh#161) and lands the idiomatic Go/gRPC pattern as the end state; the dual-channel body
is demoted to the **transition mechanism**, retired at gh#161.

## Context: the same semantic, represented two ways, only one classified

The natsclient request/reply handler signature is `func(ctx, data) ([]byte, error)`
(`natsclient/request.go:187`). It has **two error channels**, and a domain failure can travel
down either one depending only on which handler emits it:

1. **Go-error channel (classified).** A handler returns a non-nil Go error; `SubscribeForRequests`
   calls `RespondError` (`natsclient/request.go:208-226`), which stamps `X-Status: error` +
   `X-Error-Class` headers and the legacy `error: <msg>` body (`natsclient/errors.go:142-158`).
   `ClassifyReply` reconstructs a classified `*errs.ClassifiedError` from the header
   (`natsclient/errors.go:194-222`). A caller using `RequestClassified` does **one** check: branch
   on `err` + `errs.IsInvalid/IsTransient/IsFatal`.

2. **Typed-body channel (unclassified).** A handler returns `(body, nil)` where the body is a
   struct carrying `Success:false, ErrorCode:...`. `ClassifyReply` sees no error header and
   **passes the body through as `(body, nil)`** (`natsclient/errors.go:221`). The caller must
   do a **second** check — `json.Unmarshal` then `if !resp.Success`.

The graph-ingest **mutation** handlers use channel 2. `entity_not_found`
(`processor/graph-ingest/mutations.go:779,843`), `entity_already_exists`
(`mutations.go:466`), `revision_mismatch` (`mutations.go:852,894`), and `invalid_request`
(`mutations.go:426`) are all emitted as `(MutationResponse{Success:false, ErrorCode}, nil)` via
the `marshal*ErrorCoded` helpers (`mutations.go:1060,1089`) — nil Go error, no wire class. The
comment at `mutations.go:699` states the contract explicitly: "A logical CAS failure (revision
mismatch) returns (Success=false, nil err)."

**The smell:** a domain error on channel 2 carries no wire class and lives only in a struct
`bool` that neither the compiler nor the wire enforces — the root of the gh#93 silent-corruption
class. The sharpest evidence that this is incoherent rather than merely dual: the graph-ingest
**query** handlers already emit the *same* "not found" semantic down channel 1 —
`processor/graph-ingest/query.go:93` returns `errs.Classified(errs.ErrorInvalid,
fmt.Errorf("not found: %s", req.ID))`, fully classified — while the graph-ingest **mutation**
handlers emit "not found" down channel 2, unclassified. `natsclient/errors.go:35` documents that
"'not found' maps to ErrorClass=invalid at this layer," but that mapping only fires when not-found
arrives as a Go error. **The same fact is classified on the query path and unclassified on the
mutation path** — a difference with no design justification, only history.

### A third divergent shape exists and is strictly worse

The graph-**index** `*NATS` query handlers (`processor/graph-index/query.go`) emit a *third*
shape: `json.Marshal(graph.NewQueryError[T]("invalid request"))` and `(...)("internal error")`
returned via the **success** path (`query.go:91,95,105,110` and siblings through the file). This
is channel 2 but worse — the `QueryResponse[T]` envelope (`graph/query_contracts.go:10-15`) has
only a free-text `Error string`, **no `ErrorCode`, no class**, and it collapses "invalid request"
(a 400-like) and "internal error" (a 500-like) into indistinguishable free text. A
`RequestClassified` caller receives this envelope as **success data** and unmarshals it as the
success-path type — the exact silent-corruption shape gh#164 names.

### What the dual channel costs callers

In-tree callers on channel 2 do the two-step dance and are a standing silent-corruption surface:
`processor/rule/triple_mutator.go:78-84` and `:122-126` and `:185-190`,
`processor/agentic-loop/graph_writer.go:113-119,150,193`,
`processor/agentic-tools/write_todos.go:428,453`, `processor/agentic-tools/decide.go:705,736`,
`processor/research-graph-llmwrap/triplepub.go:89,112`. Each does
`Request*` → `json.Unmarshal(respData, &resp)` → `if !resp.Success`. None of them
`bytes.HasPrefix`-checks for an `error: ` body first, so a handler-*internal* failure that takes
channel 1 (e.g. a KV transport blip surfaced as `marshalUpdateEntityWithTriplesError` →
`ErrorCodeInternal`, or a future panic-to-RespondError) can decode as a zero-valued `resp` with
`Success=false` and a confusing `Error`, or worse mis-decode — the same class that shipped three
times in beta.86 (`natsclient/errors.go:50-57`).

## The root funk: errors are crammed into the success type

`MutationResponse` (`graph/mutation_responses.go:44-63`) tries to be **both** the success body
*and* the failure envelope: it carries the real payload fields (`Entity`, `KVRevision`,
`TriplesAdded`) **and** `Success bool`, `Error string`, `ErrorCode string`. A reader cannot
trust any payload field without first checking `Success`. That is the in-band error channel,
and it is the thing to retire. The fix is the idiomatic Go/gRPC separation: a **success** reply
describes only success; a **failure** reply is a single typed error value. There are then three
distinguishable outcomes, not two:

| Outcome | Wire | Caller sees |
|---------|------|-------------|
| Full success | success body, no error header | typed body, `nil` err |
| **Partial (degraded) success** | success body, no error header | typed body, `nil` err — `Degraded` / `FailedSubjects` populated |
| Hard failure | small error body + `X-Error-Class` | `nil` body, `*RPCError` |

The middle row is load-bearing: `Degraded` (`graph/mutation_responses.go:44-63`) and
`FailedSubjects` (`:171-175`) are **success-with-detail**, NOT errors. A degraded write
*committed* (`mutation_responses.go:24-30` is explicit: "the write is COMMITTED, not pending");
a batch with `FailedSubjects` committed the subjects not listed. **Do not turn partial success
into a Go error** — that would make a committed write look retryable and break the
do-not-retry contract. Only the bottom row becomes `*RPCError`.

## The target: one typed, wrappable RPC error

The end state is the gRPC `status` / standard-Go-typed-error pattern — one channel, one value:

```go
type RPCError struct {
    Class  errs.ErrorClass   // transient | invalid | fatal  (REUSE; no new class)
    Code   string            // REUSE graph.ErrorCode* constants (entity_not_found, ...)
    Detail map[string]any    // entity, revision, failed_subjects (typed-vs-map: impl-PR call)
}
func (e *RPCError) Error() string
func (e *RPCError) Is(target error) bool   // enables errors.Is(err, ErrRevisionMismatch)
```

- **Failure** → `(nil, *RPCError)`. ONE return value, the full idiomatic toolkit:
  `errs.IsInvalid(err)` / `errs.IsTransient(err)` for the coarse retry/branch decision (works
  today via `errors.As` against the embedded `Class`); `var re *RPCError; errors.As(err, &re)`
  to reach `Code` / `Detail`; `fmt.Errorf("transition %s: %w", id, err)` to **wrap for context**
  with class and code surviving the wrap. No "both-non-nil / go look in the body" contract.
- **Success / partial success** → typed success body + `nil` err. The success type goes back to
  describing only success.

`Detail` **preserves** everything the typed body carried (`entity`, `revision`,
`failed_subjects` on the hard-failure path) — `errors.As` reaches it. Nothing is lost; the
structured detail moves from a `Success:false` struct into the error value where it belongs.

## The options

### Option A (RECOMMENDED, "done right") — one typed, wrappable error channel

Hard failures become `(nil, *RPCError)`. The `{Success:false, ErrorCode, Error}` signaling is
removed from the response types entirely; the typed detail it carried lives in `RPCError.Detail`,
reachable via `errors.As`. **This is Option A done right** — the earlier rejection ("Option A
throws away the structured detail") was a strawman that equated Option A with a *bare classified
error string*. A typed error is ONE channel with the detail **preserved inside the error**: a
caller does `errs.IsInvalid(err)` for the coarse branch, `errors.As(err, &re)` for `Code` /
`Detail`, and `errors.Is(err, ErrRevisionMismatch)` for control-flow codes (next section).
Gateways that need 404-vs-400 disambiguation read `re.Code` directly instead of substring-sniffing
a body. **This is the end state.**

### Option B — classify on the wire AND keep the typed body (the TRANSITION step, not the end)

During migration, group-a handlers keep returning their `(MutationResponse{Success:false,
ErrorCode}, ...)` / `QueryResponse[T]{Error}` body **and additionally** stamp the `X-Status` /
`X-Error-Class` header, so old body-sniffing callers keep working while new callers branch on the
classified `err`. This is the gh#93-style additive step — it removes the footgun *before* the
breaking body change, exactly as gh#93 Phase 1 was additive. **It is the migration mechanism, not
the recommendation.** Keeping it forever would enshrine the dual channel; it is retired at gh#161
(step 4) when the legacy body is dropped and only the standard error body + header remain.

### Option C — status quo + permanent lint

Leave the dual channel; rely on the gh#162 AST-walker lint to catch new `Request`+`Unmarshal`
callers forever. **Rejected as the end state** (kept as the transition guard — step 0 of the
migration). The lint guards against *re-introducing* the footgun; it does not remove it for
existing callers, does not give failures a class, and leaves "same semantic, two shapes"
permanently. A permanent lint is the tax you pay for not fixing the contract — acceptable as
scaffolding, not as the answer.

## CRITICAL NUANCE: control flow is error *identity*, not a separate channel

Some channel-2 outcomes are **expected control flow, not failure.** `revision_mismatch`
(optimistic-concurrency CAS) is a "re-read current revision and retry" signal the caller acts on
in a loop. The earlier draft preserved this as a *separate in-band channel* (`(body, nil)`). That
is **not** required by the control-flow nature — it is required only by today's lack of a wire
sentinel. The idiomatic Go answer is `io.EOF`: a **sentinel error checked with `errors.Is`** is
the canonical way to carry control-flow-as-error. So:

- `revision_mismatch` becomes a sentinel `errs.ErrRevisionMismatch` that `ClassifyReply`
  reconstructs from `Code == "revision_mismatch"`. The caller writes
  `if errors.Is(err, errs.ErrRevisionMismatch) { re-read; retry }`.
- **One channel.** Failure-vs-control-flow is distinguished by **error identity** (`errors.Is` /
  `errors.As`), not by a second return value. The contract is now UNIFORM: every non-success
  reply is a typed error; the caller's tools (`Is` for identity, `As` for detail, `IsInvalid`
  for class) decide what to do. This is **simpler** than the dual channel, not more complex — one
  return value, one set of error tools, no "is this row a failure or a body?" fork.

Provide sentinels **only** for the control-flow codes consumers actually loop on — primarily
`revision_mismatch` (and `owner_lease_stale` only if a live consumer treats it as
reconcile-and-retry, `graph/mutation_responses.go:98-107`). Do **not** mint a sentinel per
`ErrorCode`; `RPCError.Code` is the general discriminator. Reflexively converting every code into
a named sentinel is the over-engineering trap the guardrails forbid.

## Decision

1. **Name the responsibility.** There is **one RPC error contract** for semstreams
   request/reply: a reply either succeeds (full or degraded), or it is **one typed error value**
   — `(nil, *RPCError)` — carrying a wire `Class` (`X-Error-Class` ∈ {transient, invalid, fatal})
   and a `Code`. A `RequestClassified` / `RequestWithRetryClassified` caller branches with a
   single `err` check and the standard `errors.Is`/`errors.As` toolkit. This contract already
   holds for the Go-error channel (gh#93) and for graph-ingest query handlers; this ADR extends
   it to the seams that still cram errors into the success body.

2. **Adopt the typed `RPCError` end state (Option A done right).** Hard domain failures
   (`entity_not_found`, `entity_already_exists`, `invalid_request`, `internal`) become
   `(nil, *RPCError)` with detail preserved in `RPCError.Detail`. The `Success`/`Error`/
   `ErrorCode` *error-signaling* fields are retired from the response types at the end state.

3. **Control flow is a sentinel, not a channel.** `revision_mismatch` (and `owner_lease_stale`
   where a live consumer reconcile-retries) is delivered as a sentinel error checked with
   `errors.Is(err, errs.ErrRevisionMismatch)`. `pkg/lifecycle.Manager.Transition`'s CAS loop
   (`manager.go:690`) already branches on a sentinel (`errEmitRevisionMismatch`) — it keeps
   working, minus the manual body-unmarshal translation it does today (see below).

4. **Separate success from failure.** Success types describe only success; `Degraded` /
   `FailedSubjects` stay on the success body with `nil` err (partial success is NOT an error).
   The failure envelope is a small standard error body, reconstructed caller-side into `*RPCError`.

5. **Reframe the in-flight issues as one migration, not separate patches** (next section), with
   Option B as the additive transition and gh#161 as the break that lands the end state.

6. **Bound the blast radius** to the four enumerated seams below. This ADR brings the *existing*
   divergent seams onto the contract; it does not speculate about hypothetical future RPCs.

## The wire: a small standard error body + the existing class header

The recommended wire shape is a **standard error envelope** — the header carries the class
(`X-Error-Class`, already exists) and a small body `{code, message, detail}` that `ClassifyReply`
parses **generically** into `*RPCError`, independent of any success type:

```json
{ "code": "entity_not_found", "message": "not found: acme.ops...drone.001", "detail": { "entity": "..." } }
```

**Trade-off (the one that matters).** The alternative is header-only — put `Code` in a new header
and leave `Detail` inside the typed success body. Rejected: it keeps detail coupled to each
success type (so `ClassifyReply` would need to know every body shape to extract it), and it
leaves a partially-populated success body on the *failure* path — the exact "is this a body or an
error?" ambiguity we are retiring. The standard envelope is **generically parseable** (one decode
path for every seam) and **decoupled from each success type** (the success type goes back to pure
success). The cost is one small body shape to define and one `ClassifyReply` branch to add — both
inside the gh#93 seam, no new transport. We do **not** invent five new headers; the class header
already exists and the rest rides in the body.

## The migration (reuse gh#93 mechanics; the issues are the steps)

This is **one** migration with the already-filed issues as ordered steps. Only the **end-state
target** changed from the prior draft (typed `RPCError`, not a permanent dual return); the steps
are unchanged.

| Step | Issue | Role in the unified migration |
|------|-------|-------------------------------|
| 0 | gh#162 | **Transition guard.** AST-walker lint failing CI on any new `Request`+`Unmarshal` without a `RequestClassified` migration or `error:` prefix check. Lands first so no new violator rides in; **deleted at the very end** once no reply carries an unclassified in-body error. |
| 1 | gh#164(b) | **Migrate the graph-index `*NATS` handlers** (`processor/graph-index/query.go`) to the transition shape (Option B): emit the `QueryResponse[T]` body AND stamp `X-Error-Class` (invalid for "invalid request", transient/fatal for "internal error"), retiring the third divergent shape. Same step retires the never-registered msg-style dead code gh#164 part 2 enumerates. |
| 2 | (this ADR) | **Migrate the graph-ingest mutation handlers** (`mutations.go` group-a sites) to the transition shape; `revision_mismatch` / control-flow `owner_lease_stale` stay in-band during the window, sentinel-reconstructed at the end. |
| 3 | (this ADR) | **Migrate in-tree callers** to `RequestClassified` / `RequestWithRetryClassified` and the single `err`-branch (`triple_mutator.go`, `graph_writer.go`, `write_todos.go`, `decide.go`, `triplepub.go`). Mutation callers that retry use `RequestWithRetryClassified` (`natsclient/request.go:275`). |
| 4 | gh#161 | **BREAKING — lands the end state.** Drop the legacy `error: <msg>` body and the in-body `Success`/`Error`/`ErrorCode` *error* fields; the standard error envelope + `X-Error-Class` become the sole failure signal, reconstructed caller-side as `*RPCError`. Carries the e2e gate + lockstep sister-project (semconnect cs-api `classifyEntityQueryError`) migration the issue scopes. **Last step**; after it, the gh#162 lint guard is retired. |

gh#163 (CLOSED) and gh#304 (CLOSED, graph-query prefix passthrough now uses `RequestClassified`,
`processor/graph-query/query.go:384`) are prior steps of this same arc already landed. gh#326
(OPEN) is a sibling silent-corruption site the gh#162 lint (step 0) will flag — it folds into the
step-0 burn-down rather than a standalone fix.

## How `revision_mismatch`-as-sentinel changes `Manager.Transition`

Today `pkg/lifecycle/graph_emit.go:130-141` manually re-implements the wire→sentinel translation:
it `json.Unmarshal`s the body, checks `if !resp.Success`, `switch`es on
`resp.ErrorCode == graph.ErrorCodeRevisionMismatch`, and returns a *package-private* sentinel
`errEmitRevisionMismatch` (`graph_emit.go:84`). `Manager.Transition`'s CAS loop then branches
`if errors.Is(err, errEmitRevisionMismatch) { continue }` (`manager.go:690`; same at `:524,:813`).

That translation layer exists **only because the wire doesn't carry a sentinel today.** Under the
target, `ClassifyReply` reconstructs `errs.ErrRevisionMismatch` directly from `Code ==
"revision_mismatch"`, so:

- `graph_emit.go`'s body-unmarshal + `Success`-check + `ErrorCode` `switch` collapses into the
  return of whatever `RequestWithRetryClassified` already produced.
- `Manager.Transition` branches `if errors.Is(err, errs.ErrRevisionMismatch) { re-read; retry }`
  — the consumer **already wants this idiom** (it hand-rolled it). The framework sentinel replaces
  the package-private one; the loop body is otherwise unchanged.

Net: the consumer gets *simpler*, and the "unmarshal body, switch on `ErrorCode` string" footgun
disappears from every CAS-retry consumer instead of being re-coded per consumer.

## What is NOT changing

- **No new error class.** The set stays {transient, invalid, fatal} (`pkg/errs/errs.go:19-26`).
  No "not_found" wire class, no HTTP-status leakage into the wire layer (404/400 disambiguation
  stays at the gateway, `natsclient/errors.go:34-37`).
- **No new transport or registry.** Reuses `X-Status` / `X-Error-Class`
  (`natsclient/errors.go:97-102`), `RespondError` / `ReplyError` / `ClassifyReply`,
  `RequestClassified` / `RequestWithRetryClassified`. The only additions are one `RPCError` type
  and one standard error body shape, both built from existing primitives.
- **No new `ErrorCode*` constant.** The closed set in `graph/mutation_responses.go:69-108` is
  unchanged; the constants become `RPCError.Code` *values*. (Implementer note: the literal is
  `entity_already_exists`, not `entity_exists`; `graph/mutation_responses.go:86`.)
- **The success DATA fields survive.** `Entity`, `KVRevision`, `Version`, `TriplesAdded`,
  `Degraded` (`graph/mutation_responses.go:44-63`), `FailedSubjects` (`:171-175`), and
  `QueryResponse[T].Data` all stay on the success body. **Partial (degraded) success stays a
  success** with `nil` err.
- **What IS retired at the end state:** the in-body error *signaling* — `Success bool`, and
  `Error`/`ErrorCode` *used as the error channel*. They are the dual channel; the typed `RPCError`
  replaces them. (They persist through steps 1–3 as the additive transition body and are dropped
  at gh#161.)
- **`revision_mismatch` is NOT a hard error.** It is a control-flow sentinel
  (`errs.ErrRevisionMismatch`), `errors.Is`-checkable — the `io.EOF` idiom, not a `Success:false`
  body and not a fatal classification.
- **`pkg/errs` semantics** (`Classified` vs `WrapInvalid/Transient/Fatal`, the dual-encoding-window
  rationale at `errs.go:247-270`) are unchanged; `RPCError` composes them, it does not replace
  them.
- **The graph-ingest *query* handlers** (`processor/graph-ingest/query.go`) are already on the
  contract (`errs.Classified` returns); no change.

## What the implementing PR decides

- `RPCError.Detail` typed (a small struct per code) vs `map[string]any` — value-equality and
  round-trip tests govern; `map[string]any` is the low-friction default.
- The exact producer helper for "reply with the standard error body + `X-Error-Class`" (a new
  `RespondRPCError(msg, *RPCError)` vs reusing `ReplyWithHeaders` + a marshalled body vs a
  handler-return convention). `ClassifyReply`'s detection order (`natsclient/errors.go:199-221`)
  is fixed; only producer-side ergonomics are open.
- The precise sentinel set beyond `revision_mismatch`: whether `owner_lease_stale` gets a
  sentinel, governed by its single live consumer's actual handling (the
  `graph/mutation_responses.go:98-107` docstring frames it as "resolve the ownership conflict" —
  reconcile-and-retry — but the consumer's code, not the docstring, decides).
- The class for `internal` (`graph/mutation_responses.go:93-96`): transient (retryable) vs fatal,
  per emit site, matching the query handlers' existing choice (`query.go:95` uses `ErrorTransient`
  for "internal error").

## Consequences

- **Positive.** One contract, one return value: a `RequestClassified` caller's `err` check is
  authoritative for *every* request/reply seam, with the full idiomatic toolkit (`Is`/`As`/wrap).
  The silent-corruption class dies structurally, not just by lint. CAS-retry consumers get
  *simpler* (the per-consumer body→sentinel translation collapses into the framework). The typed
  detail and the control-flow signal both survive — the detail inside `RPCError`, the signal as a
  sentinel.
- **Negative / risk.** Step 4 (gh#161) is a BREAKING wire change requiring the e2e gate and
  lockstep semconnect migration; until it lands, the dual body persists (acceptable — Option B is
  additive, same posture as gh#93 Phase 1). Mis-modeling partial success as an error would break
  the do-not-retry contract (`mutation_responses.go:24-30`); the success/failure split is explicit
  precisely to prevent that.
- **Neutral.** No code is committed by this ADR; the steps are the already-filed issues with the
  end-state target updated from dual-return to typed `RPCError`.

## References

- `natsclient/request.go:187,208-226` — handler signature; the two-channel split at the
  `SubscribeForRequests` wrapper.
- `natsclient/errors.go` — `X-Status`/`X-Error-Class` (`:97-102`), `RespondError` (`:142-158`),
  `ClassifyReply` (`:194-222`), `RequestClassified`/`RequestWithRetryClassified` (`:242,275`),
  the not-found-maps-to-invalid note (`:34-37`), the silent-corruption footgun warning (`:39-57`),
  the deferred `X-Error-Sentinel` idea (`:73-77`) which this ADR's sentinel reconstruction supersedes.
- `pkg/errs/errs.go:19-26,84-104,247-270` — the {transient, invalid, fatal} set, `ClassifiedError`
  (which `RPCError` composes), and the `Classified` bare constructor.
- `processor/graph-ingest/mutations.go:426,466,699,779,843,852,894,1060,1089` — domain errors
  emitted via channel 2 today (the step-2 targets).
- `processor/graph-ingest/query.go:73-95` — the *query* exemplar already on the contract
  (`errs.Classified` returns).
- `processor/graph-index/query.go:91,95,105,110` — the third divergent `NewQueryError[T]` shape
  (gh#164b target).
- `graph/mutation_responses.go:24-30,44-63,69-108,171-175` — the three-state (full/degraded/fail)
  contract, `MutationResponse`, the closed `ErrorCode*` set, `owner_lease_stale`, `FailedSubjects`.
- `graph/query_contracts.go:10-31` — `QueryResponse[T]` (free-text `Error`, no `ErrorCode`/class).
- `pkg/lifecycle/graph_emit.go:81-84,130-141` — the per-consumer body→sentinel translation the
  target collapses; `pkg/lifecycle/manager.go:524,690,813` — `Manager.Transition`'s CAS loop
  already branching on a sentinel via `errors.Is`.
- `processor/rule/triple_mutator.go:78-84,122-126,185-190`,
  `processor/agentic-loop/graph_writer.go:113-119` — in-tree channel-2 callers (step-3 targets).
- gh#93 (the seam), gh#161 (BREAKING — drop legacy body, land end state — step 4), gh#162
  (AST-walker lint — step 0), gh#164 (graph-index header migration — step 1), gh#163 (CLOSED),
  gh#304 (CLOSED, prefix passthrough RequestClassified), gh#326 (OPEN, folds into step 0).
- [ADR-055](055-graph-write-intent-taxonomy.md), [ADR-056](056-authoritative-semantic-state.md) —
  the write-intent/ownership taxonomy whose mutation handlers emit these error codes; this ADR is
  orthogonal (how those handlers *report failure*, not what they authorize).
