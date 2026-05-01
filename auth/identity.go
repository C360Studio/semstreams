// Package auth provides caller-identity types and helpers for propagating
// authenticated user context through the framework.
//
// Identity travels in three layers:
//
//  1. Go context — within a single process, passed via [WithIdentity] /
//     [IdentityFromContext].
//  2. HTTP → ctx — HTTP handlers call [IdentityFromRequest] to resolve
//     identity from middleware-populated ctx, request-body claims, or a
//     default fallback.
//  3. NATS headers — across process boundaries, serialized onto messages
//     by [InjectIdentity] and recovered by [ExtractIdentity] /
//     [ExtractIdentityFromJetStream] (see auth/headers.go).
//
// Nil *Identity means "no identity in scope" (cron fires, KV-watch
// fires that carry no caller). Zero-value Identity{} means "anonymous"
// — an intentional empty identity rather than an absent one. Callers
// and the rule engine both check for nil before inspecting fields.
package auth

import (
	"context"
	"net/http"
)

// Identity carries authenticated caller claims through the framework.
//
// ID is the caller's stable identifier (user ID, service account name).
// Role is the caller's role claim (e.g. "admin", "viewer") — opaque to
// the framework; product-defined semantics.
// Org is the caller's organization claim. Matches Platform.Org for
// same-org requests.
// Source records where the identity was resolved from (provenance for
// audit-trail authenticity). Use the Source* constants below.
//
// Identity is carried through context (WithIdentity / IdentityFromContext)
// and across NATS messages via headers (see auth/headers.go). Pointer
// nil means "no identity in scope" (cron fires, KV-watch fires); zero
// value Identity{} means "anonymous" (intentional empty identity).
type Identity struct {
	ID     string
	Role   string
	Org    string
	Source string
}

// Source* constants enumerate where Identity values can originate.
// Used in IdentityFromRequest precedence resolution and in audit logs.
const (
	SourceHTTPHeader = "http_header"
	SourceBodyClaim  = "body_claim"
	SourceCtx        = "ctx"
	SourceDefault    = "default"
)

// DefaultIdentityID is returned by IdentityFromRequest when neither
// ctx nor the request body supplies a value. Matches the pre-existing
// agenticdispatch.DefaultIdentity ("http-user") for backward-compatible
// observability.
const DefaultIdentityID = "http-user"

// identityKey is the unexported context-key type. Type uniqueness
// prevents collisions with other packages that might use string keys.
type identityKey struct{}

// WithIdentity returns a derived context with the supplied *Identity
// attached. Pass nil to explicitly clear an inherited identity (the
// resulting context will have IdentityFromContext return nil, false).
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFromContext returns the *Identity attached via WithIdentity
// and a boolean indicating whether one was present. Returns nil, false
// when no identity was attached or when WithIdentity was called with
// nil (clearing).
func IdentityFromContext(ctx context.Context) (*Identity, bool) {
	v := ctx.Value(identityKey{})
	if v == nil {
		return nil, false
	}
	id, ok := v.(*Identity)
	if !ok || id == nil {
		return nil, false
	}
	return id, true
}

// IdentityFromRequest returns the caller's identity for an incoming
// HTTP request. Resolution order: ctx (via WithIdentity) > fallbackBody
// > default. Sets Source to the winning branch. Never returns nil — at
// minimum returns &Identity{ID: DefaultIdentityID, Source: SourceDefault}.
//
// SECURITY NOTE: an explicitly-empty body field is treated as "no claim
// from the body" and falls through to ctx-or-default. This preserves
// the precedence guarantee: ctx > body, never body-empty-inherits-ctx.
// Same regression guard as the pre-existing
// agenticdispatch.IdentityFromRequest (which this replaces).
func IdentityFromRequest(r *http.Request, fallbackBody string) *Identity {
	if id, ok := IdentityFromContext(r.Context()); ok {
		// Return a copy with Source stamped so the caller can see which
		// branch won without mutating the ctx-attached value.
		return &Identity{
			ID:     id.ID,
			Role:   id.Role,
			Org:    id.Org,
			Source: SourceCtx,
		}
	}
	if fallbackBody != "" {
		return &Identity{
			ID:     fallbackBody,
			Source: SourceBodyClaim,
		}
	}
	return &Identity{
		ID:     DefaultIdentityID,
		Source: SourceDefault,
	}
}
