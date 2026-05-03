// Package rule - Cron-specific substitution context.
//
// CronRule fires have no entity in scope at MVP — the scheduler ticks on
// a clock alone. Action templates therefore cannot use the
// `$entity.*` / `$related.*` / `$state.*` namespaces that expression
// rules populate from a matched entity. Instead, cron actions read
// schedule-shaped context: `$schedule.id`, `$schedule.spec`,
// `$schedule.last_fired_at`. Plus the existing `$now` token, which
// SubstituteVariables already handles for every rule kind.
//
// # Retrofit-safe namespace seam
//
// `$schedule.*` is intentionally disjoint from `$entity.*`. When the
// deferred per-entity fan-out extension lands (Definition.ForEach), each
// iteration of the fan-out gets the existing `$entity.*` namespace
// populated identically to expression-rule firing — the cron path
// supplies an EntityID, an Entity (or *EntityState fetched on demand),
// and a State; the substitution layer needs no changes. Adding
// `for_each` is purely additive.
//
// A future `$trigger.*` namespace (planned for richer tick metadata —
// e.g. `$trigger.attempt`, `$trigger.scheduled_for`) would land here as
// a sibling shim. Do not overload `$schedule.*` to carry per-tick
// metadata; keeping spec-shaped data and tick-shaped data in separate
// namespaces preserves the door for that extension.
package rule

import (
	"strings"
	"time"
)

// ScheduleContext carries the cron-rule context that's available at fire
// time but not derivable from any other rule field. ExecutionContext
// holds a *ScheduleContext on cron-fire dispatches; expression-rule
// dispatches leave the field nil and the substitution is a no-op.
//
// Fields are exported because the substitution layer reads them
// directly; the type is constructed by the cron scheduler in fire() and
// never mutated thereafter.
type ScheduleContext struct {
	// ID is the firing rule's stable identifier — same value as
	// Definition.ID and CronRule.ID. Always non-empty for a cron fire.
	ID string

	// Spec is the cron expression as the rule was registered with it.
	// Captured at fire time so a hot-reload that changes the schedule
	// doesn't retroactively alter substitution output for in-flight
	// dispatches. Always non-empty for a cron fire.
	Spec string

	// LastFiredAt is the wallclock of the most recent successful fire
	// before this one. Zero value (time.IsZero) on the first fire of
	// a freshly-registered rule, before any persisted record exists.
	// The substitution renders zero as the empty string so authors can
	// detect first-fire via `len("$schedule.last_fired_at") == 0` in
	// downstream conditional templates.
	LastFiredAt time.Time
}

// applyScheduleSubstitutions replaces `$schedule.*` tokens in template
// against sc, returning the substituted string. A nil sc is the
// no-op path — expression-rule fires call this through
// ExecutionContext.SubstituteVariables and any `$schedule.*` token
// will then survive substitution and trip the unresolved-template
// warning in execution_context.go (= author error: cron-only token
// in an expression rule).
//
// LastFiredAt's zero value renders as the empty string rather than
// `0001-01-01T00:00:00Z`. Authors needing to detect first-fire can
// branch on emptiness; rendering the Go zero-value would be
// surprising and would push the burden of checking onto every
// downstream template.
func applyScheduleSubstitutions(template string, sc *ScheduleContext) string {
	if sc == nil || !strings.Contains(template, "$schedule.") {
		return template
	}
	result := template
	result = strings.ReplaceAll(result, "$schedule.id", sc.ID)
	result = strings.ReplaceAll(result, "$schedule.spec", sc.Spec)

	var lastFired string
	if !sc.LastFiredAt.IsZero() {
		lastFired = sc.LastFiredAt.UTC().Format(time.RFC3339)
	}
	result = strings.ReplaceAll(result, "$schedule.last_fired_at", lastFired)

	return result
}
