# ADR-060 PR-D — Breaking Change Summary (for downstream conformance)

**Audience:** every repo that pins semstreams as a dependency (semconnect, semteams, and
any future consumer of the NATS request/reply mutation or query APIs).

**What this is:** the wire + Go-API delta that lands with the semstreams **PR-D breaking tag**.
Pull that tag, then make the changes below so your repo compiles and conforms. The Go compiler is
your safety net — every removed field is a hard compile error at the call site, so a bump without
these changes fails to build (it cannot silently mis-decode).

This is the final PR of [ADR-060](060-unified-rpc-error-contract.md). PR-A/B/C were non-breaking
and are already on `main`; PR-D removes the legacy dual error channel.

---

## The one-sentence contract

A semstreams request/reply is **EITHER** a success body (with a `nil` Go error) **OR** a single
typed error value — `*errs.ClassifiedError` — carrying a wire **class** + **code**. There is no
in-band error channel anymore; **a failure carries no body.**

---

## What changed

### 1. Wire error body: `error: <msg>` text → `{message, detail}` JSON envelope

A handler-error reply now carries a JSON body, not the `error: ` text prefix:

```json
{ "message": "not found: acme.ops.robotics.gcs.drone.001", "detail": { "entity": "acme...drone.001" } }
```

- The **class** rides the `X-Error-Class` header (`transient` | `invalid` | `fatal`) — unchanged.
- The **code** rides the `X-Error-Code` header (`entity_not_found`, `revision_mismatch`, …) —
  added in PR-A, now the permanent code channel.
- `natsclient.ClassifyReply` parses the envelope and reconstructs a `*errs.ClassifiedError` with
  `Message` + `Code` + `Detail`.
- **The legacy `error: ` body fallback in `ClassifyReply` is deleted.** A reply with **no**
  `X-Status: error` header is treated as **success** (its body returned verbatim) — even if the
  body happens to start with `error:`. Failures are signalled by the header, period.

If you sniff the body for an `error: ` prefix anywhere, **delete that** — read `ce.Code` (or the
class) instead.

### 2. `graph.MutationResponse` — error-signalling fields removed

| Removed | Replacement |
|---|---|
| `Success bool` | success = `nil` Go error from `RequestClassified` |
| `Error string` (failure) | the returned `*errs.ClassifiedError` (`err.Error()`) |
| `Error string` (degraded read-back reason) | **`DegradedReason string`** (new field) |
| `ErrorCode string` | `ce.Code` via `errors.As(err, &ce)` |

Kept: `Degraded`, `DegradedReason` (new), `TraceID`, `RequestID`, `Timestamp`, `KVRevision`, plus
the per-shape success fields (`Entity`, `Version`, `TriplesAdded`, `TriplesRemoved`, `Deleted`,
`WrittenCount`, `FailedSubjects`).

### 3. `graph.QueryResponse[T]` — `Error` removed; `NewQueryError` deleted

Query failures arrive as a `*errs.ClassifiedError` on the err channel. `QueryResponse[T]` is a
success-only envelope now (`Data`, `RequestID`, `Timestamp`). `graph.NewQueryError[T]` is gone.

### 4. graph-ingest query not-found is now coded

`graph.ingest.query.entity` not-found now carries `Code == entity_not_found` (was uncoded). Read
`ce.Code` to route 404 instead of substring-matching the message.

---

## How to conform (the consumer recipe)

1. **Call the classified API.** Use `RequestClassified` / `RequestWithRetryClassified`. If you must
   send custom request headers (e.g. audit headers via `RequestWithHeaders`), call
   `natsclient.ClassifyReply(reply)` on the result yourself — it returns `(successBody, nil)` or
   `(nil, *errs.ClassifiedError)`.

2. **Branch on `err`:**
   ```go
   data, err := client.RequestClassified(ctx, subject, req, timeout)
   if err != nil {
       if errors.Is(err, errs.ErrRevisionMismatch) { /* CAS: re-read + retry */ }
       var ce *errs.ClassifiedError
       if errors.As(err, &ce) {
           switch ce.Code {           // graph.ErrorCode* values
           case graph.ErrorCodeEntityNotFound:  /* 404 */
           case graph.ErrorCodeEntityExists:    /* 409 */
           case graph.ErrorCodeOwnerLeaseStale: /* 409/403 */
           case graph.ErrorCodeInvalidRequest:  /* 400 */
           }
       }
       if errs.IsTransient(err) { /* 503 / retry */ }
       return err
   }
   // success: unmarshal data; no Success check
   ```

3. **Degraded success:** still a success (`nil` err). Read `resp.Degraded` + `resp.DegradedReason`
   (the read-back reason that used to live in `Error`). Do **not** retry — the write committed.

4. **Partial batch** (`graph.mutation.triple.add_batch`): a partial commit is a **success body**
   (`nil` err) with `FailedSubjects` populated (`map[string]string`, subject → per-subject error).
   Switch `if !resp.Success` → `if len(resp.FailedSubjects) > 0`. A **whole-batch** failure (nothing
   committed) is a typed error on the err channel.

5. **`ce.Detail` numerics are `float64`.** JSON-decoded numbers in `Detail` (e.g.
   `expected_revision`) are `float64` — `ce.Detail["expected_revision"].(float64)`, never `.(uint64)`.

---

## The closed code set (`ce.Code` values, unchanged constants)

`entity_not_found` · `entity_already_exists` · `revision_mismatch` · `invalid_request` ·
`owner_lease_stale` · `internal`

`revision_mismatch` is special: it is the one **control-flow sentinel** —
`errors.Is(err, errs.ErrRevisionMismatch)` (check it **before** `IsInvalid`, since its class is
`invalid`). Every other code is a plain `ce.Code` discriminator.

---

## What did NOT change

- The error class set `{transient, invalid, fatal}` and the `X-Error-Class` / `X-Error-Code` headers.
- The `RequestClassified` / `RequestWithRetryClassified` / `ClassifyReply` / `RespondError` surface.
- The `graph.ErrorCode*` constant **values** (they're now `ce.Code` values, not body fields).
- Success **data** fields and the `Degraded` semantics (a degraded write is committed).

---

## Detecting non-conformance

After bumping the tag, `go build ./...` fails at every site reading a removed field:
`resp.Success`, `resp.Error`, `resp.ErrorCode`, `QueryResponse.Error`, or calling
`graph.NewQueryError`. Fix each per the recipe above. There is no anonymous-struct decode path that
would silently see a zero value — verify your repo has none before you tag-bump (grep for
`json.Unmarshal` of a mutation/query reply into an inline struct).

**Out of scope (verify, don't assume):** the spatial seam (`graph-index-spatial` /
semconnect `spatial.go`) consumes a non-conforming body and treats any failure as 500. Confirm it
still yields 500 (and does not silently mis-decode the new envelope) after the bump, or fix/file it.

---

## Rollout

semstreams lands PR-D on `main`, runs the e2e gate (incl. the new negative-path assertion in the
`structural` tier — `executeValidateReferentialStub` — that drives `graph.mutation.entity.update` on
a missing entity and asserts a classified `entity_not_found` over the wire; the `structural` tier
runs graph-ingest, unlike `crud-tools`), and tags. Each downstream repo bumps to the tag, applies
this recipe, goes green on its own CI, and merges. The window where semstreams `main` has PR-D but a
consumer hasn't bumped is safe: each repo fetches the breaking tag only when it bumps, and a stale
bump fails to compile.
