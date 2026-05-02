// Package rule - Cron rule type for time-driven actions.
//
// Cron rules are a third firing path alongside KV-watch and message-path
// rules. Unlike expression rules, cron rules have no condition — they fire
// on a clock tick alone, dispatched by CronScheduler against the same
// ActionExecutor that expression rules use.
//
// CronRule is intentionally NOT an implementation of the Rule interface.
// Subscribe / Evaluate / ExecuteEvents are message-driven concepts that
// don't fit time-driven firing. Keeping cron rules outside the Rule
// registry means they don't accidentally appear in evaluateRulesForMessage
// dispatch and don't need no-op stubs cluttering the type.
package rule

import (
	"fmt"
	"slices"
	"strings"
	"time"

	cronlib "github.com/robfig/cron/v3"
)

// CronRuleType is the string used as Definition.Type for cron rules.
const CronRuleType = "cron"

// cronParser is the package-shared parser. POSIX 5-field plus descriptors
// (@hourly, @daily, @weekly, @monthly, @yearly). Deliberately excludes
// quartz seconds-precision and "first Tuesday of month" — those are
// tracked as deferred items per ADR-031.
var cronParser = cronlib.NewParser(
	cronlib.Minute | cronlib.Hour | cronlib.Dom | cronlib.Month | cronlib.Dow | cronlib.Descriptor,
)

// CronRule holds a parsed cron-rule definition ready for the scheduler to
// register and fire. It carries everything the scheduler needs at fire
// time without re-reading the source Definition.
//
// Fields are unexported behind accessor methods so the scheduler and
// metrics layer can hold a CronRule reference without coupling to its
// internals — important for the future per-tenant fan-out path which
// will need to build per-iteration Actions copies without mutating the
// shared CronRule.
type CronRule struct {
	id          string
	name        string
	description string
	mode        Mode
	scheduleStr string
	schedule    cronlib.Schedule
	actions     []Action
	cooldown    time.Duration
	fireEveryN  int
	metadata    map[string]any
}

// NewCronRule builds a CronRule from a Definition. Returns an error if
// any cron-specific validation fails: wrong Type, missing schedule,
// schedule fails to parse, cooldown unparseable, forbidden field
// present, or any action with an empty Type.
func NewCronRule(def Definition) (*CronRule, error) {
	if err := validateCronDefinition(def); err != nil {
		return nil, err
	}

	schedule, err := cronParser.Parse(def.Schedule)
	if err != nil {
		return nil, fmt.Errorf("rule %s schedule %q: %w", def.ID, def.Schedule, err)
	}

	var cooldown time.Duration
	if def.Cooldown != "" {
		cooldown, err = time.ParseDuration(def.Cooldown)
		if err != nil {
			return nil, fmt.Errorf("rule %s cooldown %q: %w", def.ID, def.Cooldown, err)
		}
	}

	return &CronRule{
		id:          def.ID,
		name:        def.Name,
		description: def.Description,
		mode:        def.Mode(),
		scheduleStr: def.Schedule,
		schedule:    schedule,
		actions:     slices.Clone(def.Actions),
		cooldown:    cooldown,
		fireEveryN:  def.FireEveryNEvents,
		metadata:    def.Metadata,
	}, nil
}

// ID returns the rule's stable identifier.
func (r *CronRule) ID() string { return r.id }

// Name returns the human-readable rule name.
func (r *CronRule) Name() string { return r.name }

// Description returns the rule's description string.
func (r *CronRule) Description() string { return r.description }

// Enabled reports whether the rule should be registered with the scheduler.
// Deprecated: prefer IsActive() which also returns true for shadow mode.
func (r *CronRule) Enabled() bool { return r.mode != ModeDisabled }

// IsActive reports whether this rule should be registered — true for both
// enabled and shadow modes. Use this in place of Enabled() wherever the
// scheduler or hot-reload path decides whether to Register the rule.
func (r *CronRule) IsActive() bool { return r.mode != ModeDisabled }

// IsShadow reports whether this rule is in shadow mode. Action dispatch is
// suppressed for shadow rules; the suppressed actions are logged and counted.
func (r *CronRule) IsShadow() bool { return r.mode == ModeShadow }

// ScheduleString returns the original cron expression, useful for metrics
// labels and substitution variables.
func (r *CronRule) ScheduleString() string { return r.scheduleStr }

// Schedule returns the parsed cron schedule. Used by the scheduler to
// compute next-fire times and by the missed-fire detector to compute
// expected fire intervals.
func (r *CronRule) Schedule() cronlib.Schedule { return r.schedule }

// Actions returns a clone of the action list to execute on each fire.
// The clone protects callers (per-iteration fan-out, in particular) from
// accidentally mutating the shared CronRule's slice across fires.
func (r *CronRule) Actions() []Action { return slices.Clone(r.actions) }

// Metadata returns the rule's metadata map, or nil if none was supplied.
// Useful for carrying provenance (e.g. operating-model entry IDs) from
// the source Definition through to action substitution.
func (r *CronRule) Metadata() map[string]any { return r.metadata }

// Cooldown returns the duration after which a fire may run again. Zero
// means no cooldown.
func (r *CronRule) Cooldown() time.Duration { return r.cooldown }

// FireEveryN returns the action-gating factor (every Nth tick fires
// actions). Zero or one means "fire every tick."
func (r *CronRule) FireEveryN() int { return r.fireEveryN }

// validateCronDefinition rejects cron-rule definitions that carry fields
// only meaningful for expression rules. Errors surface at config-load
// time so authors see them before any rule fires.
func validateCronDefinition(def Definition) error {
	if def.ID == "" {
		return fmt.Errorf("cron rule: id is required")
	}
	if def.Type != CronRuleType {
		return fmt.Errorf("cron rule %s: type %q, want %q", def.ID, def.Type, CronRuleType)
	}
	if strings.TrimSpace(def.Schedule) == "" {
		return fmt.Errorf("cron rule %s: schedule is required", def.ID)
	}
	if len(def.Actions) == 0 {
		return fmt.Errorf("cron rule %s: at least one action is required", def.ID)
	}
	for i, a := range def.Actions {
		if strings.TrimSpace(a.Type) == "" {
			return fmt.Errorf("cron rule %s: action[%d] missing type", def.ID, i)
		}
	}

	// Reject expression-rule fields that have no meaning under a clock-only
	// firing model. Failing loud at config-load beats silently ignoring
	// fields the author thought were doing something.
	var rejected []string
	if len(def.Conditions) > 0 {
		rejected = append(rejected, "conditions")
	}
	if def.Logic != "" {
		rejected = append(rejected, "logic")
	}
	if len(def.OnEnter) > 0 {
		rejected = append(rejected, "on_enter")
	}
	if len(def.OnExit) > 0 {
		rejected = append(rejected, "on_exit")
	}
	if len(def.OnRecovery) > 0 {
		rejected = append(rejected, "on_recovery")
	}
	if len(def.WhileTrue) > 0 {
		rejected = append(rejected, "while_true")
	}
	if def.RerunOnRecovery {
		rejected = append(rejected, "rerun_on_recovery")
	}
	if def.MaxIterations > 0 {
		rejected = append(rejected, "max_iterations")
	}
	if def.Entity.Pattern != "" {
		rejected = append(rejected, "entity.pattern")
	}
	if len(def.Entity.WatchBuckets) > 0 {
		rejected = append(rejected, "entity.watch_buckets")
	}
	if len(def.RelatedPatterns) > 0 {
		rejected = append(rejected, "related_patterns")
	}
	if len(rejected) > 0 {
		return fmt.Errorf("cron rule %s: forbidden fields present: %s "+
			"(cron rules accept only: schedule, actions, cooldown, fire_every_n_events, "+
			"name, description, enabled, metadata)",
			def.ID, strings.Join(rejected, ", "))
	}

	return nil
}
