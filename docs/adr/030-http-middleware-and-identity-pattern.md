# ADR-030: HTTP Middleware Contract and Authenticated Identity

## Status

**Phase 0 (beta.22) and Phase 1 (beta.23) shipped (2026-04-29).**
Phases 2 (NATS message-header identity) and 3 (server-side bypass
tokens) remain deferred until the pattern problem becomes a felt
friction point or a security incident forces them.

- Phase 0 (beta.22): `agenticdispatch.IdentityFromRequest` and
  `WithIdentity` helpers in
  `processor/agentic-dispatch/identity.go` — the small forward-
  compatible seam handlers consume.
- Phase 1 (beta.23): `service.HTTPMiddleware` type and
  `(*service.Manager).UseHTTPMiddleware` setter — the framework-
  level middleware chain that wraps every registered route.
  Products plug in their auth / logging / recovery / rate-limit
  middleware here. The framework ships zero default middleware.

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

### Phase 1 — Framework HTTP middleware contract (beta.23, shipped)

The seam landed on `service.Manager` rather than
`service.Dependencies` — `Dependencies` is per-service constructor
input, but middleware is a Manager-level concern (it wraps the
shared mux, not any individual service). Final shape:

```go
// service/middleware.go
type HTTPMiddleware func(http.Handler) http.Handler

// service/service_manager.go
func (m *Manager) UseHTTPMiddleware(mws ...HTTPMiddleware)
```

Products call `manager.UseHTTPMiddleware(...)` between
`NewServiceManager(...)` and `StartAll(ctx)`; the framework wraps
the mux at boot via `m.buildHTTPHandler()` and assigns the result
to `http.Server.Handler`. Calls after the server is running are
ignored with a warning (the `Handler` field is set at boot;
late additions can't take effect, and a warning surfaces operator
misuse instead of silently dropping).

The existing `RegisterHTTPHandlers(prefix, *http.ServeMux)`
signature stays intact; wrapping is transparent. Handlers that
use `IdentityFromRequest` get the middleware-supplied identity
for free. semteams plugs in `X-User-Id` → `WithIdentity`;
semspec plugs in OAuth bearer-token validation; semdragon does
its own thing. The framework stays neutral on auth shape and
ships zero default middleware.

Operations doc: `docs/operations/09-http-middleware.md`.
Migration: `docs/operations/migration-beta22-to-beta23.md`.

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
- Phase 1 (beta.23) is shipped. The seam landed on
  `service.Manager` (not `Dependencies` — see Phase 1 section
  above for why). Composition is reverse-build outermost-first,
  nil entries are skipped, and the no-middleware path compiles to
  the bare mux with no overhead. The framework ships zero default
  middleware; products supply auth / logging / recovery / rate
  limiting at boot.
- Phase 2 has no committed tag. The trigger is either a security
  audit finding the wire-forgery surface, or a product needing
  cross-process identity propagation that body fields can't
  carry (e.g., signed delegation chains).
- Phase 3 has no committed tag. Trigger: a real production
  exploitation of the forgery surface, or a regulated-deployment
  customer asking for it.

## Critical files

### Phase 0 (beta.22, shipped)

| File | Role |
|---|---|
| `processor/agentic-dispatch/identity.go` | `IdentityFromRequest` + `WithIdentity` + `DefaultIdentity` |
| `processor/agentic-dispatch/identity_test.go` | Resolution-order regression guards including the security-shaped body-empty-vs-ctx-precedence test |
| `processor/agentic-dispatch/http.go` | Two existing handlers (`handleHTTPMessage`, `handleLoopApproval`) consume the helper |

### Phase 1 (beta.23, shipped)

| File | Role |
|---|---|
| `service/middleware.go` | `HTTPMiddleware` type + unexported `chainMiddleware` helper |
| `service/middleware_test.go` | Order, pass-through, short-circuit, nil-skip, setter behavior, wired-boot-path coverage |
| `service/service_manager.go` | `httpMiddleware` field on `Manager`, `UseHTTPMiddleware` setter, `buildHTTPHandler` helper, two `Handler:` edits at the http.Server construction sites |
| `docs/operations/09-http-middleware.md` | Product-facing contract doc with paired-helper, panic-recovery, request-log, CORS, and per-prefix patterns |
| `docs/operations/migration-beta22-to-beta23.md` | Migration guide |

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
