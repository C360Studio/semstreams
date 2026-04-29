# ADR-030: HTTP Middleware Contract and Authenticated Identity

## Status

**Proposed (2026-04-29).** Not committed to any specific tag.
Beta.22 lands the first concrete forward-compatible step
(`agenticdispatch.IdentityFromRequest` and `WithIdentity` helpers
in `processor/agentic-dispatch/identity.go`) so the future contract
has a place to plug in without rewriting handlers. The full
implementation — middleware-chain plumbing, NATS message-header
identity, server-side bypass tokens — is deferred until the pattern
problem becomes a felt friction point or a security incident
forces it.

## Context

Identity handling across the framework's HTTP and NATS surfaces is
inconsistent today:

| Surface | Pattern | Authoritative? | Forgeable? |
|---|---|---|---|
| `POST /message` | Body field `user_id`, default `"http-user"` | No — claimed | Yes — anyone with HTTP access |
| `POST /loops/{id}/signal` | Reads `loop.UserID` from in-memory tracker (the loop's *originating* user) | Indirect — trust the original creation path | Indirectly — the original message could have lied |
| `POST /loops/{id}/approval` (beta.22) | Body field `user_id` via `IdentityFromRequest`, default `"http-user"` | No — claimed | Yes |
| NATS `agentic.UserMessage` payload | Wire field, set by publisher | No — claimed | Yes — anyone with NATS publish rights |
| NATS `agentic.ApprovalResponse.ApprovedBy` | Wire field, set by publisher | No — claimed | Yes (documented in `feedback_approval_bypass_forgery.md`) |
| NATS `agentic.ToolCall.ApprovedBy` | Bypass token on the wire | No — trusted-because-non-empty | Yes (M2 finding from beta.19 review) |

There is no notion of "authenticated identity" vs "claimed identity"
on either surface. Bodies and wire payloads are both treated as
truth. There is no HTTP middleware contract on the framework — each
component declares its own routes via `RegisterHTTPHandlers(prefix,
*http.ServeMux)`, and any cross-cutting concern (auth, rate
limiting, audit logging) has to be implemented either inside each
handler or by wrapping the mux at the binary level.

Each new HTTP entry point compounds the inconsistency. The right
moment to fix it is before the surface ossifies, but a full
refactor today would slip multiple feature deliveries with weak
forcing function. ADR-030 captures the direction so future design
pressure has a target.

## Decision

Adopt a phased migration to a context-based authenticated-identity
model with a framework-level HTTP middleware contract. **No tag is
committed to any phase except Phase 0 (the seam, beta.22).**

### Phase 0 — Forward-compatible identity helpers (beta.22, shipped)

`processor/agentic-dispatch/identity.go`:

- `IdentityFromRequest(r *http.Request, fallbackBody string) string`
  — resolution order: ctx (set by middleware via WithIdentity, when
  non-empty) > fallbackBody > `DefaultIdentity` ("http-user").
- `WithIdentity(ctx context.Context, identity string) context.Context`
  — exported for product-shell middleware (e.g., semteams reading
  `X-User-Id` and writing to ctx) without a framework-internal
  import.

Existing handlers (`handleHTTPMessage`, `handleLoopApproval`)
consult the helper. No middleware lands in beta.22; the helper
degrades to body+default, behaviour-equivalent to the prior inline
pattern.

The contract: both `user_id: ""` (field present, empty) and
`user_id` absent (field missing) fall through to ctx-or-default —
the helper treats them identically. Today this is behavior-
equivalent to the prior inline `if req.UserID == "" { ... }`
pattern. Once Phase 1 lands and middleware actually populates ctx,
both shapes correctly inherit the authenticated identity rather
than the default — which is the contract we want, and the
`TestIdentityFromRequest_BodyEmptyDoesNotInheritCtxBody` regression
test pins it. The phrasing matters because the alternative
("body-empty means do nothing, use default") would silently strip
authenticated identity from any caller that passed an empty
user_id field — a privilege-escalation-shaped surprise we want to
foreclose now while the seam is small.

### Phase 1 — Framework HTTP middleware contract

Define a middleware seam on `service.Manager` (or
`component.Dependencies`, TBD):

```go
type HTTPMiddleware func(http.Handler) http.Handler

// Future shape on service.Dependencies or service.Manager:
type Dependencies struct {
    // ... existing fields ...
    HTTPMiddleware []HTTPMiddleware // applied in order, outermost-first
}
```

When a component or service calls `RegisterHTTPHandlers(prefix, mux)`,
the framework wraps every handler with the chain before mounting.
Products supply their middleware at binary boot. semteams plugs in
a middleware that reads `X-User-Id` and calls `WithIdentity`;
semspec plugs in OAuth bearer-token validation; semdragon does
something else. The framework stays neutral on auth shape.

Migration path: the existing `RegisterHTTPHandlers` signature stays
intact; the wrapping happens transparently inside the framework's
mux setup. Handlers that already use `IdentityFromRequest` get the
middleware-supplied identity for free.

### Phase 2 — NATS message-header identity

(Depends on Phase 1's ctx-propagation discipline. Without an
established middleware-to-ctx pattern, every NATS subscriber would
invent its own `IdentityFromMsg`-to-ctx adapter and recreate today's
HTTP-side inconsistency on the NATS side.)

Identity travels in NATS message headers (parallel to
`natsclient/trace.go`'s trace-context injection), not body fields.
A new `auth.IdentityFromMsg(msg *nats.Msg) (Identity, bool)` helper
mirrors the HTTP one. Existing wire-field usages
(`UserMessage.UserID`, `ApprovalResponse.ApprovedBy`,
`ToolCall.ApprovedBy`) become "presenter claims" — opaque metadata,
never authoritative. Authoritative identity comes from the header.

### Phase 3 — Server-side bypass tokens

`ToolCall.ApprovedBy` (and any future bypass tokens) move off the
wire payload entirely. Replacement options:

- **HMAC-signed claim:** the loop signs `(call_id, approver, exp)`
  with a per-process key; the executor or a server-side filter
  wrapper verifies before bypassing. Stateless, fast, requires
  shared secret.
- **One-shot KV token:** the loop writes a per-call_id token into
  a framework-owned KV bucket when issuing the approval; the
  executor consumes the token before running the tool. Stateful,
  no shared secret, requires KV operations.

Lean toward the KV-token approach because it's auditable and doesn't
require key distribution across multi-process deployments.

This phase closes the M2 forgery finding in
`feedback_approval_bypass_forgery.md`.

## Consequences

### Positive

- Single canonical seam for HTTP identity, with one public function
  call (`IdentityFromRequest`) and one well-documented contract.
- Future middleware lands without handler edits (the seam is
  already there).
- Authenticated vs. claimed identity becomes a real architectural
  distinction, not buried in code review notes.
- Server-side bypass tokens close the wire-forgery threat to
  approval gating without breaking the existing
  `agentic.ApprovalResponse` payload shape (`ApprovedBy` becomes
  presenter claim; auth lives elsewhere).

### Negative

- More moving parts on the auth path — middleware chain, ctx
  threading, server-side token store. Adds testing surface.
- Phase 2 changes the NATS wire contract for downstream products
  expecting body-field identity. Migration coordinated separately.
- Some amount of "who's responsible for setting the header"
  ambiguity at the framework/product seam. Documentation has to
  carry the weight.

### Neutral

- The existing wire fields stay; they just become non-authoritative.
  Backwards compatibility is preserved at the type level.

## Implementation notes

- Phase 0 (beta.22) is shipped. The helper API is the public
  contract for product-shell middleware to plug into; future phases
  expand around it.
- Phase 1 has no committed tag. The right trigger is two or more
  products needing the same middleware (auth, rate limit, audit
  trail) — at that point a framework-level chain is cheaper than
  duplicating in each product binary.
- Phase 2 has no committed tag. The trigger is either a security
  audit finding the wire-forgery surface, or a product needing
  cross-process identity propagation that body fields can't
  carry (e.g., signed delegation chains).
- Phase 3 has no committed tag. Trigger: a real production
  exploitation of the forgery surface, or a regulated-deployment
  customer asking for it.

## Critical files (Phase 0, shipped)

| File | Role |
|---|---|
| `processor/agentic-dispatch/identity.go` | `IdentityFromRequest` + `WithIdentity` + `DefaultIdentity` |
| `processor/agentic-dispatch/identity_test.go` | Resolution-order regression guards including the security-shaped body-empty-vs-ctx-precedence test |
| `processor/agentic-dispatch/http.go` | Two existing handlers (`handleHTTPMessage`, `handleLoopApproval`) consume the helper |

## Related

- `feedback_approval_bypass_forgery.md` — M2 finding from beta.19,
  drives Phase 3.
- `feedback_framework_boundary.md` — products own product policy;
  this ADR clarifies how products plug auth in without forking
  framework HTTP handlers.
- `feedback_http_identity_pattern.md` — operational guidance for
  future HTTP surfaces: use `IdentityFromRequest`, don't reinvent
  body fallback inline.
- ADR-029 (Instance-Type Patterns) — analogous "the framework
  shouldn't grow N inconsistent surfaces" pattern resolution, just
  for runtime registration rather than auth.
