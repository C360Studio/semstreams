package agenticdispatch

import (
	"context"
	"net/http"
)

// identityKey is the unexported context-key type for stashing the
// caller's identity. Type uniqueness prevents collisions with other
// packages that might use string keys for their own ctx values.
type identityKey struct{}

// DefaultIdentity is returned by IdentityFromRequest when neither
// ctx nor the request body supplies a value. Matches the existing
// POST /message default ("http-user") so the new approval endpoint
// stays consistent with the established convention.
const DefaultIdentity = "http-user"

// IdentityFromRequest returns the caller's identity for an incoming
// HTTP request. Resolution order:
//
//  1. Context (set by middleware via WithIdentity) — wins when
//     non-empty.
//  2. fallbackBody — the request payload's claimed user_id if any.
//  3. DefaultIdentity ("http-user") — final fallback.
//
// SECURITY NOTE: an explicitly-empty body field (e.g., `user_id: ""`
// in JSON) is treated as "no claim from the body" and falls through
// to ctx-or-default. This is deliberate so today's behavior matches
// the prior `if req.UserID == "" { req.UserID = "http-user" }`
// inline pattern. When ADR-030's middleware contract lands, an
// authenticated identity in ctx will dominate any body claim — but a
// body with `user_id: ""` won't suddenly inherit the middleware's
// identity by accident, because the precedence is "ctx wins over
// absent body," not "body-empty means use ctx." The order above is
// the contract; the regression test
// TestIdentityFromRequest_BodyEmptyDoesNotInheritCtxBody guards it.
//
// Today no framework middleware populates ctx; the helper degrades
// to body+default, behaviorally identical to the inline pattern that
// `handleHTTPMessage` and the new `handleLoopApproval` would otherwise
// duplicate. When ADR-030's HTTP middleware contract lands, framework
// or product middleware will inject authenticated identity via
// WithIdentity and every handler picks it up automatically with no
// handler edits — a single seam that future-proofs the surface
// without locking us into a specific middleware shape today.
//
// The fallbackBody parameter is the body's claimed identity (e.g.,
// `req.UserID` from `HTTPMessageRequest` or `ApprovalRequest`).
// Pass an empty string when the request has no body identity field —
// the helper treats it the same as a missing ctx value.
func IdentityFromRequest(r *http.Request, fallbackBody string) string {
	if v, ok := r.Context().Value(identityKey{}).(string); ok && v != "" {
		return v
	}
	if fallbackBody != "" {
		return fallbackBody
	}
	return DefaultIdentity
}

// WithIdentity returns a derived context with the supplied identity
// attached. Intended for HTTP middleware to populate before the
// framework handler runs. Empty identity is allowed (the helper's
// resolution order treats it the same as missing); callers can use
// this to explicitly clear an inherited identity if they need to.
//
// Not yet part of the public component API surface — the contract
// settles in ADR-030. Exported for product-shell middleware (e.g.,
// semteams reading X-User-Id and writing to ctx) to call without a
// framework-internal import.
func WithIdentity(ctx context.Context, identity string) context.Context {
	return context.WithValue(ctx, identityKey{}, identity)
}
