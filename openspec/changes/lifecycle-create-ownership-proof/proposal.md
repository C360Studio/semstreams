# Lifecycle create: prove ownership from the mutation response, never reconstruct it

## Why

**A losing concurrent create was reported to its caller as a success.** Measured on
`origin/main` (gh#861): eight goroutines creating the same lifecycle instance produced
**two** winners, reproducibly, under `go test -race -count=40 -cpu 2`.

The mechanism is not a subtle interleaving — it is an identity that is not unique.
`Manager.Create` recovered a "lost reply" by re-reading the entity and comparing the audit
stamp it was about to write, `now.Format(time.RFC3339Nano)`, on the stated premise that
"`now` is generated in this call, so an entity carrying it was written by this request and
nothing else". Measured on the development host, that premise is false:

| measurement | result |
|---|---|
| distinct values from 1000 sequential `time.Now()` | **155–251** |
| rounds where ≥2 of 8 goroutines shared one RFC3339Nano stamp | **200 / 200** |

Wall-clock granularity is microsecond-scale, so the nanosecond format is cosmetic. Two
concurrent creates build **byte-identical** initial deltas; the loser re-reads, matches the
**winner's** stamp, and returns a degraded success for a birth it did not make.

**The decisive argument is conformance, not the timestamp.** `openspec/specs/lifecycle/spec.md`
already requires that "the answer returned to the caller MUST be derived from the causal
mutation response rather than from a separate read issued afterwards", and names this exact
failure mode: a separate read "can also observe another writer's later state". The
reconciliation *is* that separate read. Deleting it restores conformance; it does not
regress a contract.

**Deleting it alone would be wrong, because the framework manufactures the ambiguity it
then guessed about** (all verified in code):

| | |
|---|---|
| lifecycle create per-attempt deadline | **5 s** (`pkg/lifecycle/manager.go`) |
| graph-ingest handler deadline | **30 s** (`natsclient.DefaultRequestHandlerTimeout`) |
| retry-loop continue condition | **any** non-nil error, including a plain timeout (`natsclient.requestMsgWithRetry`) |
| retries | 10, ~13 s cumulative |

The client gives up six times sooner than the handler, then re-sends a **non-idempotent
create** against a handler that may still be executing the first one — and the second
delivery answers `entity_already_exists` for a birth this same request made. That is
structural, not exotic: it fires on cold start, KV contention, and load. The framework's
better-behaved sibling `pkg/projection/mutation_client.go` already retries **only**
`natsclient.IsNoResponders`, and the emitter's own doc comment concedes that the gh#170 race
it exists for is "no responders **before** graph-ingest receives anything" — the
provably-pre-commit class, the only class a create retry is ever justified for.

## What Changes

- **Delete the ownership reconstruction.** `Manager.committedByThisRequest` and its call site
  are removed; an `ErrAlreadyExists` from the emitter is reported as a conflict. No nonce, no
  new correlation field, no ADR — this increment adds no surface.
- **Narrow `create`'s retry to the provably-pre-commit class.** Only
  `natsclient.IsNoResponders` re-sends; the ~13 s gh#170 cold-start budget is preserved for
  it. Every other transport failure is returned as an unknown outcome without a re-send.
- **`update` and `delete` keep the wider retry, deliberately.** `update`'s CAS surfaces a
  duplicate delivery as `revision_mismatch` into `Transition`'s re-read loop, and `delete` is
  idempotent at the handler. `Transition`'s cold-start protection genuinely depends on
  retrying a sub-case that presents as a timeout — narrowing there would remove protection,
  not a defect.

### The one behavior change on a public surface

`POST /lifecycle/workflows/{workflow}` returns **409** where it previously returned **201
with `degraded: true` and no instance body** — but only in the case where the birth was
*another writer's*. The 409 matches the route's own documented create-or-fail contract and
the existing `ErrAlreadyExists → StatusConflict` mapping; the 201-degraded lane still serves
the genuine degraded commit (write landed, read-back failed), which is what that lane was
built for.

`agentic/agentrun.Mint` improves: a concurrent loser now takes its idempotent
`ErrAlreadyExists → Get` branch and returns the winner's read-back run, instead of returning
the un-read-back struct it submitted.

## Deferred, deliberately

Three consumers want request-scoped idempotency on the graph mutation seam — this issue's
residual (a create whose single delivery times out has a genuinely unknown outcome), gh#689,
and gh#807. That is one primitive with cross-repo wire semantics, i.e. ADR-shaped; gh#807
enumerates four unanswered questions about its shape. A shape with four open questions is too
unstable to document, therefore too unstable to export, therefore too unstable to build into
a bug fix. Filed as engine work with its consumers named: **gh#869**.

Three further follow-ups are filed rather than folded in: **gh#870** (the attach path carries
the mirror-image bug — a real fence that errs conservative and invents a 409 for a birth this
request just made), **gh#871** (the two other content-equality-as-ownership sites, one of them
spec-blessed), and **gh#872** (the e2e coverage gap — no tier fires concurrent lifecycle
creates, and the 409 above is a live behavior change no tier observes).

This change **does not unblock gh#689**. `pkg/projection/mutation_client.go` returns
`CommitVerified` for two concurrent identical claimers and
`openspec/specs/projection-mutation-client/spec.md` blesses that — defensible for a
convergence API, indefensible for an election. Changing it is a separate ruling on a spec'd
surface.

## Impact

- **Affected specs:** `lifecycle` (modified requirement on committed births; new requirement
  on when a creation may be re-sent).
- **Affected code:** `pkg/lifecycle/manager.go`, `pkg/lifecycle/graph_emit.go`,
  `agentic/agentrun/agentrun.go` (comment only).
- **Not affected:** `pkg/projection`, `graph/`, `natsclient`'s exported surface,
  `processor/graph-ingest`. No new exported symbol, config key, or adopter-facing knob.
- **Closes:** gh#861. **Repudiates:** gh#178's proposed mechanism (the audit-triple re-read),
  which is what shipped and what this removes.
