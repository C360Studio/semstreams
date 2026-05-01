// Package rule — adapter from auth.Identity to CallerContext.
//
// Boundary discipline: the rule package imports auth, not the reverse.
// auth/ is a leaf package (no rule-engine knowledge); rule/ consumes it.
// This converter is the sole crossing point between the two packages.
package rule

import "github.com/c360studio/semstreams/auth"

// identityToCallerContext converts the auth/transport identity type into the
// rule-engine consumer type. Returns nil for nil input, preserving the tag-1
// nil-Caller contract: cron fires and KV-watch fires have no caller in scope.
//
// Source is intentionally dropped — the rule engine does not gate on
// provenance today. If a future $caller.source substitution lands, add the
// field to CallerContext and extend this converter (additive change only).
func identityToCallerContext(id *auth.Identity) *CallerContext {
	if id == nil {
		return nil
	}
	return &CallerContext{
		ID:   id.ID,
		Role: id.Role,
		Org:  id.Org,
	}
}
