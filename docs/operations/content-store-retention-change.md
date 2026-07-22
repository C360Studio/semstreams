# Content-store retention change (adopter heads-up)

Shipped in #632 (Epic A increment 2, `content-evidence-retention`). This is an
**FYI for adopters** (semsource, semboids, any repo attaching content
ObjectStores through the framework constructor) — **no sister code change is
required**; the behavior is inherited via the shared constructor.

## What changed

- **Content ObjectStores no longer carry a lifecycle TTL.** The shared
  constructor previously stamped a hard-coded 24h `MaxAge` on every content
  store's backing stream, which silently expired live-referenced verbatim bodies
  (#600). That is removed.
- **Boot now guards retention.** On startup each content store's backing stream
  (`OBJ_<bucket>`) is reconciled: a legacy `MaxAge`/`MaxBytes` (e.g. the old 24h
  on a persistent bucket) is **stripped in place via `UpdateStream` and logged at
  WARN** — self-healing, deletes nothing — then re-asserted. If a binding
  retention config is still present (e.g. an out-of-band NATS edit the strip
  could not clear), **boot fails closed** rather than silently expiring evidence.
- **Fusion reports missing bodies.** `Node.BodyReason` (a new omitempty field on
  the fusion response) carries `not_found` (the referenced body object is absent)
  or `error` (a hydration fault) when a requested body cannot be loaded. The wire
  is **byte-unchanged** for a fully-hydrated response; reading the field is
  opt-in. A new counter `semstreams_fusion_body_hydration_failures_total{reason}`
  meters failures.

## What adopters should check

- **If you deliberately set a TTL/MaxAge on a content ObjectStore**, stop — it
  will be stripped at boot (WARN) and, if un-strippable, will fail boot closed.
  Retention on live-referenced content is forbidden (ADR-068).
- **If you read fusion node bodies**, an empty `Body` now comes with a
  `body_reason` when it was requested-but-unloadable; a body-less entity carries
  no reason (unchanged behavior, just now explicit).
- **Watch for a WARN** `removed lifecycle retention from content store` on first
  boot after upgrade on any persistent NATS — that is the legacy 24h being
  reclaimed to the safe state, expected once per bucket.

## Not addressed here

Orphaned-blob garbage collection is **deferred** (#633, ADR-068 increment 6).
With the TTL removed, orphaned content-addressed blobs accumulate — an
owner-approved pre-v1 tradeoff (disk is not the concern; silent disappearance
was). Reference-aware reclamation is tracked in #633.
