// Package governance provides filter-chain output types and helpers for
// propagating content-governance evaluation results through the framework.
//
// Context travels in two layers:
//
//  1. Go context — within a single process, passed via [WithContext] /
//     [FromContext].
//  2. NATS headers — across process boundaries, serialized onto messages
//     by [InjectGovernance] and recovered by [ExtractGovernance] /
//     [ExtractGovernanceFromJetStream] (see governance/headers.go).
//
// Nil *Context means "no governance evaluation in scope" (cron fires,
// KV-watch fires that carry no filter-chain pass). Zero-value Context{}
// means "filter chain ran but detected nothing" — an intentional clean
// result rather than an absent evaluation. Callers and the rule engine
// both check for nil before inspecting fields.
package governance

import "context"

// Context carries the filter chain's output summary through the framework.
// Each boolean field signals whether the corresponding filter detected a
// violation.
//
// Pointer-everywhere semantics: nil = no governance evaluation in scope;
// zero-value Context{} = filter chain ran but detected nothing.
type Context struct {
	HasPii               bool
	HasInjection         bool
	HasContentModeration bool
	HasRateLimiting      bool
	ViolationCount       int
	MaxSeverity          string // one of the Severity* constants
	Allowed              bool
}

// Severity* are the canonical string values for Context.MaxSeverity.
// Match the existing agentic-governance Severity enum at
// processor/agentic-governance/config_injection.go:31-36, plus SeverityNone
// for the "no violations" case.
const (
	SeverityNone     = "none"
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

// FilterName* are stable wire-format identifiers matching the
// agentic-governance filter package's Filter.Name() values. Used by
// header helpers (see governance/headers.go) and chunk 5 producer
// wire-up to map per-filter outcomes to Context fields.
const (
	FilterNamePii               = "pii_redaction"
	FilterNameInjection         = "injection_detection"
	FilterNameContentModeration = "content_moderation"
	FilterNameRateLimiting      = "rate_limiting"
)

// contextKey is the unexported context-key type for stashing a *Context.
// Type uniqueness prevents collisions with other packages that might use
// identical underlying types as keys.
type contextKey struct{}

// WithContext returns a derived context with the supplied *Context attached.
// Pass nil to explicitly clear an inherited context (the resulting context
// will have FromContext return nil, false).
func WithContext(ctx context.Context, gc *Context) context.Context {
	return context.WithValue(ctx, contextKey{}, gc)
}

// FromContext returns the *Context attached via WithContext and a boolean
// indicating whether one was present. Returns (nil, false) when no context
// was attached or when WithContext was called with nil (clearing).
func FromContext(ctx context.Context) (*Context, bool) {
	v := ctx.Value(contextKey{})
	if v == nil {
		return nil, false
	}
	gc, ok := v.(*Context)
	if !ok || gc == nil {
		return nil, false
	}
	return gc, true
}
