// Package rule — adapter from governance.Context to MessageContext.
//
// Boundary discipline: the rule package imports governance, not the reverse.
// governance/ is a leaf package (no rule-engine knowledge); rule/ consumes it.
// This converter is the sole crossing point between the two packages.
package rule

import "github.com/c360studio/semstreams/governance"

// governanceToMessageContext converts the governance/transport context type into the
// rule-engine consumer type. Returns nil for nil input, preserving the nil-Message
// contract: cron fires and KV-watch fires that carry no governance context leave the
// field nil, and the substitution is a no-op.
func governanceToMessageContext(gc *governance.Context) *MessageContext {
	if gc == nil {
		return nil
	}
	return &MessageContext{
		HasPii:               gc.HasPii,
		HasInjection:         gc.HasInjection,
		HasContentModeration: gc.HasContentModeration,
		HasRateLimiting:      gc.HasRateLimiting,
		ViolationCount:       gc.ViolationCount,
		MaxSeverity:          gc.MaxSeverity,
		Allowed:              gc.Allowed,
	}
}
