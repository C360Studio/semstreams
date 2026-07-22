## Context

`graph-embedding` runs a two-hop pipeline. Hop 1 (`component.go`, the ENTITY_STATES
watcher) decides how an entity's embedding text is sourced and writes a pending
`Record`; hop 2 (`graph/embedding/worker.go`) produces the text, derives the dedup
key over it, and embeds.

Two lanes write the pending record:
- **Inline** — `SavePending(entityID, contentHash, sourceText, rev)` sets
  `Record.SourceText` to the full text extracted from the entity's text-suffix
  triples (`extractTextForEmbedding`, driven by `Config.TextSuffixes`).
- **Offloaded** — `SavePendingWithStorageRef(entityID, contentHash, storageRef,
  contentFields, rev)` sets `Record.StorageRef` (the body address) and leaves
  `Record.SourceText` **empty**.

Hop 2's single text-production site is `getSourceText` (`worker.go:663`):

```go
if record.SourceText != "" {          // inline lane: use it
    text = record.SourceText
} else if record.StorageRef != nil {  // offloaded lane: fetch body ONLY
    text, _ = w.fetchTextFromStorage(record.StorageRef)
}
```

The two branches are **mutually exclusive**. For any offloaded entity, only the
body is embedded; its inline identity triples (`title`, `.signature`, `.comment`,
`dc.terms.title`) never enter the vector, and `TextSuffixes` is silently inert for
it (#601). The prior rework added `reportOffloadedContentExcluded` only for the
*no-store fallthrough*; the *has-store* path (semsource's production case) is still
silent.

Owner decision (this change): **concatenate identity-first into a single vector**;
embed-both (a dedicated identity vector) is explicitly deferred — see Non-Goals.

## Goals / Non-Goals

**Goals:**

- An offloaded entity's inline identity triples (selected by `TextSuffixes`) are
  embedded **alongside** its offloaded body, identity-first, in one vector — so
  `TextSuffixes` takes effect for offloaded entities exactly as it does inline.
- The identity portion survives the source-text cap (it is at the front).
- The cross-lane dedup contract (#623/#628) stays correct with zero new machinery:
  one vector per entity, one completion latch, one dedup key over the exact bytes
  embedded.
- The effect is **observable** — a producer tuning `TextSuffixes` on offloaded
  entities can tell it took effect, not infer it from silence (Epic A theme).

**Non-Goals:**

- **Embed-both** (identity-only + body-only vectors, keep-better-score at query).
  It doubles embedding cost/storage on a lane semsource uses for every entity and,
  worse, breaks the one-vector-per-entity invariant — forcing a coupled two-vector
  lifecycle (regenerate both on write, reap both on delete, two-part readiness
  completion, search-time dedup-to-entity, and a new drift failure mode). The
  recall gain is second-order. Deferred to a **measured** follow-up: ship
  concatenate, and only build embed-both if identity-query recall on semsource is
  demonstrably insufficient, justified by numbers.
- Not a change to the offloaded fetch/dedup machinery (#628) — only the text
  *selection* is wrong.
- Not the retention (#632) or orphaned-blob GC (#633) concerns on this component.

## Decisions

### D1 — Concatenate at `getSourceText`, re-branched on `StorageRef` primary

The fix lives at the single text-production site so the dedup key and cap follow
for free. Re-branch from `SourceText`-primary to `StorageRef`-primary:

```go
if record.StorageRef != nil {
    body, err := w.fetchTextFromStorage(record.StorageRef)   // (err handling unchanged)
    if record.SourceText != "" {
        text = record.SourceText + identityBodySeparator + body   // identity-first
    } else {
        text = body
    }
} else {
    text = record.SourceText   // inline lane, unchanged
}
```

*Why re-branch, not just fill `SourceText`:* today's `else if` means an offloaded
record carrying `SourceText` would take the first branch and embed identity-**only**,
silently dropping the body. `StorageRef != nil` is the correct discriminator for
"this body is offloaded"; `SourceText` on an offloaded record now means "identity
prefix", not "the whole text". *Alternative rejected:* concatenating in hop 1 and
writing the combined text into the record — impossible, the body isn't resolved
until hop 2.

### D2 — Identity text = `extractTextForEmbedding(state)`, threaded through a new `SavePendingWithStorageRef` param

At `queueEmbeddingWithStorageRef` (`component.go`), compute the identity text with
the **existing** `extractTextForEmbedding(state)` — for an offloaded entity its
body is not an inline triple, so this returns exactly the inline identity triples
selected by `TextSuffixes`. Add a `sourceText string` parameter to
`SavePendingWithStorageRef` and set `Record.SourceText` from it. An entity with no
inline text triples yields `""` → hop 2 embeds body-only (today's behavior, no
regression).

*Alternative rejected:* a new `Record.IdentityText` field — needless; `SourceText`
already means "inline text to embed", and reusing it keeps the record shape and the
dedup derivation single-sourced.

### D3 — Cap is identity-first; combined text truncated at the end

`getSourceText`'s existing `truncateAtWord(text, maxSourceTextLen)` applies to the
**combined** string. Identity-first ordering means truncation trims the body tail
and the identity always survives. Note the interaction with
`fetchTextFromStorage`, which already clamps its stream read to `maxSourceTextLen`
for memory safety: the body arrives pre-clamped, identity is prepended, and the
combined is re-truncated to the cap — so the body effectively gets
`cap − len(identity)` and the whole stays memory-bounded. Design confirms the
separator (`"\n\n"`) is part of the embedded bytes and therefore the key.

### D4 — Dedup key and completion are automatically correct

The hop-2 dedup key (`DedupKey(embedderIdentity, sourceText)`, `worker.go:453`) is
derived over the combined+truncated text — the exact bytes embedded (#623). A
change to the identity **or** the body changes those bytes → a new key → a correct
re-embed; identical combined content across lanes still collapses (#627 stays
moot). One vector, one `Record`, one completion latch (ADR-066) — unchanged.

### D5 — Observability: `TextSuffixes` took effect is reportable

Make the identity inclusion visible rather than silent. Lightweight: a metric
counting offloaded entities that embedded identity text alongside the body (and,
symmetrically, offloaded entities with no inline identity text), so a producer
tuning `TextSuffixes` can confirm the effect from `/metrics`. Exact metric
name/shape resolved in the spec; reuse the `graph-embedding` metrics precedent
(`text_truncated_total`, #602).

## Risks / Trade-offs

- **Identity dilutes the single vector** → accepted; that is the concatenate/embed-both
  trade the owner chose. Identity terms are now *present* (the bug was total
  absence); a measured embed-both follow-up is the escape hatch if recall demands it.
- **Existing offloaded vectors change** → every offloaded entity's combined text now
  differs, so its dedup key changes and it re-embeds on next write (or on a
  re-embed sweep). Expected and desired (the vector was wrong). semsource should
  anticipate a one-time re-embed and a recall shift — coordinate via
  `semstreams-asks`.
- **`fetchTextFromStorage` double-cap** → prepending identity to a body already
  clamped at the cap then re-truncating is idempotent-safe (identity-first), but
  design must confirm the truncation metric (#602) is not double-counted across the
  fetch clamp and the combined truncate.

## Migration Plan

- Clean, additive: a new `sourceText` param on `SavePendingWithStorageRef` (internal
  API) and a re-branch in `getSourceText`. No wire/contract change to consumers.
- Re-embed: offloaded entities re-embed on their next write; a full effect requires
  re-ingest or a re-embed pass — call this out for semsource.
- Rollback: reverting restores body-only offloaded embedding; the identity text
  simply stops being prepended. Vectors written under the fix stay valid (they are
  just richer text); they converge back on the next re-embed after rollback.

## Open Questions

- **Separator** between identity and body — `"\n\n"` vs a sentinel. It is part of
  the embedded bytes and the key; pick one and freeze it (changing it later
  re-keys every offloaded entity). Lean: `"\n\n"`.
- **Metric shape** (D5) — a single `offloaded_identity_included_total`, or a
  paired included/absent counter? Resolve in the spec.
- **Cap budget** — do we reserve a portion of `maxSourceTextLen` for identity so a
  huge body cannot crowd it out under truncation, or is identity-first ordering
  sufficient? Lean: ordering is sufficient (identity is small and always first);
  no reserved budget.
