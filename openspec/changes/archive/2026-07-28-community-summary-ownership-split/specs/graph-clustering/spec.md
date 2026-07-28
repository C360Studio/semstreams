## ADDED Requirements

### Requirement: LLM community summaries live in a worker-exclusive, content-addressed store

LLM-generated community summaries SHALL be stored in a `COMMUNITY_SUMMARIES` KV bucket keyed by
`{level}.{membership_hash}`, written ONLY by the enhancement worker; the detector SHALL NOT write
this bucket, and the enhancement worker SHALL NOT write the partition bucket (`COMMUNITY_INDEX`).
The partition bucket remains detector-exclusive (partition, keywords, and the statistical summary);
LLM prose lives only in the summary store.

Because the summary key is derived from the content the worker summarized (the sorted member set),
a write is correct for that membership whether or not the membership is still current — so a
lagging or slow worker CANNOT overwrite a fresher partition (there is no shared key) and CANNOT
resurrect a `Prune`-deleted community (the read path joins by the *current* community's membership
hash, so an orphaned summary is served only when a current community has that exact member set).
The write therefore requires no revision CAS, no membership-similarity transfer, and no archive
step. A same-membership double-write by two workers SHALL be idempotent, not an error.

#### Scenario: A stale-snapshot worker write cannot corrupt the partition

- **GIVEN** a detection cycle has replaced community `X`'s membership since a worker read its snapshot
- **WHEN** that worker finishes summarizing the stale snapshot and writes its result
- **THEN** the write lands in `COMMUNITY_SUMMARIES` keyed by the stale membership's hash
- **AND** `COMMUNITY_INDEX` and its entity mappings are unchanged
- **AND** no `Prune`-deleted community is resurrected in the partition bucket

#### Scenario: An unchanged membership is a cache hit, not a re-summarization

- **GIVEN** a community whose membership hash already has an `llm-enhanced` summary record
- **WHEN** the enhancement worker is triggered for that community again
- **THEN** it serves the stored summary and performs no LLM call
- **AND** a `summary_cache_hits_total` observation is recorded

#### Scenario: A failed summary is retried only after a backoff

- **GIVEN** a membership hash whose summary record has status `llm-failed`
- **WHEN** the enhancement worker is triggered for it before the retry backoff elapses
- **THEN** it does not perform an LLM call
- **AND** after the backoff elapses a subsequent trigger does retry

### Requirement: The community membership hash has a single shared definition

The membership hash that keys `COMMUNITY_SUMMARIES` SHALL be produced by one shared exported helper
(`clustering.MembershipHash`) computing sha256 over the newline-joined, lexically-sorted member IDs,
hex-encoded. Every producer and consumer of the key — the enhancement worker, the graph-query
read-join, and the B0 thematic eval — SHALL derive the hash through that one helper so the
definition cannot drift into two subtly different hashes that never join.

#### Scenario: The store and the eval agree on the hash

- **GIVEN** a fixed member set
- **WHEN** the enhancement worker and the B0 eval each compute its membership hash
- **THEN** both obtain the identical value from `clustering.MembershipHash`

### Requirement: Community-summary volume is observable

The number of stored community summaries SHALL be exposed as a gauge, so the operational question
"is the content-addressed summary store accumulating unboundedly?" is answered by a metric read
rather than an estimate. (The store is a reuse cache with no summaries GC in this increment; the
gauge is the trigger for a future bounded-GC decision.)

#### Scenario: The summary-store size is a metric

- **GIVEN** a running graph-clustering component with a populated `COMMUNITY_SUMMARIES` bucket
- **WHEN** an operator scrapes metrics
- **THEN** a gauge reports the current number of stored summary records
