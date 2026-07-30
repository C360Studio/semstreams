# Design — add-lane triple deduplication

## D1. Placement: one pure helper, called from inside both CAS closures

`Component.AddTriple` (closure `component.go:2995`, append `:3012`) and `Component.AddTriples`
(closure `:3124`, append `:3140`) share no code. Their public signatures differ — `AddTriple`
returns `error`, `AddTriples` returns `(writtenCount, failedSubjects, err)` — so unifying the two
methods in this change risks the response shape for no benefit. Instead: one I/O-free helper that
takes stored triples plus incoming triples and returns the survivors, invoked from inside each
closure before the append, plus one shared skip sentinel.

**Atomicity.** The closure receives `current` bytes read at revision R (`natsclient/kv.go:281`)
and the write is `kv.Update(..., revision)` CAS'd on R (`:344`). Two concurrent identical
requests: one commits R→R+1; the loser's `Update` returns a conflict, which `kv.go:348` returns
for retry, so the closure re-runs against R+1, now observes the tuple stored, and suppresses.
This is what satisfies the concurrent-identical and late-commit scenarios.

**Rejected — a pre-read outside `UpdateWithRetry`.** Reintroduces the TOCTOU window that
gh#697's own acceptance case exists to catch. This is also the precise misreading available in
the triage's phrase "short-circuit before the CAS write": that means before the revision-checked
`Update` *inside* the read-modify-write loop, not before entering the loop.

**Rejected — a KV-layer "skip if bytes unchanged" in `UpdateWithRetry`.** Silently changes
semantics for every caller of a shared primitive, including callers that deliberately rewrite
identical bytes.

**Rejected — an exists-check inside `hierarchy.go`.** Fixes one consumer and leaves the class
open for every replaying inference or projection consumer. Explicitly rejected by the gh#713
triage.

## D2. Identity: six fields, from one shared implementation

Identity is subject, predicate, object, datatype, source, context. `message.Triple` also carries
`Timestamp`, `Confidence`, and `ExpiresAt` (`message/triple.go:37-93`); all three are excluded.

The governing reason is client/server parity, not gh#713. `sameAppendTuple`
(`pkg/projection/mutation_client.go:1324`) is **merged code** already matching on exactly these
six, with the nine-field `sameFullTriple` (`:1333`) reserved for replace/create verification. A
server key that differs from the client's is a drift class. The two must come from one
implementation, not two that agree today.

Per-field consequences, each accepted deliberately:

- **`Timestamp`** is the consequential exclusion. Producers stamp `time.Now()`, so including it
  would mean dedup never fires for anyone — the contract would be decorative. Effect: stored
  `Timestamp` becomes first-assertion time. No consumer in `graph/` or `graph-ingest` reads triple
  `Timestamp` for freshness; the only reads are envelope metadata (`component.go:1476`).
- **`Confidence`** is a float, so equality is fragile. Excluding it means a confidence-only
  refresh on the add lane becomes a no-op. This is not a regression: today it appends a second,
  contradictory copy, and the only in-repo read resolving by confidence takes the first triple
  with `Confidence > 0` (`graph/clustering/summarizer.go:375`) — i.e. today's behavior is already
  nondeterministic. Producers needing confidence to be identity-bearing already encode it into the
  predicate (`graph/inference/detector.go:479`). Value changes belong on a replace verb.
- **`ExpiresAt`** exclusion means a re-assert-to-extend becomes a no-op. Currently harmless:
  `Triple.IsExpired()` has zero non-test consumers repo-wide, i.e. triple TTL is unenforced.
  **Forward hazard** — if TTL enforcement lands, either revisit the key or move TTL refresh to a
  replace lane. Recorded here so it is not rediscovered.

**Rejected — refresh-in-place on a suppression hit.** A write is a write: it advances a revision
and defeats the zero-write requirement.

## D3. Key construction

`Object` is `any` (`message/triple.go:53`), so canonicalize the way `objectsEqual`
(`mutation_client.go:1355`) does — `reflect.DeepEqual`, falling back to JSON bytes — so `int(85)`
and `float64(85)` match rather than splitting the keyspace.

Use a **key-based set**, O(n+k), not pairwise comparison, O(n·k) — hierarchy container entities
accumulate a `contains` edge per child, so the quadratic form degrades exactly where this fires
most.

Build the key **NUL-separated or length-prefixed**, never delimiter-joined. Predicates, sources,
and contexts are arbitrary strings; a dot or pipe join reopens the live raw-key-collision class
(gh#741, currently causing silent data loss in a shipped config).

## D4. Zero-write: sentinel, and where it is recovered

Return a package sentinel from the closure before building the candidate, following the existing
`errNoOpRemove` precedent (`component.go:3202`), whose comment states the hazard directly:
"`return current, nil` would be an identity rewrite (revision bump + watcher re-fire), not a
skip" (`:3257`), recovered at `:3282`.

The wrap chain is safe for `errors.Is`: `kv.go:305` wraps closure errors in
`retry.NonRetryable(fmt.Errorf("...: %w", err))` and `NonRetryableError.Unwrap`
(`pkg/retry/retry.go:28`) preserves unwrapping.

Recovery points, and what each must precede:

- `AddTriple` — recover at `:3023`, **before** `atomic.AddInt64(&c.errors, 1)`.
- `AddTriples` — recover inside the `if casErr != nil` block at `:3151`, **before** the
  `failedSubjects`, `c.errors`, and `allAbsences` handling. `allAbsences` (`:3158`) matters most:
  a suppression counted as an absence misclassifies a mixed batch.

Skipping the write also skips `MarshalEntityState` and its full-pass revalidation, so the
duplicate path gets *cheaper*, not more expensive (see D6).

**Accepted minor consequence:** a duplicate-only write no longer clears a stale poison-inventory
entry. The inventory is observability-only by design — "no read or write path consults it for a
decision" (`component.go:604`) — and other clear paths remain.

## D5. Client-side repair (same PR)

`appendFactsPresent` (`mutation_client.go:1272`) verifies by consuming matches from a multiset:
two identical evidence triples require two stored copies. Under dedup only one exists →
`found < 0` → `MutationInternal` + fatal → `CommitNotCommitted` (`:1034`). Fix:

1. `canonicalizeAppend` (`:1157`) collapses duplicate evidence, preserving first-input order.
2. The presence check becomes set-based rather than multiset-consuming.

**Rejected — reject an internally-duplicated batch as `invalid_request`.** Defensible, but hostile
to sister callers assembling evidence lists innocently.

Note `mutation_client.go:1010` already treats `WrittenCount != expectedCount` as an *anomaly*, not
a failure, routing to `verifyAnomalousAppend` (`:1020`) which read-backs and returns
`CommitVerified`; a merged test already pins `WrittenCount: 0` → `CommitVerified`
(`mutation_client_test.go:2054`). So the client degrades correctly today at the cost of one extra
`ReadAuthoritative` per deduped append — which is why the additive `Deduplicated` field is worth
carrying: it lets the client short-circuit without the round-trip.

## D6. Cost

Net-negative on the duplicate path, bounded on the miss path. The closure already pays O(stored
triples) twice — `UnmarshalEntityStateTrusted` (`:3001`/`:3130`) decodes every triple, and
`MarshalEntityState` re-encodes and revalidates every triple
(`graph/entity_predicate_contract.go:145`). Seeding a hash set is O(n) over data already decoded,
with no new I/O; on a fully-duplicate write it *saves* the marshal, the validation pass, and the
KV round-trip.

Bound: entity size is capped at 1 MB (`natsclient/kv.go:28`, enforced `:312`). There is no
`MaxTriples` cap, so the set is bounded only by that.

Not fixed here: hierarchy still performs an O(N) `ListWithPrefix` (`hierarchy.go:281`) plus three
container existence reads per re-registration. Separate issue.

## D7. What this does not cover

`MergeEntity` (predicate-level replacement via `graph/helpers.go:108`), `createEntity` /
`CreateEntityStrict` (whole-candidate write — a candidate carrying internal duplicates still
commits them), `handleEntityUpdateWithTriples` and `..._CAS` (replace verbs), and `pkg/lifecycle`
(never uses the add lane). `hierarchy.OnEntityCreated` (`hierarchy.go:239`) has no production
callers and is legacy.

**ADR-056 forward note.** ADR-056 mandates a three-field `(s,p,o)` dedupe-merge and an
"exactly-one" gate for the `PENDING_EDGES` drain. That drain is unimplemented
(`pkg/ownership/bootstrap.go:36`, `component.go:1802`, `:1871`), so there is no contradiction
today. The six-field key satisfies the exactly-one gate **iff** the drain re-applies the buffered
triple verbatim rather than recomputing `source`/`context`. This change also removes the reason
for ADR-056's "the drain MUST NOT use `AddTriples`".

## D8. Implementation constraint

`revive.toml` enforces a 50-statement function cap and both CAS closures are already long
(`component.go:2619` records this). The helper must be a package-level function, not inlined.
