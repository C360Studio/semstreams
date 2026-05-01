package auth

import (
	"context"

	"github.com/nats-io/nats.go"
)

// NATS header constants for the X-Caller-* identity wire format.
//
// Three-flat-headers shape (vs single JSON header): simpler debugging
// in `nats trace` output, no JSON-decoder failure mode, future-additive
// (a new header field doesn't break existing consumers). Mirrors the
// W3C-style header naming established by natsclient/trace.go.
const (
	XCallerIDHeader     = "X-Caller-Id"
	XCallerRoleHeader   = "X-Caller-Role"
	XCallerOrgHeader    = "X-Caller-Org"
	XCallerSourceHeader = "X-Caller-Source"
)

// InjectIdentity writes the *Identity from ctx (if present) onto the
// outbound NATS message as X-Caller-* headers. No-op when no identity
// is in ctx. Mirrors natsclient/trace.go InjectTrace pattern exactly.
func InjectIdentity(ctx context.Context, msg *nats.Msg) {
	id, ok := IdentityFromContext(ctx)
	if !ok || id == nil {
		return
	}

	if msg.Header == nil {
		msg.Header = make(nats.Header)
	}

	msg.Header.Set(XCallerIDHeader, id.ID)
	msg.Header.Set(XCallerRoleHeader, id.Role)
	msg.Header.Set(XCallerOrgHeader, id.Org)
	msg.Header.Set(XCallerSourceHeader, id.Source)
}

// ExtractIdentity reads X-Caller-* headers from an inbound NATS message
// and returns *Identity if any header was set, nil if none were. The
// returned Identity has Source = SourceHTTPHeader to track wire-origin
// in downstream audit triples.
//
// Mirrors natsclient/trace.go ExtractTrace pattern exactly.
func ExtractIdentity(msg *nats.Msg) *Identity {
	if msg == nil || msg.Header == nil {
		return nil
	}
	return extractFromHeader(msg.Header)
}

// ExtractIdentityFromJetStream reads X-Caller-* from raw nats.Header
// (the JetStream consumer surfaces headers as a typed map, not on a
// nats.Msg). Mirror of natsclient/trace.go ExtractTraceFromJetStream.
func ExtractIdentityFromJetStream(headers nats.Header) *Identity {
	if headers == nil {
		return nil
	}
	return extractFromHeader(headers)
}

// extractFromHeader is the shared implementation for both Extract paths.
// Returns nil when no X-Caller-* header is present; otherwise returns
// a populated Identity with Source = SourceHTTPHeader (wire-origin
// marker for downstream audit triples).
func extractFromHeader(h nats.Header) *Identity {
	id := h.Get(XCallerIDHeader)
	role := h.Get(XCallerRoleHeader)
	org := h.Get(XCallerOrgHeader)
	// source from the wire is the outbound source stamped by the sender;
	// we override with SourceHTTPHeader to mark wire-origin for the receiver.
	_ = h.Get(XCallerSourceHeader)

	if id == "" && role == "" && org == "" {
		return nil
	}

	return &Identity{
		ID:     id,
		Role:   role,
		Org:    org,
		Source: SourceHTTPHeader,
	}
}
