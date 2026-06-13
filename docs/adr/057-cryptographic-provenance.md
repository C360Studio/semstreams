# ADR-057: Cryptographic Provenance — Signed Owner Claims and Signed Envelopes (Scope-Only Stub)

## Status

**Proposed (scope-only)** — 2026-06-13. This is a SCOPE STUB, not a design. It reserves the
follow-up named in [ADR-056 §"Cryptographic provenance"](056-authoritative-semantic-state.md)
and bounds what a future authenticity layer would cover. It is NOT on any wave plan and does
NOT block ADR-056's Accept. No mechanism here is decided; this document exists so the seam is
named and the scope is agreed before anyone designs it.

## Context: authorization is not authentication

ADR-056 establishes an **authorization / provenance contract**: the `OWNER_CLAIMS` registry
asserts *"this owner is ALLOWED to mutate this predicate group"* and the semantic envelope
(ADR-055) asserts *"this write carries a declared type/domain/category/version."* Neither
proves **authenticity** — that the write *claiming* to be from an owner *actually originated*
from that owner, and that the bytes were neither forged nor replayed.

Today's envelope is **unauthenticated.** `BaseMessage.Hash()`
(`message/base_message.go:158-180`) is a SHA256 over message-type + payload — a content
integrity digest with **no signing key and no authentication**. Anyone able to publish to the
bus can mint an envelope with any `owner_id` and any `MessageType`. In a single-trust-domain
deployment this is acceptable: the registry's value is catching honest wiring collisions, not
adversaries. Cryptographic provenance becomes relevant only when the trust boundary widens
(multi-tenant, cross-org federation, untrusted producers, or a compliance requirement for
non-repudiable authorship).

These are **adjacent, not identical** properties. ADR-056 stands on its own as an authorization
contract; ADR-057 is the *optional* authenticity layer that can ride the same envelope +
registry seams later, IF a trust-domain requirement materializes.

## Scope of the follow-up (what a future ADR-057 design would cover)

A full ADR-057 design — when/if commissioned — must decide all of the following. None is
decided here:

1. **Canonical bytes.** A deterministic canonical-byte serialization for `BaseMessage` and for
   mutation requests (`UpdateEntityWithTriplesRequest` and the `triple.add*` shapes), so a
   signature is computed over stable bytes independent of JSON key ordering / whitespace /
   map iteration order. Without canonicalization a signature is unverifiable across encoders.
2. **Envelope signature metadata.** New envelope fields — `key_id`, `signature`, `algorithm`,
   `signed_at` — carried alongside the existing type/domain/category/version. (`key_id`
   identifies the signing key without embedding it; `algorithm` allows rotation; `signed_at`
   pairs with replay posture below.)
3. **Verification point + policy.** Signature verification at graph-ingest **before** accepting
   a fact-arrival or owner-reconcile write. Policy choice: fail-closed (reject unsigned/invalid
   when verification is enabled) vs observe-only (metric + accept, the migration on-ramp) —
   mirroring ADR-056 Decision 5's lint-then-metric staging.
4. **Owner-registry key binding.** An `owner_id → allowed key_ids` mapping in the owner
   registry (an extension of the `OWNER_CLAIMS` record), so verification can confirm the
   signing key is one the claimed owner is authorized to use. This is the join between ADR-056's
   authorization contract and ADR-057's authenticity check: *authorized owner* AND *valid
   signature from a key bound to that owner*.
5. **Key management.** Issuance, distribution, rotation, and revocation of signing keys — the
   hardest part, and the reason this is deferred. ADR-057 does not assume any particular KMS;
   the design must name its key-infrastructure dependency explicitly.
6. **Replay posture.** A signature proves *origin*, NOT *freshness* — a valid signed message can
   be replayed verbatim. Freshness requires pairing the signature with a nonce, a monotonic
   revision/sequence, or a TTL on `signed_at`. The design must state which, and how graph-ingest
   detects a replayed-but-validly-signed write (e.g. reject a write whose `(owner, entity,
   revision)` is not strictly ahead of the stored revision).

## Out of scope (explicitly NOT in any 057 design, to keep it bounded)

- Transport-layer security (NATS TLS / mTLS) — a separate, orthogonal concern from
  message-level authorship signing.
- Encryption / confidentiality of payloads — 057 is about authenticity (who authored this) and
  integrity (was it altered), not secrecy.
- Re-litigating ADR-056's authorization model. 057 is purely additive on top of it.

## Decision

**None yet.** This stub records scope only. A future revision (Status: Proposed → Accepted)
would carry the actual design across the six scope items above, gated on a concrete
trust-domain requirement that does not exist as of 2026-06-13.

## References

- [ADR-056: Authoritative Semantic State](056-authoritative-semantic-state.md) — the
  authorization contract this layer would authenticate; §"Cryptographic provenance" reserves
  this seam.
- [ADR-055](055-graph-write-intent-taxonomy.md) — the semantic-envelope-on-birth rule whose envelope would carry the
  signature metadata.
- `message/base_message.go:158-180` — today's unauthenticated `Hash()` (content digest, no
  signing) that a signed-envelope design would extend.
