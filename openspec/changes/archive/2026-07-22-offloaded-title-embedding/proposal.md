## Why

`graph-embedding`'s offloaded (StorageRef) lane embeds **body bytes only**
(`queueEmbeddingWithStorageRef` passes `nil` inline text, `component.go:1508`).
For any entity with a servable content store — every code symbol and doc passage
SemSource produces — the `title` and every identifying triple (`.signature`,
`.comment`, `dc.terms.title`) are excluded from the vector. The identity of a
thing is exactly what a natural-language query names, and it is the part that does
not get embedded: a passage titled `"Retry Policy § Exponential Backoff"` whose
body never repeats those words is unreachable by a query naming them.

This also makes the `text_suffixes` config **silently inert** for offloaded
entities — it appears to work and does nothing, with no error or warning (#601).
The prior offloaded-lane rework only reported the *no-store fallthrough*
(`reportOffloadedContentExcluded`); the *has-store* production path still excludes
inline triples silently. Verified open on `main`.

## What Changes

- **Embed the inline identity triples alongside the offloaded body** for the
  has-store lane, so `text_suffixes` takes effect for offloaded entities. The
  inline text is available on the `EntityState` at hop 1 (`extractTextForEmbedding`);
  the body resolves at hop 2. The fix threads the inline text through the `nil`
  seam at `SavePendingWithStorageRef` (`component.go:1508`) so hop 2 combines
  identity + body before embedding. **The combine strategy is a design decision:**
  concatenate (identity ‖ body) vs. embed-both-and-keep-better-score.
- **Make the exclusion/config-effect observable** — a producer tuning
  `text_suffixes` must be able to tell it took effect on offloaded entities,
  rather than the current indistinguishable-from-working silence. (Epic A theme.)

## Capabilities

### New Capabilities
<!-- none expected; confirm in design against openspec/specs/ -->

### Modified Capabilities

- `graph-embedding`: the embedding-text contract now covers offloaded entities —
  their inline identity triples (per `text_suffixes`) are embedded alongside the
  offloaded body, and the offloaded lane's text selection is observable rather
  than silently body-only.

## Impact

- **Code:** `processor/graph-embedding/component.go` — `queueEmbeddingWithStorageRef`
  (the `nil` seam at `:1508`) and the hop-2 worker (resolve body → combine with
  inline identity text → embed); `SavePendingWithStorageRef` signature (the inline
  slot). Touches the storage/pending record shape for the offloaded lane.
- **Dedup key (#623):** hop 2 keys over the *embedded bytes*; combining inline +
  body changes those bytes, so the key changes — correct (the vector content
  changed), but the interaction with the content-addressed dedup + cap contract
  (#628) must be spelled out in design (cap applies to the combined text; order
  matters for truncation).
- **Sister repos:** semsource is the primary adopter — its `text_suffixes`
  (`.signature`/`.comment`, `run.go:830-833`) start taking effect on offloaded
  entities, so their vectors change (recall improves; a re-embed is expected).
  Coordinate via `semstreams-asks`.
- **Related:** #599 (fusion/offloaded-lane e2e coverage gap), increment 1's
  offloaded-lane dedup rework (#628) is the substrate this builds on.

## Non-goals

- **Not** a rewrite of the offloaded fetch/dedup machinery (#628 shipped it) —
  only the text *selection* is wrong.
- **Not** the orphaned-blob GC (#633) or the retention contract (#632) — a
  separate lane concern on the same component.
