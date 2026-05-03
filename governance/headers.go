package governance

import (
	"context"
	"strconv"

	"github.com/nats-io/nats.go"
)

// NATS header constants for the X-Gov-* governance wire format.
//
// Three-flat-headers shape (NOT single JSON header) per the project convention
// from auth/headers.go: simpler debugging in `nats trace`, no JSON-decoder
// failure mode, future-additive (a new header field doesn't break existing
// consumers).
const (
	XGovAllowedHeader              = "X-Gov-Allowed"
	XGovHasPiiHeader               = "X-Gov-Has-Pii"
	XGovHasInjectionHeader         = "X-Gov-Has-Injection"
	XGovHasContentModerationHeader = "X-Gov-Has-Content-Moderation"
	XGovHasRateLimitingHeader      = "X-Gov-Has-Rate-Limiting"
	XGovViolationCountHeader       = "X-Gov-Violation-Count"
	XGovMaxSeverityHeader          = "X-Gov-Max-Severity"
)

// InjectGovernance writes the *Context from ctx (if present) onto the outbound
// NATS message as X-Gov-* headers. No-op when no governance context is in ctx.
// Mirrors auth.InjectIdentity pattern exactly.
//
// Booleans are encoded as "true"/"false". ViolationCount as decimal string.
// MaxSeverity as the literal Severity* constant (defaulting to SeverityNone
// when empty). X-Gov-Allowed is always written when a context is present,
// serving as the presence sentinel for ExtractGovernance.
func InjectGovernance(ctx context.Context, msg *nats.Msg) {
	gc, ok := FromContext(ctx)
	if !ok || gc == nil {
		return
	}

	if msg.Header == nil {
		msg.Header = make(nats.Header)
	}

	msg.Header.Set(XGovAllowedHeader, strconv.FormatBool(gc.Allowed))
	msg.Header.Set(XGovHasPiiHeader, strconv.FormatBool(gc.HasPii))
	msg.Header.Set(XGovHasInjectionHeader, strconv.FormatBool(gc.HasInjection))
	msg.Header.Set(XGovHasContentModerationHeader, strconv.FormatBool(gc.HasContentModeration))
	msg.Header.Set(XGovHasRateLimitingHeader, strconv.FormatBool(gc.HasRateLimiting))
	msg.Header.Set(XGovViolationCountHeader, strconv.Itoa(gc.ViolationCount))

	maxSeverity := gc.MaxSeverity
	if maxSeverity == "" {
		maxSeverity = SeverityNone
	}
	msg.Header.Set(XGovMaxSeverityHeader, maxSeverity)
}

// ExtractGovernance reads X-Gov-* headers from an inbound NATS message and
// returns *Context if any header was set, nil if none were.
// Mirrors auth.ExtractIdentity pattern exactly.
func ExtractGovernance(msg *nats.Msg) *Context {
	if msg == nil || msg.Header == nil {
		return nil
	}
	return extractFromHeader(msg.Header)
}

// ExtractGovernanceFromJetStream reads X-Gov-* from raw nats.Header (the
// JetStream consumer surfaces headers as a typed map, not on a nats.Msg).
// Mirror of auth.ExtractIdentityFromJetStream.
func ExtractGovernanceFromJetStream(headers nats.Header) *Context {
	if headers == nil {
		return nil
	}
	return extractFromHeader(headers)
}

// extractFromHeader is the shared implementation for both Extract paths.
// Uses X-Gov-Allowed as the presence sentinel: returns nil when that header
// is absent (indicating no governance filter chain ran), returns a populated
// Context when it is present.
//
// Missing boolean headers default to false. Missing ViolationCount defaults
// to 0. Missing MaxSeverity defaults to SeverityNone.
func extractFromHeader(h nats.Header) *Context {
	// X-Gov-Allowed is the presence sentinel: the header is always written
	// by InjectGovernance, so its absence means governance headers were never
	// injected on this message.
	allowedRaw := h.Get(XGovAllowedHeader)
	if allowedRaw == "" {
		return nil
	}

	gc := &Context{}

	gc.Allowed, _ = strconv.ParseBool(allowedRaw)
	gc.HasPii, _ = strconv.ParseBool(h.Get(XGovHasPiiHeader))
	gc.HasInjection, _ = strconv.ParseBool(h.Get(XGovHasInjectionHeader))
	gc.HasContentModeration, _ = strconv.ParseBool(h.Get(XGovHasContentModerationHeader))
	gc.HasRateLimiting, _ = strconv.ParseBool(h.Get(XGovHasRateLimitingHeader))
	gc.ViolationCount, _ = strconv.Atoi(h.Get(XGovViolationCountHeader))

	maxSeverity := h.Get(XGovMaxSeverityHeader)
	if maxSeverity == "" {
		maxSeverity = SeverityNone
	}
	gc.MaxSeverity = maxSeverity

	return gc
}
