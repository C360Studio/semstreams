// Package rule - Caller-specific substitution context.
//
// Rules that gate on who is making a request need a caller-shaped
// substitution namespace: `$caller.id`, `$caller.role`, `$caller.org`.
// This file defines that namespace alongside the existing
// `$schedule.*` (cron_substitution.go) and `$entity.*` / `$state.*`
// namespaces (execution_context.go).
//
// # Namespace boundary discipline
//
// `$caller.*` is intentionally disjoint from `$entity.*`, `$state.*`,
// and `$schedule.*`. Do not add caller fields to those namespaces or
// entity fields to this one. Each namespace maps to a distinct
// ExecutionContext field (`Caller`, `Entity`/`Related`, `State`,
// `Schedule`) populated by a different code path. Keeping them disjoint
// lets rule authors reason unambiguously about where a value comes from,
// and lets the substitution layer apply each in a safe, nil-guarded pass.
//
// A future `$caller.trust_score` or `$caller.source` (provenance claim:
// "http_header" | "body_claim" | "ctx" | "default") would land here as
// additional ReplaceAll lines in applyCallerSubstitutions. Do not
// overload existing namespaces; keep the caller namespace the sole source
// of identity-shaped data for rule templates.
package rule

import "strings"

// CallerContext carries the identity of the caller whose request caused
// the rule to evaluate. ExecutionContext holds a *CallerContext when the
// message path or rule fire has an authenticated caller in scope; rules
// fired by cron or by pure KV-watch matches with no caller leave the
// field nil, and the substitution is a no-op.
//
// Fields are exported because the substitution layer reads them directly;
// the type is constructed by the rule processor (or its callers) and
// never mutated thereafter.
type CallerContext struct {
	// ID is the caller's stable identifier (user ID, service account name,
	// or equivalent). Empty string when not provided.
	ID string

	// Role is the caller's role claim (e.g. "admin", "viewer"). Empty
	// string when not provided. Role semantics are product-defined;
	// the framework treats this as an opaque string.
	Role string

	// Org is the caller's organization claim. For most deployments this
	// matches Platform.Org (same-org request), but is kept as a separate
	// field for clarity and for future cross-org scenarios at the proxy
	// tier. Empty string when not provided.
	Org string
}

// applyCallerSubstitutions replaces `$caller.*` tokens in template
// against cc, returning the substituted string. A nil cc is the
// no-op path — rules fired without a caller context (cron fires, pure
// KV-watch matches) call this through ExecutionContext.SubstituteVariables
// and any `$caller.*` token will then survive substitution and trip the
// unresolved-template warning in execution_context.go (= author error:
// caller-only token in a rule that never has a CallerContext).
func applyCallerSubstitutions(template string, cc *CallerContext) string {
	if cc == nil || !strings.Contains(template, "$caller.") {
		return template
	}
	result := template
	result = strings.ReplaceAll(result, "$caller.id", cc.ID)
	result = strings.ReplaceAll(result, "$caller.role", cc.Role)
	result = strings.ReplaceAll(result, "$caller.org", cc.Org)
	return result
}
