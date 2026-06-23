# ADR-060: Unified RPC Error Contract — One Typed, Wrappable Error Over the Wire

## Status

**Accepted (scope/decision) — amended 2026-06-23.** This ADR **names a responsibility, decides a
target shape, AND decides its migration as ONE idiomatic breaking change.** Pre-1.0, and we own
every consumer (semconnect, semteams), so per the "take the break now, no compat shim" discipline
this ADR does **not** stage an Option-B transition: it collapses the prior draft's steps 1–4 into
a single coordinated break, landed in lockstep across the three repos under one tag.

It reuses the gh#93 header-classification seam (`X-Status` / `X-Error-Class`),
`pkg/errs.ClassifiedError` and its three-value `ErrorClass` set {transient, invalid, fatal}, the
`graph.ErrorCode*` constants, and the `RequestClassified` / `RequestWithRetryClassified` /
`ClassifyReply` / `RespondError` surface. It invents **no new error class, transport, or
registry.**

**Type decision (amended 2026-06-23).** The target is **NOT a new `RPCError` type.** Every live
consumer already branches on `*errs.ClassifiedError` via `errs.IsInvalid`/`IsTransient`
(semconnect `gateway/cs-api/systems.go:832,835,880,882`; semteams
`chainpause/decision_handler.go:421`, `chain/entity_reader.go:112`,
`tools/addsource/executor.go:194`). `IsInvalid`/`IsTransient`/`IsFatal` dispatch via
`errors.As(err, &ce *ClassifiedError)` (`pkg/errs/errs.go:113-114,157-158,200-201`), which matches
by concrete type and is indifferent to added fields/methods. A *new* type would force every one of
those sites to migrate a working `IsInvalid` check to a new `errors.As(&re)` check for **no
behavioral gain**. Instead this ADR **extends `ClassifiedError`** with `Code string` +
`Detail map[string]any` + an `Is` method for sentinel matching. Consumers' existing class branches
stay untouched and gain `Code` for free; the per-consumer hand-rolled discriminators (semconnect's
`"not found:"` string-sniff at `systems.go:878`, its `mutationFailure` `ErrorCode` switch at
`systems_post.go:440-448`) are retired by reading `ce.Code`. This is the **bigger** break (the
legacy body still dies) with **less** consumer churn — the idiomatic-for-us type is the one the
wire already reconstructs.

Every current-behavior claim below is grounded in `file:line` and was read against main on
2026-06-22, with the consumer-side and migration claims adversarially re-verified across all three
repos on 2026-06-23 (extend-vs-new-type, one-sentinel, compile-enforced field deletion, and the
`Is` empty-Code hazard were each checked by reading the code).

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
| **Partial (degraded) success** | success body, no error header | typed body, `nil` err — `Degraded` / `DegradedReason` / `FailedSubjects` populated |
| Hard failure | small error body + `X-Error-Class` | `nil` body, `*errs.ClassifiedError` |

The middle row is load-bearing: `Degraded` (`graph/mutation_responses.go:44-63`) and
`FailedSubjects` (`:171-175`) are **success-with-detail**, NOT errors. A degraded write
*committed* (`mutation_responses.go:24-30` is explicit: "the write is COMMITTED, not pending");
a batch with `FailedSubjects` committed the subjects not listed. **Do not turn partial success
into a Go error** — that would make a committed write look retryable and break the
do-not-retry contract. Only the bottom row becomes `*errs.ClassifiedError`. (The degraded
read-back *reason* moves from the retired `Error` field to a new `DegradedReason string` on the
success body — see "What the implementing PR decides".)

## The target: extend `ClassifiedError` — one typed, wrappable error on the type the wire already speaks

The end state is the standard-Go-typed-error pattern on the type `ClassifyReply` already
reconstructs — `*errs.ClassifiedError`, extended:

```go
type ClassifiedError struct {
    Class     ErrorClass     // transient | invalid | fatal (REUSE; no new class)
    Err       error          // wrapped cause (unchanged; Unwrap returns it)
    Message   string         // verbatim wire text (unchanged)
    Component, Operation string
    Code      string         // NEW: REUSE graph.ErrorCode* values (entity_not_found, ...); "" = uncoded
    Detail    map[string]any // NEW: entity, revision, failed_subjects; nil = none
}

// Is matches a sentinel by Code. REQUIRED guards (locked by test, see below):
//   1. target must be a *ClassifiedError (else false — protects errors.Is(err, context.Canceled))
//   2. Code must be NON-EMPTY (else "" == "" false-matches every uncoded error)
func (ce *ClassifiedError) Is(target error) bool {
    var t *ClassifiedError
    if !errors.As(target, &t) {
        return false
    }
    return t.Code != "" && t.Err == nil && ce.Code == t.Code
}
```

- **Failure** → `(nil, *errs.ClassifiedError)`. ONE return value, the full idiomatic toolkit:
  `errs.IsInvalid(err)` / `errs.IsTransient(err)` for the coarse retry/branch decision (works
  today, unchanged); `errors.As(err, &ce)` to reach `Code` / `Detail`;
  `errors.Is(err, errs.ErrRevisionMismatch)` for the one control-flow sentinel;
  `fmt.Errorf("transition %s: %w", id, err)` to **wrap for context** with class, code, and detail
  surviving the wrap. No "both-non-nil / go look in the body" contract.
- **Success / partial success** → typed success body + `nil` err. The success type goes back to
  describing only success.

`Detail` **preserves** everything the typed body carried (`entity`, `revision`,
`failed_subjects` on the hard-failure path) — `errors.As` reaches it. Nothing is lost; the
structured detail moves from a `Success:false` struct into the error value where it belongs. New
coded constructors `errs.ClassifiedCode(class, code, err)` and
`errs.ClassifiedCodeDetail(class, code, detail, err)` produce these; the existing
`errs.Classified(class, err)` is unchanged (empty `Code`, the graph-ingest query exemplar keeps
compiling).

## The options

### Option A (ACCEPTED, as extend-`ClassifiedError`) — one typed, wrappable error channel

Hard failures become `(nil, *errs.ClassifiedError)`. The `{Success:false, ErrorCode, Error}`
signaling is removed from the response types entirely; the typed detail it carried lives in
`ClassifiedError.Detail`, reachable via `errors.As`. **This is Option A done right** — the earlier
rejection ("Option A throws away the structured detail") was a strawman that equated Option A with
a *bare classified error string*. A typed error is ONE channel with the detail **preserved inside
the error**: a caller does `errs.IsInvalid(err)` for the coarse branch, `errors.As(err, &ce)` for
`Code` / `Detail`, and `errors.Is(err, errs.ErrRevisionMismatch)` for control-flow codes (next
section). Gateways that need 404-vs-400 disambiguation read `ce.Code` directly instead of
substring-sniffing a body. **This is the end state.** (The 2026-06-23 amendment fixes the *type*:
extend `ClassifiedError`, do not mint a parallel `RPCError` — see Status.)

### Option B — classify on the wire AND keep the typed body — **REJECTED as a stage**

The earlier draft kept this as the additive transition: handlers stamp the class header *and*
keep returning their `{Success:false, ErrorCode}` / `QueryResponse[T]{Error}` body so old
body-sniffing callers keep working. **Per the 2026-06-23 amendment the break is taken directly;
Option B's additive dual-stamp is NOT landed as an intermediate state.** Keeping it would enshrine
the dual channel; pre-1.0 and owning every consumer, we flip in lockstep instead (see "The
migration").

### Option C — status quo + permanent lint

Leave the dual channel; rely on the gh#162 AST-walker lint to catch new `Request`+`Unmarshal`
callers forever. **Rejected as the end state** (kept as the **transition guard** — armed through
PR-A..C, deleted in PR-D once the allowlist is empty). The lint guards against *re-introducing* the
footgun; it does not remove it for existing callers, does not give failures a class, and leaves
"same semantic, two shapes" permanently. A permanent lint is the tax you pay for not fixing the
contract — acceptable as scaffolding, not as the answer.

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

Provide sentinels **only** for the control-flow codes consumers actually loop on. Adversarial
verification (2026-06-23, all three repos) found **exactly one**: `revision_mismatch`
(`pkg/lifecycle/graph_emit.go:135` → `manager.go:524,690,813` CAS `continue`). `owner_lease_stale`
has **zero** branching consumers anywhere (only emit sites `mutations.go:456,686`); semconnect
flat-maps it to 400 (`systems_post.go:453`). `entity_not_found` has no *looping* consumer either —
it is a `ce.Code` discriminator (see the lifecycle note below for its `graph_emit.go:138`
translation, which collapses to `ce.Code == "entity_not_found"` but needs no framework sentinel).
Do **not** mint a sentinel per `ErrorCode`; `ce.Code` is the general discriminator. The net
sentinel set is **one**.

## Decision

1. **Name the responsibility.** There is **one RPC error contract** for semstreams
   request/reply: a reply either succeeds (full or degraded), or it is **one typed error value**
   — `(nil, *errs.ClassifiedError)` — carrying a wire `Class` (`X-Error-Class` ∈ {transient,
   invalid, fatal}) and a `Code`. A `RequestClassified` / `RequestWithRetryClassified` caller
   branches with a single `err` check and the standard `errors.Is`/`errors.As` toolkit. This
   contract already holds for the Go-error channel (gh#93) and for graph-ingest query handlers;
   this ADR extends it to the seams that still cram errors into the success body.

2. **Extend `ClassifiedError`; do NOT mint `RPCError`.** Add `Code` + `Detail` + an `Is` method
   (with the two required guards) and the `ClassifiedCode`/`ClassifiedCodeDetail` constructors.
   Hard domain failures (`entity_not_found`, `entity_already_exists`, `invalid_request`,
   `internal`) become `(nil, *errs.ClassifiedError)` with detail preserved in `.Detail`. The
   `Success`/`Error`/`ErrorCode` *error-signaling* fields are retired from the response types.

3. **Control flow is a sentinel, not a channel.** `revision_mismatch` is delivered as
   `errs.ErrRevisionMismatch`, checked with `errors.Is(err, errs.ErrRevisionMismatch)`.
   `pkg/lifecycle.Manager.Transition`'s CAS loop (`manager.go:690`) already branches on a
   package-private sentinel (`errEmitRevisionMismatch`) — it swaps to the framework sentinel, minus
   the manual body-unmarshal translation it does today (see below). **One sentinel only**;
   everything else is `ce.Code`.

4. **Separate success from failure.** Success types describe only success; `Degraded` /
   `DegradedReason` / `FailedSubjects` stay on the success body with `nil` err (partial success is
   NOT an error). The failure envelope is a small standard error body, reconstructed caller-side
   into `*errs.ClassifiedError`.

5. **One break, in lockstep — not a staged transition.** Pre-1.0, we own every consumer, so the
   four-PR plan below lands the end state directly; the sister repos flip with it under one tag.
   Option B (additive dual-stamp) is not landed as an intermediate state.

6. **Bound the blast radius** to the four enumerated seams below (graph-ingest mutations,
   graph-index `*NATS` queries, the in-tree callers, the legacy body). The graph-ingest *query*
   handlers are already on the contract. The semconnect **spatial** seam (`spatial.go:310`,
   `graph-index-spatial`) is a known **out-of-scope fifth seam** — it consumes a non-conforming
   `error: <text>` body, treats any failure as 500 (no class branching to break), and is **not**
   migrated here; it is filed separately so "unified contract" is honestly bounded.

## The wire: class + code headers, and a small standard error body for detail

The class and the code ride **headers**; the detail rides a small body. This split is what lets
PR-A be **purely additive**: a NATS reply has one body, and legacy consumers on the un-bumped
sister tags still sniff the `error: <msg>` body prefix — so the body cannot change in the additive
window. The new `Code` therefore travels as a **header**, leaving the body untouched until the
breaking PR:

- `X-Status: error` + `X-Error-Class: <class>` — exist (gh#93); unchanged.
- `X-Error-Code: <code>` — **NEW, additive (PR-A), and the permanent code channel.** Carries the
  stable machine code (`entity_not_found`, `revision_mismatch`, ...). Stamped only when the handler
  error carries a non-empty `Code`, so uncoded errors are byte-for-byte unchanged on the wire. It
  is small (a short token), header-appropriate, and present from PR-A onward so the sentinel and
  `ce.Code` discrimination work *during* the transition (PR-B/PR-C need it).
- **Body.** Through PR-C the body stays the legacy `error: <msg>` text (additive). PR-D (breaking)
  switches it to a small standard envelope carrying the **message and detail** —
  `{ "message": "not found: acme.ops...drone.001", "detail": { "entity": "..." } }` — and drops the
  `error:` prefix. `Detail` (entity, revisions) lands here, not in a header (it is structured and
  potentially multi-field; the body is the right home).

`ClassifyReply` reconstructs a `*errs.ClassifiedError` **generically** — one decode path for every
seam, independent of any success type: it reads the class + code headers, sets `ce.Code`, and the
`(*ClassifiedError).Is` method matches `errs.ErrRevisionMismatch` by code so
`errors.Is(err, errs.ErrRevisionMismatch)` round-trips. This supersedes the deferred
`X-Error-Sentinel`-header idea (`natsclient/errors.go:73-77`) — the code header *is* that
mechanism, generalized.

**Producer ergonomics.** No new `RespondRPCError` helper is needed. Because `SubscribeForRequests`
(`request.go:208`) already calls `RespondError` **only** on a non-nil Go-error return, the entire
producer migration is "return `(nil, errs.ClassifiedCode(...))` instead of `(marshaledBody, nil)`"
— the convention the graph-ingest *query* handlers already use. `RespondError` / `ReplyError`
(`errors.go:142-158,165`) learn to marshal the `{code,message,detail}` body from the
`*ClassifiedError`; that is the only producer change.

**Trade-off (the one that matters).** The alternative is header-only — put `Code` in a new header
and leave `Detail` inside the typed success body. Rejected: it keeps detail coupled to each
success type (so `ClassifyReply` would need to know every body shape to extract it), and it
leaves a partially-populated success body on the *failure* path — the exact "is this a body or an
error?" ambiguity we are retiring. The standard envelope is **generically parseable** and
**decoupled from each success type**.

## The migration — ONE break, four stacked PRs, lockstep tag

The prior staged table (Option B steps 1–4) is **superseded**. This is a single coordinated break;
the in-tree PRs are non-breaking until the final PR, which lands in lockstep with the sister repos.
The gh#162 lint stays armed PR-A→PR-C and is deleted in PR-D.

| PR | Scope | Breaks wire? | Clears |
|----|-------|--------------|--------|
| A | **Framework types.** `pkg/errs`: add `Code`/`Detail` fields, the guarded `Is` method, `ClassifiedCode`/`ClassifiedCodeDetail` ctors, the `ErrRevisionMismatch` sentinel (literal `"revision_mismatch"` + graph assertion test). `natsclient/errors.go`: add the `X-Error-Code` header — `RespondError`/`ReplyError` stamp it when the error carries a non-empty `Code` (**body unchanged**, legacy `error:` text retained); `ClassifyReply` reads it onto `ce.Code` so the sentinel + `ce.Code` round-trip. `Detail` is carried on the error value but not yet serialized (lands in PR-D's body). | No (additive — header only, body untouched) | — |
| B | **graph-index query seam (producers + their in-tree callers, together).** The 7 `*NATS` handlers (`graph-index/query.go:91,95,105,110` + siblings) return `(nil, errs.ClassifiedCode(...))` — `invalid request` → invalid/`invalid_request`, `internal error` → transient/`internal` (not-found stays an empty-result *success*, unchanged). Every in-tree caller of `graph.index.query.*` moves to `RequestClassified` + an `err` branch in the SAME PR (a plain `Request`+`Unmarshal` caller chokes on the `error:` body once the handler flips — producer and callers are coupled). Delete `NewQueryError` once unused; keep `QueryResponse[T].Error` field until PR-D. Clear the gh#326 + graph-index gh#334 allowlist entries. | No (semstreams-internal; sister consumers move at the PR-D tag) | gh#164 part 2, gh#326 |
| C | **graph-ingest mutation seam (producers + their in-tree callers + lifecycle, together).** Mutation handlers (`mutations.go:1057,1086` helpers) return `(nil, errs.ClassifiedCodeDetail(...))`; response structs keep `Success`/`Error`/`ErrorCode` fields (unwritten on failure) until PR-D. The mutation callers drop the `!resp.Success` second check and branch on `err` — preserving any `ce.Code`-as-success semantics (e.g. `write_todos.go:428` "entity_not_found is success for us" → `if errs.IsInvalid(err) && ce.Code == "entity_not_found" { return nil }`). `pkg/lifecycle/graph_emit.go` deletes **both** body→sentinel translations (`:130-141`, `:84`) — revision_mismatch → `errs.ErrRevisionMismatch`, entity_not_found → `ce.Code`; `manager.go:524,690,813` use `errs.ErrRevisionMismatch`. Clear the remaining gh#334 allowlist entries (empties it). | No (semstreams-internal) | gh#334 (empties allowlist) |
| D | **BREAKING.** Switch the failure body from the legacy `error: <msg>` text to the standard `{message, detail}` envelope (class + code stay in headers); `ClassifyReply` parses it for `Detail` + drops the `error:` fallback + `legacyErrorBodyPrefix`. Delete `Success`/`Error`/`ErrorCode` from `MutationResponse` (add `DegradedReason string`), `Error` from `QueryResponse[T]`; assert the lint allowlist is empty, then delete the gh#162 guard. Add the `Detail` `float64` round-trip test (it now crosses the wire). Lands with the sister-repo PRs under one tag; e2e gate (incl. the new negative-path assertion) green first. | **Yes** | gh#161, gh#162 |

**Issue mapping:** #164 → PR-B; #334 → PR-A (framework support) + PR-C (burn-down); #326 → PR-C;
#161 → PR-D; #330 (predicate value-filter coverage) is enabled by PR-B and files as its own
non-breaking follow-up, not a blocker. gh#163 / gh#304 are prior steps of this arc already landed.

### Lockstep rollout + e2e gate

Both sister repos pin a **published tag** (no `replace`), so nothing breaks until they bump. Order:

1. Land semstreams PR-A → PR-B → PR-C on `main` (all non-breaking; sister repos on beta.113/114
   keep working — PR-B/C keep the response fields present and the legacy `error:` fallback intact).
2. Run the e2e gate on `main` (PR-A..C).
3. Prepare semconnect + semteams branches against a `replace`/RC of semstreams `main`-with-PR-D;
   make the consumer changes; go green locally. **semconnect** (heavier): `systems_post.go:392` /
   `systems_crd.go:163,193` drop the `!resp.Success` check; `mutationFailure` (`systems_post.go:440-448`)
   switches on `ce.Code` not `resp.ErrorCode`; `classifyEntityQueryFailure` (`systems.go:874-887`)
   replaces the `"not found:"` sniff with `ce.Code == "entity_not_found"`; the degraded-reason logs
   (`systems_post.go:394`, `systems_crd.go:165`) read `resp.DegradedReason` not `resp.Error`;
   `listEntitiesByType` (`systems.go:918,939`) → `RequestClassified`. **semteams** (one site):
   `tools/emitdevviatestplan/remover.go:75` → `RequestWithRetryClassified` + `err` branch (the
   other three sites are already safe).
4. Land semstreams PR-D on `main`.
5. **Re-run the e2e gate** on `main` post-PR-D (BREAKING-rule gate — green BEFORE the tag).
6. Tag semstreams (BREAKING in changelog).
7. Bump + merge semconnect and semteams (drop the `replace`); each repo's CI is its merge gate.

The window where `main` has PR-D but sister repos haven't bumped is **safe**: there is no shared
running module; each repo fetches the breaking tag only when *it* bumps, and a stale consumer that
bumps *without* the code change fails to **compile** (the read of the deleted `resp.Success` field
won't build — verified there is no anonymous-struct json-decode shape that would silently see a
zero value). The compiler is the safety net.

**E2E tiers (BREAKING rule):** `e2e:lifecycle` (CAS / `revision_mismatch` sentinel via
`Manager.Transition`), `e2e:crud-tools` + `e2e:agentic` (mutation callers), `e2e:structural`
(graph-index queries). **Coverage gap (filed):** no tier today asserts the wire error *class* —
they all assert the happy path. PR-D's gate MUST add a negative-path assertion (e.g. update on a
missing entity → assert `ce.Code == "entity_not_found"` / invalid class) in `crud-tools`, per the
"breaking change needs e2e on the touched path" hard rule. The sufficient gate is the PR-A
production-decoder round-trip **plus** this new e2e stage — green e2e on the happy path alone is
necessary but not sufficient.

## How the sentinel collapses `Manager.Transition` (and the second `entity_not_found` translation)

Today `pkg/lifecycle/graph_emit.go:130-141` manually re-implements **two** wire→sentinel
translations: it `json.Unmarshal`s the body, checks `if !resp.Success`, and `switch`es on
`resp.ErrorCode` — returning the *package-private* `errEmitRevisionMismatch` (`graph_emit.go:84`)
for `revision_mismatch` **and** a package-local `ErrEntityNotFound` for `entity_not_found`
(`graph_emit.go:138`). `Manager.Transition`'s CAS loop branches
`if errors.Is(err, errEmitRevisionMismatch) { continue }` (`manager.go:690`; same at `:524,:813`).

Both translations exist **only because the wire doesn't carry a sentinel/code today.** Under the
target:

- The `revision_mismatch` translation collapses: `ClassifyReply` reconstructs
  `errs.ErrRevisionMismatch` directly, so `graph_emit.go`'s body-unmarshal + `Success`-check +
  `ErrorCode` `switch` becomes the return of whatever `RequestWithRetryClassified` produced.
- The `entity_not_found` translation collapses to `ce.Code == "entity_not_found"` (no framework
  sentinel — no looping consumer). PR-C must address **both** so `graph_emit.go` is not left
  half-migrated (revision via sentinel, not-found still hand-unmarshaled).
- `Manager.Transition` branches `if errors.Is(err, errs.ErrRevisionMismatch) { re-read; retry }`.
  **Check the sentinel BEFORE `errs.IsInvalid`** — the sentinel's class is `ErrorInvalid`, so a
  consumer that checked `IsInvalid` first would treat a CAS-retry signal as a hard 400. `manager.go`
  already checks only the sentinel; document the ordering in the sentinel's doc comment.

Net: the consumer gets *simpler*, and the "unmarshal body, switch on `ErrorCode` string" footgun
disappears from every CAS-retry consumer instead of being re-coded per consumer.

## What is NOT changing

- **No new error class.** The set stays {transient, invalid, fatal} (`pkg/errs/errs.go:19-26`).
  No "not_found" wire class, no HTTP-status leakage into the wire layer (404/400 disambiguation
  stays at the gateway via `ce.Code`, `natsclient/errors.go:34-37`). semconnect keeps its *local*
  `errEntityNotFound` (404 is a gateway concern) but populates it from `ce.Code`, not a body sniff.
- **No new transport or registry.** Reuses `X-Status` / `X-Error-Class`
  (`natsclient/errors.go:97-102`), `RespondError` / `ReplyError` / `ClassifyReply`,
  `RequestClassified` / `RequestWithRetryClassified`. The only additions are **two fields + an `Is`
  method on `ClassifiedError`** and one standard error body shape — no new type.
- **No new `ErrorCode*` constant.** The closed set in `graph/mutation_responses.go:69-108` is
  unchanged; the constants become `ce.Code` *values*. (Implementer note: the literal is
  `entity_already_exists`, not `entity_exists`; `graph/mutation_responses.go:86`. The
  `revision_mismatch` literal is at `:75`.)
- **The success DATA fields survive.** `Entity`, `KVRevision`, `Version`, `TriplesAdded`,
  `Degraded` (`graph/mutation_responses.go:44-63`), `FailedSubjects` (`:171-175`), and
  `QueryResponse[T].Data` all stay. **One additive success field:** `DegradedReason string` (the
  degraded read-back reason that the retired `Error` field used to carry, read by semconnect
  `systems_post.go:394` / `systems_crd.go:165`). **Partial (degraded) success stays a success**
  with `nil` err.
- **What IS retired:** the in-body error *signaling* — `Success bool`, and `Error`/`ErrorCode`
  *used as the error channel* (and `QueryResponse[T].Error`). They are the dual channel; the typed
  `*errs.ClassifiedError` replaces them. Dropped in PR-D.
- **`revision_mismatch` is NOT a hard error.** It is a control-flow sentinel
  (`errs.ErrRevisionMismatch`), `errors.Is`-checkable — the `io.EOF` idiom.
- **`pkg/errs` semantics** (`Classified` vs `WrapInvalid/Transient/Fatal`, the dual-encoding-window
  rationale at `errs.go:247-270`) are unchanged; the new fields/method extend `ClassifiedError`,
  they do not alter class detection or wrapping. `Error()`/`Unwrap()` are unchanged.
- **The graph-ingest *query* handlers** (`processor/graph-ingest/query.go`) are already on the
  contract (`errs.Classified` returns); no change.

## What the implementing PR decides

- **The `Is` two-guard contract is a REQUIRED, named test invariant — not implementer
  discretion.** Adding `Is` changes `errors.Is` semantics for *all* `ClassifiedError`s. An
  unguarded `Code`-equality `Is` returns **true for any two empty-`Code` errors** (`"" == ""`),
  and today every `classifiedFromHeader` reconstruction (`natsclient/errors.go:319-333`) is
  empty-`Code` — so an unguarded `Is` would silently false-match across unrelated errors, breaking
  the very `errors.Is` checks this ADR adds. Lock both guards with named tests:
  (1) target must type-assert to `*ClassifiedError` (so `errors.Is(err, context.Canceled)` /
  `sql.ErrNoRows` / the non-classified `errEntityNotFound` stay false); (2) `Code` must be
  non-empty. Adversarially verified 2026-06-23: the guarded form returns the correct verdict on
  every real semconnect collision site; the unguarded form does not.
- **`Detail` typed struct vs `map[string]any`** — `map[string]any` is the low-friction default and
  is safe today (no consumer reads structured detail out of the error path; revision numbers are
  currently baked into free text at `mutations.go:852,894`). The round-trip test **must assert the
  `float64` reality** of JSON-decoded numerics so no future consumer writes
  `.Detail["expected_revision"].(uint64)` and panics.
- **Where the degraded read-back reason lives** — `DegradedReason string` on the success body
  (mirroring the `Degraded`+`DegradedReason` pattern already used in graph-query / research
  outputs), populated where `Error` is populated on the degraded path today.
- **The class for `internal`** (`graph/mutation_responses.go:93-96`): **transient** (retryable),
  matching the query handlers' existing choice (`query.go:95` uses `ErrorTransient`) and the
  marshal-helper emit (`mutations.go:1057,1086`).
- **The negative-path e2e stage** placement (`crud-tools`) and exact assertion shape.

## Consequences

- **Positive.** One contract, one return value: a `RequestClassified` caller's `err` check is
  authoritative for *every* request/reply seam, with the full idiomatic toolkit (`Is`/`As`/wrap),
  on the type consumers already speak. The silent-corruption class dies structurally, not just by
  lint. CAS-retry consumers get *simpler* (the per-consumer body→sentinel translation collapses
  into the framework). semconnect's hand-rolled `"not found:"` sniff and `ErrorCode` switch are
  retired into `ce.Code`. The typed detail and the control-flow signal both survive.
- **Negative / risk.** PR-D is a BREAKING wire change requiring the e2e gate (incl. the new
  negative-path assertion) and a lockstep semconnect + semteams tag bump. The break is
  compile-enforced in both sister repos (no silent zero-value shape), which bounds the risk. The
  `Is` empty-`Code` hazard is real and is mitigated by the required guard tests. Mis-modeling
  partial success as an error would break the do-not-retry contract
  (`mutation_responses.go:24-30`); the success/failure split is explicit to prevent that.
- **Neutral.** No code is committed by this ADR. The `pkg/errs` → `graph` import cycle blocks
  putting `graph.ErrorCodeRevisionMismatch` in the sentinel literal directly (`graph` imports
  `errs`, not vice versa); the sentinel uses the literal `"revision_mismatch"` in `pkg/errs`, with
  an assertion test **in the `graph` package** locking
  `errs.ErrRevisionMismatch.Code == graph.ErrorCodeRevisionMismatch` so the two never drift.

## References

- `natsclient/request.go:187,208-226` — handler signature; the two-channel split at the
  `SubscribeForRequests` wrapper (stamps the class only on a non-nil Go-error return).
- `natsclient/errors.go` — `X-Status`/`X-Error-Class` (`:97-102`), `RespondError` (`:142-158`),
  `ReplyError` (`:165`), `ClassifyReply` (`:194-222`), `classifiedFromHeader` (`:319-333`),
  `RequestClassified`/`RequestWithRetryClassified` (`:242,275`), the not-found-maps-to-invalid note
  (`:34-37`), the silent-corruption footgun warning (`:39-57`), the deferred `X-Error-Sentinel`
  idea (`:73-77`) which the body-`code` sentinel reconstruction supersedes.
- `pkg/errs/errs.go:19-26,84-104,113-114,157-158,200-201,247-270` — the {transient, invalid,
  fatal} set, `ClassifiedError` (extended here), `IsInvalid`/`IsTransient`/`IsFatal` via
  `errors.As`, and the `Classified` bare constructor.
- `processor/graph-ingest/mutations.go:426,456,466,686,699,779,843,852,894,1057,1060,1086,1089` —
  domain errors emitted via channel 2 today (PR-B producer targets).
- `processor/graph-ingest/query.go:73-95` — the *query* exemplar already on the contract
  (`errs.Classified` returns).
- `processor/graph-index/query.go:91,95,105,110` — the third divergent `NewQueryError[T]` shape
  (PR-B target).
- `graph/mutation_responses.go:24-30,44-63,69-108,75,86,171-175` — the three-state
  (full/degraded/fail) contract, `MutationResponse`, the closed `ErrorCode*` set (incl. the
  `revision_mismatch` literal at `:75`), `owner_lease_stale`, `FailedSubjects`.
- `graph/query_contracts.go:10-31` — `QueryResponse[T]` (free-text `Error`, no `ErrorCode`/class);
  `NewQueryError` deleted in PR-B.
- `pkg/lifecycle/graph_emit.go:81-84,130-141` — the per-consumer body→sentinel translations the
  target collapses (**both** revision_mismatch and entity_not_found); `pkg/lifecycle/manager.go:524,690,813`
  — `Manager.Transition`'s CAS loop branching on the sentinel via `errors.Is`.
- `processor/rule/triple_mutator.go:78-84,122-126,185-190`,
  `processor/agentic-loop/graph_writer.go:113-119,150,193`,
  `processor/agentic-tools/write_todos.go:428,453`, `processor/agentic-tools/decide.go:705,736`,
  `processor/research-graph-llmwrap/triplepub.go:89,112` — in-tree channel-2 callers (PR-C targets).
- `test/natsclient/request_guard_test.go` — the gh#162 AST-walker lint + allowlist (gh#326/#334
  entries); deleted in PR-D once the allowlist is empty.
- `test/e2e/scenarios/` (lifecycle, crud-tools, agentic, structural) — e2e gate; **no tier asserts
  the wire error class today** (the filed coverage gap PR-D must close).
- **semconnect** `gateway/cs-api/systems.go:787,830,832,835,857,874-887,918,939` (ClassifyReply
  consumer, the `"not found:"` sniff at `:878`, the local `errEntityNotFound` at `:857`),
  `systems_post.go:388-458` (`Success` check `:392`, `mutationFailure` `:440-448`, degraded-reason
  log `:394`), `systems_crd.go:159-197` (`Success` `:163,193`, degraded-reason `:165`),
  `spatial.go:300-328` (the out-of-scope fifth seam) — lockstep consumer targets.
- **semteams** `cmd/semteams/tools/emitdevviatestplan/remover.go:75,83-84` (the one vulnerable
  site); `chainpause/decision_handler.go:421`, `chain/entity_reader.go:112`,
  `tools/addsource/executor.go:194` (already on the contract — no change).
- gh#93 (the seam), gh#161 (BREAKING — PR-D), gh#162 (AST-walker lint — PR-A..C guard, deleted
  PR-D), gh#164 (graph-index header migration — PR-B), gh#326 / gh#334 (caller burn-down — PR-C),
  gh#163 / gh#304 (prior steps, CLOSED), gh#330 (predicate value-filter coverage, enabled by PR-B).
- [ADR-055](055-graph-write-intent-taxonomy.md), [ADR-056](056-authoritative-semantic-state.md) —
  the write-intent/ownership taxonomy whose mutation handlers emit these error codes; this ADR is
  orthogonal (how those handlers *report failure*, not what they authorize).
