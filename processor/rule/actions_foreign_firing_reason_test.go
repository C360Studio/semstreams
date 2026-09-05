package rule

import (
	"context"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/internal/semantictest"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

// spec: graph-ingest / Framework-minted runtime state carries the deployment's own authority and never writes to an imported firing entity

// TestPublishAgentSkipReasonSeparatesUnresolvableFromForeign pins the reason
// vocabulary of rule_foreign_firing_writes_skipped_total (#1169). The negative
// half is the one that was missing: a cron publish_agent dispatch carries no
// firing entity at all — cron_scheduler.go builds its ExecutionContext with
// Schedule alone — and before this test every such dispatch was counted and
// logged as foreign_authority, so an operator filtering the counter for
// import-boundary activity read cron noise.
//
// Four firing-entity states, each asserting BOTH labels and BOTH log messages,
// because a fix that merely renames the label for every skip would satisfy a
// one-sided assertion:
//
//   - cron: no EntityID → unresolvable_firing_entity, and foreign_authority does
//     not move. Restoring the hardcoded reason in foreignFiringSkipRecorder turns
//     this case red on the foreign_authority assertion, which is the
//     fails-without-fix pin.
//   - a structurally invalid EntityID → the same: no entity was established.
//   - a canonical imported entity → foreign_authority, unchanged by this change.
//   - a local entity → neither label, and rule.task.spawned is written.
func TestPublishAgentSkipReasonSeparatesUnresolvableFromForeign(t *testing.T) {
	t.Parallel()

	local := semantictest.EntityID(t, "acme", "ops", "domain", "system", "type", "001")
	foreign := semantictest.EntityID(t, "foreign", "dep9", "domain", "system", "type", "001")

	tests := []struct {
		name        string
		ec          *ExecutionContext
		wantReason  string // "" means the write proceeds and nothing is counted
		wantRuleID  string // the skip line's rule_id; from Schedule.ID on the cron path
		wantTriples int
	}{
		{
			name:        "cron dispatch has no firing entity by construction",
			ec:          &ExecutionContext{Schedule: &ScheduleContext{ID: "cron-rule", Spec: "@hourly"}},
			wantReason:  foreignFiringSkipReasonUnresolvable,
			wantRuleID:  "cron-rule",
			wantTriples: 0,
		},
		{
			name:        "structurally invalid firing entity cannot be established",
			ec:          &ExecutionContext{EntityID: "not-an-entity-id", State: &MatchState{RuleID: "malformed-rule"}},
			wantReason:  foreignFiringSkipReasonUnresolvable,
			wantRuleID:  "malformed-rule",
			wantTriples: 0,
		},
		{
			name:        "canonical imported entity is a foreign authority",
			ec:          &ExecutionContext{EntityID: foreign, State: &MatchState{RuleID: "import-rule"}},
			wantReason:  semtypes.EntityIDReasonForeignAuthority,
			wantRuleID:  "import-rule",
			wantTriples: 0,
		},
		{
			name:        "local entity is written and nothing is counted",
			ec:          &ExecutionContext{EntityID: local, State: &MatchState{RuleID: "local-rule"}},
			wantReason:  "",
			wantTriples: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			logs := &capturingHandler{}
			mockPub := &mockPublisher{}
			mockMut := &mockTripleMutator{}
			executor := NewActionExecutorFull(slog.New(logs), mockMut, mockPub, testExecutorPlatform())
			metrics := foreignFiringSkipTestMetrics()
			executor.setMetrics(metrics)

			action := Action{
				Type:    ActionTypePublishAgent,
				Subject: "agent.task.test",
				Role:    "researcher",
				Model:   "mock-model",
				Prompt:  "investigate",
			}
			require.NoError(t, executor.Execute(context.Background(), action, tc.ec))

			// The dispatch itself always goes out; the guard governs only the
			// framework's write BACK onto the firing entity.
			require.Len(t, mockPub.published, 1)
			require.Len(t, mockMut.addedTriples, tc.wantTriples,
				"rule.task.spawned is written only when the firing entity is local")

			foreignCount := testutil.ToFloat64(
				metrics.foreignFiringWritesSkippedTotal.WithLabelValues(semtypes.EntityIDReasonForeignAuthority))
			unresolvableCount := testutil.ToFloat64(
				metrics.foreignFiringWritesSkippedTotal.WithLabelValues(foreignFiringSkipReasonUnresolvable))
			foreignLines := logs.withMessage(foreignFiringSkipLogMessage)
			unresolvableLines := logs.withMessage(unresolvableFiringSkipLogMessage)

			switch tc.wantReason {
			case "":
				assert.InDelta(t, 0, foreignCount, 0.0001)
				assert.InDelta(t, 0, unresolvableCount, 0.0001)
				assert.Empty(t, foreignLines)
				assert.Empty(t, unresolvableLines)
				return
			case semtypes.EntityIDReasonForeignAuthority:
				assert.InDelta(t, 1, foreignCount, 0.0001, "an import is counted as foreign_authority")
				assert.InDelta(t, 0, unresolvableCount, 0.0001, "an import is not unresolvable")
				require.Len(t, foreignLines, 1, "one Info line per dispatch")
				assert.Empty(t, unresolvableLines)
				assertSkipLine(t, foreignLines[0], tc.wantReason, tc.wantRuleID)
			case foreignFiringSkipReasonUnresolvable:
				assert.InDelta(t, 0, foreignCount, 0.0001,
					"a dispatch with no establishable firing entity MUST NOT be counted as foreign_authority")
				assert.InDelta(t, 1, unresolvableCount, 0.0001, "it is still a counted skip, never silent")
				require.Len(t, unresolvableLines, 1, "one Info line per dispatch")
				assert.Empty(t, foreignLines, "the line must not claim a foreign authority")
				assertSkipLine(t, unresolvableLines[0], tc.wantReason, tc.wantRuleID)
			default:
				t.Fatalf("unexpected wantReason %q", tc.wantReason)
			}
		})
	}
}

// assertSkipLine checks the operator-facing shape shared by both skip lines:
// Info level, the reason field carrying the same token as the counter label,
// the rule attributed — on the cron path from Schedule.ID, since a cron
// context has no MatchState and RuleID() is empty there — and the declined
// write named: under run_scope inherit (the default here) rule.task.spawned
// alone.
func assertSkipLine(t *testing.T, line capturedRecord, wantReason, wantRuleID string) {
	t.Helper()
	assert.Equal(t, slog.LevelInfo, line.level)
	assert.Equal(t, wantRuleID, line.attrs["rule_id"], "the skip line is the operator's only rule attribution")
	assert.Equal(t, wantReason, line.attrs["reason"], "the log's reason is the counter's label")
	assert.Equal(t, "rule.task.spawned", line.attrs["skipped"])
}

// entity-id-audit:classify intentional-malformed "not-an-entity-id" line=59 column=45 surface=go-field:ExecutionContext.EntityID entity_id_invalid:arity structurally invalid firing entity fixture — the unresolvable skip reason's malformed case
