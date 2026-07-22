# Offloaded-entity identity embedding (adopter heads-up)

Shipped in the `offloaded-title-embedding` change (Epic A increment 3, #601).
**FYI for adopters** that offload entity bodies to a content store and rely on
semantic search — primarily **semsource** (every code symbol and doc passage).
No sister code change is required.

## What changed

Previously, `graph-embedding`'s offloaded (StorageRef) lane embedded **body bytes
only**. An offloaded entity's inline identity triples — `title`, `.signature`,
`.comment`, `dc.terms.title`, and anything else selected by `text_suffixes` — were
**excluded from the vector**, and `text_suffixes` was silently inert for those
entities.

Now the offloaded lane embeds the inline identity text **concatenated
identity-first, ahead of the body**, in a single vector. `text_suffixes` takes
effect on offloaded entities exactly as it does on inline ones.

## What adopters should expect

- **`text_suffixes` now works on offloaded entities.** If you restated the
  defaults and added predicates like `.signature` / `.comment` specifically so code
  signatures and docstrings enter the index (semsource does), that configuration
  now actually takes effect on offloaded entities — it did nothing before.
- **Vectors change → a one-time re-embed.** Because the embedded text now includes
  the identity, the content-addressed dedup key of **every offloaded entity that
  carries identity text** changes. Those entities re-embed on their next write; a
  full effect requires a re-ingest or a re-embed pass. Body-only offloaded entities
  (no inline identity triples) are unchanged and do not re-embed.
- **Recall shifts (for the better).** Queries that name a thing's title/identity —
  the natural-language case — now retrieve it even when the body never repeats those
  words. Expect a recall improvement on identity-named queries; re-run any recall
  baselines.
- **Observability.** Two counters report the effect:
  `semstreams_graph_embedding_offloaded_identity_included_total` and
  `..._offloaded_identity_absent_total`. If you tune `text_suffixes` on offloaded
  entities and want to confirm it took effect, watch the `included` counter.

## Not changed

- One vector per entity — no new storage or lifecycle. Embed-both (a dedicated
  identity vector for higher identity-query recall) was **deliberately deferred** to
  a measured follow-up; it breaks one-vector-per-entity and doubles embedding cost.
  Raise it with numbers if identity-query recall proves insufficient after this.
