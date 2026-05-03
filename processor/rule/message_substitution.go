// Package rule - Message-specific substitution context.
//
// Rules that gate on governance filter results need a message-shaped
// substitution namespace: `$message.violations.has_pii`, `$message.allowed`,
// etc. This file defines that namespace alongside the existing
// `$schedule.*` (cron_substitution.go), `$caller.*` (caller_substitution.go),
// and `$entity.*` / `$state.*` namespaces (execution_context.go).
//
// # Namespace boundary discipline
//
// `$message.*` is intentionally disjoint from `$entity.*`, `$state.*`,
// `$schedule.*`, and `$caller.*`. Do not add message fields to those
// namespaces or entity fields to this one. Each namespace maps to a distinct
// ExecutionContext field populated by a different code path. Keeping them
// disjoint lets rule authors reason unambiguously about where a value comes
// from, and lets the substitution layer apply each in a safe, nil-guarded pass.
//
// A future `$message.user_id` or other governance-adjacent field would land
// here as additional ReplaceAll lines in applyMessageSubstitutions. Do not
// overload existing namespaces; keep the message namespace the sole source of
// governance-shaped data for rule templates.
package rule

import (
	"fmt"
	"strings"
)

// applyMessageSubstitutions replaces `$message.*` tokens in template
// against mc, returning the substituted string. A nil mc is the
// no-op path — rules fired without a governance context (cron fires, pure
// KV-watch matches) call this through ExecutionContext.SubstituteVariables
// and any `$message.*` token will then survive substitution and trip the
// unresolved-template warning in execution_context.go (= author error:
// message-only token in a rule that never has a MessageContext).
func applyMessageSubstitutions(template string, mc *MessageContext) string {
	if mc == nil {
		return template
	}
	result := template
	result = strings.ReplaceAll(result, "$message.violations.has_pii", fmt.Sprintf("%t", mc.HasPii))
	result = strings.ReplaceAll(result, "$message.violations.has_injection", fmt.Sprintf("%t", mc.HasInjection))
	result = strings.ReplaceAll(result, "$message.violations.has_content_moderation", fmt.Sprintf("%t", mc.HasContentModeration))
	result = strings.ReplaceAll(result, "$message.violations.has_rate_limiting", fmt.Sprintf("%t", mc.HasRateLimiting))
	result = strings.ReplaceAll(result, "$message.violations.count", fmt.Sprintf("%d", mc.ViolationCount))
	result = strings.ReplaceAll(result, "$message.max_severity", mc.MaxSeverity)
	result = strings.ReplaceAll(result, "$message.allowed", fmt.Sprintf("%t", mc.Allowed))
	return result
}
