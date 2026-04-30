package rule

import (
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/processor/rule/expression"
)

func validCronDef() Definition {
	return Definition{
		ID:       "weekly-planning",
		Type:     CronRuleType,
		Name:     "Weekly planning prompt",
		Enabled:  true,
		Schedule: "0 9 * * MON",
		Actions: []Action{
			{Type: ActionTypePublish, Subject: "system.cron.heartbeat"},
		},
	}
}

func TestNewCronRule_HappyPath(t *testing.T) {
	r, err := NewCronRule(validCronDef())
	if err != nil {
		t.Fatalf("NewCronRule = %v, want nil", err)
	}
	if r.ID() != "weekly-planning" {
		t.Errorf("ID = %q, want weekly-planning", r.ID())
	}
	if r.ScheduleString() != "0 9 * * MON" {
		t.Errorf("ScheduleString = %q, want %q", r.ScheduleString(), "0 9 * * MON")
	}
	if !r.Enabled() {
		t.Errorf("Enabled = false, want true")
	}
	if len(r.Actions()) != 1 {
		t.Errorf("Actions = %d, want 1", len(r.Actions()))
	}
	if r.Cooldown() != 0 {
		t.Errorf("Cooldown = %v, want 0", r.Cooldown())
	}
	// Schedule must produce a future fire time.
	next := r.Schedule().Next(time.Now())
	if !next.After(time.Now()) {
		t.Errorf("Schedule.Next(now) = %v, want a future time", next)
	}
}

func TestNewCronRule_DescriptorSchedules(t *testing.T) {
	for _, spec := range []string{"@hourly", "@daily", "@weekly", "@monthly", "@yearly"} {
		t.Run(spec, func(t *testing.T) {
			def := validCronDef()
			def.Schedule = spec
			r, err := NewCronRule(def)
			if err != nil {
				t.Fatalf("NewCronRule(%q) = %v, want nil", spec, err)
			}
			if r.ScheduleString() != spec {
				t.Errorf("ScheduleString = %q, want %q", r.ScheduleString(), spec)
			}
		})
	}
}

func TestNewCronRule_CooldownParsed(t *testing.T) {
	def := validCronDef()
	def.Cooldown = "30s"
	r, err := NewCronRule(def)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if r.Cooldown() != 30*time.Second {
		t.Errorf("Cooldown = %v, want 30s", r.Cooldown())
	}
}

func TestNewCronRule_RejectsBadSchedule(t *testing.T) {
	def := validCronDef()
	def.Schedule = "this is not cron"
	_, err := NewCronRule(def)
	if err == nil {
		t.Fatal("err = nil, want parse error")
	}
	if !strings.Contains(err.Error(), "schedule") {
		t.Errorf("error message %q should mention 'schedule'", err.Error())
	}
}

func TestNewCronRule_RejectsBadCooldown(t *testing.T) {
	def := validCronDef()
	def.Cooldown = "not a duration"
	_, err := NewCronRule(def)
	if err == nil {
		t.Fatal("err = nil, want cooldown parse error")
	}
}

func TestNewCronRule_RequiresID(t *testing.T) {
	def := validCronDef()
	def.ID = ""
	_, err := NewCronRule(def)
	if err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Errorf("err = %v, want id-required error", err)
	}
}

func TestNewCronRule_RequiresSchedule(t *testing.T) {
	def := validCronDef()
	def.Schedule = ""
	_, err := NewCronRule(def)
	if err == nil || !strings.Contains(err.Error(), "schedule is required") {
		t.Errorf("err = %v, want schedule-required error", err)
	}
}

func TestNewCronRule_RequiresAtLeastOneAction(t *testing.T) {
	def := validCronDef()
	def.Actions = nil
	_, err := NewCronRule(def)
	if err == nil || !strings.Contains(err.Error(), "action is required") {
		t.Errorf("err = %v, want action-required error", err)
	}
}

func TestNewCronRule_RejectsForbiddenFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Definition)
		field  string
	}{
		{"conditions", func(d *Definition) {
			d.Conditions = []expression.ConditionExpression{{Field: "x", Operator: "eq", Value: 1}}
		}, "conditions"},
		{"logic", func(d *Definition) { d.Logic = "and" }, "logic"},
		{"on_enter", func(d *Definition) { d.OnEnter = []Action{{Type: ActionTypePublish}} }, "on_enter"},
		{"on_exit", func(d *Definition) { d.OnExit = []Action{{Type: ActionTypePublish}} }, "on_exit"},
		{"on_recovery", func(d *Definition) { d.OnRecovery = []Action{{Type: ActionTypePublish}} }, "on_recovery"},
		{"while_true", func(d *Definition) { d.WhileTrue = []Action{{Type: ActionTypePublish}} }, "while_true"},
		{"rerun_on_recovery", func(d *Definition) { d.RerunOnRecovery = true }, "rerun_on_recovery"},
		{"max_iterations", func(d *Definition) { d.MaxIterations = 5 }, "max_iterations"},
		{"entity.pattern", func(d *Definition) { d.Entity.Pattern = "*.foo.*" }, "entity.pattern"},
		{"entity.watch_buckets", func(d *Definition) { d.Entity.WatchBuckets = []string{"X"} }, "entity.watch_buckets"},
		{"related_patterns", func(d *Definition) { d.RelatedPatterns = []string{"*"} }, "related_patterns"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := validCronDef()
			tc.mutate(&def)
			_, err := NewCronRule(def)
			if err == nil {
				t.Fatalf("err = nil, want forbidden-field error mentioning %q", tc.field)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("err = %q, want it to mention %q", err.Error(), tc.field)
			}
			if !strings.Contains(err.Error(), "forbidden fields present") {
				t.Errorf("err = %q, want it to mention 'forbidden fields present'", err.Error())
			}
		})
	}
}

func TestNewCronRule_AllowsCooldownAndFireEveryN(t *testing.T) {
	// Cooldown and fire_every_n_events are deliberately allowed on cron
	// rules because they're orthogonal to scheduling: cooldown suppresses
	// overlapping fires, fire_every_n_events gates the action counter.
	def := validCronDef()
	def.Cooldown = "5s"
	def.FireEveryNEvents = 3
	r, err := NewCronRule(def)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if r.FireEveryN() != 3 {
		t.Errorf("FireEveryN = %d, want 3", r.FireEveryN())
	}
	if r.Cooldown() != 5*time.Second {
		t.Errorf("Cooldown = %v, want 5s", r.Cooldown())
	}
}

func TestNewCronRule_RejectsWrongType(t *testing.T) {
	def := validCronDef()
	def.Type = "expression"
	_, err := NewCronRule(def)
	if err == nil || !strings.Contains(err.Error(), `type "expression"`) {
		t.Errorf("err = %v, want type-mismatch error", err)
	}
}

func TestNewCronRule_RejectsActionWithEmptyType(t *testing.T) {
	def := validCronDef()
	def.Actions = []Action{{Subject: "x"}} // Type empty — would bomb in executor
	_, err := NewCronRule(def)
	if err == nil || !strings.Contains(err.Error(), "missing type") {
		t.Errorf("err = %v, want missing-type error", err)
	}
}

func TestNewCronRule_PreservesMetadata(t *testing.T) {
	def := validCronDef()
	def.Metadata = map[string]any{"om_entry_id": "om-rhythms-abc123", "source": "onboarding"}
	r, err := NewCronRule(def)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	got := r.Metadata()
	if got["om_entry_id"] != "om-rhythms-abc123" || got["source"] != "onboarding" {
		t.Errorf("Metadata = %+v, want provenance preserved", got)
	}
}

func TestNewCronRule_ActionsAccessorReturnsClone(t *testing.T) {
	// Mutating the returned slice must not affect the rule's internal state.
	// Important for the future per-tenant fan-out path which will build
	// per-iteration Action copies.
	def := validCronDef()
	def.Actions = []Action{{Type: ActionTypePublish, Subject: "first"}}
	r, err := NewCronRule(def)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	a := r.Actions()
	a[0].Subject = "MUTATED"
	if r.Actions()[0].Subject != "first" {
		t.Errorf("Actions() returned the live slice; mutation leaked into the rule (%q)", r.Actions()[0].Subject)
	}
}

func TestCronRuleType_Constant(t *testing.T) {
	// The const value is part of the public config surface — pinning it
	// here so nobody renames "cron" without realizing it's a config break.
	if CronRuleType != "cron" {
		t.Errorf("CronRuleType = %q, want %q", CronRuleType, "cron")
	}
}
