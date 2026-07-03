# ADR-065: predicate-index composite-key sharding (fix gh#430 O(N²))

## Status

**Accepted** (2026-07-03, GH #430). Reached after two review rounds: a
3-way adversarial review (architect, go-reviewer, semstreams-reviewer) that
found and fixed the raw-predicate-prefix collision bug, then a final
architect gate-check on this document that returned GO with the
refinements folded into this revision (single-scan `predicateList`, new
bucket name for the cutover, catalog key-identity clarification). Adjacent
correctness gap found during this investigation filed separately as
GH #433 (entity-delete never cleans up
PREDICATE_INDEX/NAME_INDEX/ALIAS_INDEX/CONTEXT_INDEX) — explicitly out of
scope here.

## Decision

Replace `PREDICATE_INDEX`'s one-key-per-predicate monolithic JSON blob
(CAS read-modify-write on every entity write) with **one KV key per
(predicate, entity) membership pair**, keyed on a **hash of the predicate**
rather than the raw predicate string:

```
key   = sha256Hex(predicate) + "." + entityID   // 1 hash token + 6 entity-ID tokens = fixed 7-token key
value = small marker (empty or a debug timestamp)
```

Writes become an unconditional `Put` — no CAS, no read, no JSON parse of
anything proportional to corpus size, and no contention (no two entities
ever write the same key). Reads enumerate membership via
`natsclient.KVStore.KeysByPrefix(sha256Hex(predicate) + ".")`, NATS
JetStream KV's server-side prefix-filtered key listing.

A second, small **PREDICATE_CATALOG** bucket — its own bucket, not a
shared keyspace with membership keys — maps `predicate name → exists`,
updated via the same cheap idempotent `Put` whenever a predicate is first
seen. Its sole job is being the cheap hash→name recovery source for
`graph.index.query.predicateList`: **key = the raw predicate name**
(never hashed), read only via exact `Get` or a full `Keys()` enumeration,
**never** `KeysByPrefix` on this bucket — the exact-string-only discipline
of ADR-056 applies to the catalog too, not just membership keys. Note the
catalog does not, by itself, avoid a full-corpus scan for `predicateList`'s
per-predicate counts — see the single-scan design in the read-path table
below for how that's actually avoided.

The NATS wire contract (`graph.PredicateData`, `graph.PredicateListData`,
`graph.PredicateStatsData`, `graph.CompoundPredicateData`) does not change.
This is an internal storage-layer swap inside `graph-index`; the query API
consumers (gateway, GraphQL, `graph-query/summary.go`, semsource's MCP
tools) do not need to change.

## Context

### The bug (GH #430)

`processor/graph-index/component.go:1287-1358`, `UpdatePredicateIndex`:
PREDICATE_INDEX stores one KV key per predicate, value =
`graph.PredicateIndexEntry{Entities []string, ...}` — the full list of
every entity carrying that predicate. Every entity write CAS-updates this
key (`UpdateWithRetry`, `component.go:1305-1337`): fetch, unmarshal,
dedup-scan, append, marshal, CAS-write. For a predicate carried by ~all
entities (e.g. `code.artifact.type` on 21,265 code entities — the corpus
that surfaced this bug, dogfooding semsource's MCP `code_context` tools
against semstreams beta.124 itself), the blob for that one key grows to
~21k entries; each of the N writes re-parses/re-serializes an O(N) blob ⇒
**O(N²)** total. A 30s CPU profile during ingest showed
`UpdatePredicateIndex` at 63% of total CPU (~37% of that in
`encoding/json`, ~29% in NATS KV round-trip syscalls). Raising `workers`
1→8 barely moved the needle (9/15→10/15 byName coverage on the same
corpus) — this is algorithmic, not a concurrency-tuning problem.

Indexing 22k entities currently takes minutes, not seconds; `byName`
readiness lags for minutes behind `phase: ready`; `graph-embedding` is
starved during ingest (semantic search effectively non-functional while
`PREDICATE_INDEX` churns).

### Why composite keys, not a monolithic-blob variant — codebase precedent

Every *other* multi-membership index in this codebase already uses
per-member composite KV keys + server-side prefix-filtered enumeration,
not a monolithic blob:

- `graph/inference/storage.go:313-343` (`getByIndexPrefix`) — anomaly
  type/status indexes.
- `graph/structural/storage.go:299,341` — pivot-entity / structural
  indexes.
- `graph/clustering/storage.go:206`, `processor/graph-clustering/query.go:304`
  — community detection, keys `<level>.<communityID>`.
- `processor/graph-index-temporal/query.go:68` — temporal index.
- `graph/datamanager/batch_ops.go:92,119` — batch operations.
- Inside `graph-index` itself: `processor/graph-ingest/query.go:150-209`
  (`handleQueryPrefixNATS`) — entity-ID prefix lookup with sort + cursor
  pagination, the direct precedent for prefix-filtered enumeration at this
  layer.
- Even within `graph-index`'s own indexes: `outgoingBucket` (unconditional
  `Put`, keyed by owning entity — bounded by that entity's own out-degree)
  and `incomingBucket` (CAS, keyed by target entity — bounded by that
  entity's in-degree) both avoid PREDICATE_INDEX's mistake by never being
  keyed on a low-cardinality *global* dimension. PREDICATE_INDEX is the
  only index keyed that way, and the only one still using the monolithic
  pattern the rest of the codebase already migrated away from.

`natsclient.KVStore.KeysByPrefix` (`natsclient/kv.go:349-376`) is the
underlying primitive: converts a `prefix` to the NATS wildcard `prefix +
">"`, resolved via a genuinely server-side filtered
`ListKeysFiltered`/bound ephemeral JetStream consumer (verified against
the SDK, not a client-side full-bucket scan).

**Facts vs Requests doctrine check**: predicate membership is current
state ("does entity X currently carry predicate Y"), not a request — KV
Watch is the correct primitive per this codebase's KV Twofer doctrine. A
JetStream Stream would be the wrong primitive here. This decision keeps
membership in KV; it only changes the key layout.

## Rejected alternative: raw predicate string as the KV key prefix

The first draft of this design used `predicate + "." + entityID` directly
— human-readable keys, no hashing. **Three independent adversarial
reviews (architect, go-reviewer, semstreams-reviewer subagents), each
verifying against the live code rather than trusting the draft's
citations, found the same blocking bug and rejected this variant.**

`KeysByPrefix` converts to the NATS wildcard `prefix + ">"`, which matches
on **token position, not token value**. A query for predicate `A` via
`KeysByPrefix(A + ".")` also matches every composite key for any predicate
`B` where `A` is a dot-token-prefix of `B` — because `B`'s keys
(`B.<entityID>`) literally begin with the string `A.` followed by more
tokens (`B`'s remaining tokens, then the entity ID). This silently
corrupts every read path (`queryPredicateEntities`,
`handleQueryPredicateStatsNATS`, and especially the AND/OR set math in
`handleQueryPredicateCompoundNATS`), and none of them re-validate against
`ENTITY_STATES` in the no-value-filter path.

This is not hypothetical. Confirmed prefix/extension pairs already ship in
this codebase's vocabulary: `agent.run` / `agent.run.phase`
(`vocabulary/agentic/predicates.go`), `c360.logistics.sensor` /
`c360.logistics.sensor.document`. Predicate arity is open — 2-token,
3-token, and 4-token predicates all ship today (`domain.type`, `rdf.type`,
`network.traffic.bytes.in`) — so there is no fixed-arity invariant to lean
on to rule this out. `vocabulary.IsValidPredicate`
(`vocabulary/predicates.go:455-472`, requiring exactly 2 dots) exists but
is dead code with zero call sites outside its own tests, and would reject
predicates already in production use if it were ever wired in.

**This codebase already has this exact lesson on record.**
`docs/adr/056-authoritative-semantic-state.md:905-913` (re-review HIGH):
"Predicate sets are EXACT-STRING enumeration only... There is no
prefix/namespace/glob on predicates... Without this rule the *first*
namespace-registration attempt would silently false-negative... or
over-match, and the check would ship feeling-like-coverage while catching
nothing." The raw-prefix design reintroduces exactly this failure class in
a sibling subsystem — a $B$ predicate silently reads as membership of its
prefix $A$, feeling like coverage while corrupting it.

**Fix**: hash the predicate to a fixed-width single token before composing
the key, mirroring `nameIndexKey` in
`processor/graph-index/name_index.go:24-27`
(`sha256(normalizeName(name))`) — the exact same technique this codebase
already uses one file over for the same reason (names, like predicates,
are arbitrary-content strings not safe to use as raw multi-token KV key
material). Fixed-width hex digests cannot dot-token-prefix-collide with
each other, which eliminates the bug structurally rather than by
convention or vocabulary discipline.

**Bonus this rejection surfaced**: hashing also collapses the composite
key to **fixed arity** (1 hash token + the entity ID's fixed 6 tokens = 7
tokens, always), which the raw-predicate variant did not have (predicate
token count varies). Fixed arity means "every predicate a given entity
carries" becomes a single legal NATS filter, `*.<entity's 6 tokens>`
(leading single-token wildcard, literal suffix) — directly useful for
GH #433's entity-delete cleanup, whenever that's implemented, without a
separate reverse-index bucket. The raw-predicate variant could not express
this filter at all (`>` is trailing-only; a leading wildcard over a
variable-length predicate prefix has no legal NATS pattern).

## Other rejected alternatives

- **Shard the blob N ways** (`predicate.0`..`predicate.15`, hash(entityID)
  mod 16): reduces CAS contention by a constant factor but each shard is
  still O(N/16) and grows the same way — O(N²/16), not O(N). Doesn't fix
  the algorithmic problem.
- **Batch/coalesce writes per ingest flush**: fewer, larger CAS cycles,
  but each flush still reads+parses+rewrites an O(N)-sized blob — same
  asymptotic total cost amortized over fewer round trips. Might help
  constants, not the complexity class.
- **CRDT/set value type**: NATS KV has no native mergeable-value support;
  building one is unneeded complexity next to composite keys, which
  sidestep the need for a mergeable value entirely by making the *key*
  the unit of membership.
- **JetStream Stream instead of KV**: wrong primitive per Facts-vs-Requests
  doctrine (see above) — rejected on architectural grounds independent of
  performance.

## Read-path changes

All four NATS handlers in `processor/graph-index/query.go`:

| Handler | Today | Proposed |
|---|---|---|
| `queryPredicateEntities` (→`handleQueryPredicateNATS`) | `predicateBucket.Get(predicate)` → unmarshal `.Entities` | `predicateBucket.KeysByPrefix(sha256Hex(predicate)+".")` → strip prefix per key |
| `handleQueryPredicateListNATS` | `Keys()` (all predicate keys) then `Get` each | **One** `KeysByPrefix("")` (or `Keys()`) over the whole membership bucket, group by first token (the hash) to get counts; `Keys()` on `PREDICATE_CATALOG` + forward-hash each name to join hash→name. **Not** a per-predicate `KeysByPrefix` fan-out — see below. |
| `handleQueryPredicateStatsNATS` | `Get(predicate)` → count + slice sample | `KeysByPrefix` → `len(keys)` for count, sorted-first-N for sample |
| `handleQueryPredicateCompoundNATS` | `Get` per predicate → build set from `.Entities` | `KeysByPrefix` per predicate → build set from stripped keys |

**`handleQueryPredicateListNATS` must not be implemented as N separate
`KeysByPrefix` calls, one per catalog entry.** `KeysByPrefix` is a bound
ephemeral-JetStream-consumer operation, not a cheap `Get` — serial fan-out
over a growing, never-pruned catalog risks the handler's 5s timeout at
scale, and this handler is now load-bearing for the mandatory
`CountVirtualEdges` e2e migration (see Breaking-change scope below), so
its cost is no longer just an admin-query concern. Implement as **one**
grouped enumeration of the whole membership bucket (single `KeysByPrefix`
call with an empty/root prefix, or `Keys()`), bucket the returned keys by
their first token (the hash), then join names via the catalog. This is one
expensive scan plus one cheap catalog enumeration, with no per-predicate
round-trip fan-out at all. If a fully grouped scan turns out to be
impractical during implementation, the fallback is bounded-concurrency
fan-out (precedent: `defaultMaxConcurrent = 10`, `graph-ingest/query.go:20`)
with a longer timeout matching `handleQueryPrefixNATS`'s existing 10s
precedent (`graph-ingest/query.go:153`) — but the single-scan design is
the target, not the fallback. Load-test either implementation against a
corpus sized like GH #430's (21k entities) before merge.

`graph.PredicateIndexEntry` becomes fully dead code once these four
handlers and `UpdatePredicateIndex` no longer construct/consume it.
Before deleting it, grep the whole repo for `PredicateIndexEntry` (not
just the four handlers and `component.go` cited above) to confirm nothing
else constructs or consumes it — the wire response types
(`PredicateData`, `PredicateListData`, `PredicateStatsData`,
`CompoundPredicateData`) are a separate set of types and are unaffected;
only the stored/CAS'd blob type dies.

## Migration

PREDICATE_INDEX is a derived index, not source-of-truth data:
`watchEntityStates` (`component.go:687-748`) replays full current
`ENTITY_STATES` before signaling "initial sync complete" (the KV Twofer's
documented restart semantics), and index writes are idempotent for
additions (append-with-dedup; not idempotent for *removals*, which is the
pre-existing gap GH #433 tracks, unrelated to this change).

This makes the storage-format change a **bucket cutover, not a data
migration**: ship under the new bucket name **`PREDICATE_INDEX_V2`**
(decided; see rationale below) and let the watcher naturally repopulate it
from `ENTITY_STATES` on next boot, then retire the old `PREDICATE_INDEX`
bucket. No custom migration code, no dual-read transitional logic, no
in-place format detection. This is internal derived state with no
external mutation compatibility concern (pre-1.0, semstreams owns every
writer of this bucket).

**Decided: new bucket name, not wipe-in-place.** The old format is never
read again once `CountVirtualEdges` and the two raw-blob unit tests move
onto the query API/hashed format in this same PR, so a new bucket name has
essentially zero transitional cost — point the code at
`PREDICATE_INDEX_V2`, abandon the old bucket, watcher repopulates clean.
Wipe-in-place needs a reliable purge step; a partial/failed purge leaves
orphaned old-format keys mixed into the same bucket as new-format keys.
Those orphans would be mostly harmless on their own (a raw key like
`code.artifact.type` can never match `KeysByPrefix(64-hex + ".")`), but
there's no reason to accept even that residual mess when a new bucket name
costs nothing extra. `PREDICATE_CATALOG` is provisioned the same way, at
the same site — see `createOutputBuckets`/`assignBucket`
(`processor/graph-index/component.go:606-660`), where `PREDICATE_INDEX` is
currently created; both new buckets must be created eagerly there,
alongside (not instead of, until the old bucket is fully retired) the
existing bucket wiring.

## Breaking-change scope (this is NOT purely internal)

The NATS query API is stable, but the raw bucket format is not, and three
things in this repo read the raw format directly, discovered during
review:

- `test/e2e/client/nats.go:849-914` (`CountVirtualEdges`) opens
  `PREDICATE_INDEX` directly via `js.KeyValue`, iterates `Keys()`, and
  unmarshals each value as the old `{"entities":[...]}` blob. Under the
  new format this fails to unmarshal on every key, is swallowed by a
  `continue`, and the function silently returns `Total: 0` forever. Its
  only caller, `test/e2e/scenarios/tiered_semantic.go:534-588`
  (`executeValidateVirtualEdges`, wired into `task e2e:semantic`), treats
  a zero count as a soft warning, not a failure — so this tier would go
  green while validating nothing. **Must be migrated onto the query API
  in this PR**, and the e2e stage should be tightened to hard-fail rather
  than warn on an unparseable/zero result going forward. Note this
  migration is not just "swap the transport": `CountVirtualEdges`'s band
  logic (lines 905-909, parsing `inferred.semantic.<band>` out of the raw
  predicate name) depends on the *human-readable* predicate name, which
  under hashed keys is only recoverable via `PREDICATE_CATALOG` — route
  this through `graph.index.query.predicateList` (which does the
  hash→name join) rather than trying to parse a band out of a raw key.
- `processor/graph-index/attack_test.go:444-458` and
  `processor/graph-index/integration_test.go:139-154` read
  `predicateBucket.Get`/unmarshal the raw blob shape directly. Rewrite in
  this PR.

Per this project's hard rule on breaking changes, this qualifies:
`task e2e:structural` and `task e2e:semantic` must be green **before**
this change merges, with the e2e-client migration in scope for this PR,
not deferred.

## Consequences

### Positive

- Collapses ingest cost for `UpdatePredicateIndex` from O(N²) to O(N):
  writes become O(1), unconditional, contention-free.
- Matches the established idiom used by every sibling index in this
  codebase — not a novel pattern, finishes a migration already ~90% done
  elsewhere.
- Fixed-arity keys make GH #433's entity-delete cleanup tractable for
  PREDICATE_INDEX specifically, whenever that's tackled, without a
  separate reverse-index bucket.
- No NATS wire-contract change for any consumer going through the query
  API (gateway, GraphQL, `graph-query/summary.go`, semsource).

### Negative / cost

- Real breaking change to the bucket's on-disk format — requires the
  e2e-client migration and green breaking-change e2e gate described above,
  not a drop-in refactor.
- Keys become opaque (hashed); debugging requires the catalog bucket or
  recomputing the hash. Acceptable — mirrors the existing, already-shipped
  `NAME_INDEX` tradeoff.
- `PREDICATE_CATALOG` entries are never pruned (predicate vocabulary
  treated as a small, effectively monotonic taxonomy, unlike unbounded
  entity cardinality) — accepted tradeoff, flagged here rather than
  silently assumed.
- Full reindex-on-deploy: acceptable because the index already pays this
  cost on every boot from an empty bucket; this makes every future boot
  pay it once via replay rather than never triggering a rebuild at all.

### Risks

- `handleQueryPredicateListNATS` cost is resolved by the single-scan
  design in the read-path table above (not a per-catalog-entry fan-out) —
  see that section for the mandatory implementation shape and the load-test
  requirement against a GH #430-sized (21k-entity) corpus.
- Rebuild window after a cutover has no readiness signal for predicate
  queries specifically (unlike `NAME_INDEX`, which got the GH #397
  `graph.index.query.status` envelope). For a 21k-entity corpus the window
  is short but non-zero; a consumer polling immediately post-boot could
  observe transient under-counts. Deferred: extending the GH #397 status
  envelope to cover predicate-index build completion is a reasonable
  follow-up, not required for this fix (predicate queries are already
  best-effort/eventually-consistent post-boot today).
- Unconditional `Put` (no CAS) on this bucket, adjacent in the same file to
  several `UpdateWithRetry` CAS calls (outgoing/incoming/name/alias), is a
  legible "someone changes this back to CAS later without understanding
  why" footgun. Mitigate with an explicit code comment stating the key-
  uniqueness invariant (no two entities ever write the same composite key)
  that makes CAS unnecessary here specifically.
- The "fixed 7-token key" framing (and the GH #433 `*.<entityID>` bonus
  filter it enables) assumes entity IDs are always exactly 6 dot-tokens
  with no embedded dots per token — true today (`entityIDRegex`,
  `graph/datamanager/manager.go:35-36`) and this design's read/write paths
  don't actually depend on that arity (they strip the fixed hash prefix
  and take everything after as the entity ID, whatever its length), so
  this assumption affects only the advertised GH #433 bonus, not this
  ADR's correctness. Documented here so a future entity-ID format change
  doesn't silently invalidate that bonus without anyone noticing.

## Open questions

Resolved during the final architect gate-check (folded into this
revision): bucket naming (`PREDICATE_INDEX_V2`, new bucket — see
Migration), and `handleQueryPredicateListNATS`'s implementation shape
(single grouped scan, not per-predicate fan-out — see Read-path changes).

Still open, safe to resolve during implementation:

- Marker value contents: bare empty value, or a debug timestamp? Leaning
  empty/fixed-constant by construction (see the footgun-comment mitigation
  above) — a timestamp invites a future reader to treat it as "most
  recent," which isn't a guarantee this design provides (worker-pool
  completion order is not ordered across entities).
- Whether to extend the GH #397 readiness envelope to predicate-index
  build completion now or defer — leaning defer, flagged as a risk above
  rather than blocking this fix.

## Related decisions

- **ADR-056** — the exact-string-only predicate doctrine this design's
  hashed-key fix is required to honor; the rejected raw-prefix variant
  would have reintroduced the failure class ADR-056 already named HIGH in
  a sibling subsystem.
- **GH #376 / `NAME_INDEX`** (`processor/graph-index/name_index.go`) — the
  direct precedent for hashing arbitrary-content strings into single-token
  KV keys, reused here for predicates. Note `NAME_INDEX` itself has the
  identical CAS-blob-per-hash-key shape as PREDICATE_INDEX pre-fix,
  currently masked by better-behaved cardinality (names are rarely shared
  by thousands of entities the way a common predicate is) — not addressed
  by this ADR, flagged as a one-line callout for a future fix if it ever
  becomes hot.
- **GH #397** — the index-readiness envelope (`graph.index.query.status`)
  this ADR's risk section suggests extending to predicate-index build
  completion, deferred rather than included here.
- **GH #433** — entity-delete index cleanup, filed separately; this
  design's fixed-arity key shape makes that fix cheaper whenever it's
  tackled, but implementing it is explicitly out of scope here.

## References

- GH #430 — the filing, evidence profile, and the issue's own suggested
  fix direction (which this ADR follows: "append-only per-entity
  sub-keys... presence marker").
- `processor/graph-index/component.go:1287-1358` — `UpdatePredicateIndex`,
  the function being redesigned.
- `processor/graph-index/query.go:204-489` — the four read handlers being
  redesigned.
- `natsclient/kv.go:349-397` — `KeysByPrefix`/`FilteredKeys`, the
  server-side prefix-filtering primitive this design is built on.
- `processor/graph-index/name_index.go:24-27` — `nameIndexKey`, the
  hash-to-single-token precedent this design mirrors.
- `docs/adr/056-authoritative-semantic-state.md:905-913` — the exact-string
  predicate doctrine the rejected raw-prefix variant would have violated.
- `test/e2e/client/nats.go:849-914`, `test/e2e/scenarios/tiered_semantic.go:534-588`
  — the raw-bucket e2e reader that makes this a breaking change requiring
  an in-scope migration and a hard-fail (not warn) gate.
