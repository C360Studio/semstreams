# ADR-065: predicate-index composite-key sharding (fix gh#430 O(N²))

## Status

**Accepted** (2026-07-03, GH #430). Reached after three review rounds: a
3-way adversarial review (architect, go-reviewer, semstreams-reviewer) that
found and fixed the raw-predicate-prefix collision bug; a final architect
gate-check on this document that returned GO with refinements (single-scan
`predicateList`, catalog key-identity clarification); and a
during-implementation correction (bucket-name decision reversed back to
in-place cutover after discovering ~9 operator-facing configs hardcode the
bucket name, plus explicit sem*-team migration guidance and a wired-up
namespace-query capability, both prompted directly by Coby). Adjacent
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
seen. Its keys are **the raw, unhashed predicate name** — deliberately,
for two reasons: (1) it's the hash→name recovery source for
`graph.index.query.predicateList`'s unfiltered listing, and (2) unlike the
membership bucket, `KeysByPrefix` on *this* bucket is safe — it can only
return a superset of predicate names sharing a dotted namespace, never
corrupt entity-membership correctness, because it carries no membership
data. `graph.index.query.predicateList` gains an optional `prefix`
request field that uses exactly this: a genuine, deliberate,
namespace-style predicate query (NATS-KV-wildcard "SQL-`LIKE`-prefix"
semantics, intentionally preserved here rather than given up wholesale —
see Wildcard-semantics tradeoff below). Note the catalog does not, by
itself, avoid a full-corpus scan for the *unfiltered* `predicateList`
call's per-predicate counts — see the single-scan design in the read-path
table below for how that's actually avoided.

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

## Wildcard-semantics tradeoff (does hashing give up the dotted-key pattern's value?)

Worth stating explicitly, since it's easy to read this ADR as "give up on
NATS KV's dotted-subject wildcard querying" more broadly than it actually
does. This codebase leans hard on that pattern elsewhere — entity-ID
prefix queries (`graph-ingest`'s `handleQueryPrefixNATS`), community
levels, structural pivot points, anomaly types/status — and it is
genuinely valuable there: server-side hierarchical filtering with
SQL-`LIKE`-prefix-style semantics, "for free" from NATS subject matching.
Hashing the predicate trades that away for `PREDICATE_INDEX` membership
keys specifically. What's actually given up, and what isn't:

**Given up**: server-side wildcard/prefix filtering on the *predicate*
axis of `PREDICATE_INDEX` membership keys. You cannot ask NATS KV
"give me every membership key under the `inferred.semantic.*` namespace"
directly against the membership bucket anymore — the predicate is now an
opaque hash there.

**Not given up**:
- The *entity-ID* axis of every key in this codebase, including this one,
  is untouched — entity IDs stay raw, dotted, 6-part, and every other
  index that wildcards on entity ID (not predicate) is completely
  unaffected by this change. This is a scoped exception to one axis of
  one index, not a retreat from the dotted-key pattern generally.
- This codebase's own doctrine already forbids relying on predicate-axis
  prefix/namespace semantics for anything correctness-sensitive:
  `docs/adr/056-authoritative-semantic-state.md:905-913`, "Predicate sets
  are EXACT-STRING enumeration only... no prefix/namespace/glob on
  predicates." Nothing sanctioned is lost — what's removed is an
  *accidental*, previously-untested capability (the raw-prefix draft of
  this design) that turned out to be actively dangerous the moment two
  predicates happened to nest (§ "Rejected alternative" above). There was
  no working namespace-query capability on `PREDICATE_INDEX` before this
  ADR to preserve — today's code requires an *exact* predicate string via
  `Get`, full stop.
- A safe, deliberate path to namespace-style predicate queries still
  exists if a real use case wants one: **`PREDICATE_CATALOG` keys are the
  raw, unhashed predicate name** (see Decision above) specifically so this
  door stays open. Prefix-matching there cannot corrupt entity-membership
  correctness the way it could on the membership bucket, because the
  catalog carries no membership data — a `KeysByPrefix` there can only
  return a superset of predicate *names* that share a dotted namespace,
  which is exactly the intended semantics of a deliberate namespace query
  (a caller who wants "all predicates under `inferred.semantic.`" and
  gets `inferred.semantic.high` *and* a hypothetical
  `inferred.semantic.high.confidence` is seeing correct namespace
  inclusion, not corruption). The distinction that made the membership-key
  prefix design unsafe was call-site *intent* — every existing membership
  read wants an exact single-predicate match, not a namespace scan — not
  something inherent to prefix-matching on dotted predicate strings in
  general. **This ADR wires up catalog prefix-querying** (not deferred —
  see the `prefix` field on `graph.index.query.predicateList` in the
  Read-path changes table below), specifically because `CountVirtualEdges`'s
  migration is a concrete, immediate, real caller: it wants exactly "every
  `inferred.semantic.*` predicate," which is a namespace query, not N
  exact lookups. This preserves a genuine, useful slice of the dotted-key
  pattern's SQL-`LIKE`-prefix value rather than only preserving it in
  the abstract.

## Read-path changes

All four NATS handlers in `processor/graph-index/query.go`:

| Handler | Today | Proposed |
|---|---|---|
| `queryPredicateEntities` (→`handleQueryPredicateNATS`) | `predicateBucket.Get(predicate)` → unmarshal `.Entities` | `predicateBucket.KeysByPrefix(sha256Hex(predicate)+".")` → strip prefix per key |
| `handleQueryPredicateListNATS` | `Keys()` (all predicate keys) then `Get` each | Request gains an optional `prefix` field (new — wires up the namespace-query door left open in the Wildcard-semantics tradeoff section, requested explicitly rather than left theoretical). Unfiltered (no `prefix`): **one** `Keys()` over the whole membership bucket, group by first token (the hash) to get counts; `Keys()` on `PREDICATE_CATALOG` + forward-hash each name to join hash→name — **not** a per-predicate fan-out, see below. Filtered (`prefix` set): `PREDICATE_CATALOG.KeysByPrefix(prefix)` for the (now namespace-bounded, small) matching names, then a per-name `KeysByPrefix` against the membership bucket for each — a fan-out is fine here because the namespace filter already bounds the result set, unlike the unfiltered case. |
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

This makes the storage-format change a **bucket cutover**: the code that
reads/writes `PREDICATE_INDEX` switches from the blob format to the
hashed composite-key format, and the watcher naturally repopulates
correctly-formatted entries from `ENTITY_STATES` on next boot. No custom
migration code, no dual-read transitional logic.

**Corrected: same bucket name (`PREDICATE_INDEX`), not a rename —
reversing this ADR's earlier "new bucket name" decision.** That earlier
decision assumed the bucket name was purely internal Go plumbing
(referenced only symbolically via `graph.BucketPredicateIndex`) with "zero
external mutation compatibility concern." That assumption was wrong: a
repo-wide grep for the literal string `PREDICATE_INDEX` (not just Go
source) turned up **~9 operator-facing deployment/reference configs**
that hardcode `"subject": "PREDICATE_INDEX"` as an output port
independently of the Go constant —
`configs/hello-world.json:162`, `configs/semantic.json:532`,
`configs/graph-backend.json:65`, `configs/structural.json`,
`configs/statistical.json`, `configs/e2e-structural.json`,
`configs/semantic-basic.json`, and two files under
`configs/examples/`. Renaming the bucket via the constant would make
`Config.Validate()`'s `requiredBuckets` check
(`processor/graph-index/component.go:59-64`) fail for every one of these
configs unless each is updated in lockstep — turning an "internal
storage swap" into an operator-facing config migration across most of
this repo's reference deployments, which is a materially larger and
different kind of breaking change than the rest of this ADR scopes for.

Reusing the same bucket name avoids that entirely: `Ports.Outputs`
subject, `requiredBuckets`, and `assignBucket`'s switch
(`processor/graph-index/component.go:648-660`) all stay keyed on the
unchanged string `"PREDICATE_INDEX"`; none of the ~9 configs need to
change.

**No explicit purge of old-format keys is required for correctness.**
Old blob-format keys (e.g. a literal key `code.artifact.type` holding the
old JSON blob) are **structurally inert** under the new read paths: every
new read either does `KeysByPrefix(sha256Hex(predicate) + ".")` — a
64-hex-char literal prefix an old plain-predicate key can never match —
or, in `handleQueryPredicateListNATS`'s grouped scan, splits each key on
its first `.` and only credits counts to hashes that a `PREDICATE_CATALOG`
name forward-hashes to; an old key's first token (e.g. `"code"`) is never
a valid 64-hex-char hash and so is silently dropped from the join, exactly
like today's code already silently drops unparseable entries. Old-format
keys become inert storage residue, not a correctness hazard — cosmetic
cleanup (an optional explicit wipe-then-rebuild, or a TTL) is a reasonable
follow-up but not required to ship this fix.

`PREDICATE_CATALOG` is a genuinely new bucket (no existing config
declares it, so no lockstep-update problem) and is provisioned inline in
`createOutputBuckets`, the same way `NAME_INDEX`/`CONTEXT_INDEX` are today
— not a declared output port, created eagerly before the entity watcher
starts (`processor/graph-index/component.go:606-660`).

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

### Migration guidance for sem* teams

Project status note (Coby, 2026-07-03): semstreams is pre-1.0/greenfield
with no external production users, and C360 controls every sem* consumer
repo directly — breaking changes in beta are acceptable when well
justified, with direct migration guidance to the sem* teams rather than a
compatibility shim. This is that guidance for this specific change:

- **No code changes required for any sem* consumer.** Every documented
  consumer (semsource, semconnect, semteams) reaches `PREDICATE_INDEX`
  exclusively through the NATS query API
  (`graph.index.query.predicate*`), which does not change shape. This was
  explicitly checked during review (semstreams-reviewer's pass) —
  no evidence of a sister repo reading the raw bucket, only this repo's
  own e2e test harness did that, and it's fixed in this same PR.
- **No deployment-config changes required.** The bucket name is unchanged
  (`PREDICATE_INDEX`); the ~9 configs in this repo that declare it as an
  output port subject need no edits, and neither does any sem* team's own
  copy/derivative of a graph-index component config, if one exists.
- **What silently changes**: any *ad hoc* tooling outside this repo that
  reads the `PREDICATE_INDEX` bucket directly (NATS CLI inspection
  scripts, a debugging notebook, etc.) rather than through the query API
  will see opaque hashed keys instead of readable predicate-named keys
  after this deploys. If any sem* team has such tooling, it needs to move
  to `graph.index.query.predicate*` (which now also supports namespace
  queries via the new `prefix` field on `predicateList` — see Decision).
- **Operationally**: after this version deploys, `PREDICATE_INDEX` rebuilds
  from `ENTITY_STATES` via the existing KV-watch replay on next boot (same
  mechanism as any restart) — no manual migration step. Old blob-format
  keys are inert and can be left in place; operators who want a clean
  bucket for storage hygiene may optionally delete-and-let-rebuild, but
  this is not required for correctness.

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

Resolved during the final architect gate-check
(`handleQueryPredicateListNATS`'s implementation shape: single grouped
scan, not per-predicate fan-out — see Read-path changes). Bucket naming
was also resolved, but the opposite direction the gate-check recommended:
in-place format cutover on the existing `PREDICATE_INDEX` name, not a
rename to a new bucket — see the correction in Migration above (found
during implementation: ~9 operator-facing configs hardcode the bucket
name as a literal port subject, so a rename is not the zero-cost move it
appeared to be from Go source alone).

Also resolved during implementation, prompted by a direct question about
whether this design gives up NATS KV's dotted-subject wildcard/prefix
query semantics: see the new **Wildcard-semantics tradeoff** section
below — the answer is that this design gives that capability up only on
the predicate axis of `PREDICATE_INDEX` specifically, where it was never
safely available to begin with (ADR-056 already forbids relying on it),
and preserves it everywhere else, including a deliberate, safe path to
namespace-style predicate queries via `PREDICATE_CATALOG` if a real use
case wants it later.

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
