// Package rule - Tests for Rule Actions (TDD - RED Phase)
package rule

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/governance"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newActionsTestDecoder builds a message Decoder bound to a per-test
// registry pre-populated with the payloads the rule action tests need
// (agentic types for TaskMessage).
func newActionsTestDecoder(t *testing.T) *message.Decoder {
	t.Helper()
	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
	return message.NewDecoder(reg)
}

// T039: Test Action AddTriple - creates a relationship triple
func TestAction_AddTriple(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name       string
		action     Action
		entityID   string
		relatedID  string
		wantTriple message.Triple
		wantErr    bool
	}{
		{
			name: "create proximity relationship",
			action: Action{
				Type:      ActionTypeAddTriple,
				Predicate: "proximity.near",
				Object:    "$related.id",
				TTL:       "5m",
			},
			entityID:  "c360.platform1.robotics.mav1.drone.001",
			relatedID: "c360.platform1.robotics.mav1.drone.002",
			wantTriple: message.Triple{
				Subject:   "c360.platform1.robotics.mav1.drone.001",
				Predicate: "proximity.near",
				Object:    "c360.platform1.robotics.mav1.drone.002",
			},
			wantErr: false,
		},
		{
			name: "create fleet membership",
			action: Action{
				Type:      ActionTypeAddTriple,
				Predicate: "fleet.member_of",
				Object:    "fleet.alpha",
			},
			entityID:  "c360.platform1.robotics.mav1.drone.003",
			relatedID: "",
			wantTriple: message.Triple{
				Subject:   "c360.platform1.robotics.mav1.drone.003",
				Predicate: "fleet.member_of",
				Object:    "fleet.alpha",
			},
			wantErr: false,
		},
		{
			name: "missing predicate should fail",
			action: Action{
				Type:   ActionTypeAddTriple,
				Object: "test.value",
			},
			entityID: "c360.platform1.robotics.mav1.drone.004",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create action executor (will fail - type doesn't exist yet)
			executor := &ActionExecutor{}

			// Execute action
			triple, err := executor.ExecuteAddTriple(ctx, tt.action, &ExecutionContext{EntityID: tt.entityID, RelatedID: tt.relatedID})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantTriple.Subject, triple.Subject)
				assert.Equal(t, tt.wantTriple.Predicate, triple.Predicate)
				assert.Equal(t, tt.wantTriple.Object, triple.Object)

				// Verify TTL is set if specified
				if tt.action.TTL != "" {
					assert.NotNil(t, triple.ExpiresAt, "Triple should have expiration time")
					assert.True(t, triple.ExpiresAt.After(time.Now()), "Expiration should be in the future")
				}
			}
		})
	}
}

// T040: Test Action RemoveTriple - removes a relationship triple
func TestAction_RemoveTriple(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name      string
		action    Action
		entityID  string
		relatedID string
		wantErr   bool
	}{
		{
			name: "remove proximity relationship",
			action: Action{
				Type:      ActionTypeRemoveTriple,
				Predicate: "proximity.near",
				Object:    "$related.id",
			},
			entityID:  "c360.platform1.robotics.mav1.drone.001",
			relatedID: "c360.platform1.robotics.mav1.drone.002",
			wantErr:   false,
		},
		{
			name: "remove static relationship",
			action: Action{
				Type:      ActionTypeRemoveTriple,
				Predicate: "fleet.member_of",
				Object:    "fleet.alpha",
			},
			entityID: "c360.platform1.robotics.mav1.drone.003",
			wantErr:  false,
		},
		{
			name: "missing predicate should fail",
			action: Action{
				Type:   ActionTypeRemoveTriple,
				Object: "test.value",
			},
			entityID: "c360.platform1.robotics.mav1.drone.004",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &ActionExecutor{}

			err := executor.ExecuteRemoveTriple(ctx, tt.action, &ExecutionContext{EntityID: tt.entityID, RelatedID: tt.relatedID})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// T040a: Test Action struct
func TestAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		action Action
		valid  bool
	}{
		{
			name: "valid add_triple action",
			action: Action{
				Type:      ActionTypeAddTriple,
				Predicate: "proximity.near",
				Object:    "$related.id",
				TTL:       "5m",
			},
			valid: true,
		},
		{
			name: "valid remove_triple action",
			action: Action{
				Type:      ActionTypeRemoveTriple,
				Predicate: "proximity.near",
				Object:    "$related.id",
			},
			valid: true,
		},
		{
			name: "valid publish action",
			action: Action{
				Type:    ActionTypePublish,
				Subject: "alerts.low-battery",
				Properties: map[string]any{
					"severity": "high",
					"message":  "Battery critically low",
				},
			},
			valid: true,
		},
		{
			name: "valid update_triple action",
			action: Action{
				Type:      ActionTypeUpdateTriple,
				Predicate: "proximity.near",
				Object:    "$related.id",
				Properties: map[string]any{
					"distance": 50.0,
				},
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify action type is one of the valid constants
			validTypes := []string{
				ActionTypePublish,
				ActionTypeAddTriple,
				ActionTypeRemoveTriple,
				ActionTypeUpdateTriple,
			}

			if tt.valid {
				assert.Contains(t, validTypes, tt.action.Type)
			}
		})
	}
}

// T040b: Test Action constants
func TestActionConstants(t *testing.T) {
	t.Parallel()

	// Verify constants exist
	assert.Equal(t, "publish", ActionTypePublish)
	assert.Equal(t, "add_triple", ActionTypeAddTriple)
	assert.Equal(t, "remove_triple", ActionTypeRemoveTriple)
	assert.Equal(t, "update_triple", ActionTypeUpdateTriple)
}

// T040c: Test Action TTL parsing
func TestAction_TTLParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ttl         string
		wantError   bool
		minDuration time.Duration
		maxDuration time.Duration
	}{
		{
			name:        "5 minutes",
			ttl:         "5m",
			wantError:   false,
			minDuration: 4 * time.Minute,
			maxDuration: 6 * time.Minute,
		},
		{
			name:        "1 hour",
			ttl:         "1h",
			wantError:   false,
			minDuration: 55 * time.Minute,
			maxDuration: 65 * time.Minute,
		},
		{
			name:        "30 seconds",
			ttl:         "30s",
			wantError:   false,
			minDuration: 25 * time.Second,
			maxDuration: 35 * time.Second,
		},
		{
			name:      "invalid format",
			ttl:       "invalid",
			wantError: true,
		},
		{
			name:      "negative duration",
			ttl:       "-5m",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := Action{
				Type:      ActionTypeAddTriple,
				Predicate: "test.predicate",
				Object:    "test.value",
				TTL:       tt.ttl,
			}

			// Parse TTL (function doesn't exist yet)
			duration, err := action.ParseTTL()

			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.True(t, duration >= tt.minDuration, "Duration should be >= %v", tt.minDuration)
				assert.True(t, duration <= tt.maxDuration, "Duration should be <= %v", tt.maxDuration)
			}
		})
	}
}

// T040d: Test variable substitution in actions
func TestAction_VariableSubstitution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		template  string
		entityID  string
		relatedID string
		want      string
	}{
		{
			name:      "substitute related.id",
			template:  "$related.id",
			entityID:  "c360.platform1.robotics.mav1.drone.001",
			relatedID: "c360.platform1.robotics.mav1.drone.002",
			want:      "c360.platform1.robotics.mav1.drone.002",
		},
		{
			name:      "substitute entity.id",
			template:  "$entity.id",
			entityID:  "c360.platform1.robotics.mav1.drone.001",
			relatedID: "",
			want:      "c360.platform1.robotics.mav1.drone.001",
		},
		{
			name:      "no substitution",
			template:  "static.value",
			entityID:  "c360.platform1.robotics.mav1.drone.001",
			relatedID: "",
			want:      "static.value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ec := &ExecutionContext{EntityID: tt.entityID, RelatedID: tt.relatedID}
			result := ec.SubstituteVariables(tt.template)
			assert.Equal(t, tt.want, result)
		})
	}
}

// T040e: Test action execution context
func TestActionExecutor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func() *ActionExecutor
		wantErr bool
	}{
		{
			name: "valid executor",
			setup: func() *ActionExecutor {
				return &ActionExecutor{}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := tt.setup()
			assert.NotNil(t, executor)
		})
	}
}

// mockPublisher implements Publisher interface for testing
type mockPublisher struct {
	published []publishedMessage
	err       error
}

type publishedMessage struct {
	subject string
	data    []byte
}

func (m *mockPublisher) Publish(_ context.Context, subject string, data []byte) error {
	if m.err != nil {
		return m.err
	}
	m.published = append(m.published, publishedMessage{subject: subject, data: data})
	return nil
}

// mockVerdictAuditor captures emitted governance verdict events (ADR-055 §3a),
// or fails when err is set, so tests can assert the deny/approve audit emit and
// the best-effort "audit failure never flips the verdict" invariant.
type mockVerdictAuditor struct {
	emitted []governance.VerdictEvent
	err     error
}

func (m *mockVerdictAuditor) EmitVerdict(_ context.Context, ev governance.VerdictEvent) error {
	if m.err != nil {
		return m.err
	}
	m.emitted = append(m.emitted, ev)
	return nil
}

// T041: Test Action Publish - sends message to NATS subject
func TestAction_Publish(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name        string
		action      Action
		entityID    string
		relatedID   string
		wantSubject string
		wantErr     bool
		errMsg      string
	}{
		{
			name: "publish to static subject",
			action: Action{
				Type:    ActionTypePublish,
				Subject: "alerts.battery.low",
				Properties: map[string]any{
					"severity": "critical",
				},
			},
			entityID:    "c360.platform.robotics.mav1.drone.001",
			relatedID:   "",
			wantSubject: "alerts.battery.low",
			wantErr:     false,
		},
		{
			name: "publish with entity variable substitution",
			action: Action{
				Type:    ActionTypePublish,
				Subject: "events.$entity.id",
			},
			entityID:    "c360.platform.robotics.mav1.drone.001",
			relatedID:   "",
			wantSubject: "events.c360.platform.robotics.mav1.drone.001",
			wantErr:     false,
		},
		{
			name: "publish with related variable substitution",
			action: Action{
				Type:    ActionTypePublish,
				Subject: "proximity.$related.id",
			},
			entityID:    "c360.platform.robotics.mav1.drone.001",
			relatedID:   "c360.platform.robotics.mav1.drone.002",
			wantSubject: "proximity.c360.platform.robotics.mav1.drone.002",
			wantErr:     false,
		},
		{
			name: "missing subject should fail",
			action: Action{
				Type: ActionTypePublish,
			},
			entityID: "c360.platform.robotics.mav1.drone.001",
			wantErr:  true,
			errMsg:   "subject is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockPublisher{}
			executor := NewActionExecutorFull(nil, nil, mock)

			err := executor.Execute(ctx, tt.action, &ExecutionContext{EntityID: tt.entityID, RelatedID: tt.relatedID})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				require.Len(t, mock.published, 1, "should have published one message")
				assert.Equal(t, tt.wantSubject, mock.published[0].subject)
			}
		})
	}
}

// T042: Test Publish action payload format
func TestAction_Publish_PayloadFormat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublish,
		Subject: "test.subject",
		Properties: map[string]any{
			"custom_field": "custom_value",
			"priority":     1,
		},
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001", RelatedID: "related.002"})
	require.NoError(t, err)
	require.Len(t, mock.published, 1)

	// Parse the published payload
	var payload map[string]any
	err = json.Unmarshal(mock.published[0].data, &payload)
	require.NoError(t, err)

	// Verify required fields
	assert.Equal(t, "entity.001", payload["entity_id"])
	assert.Equal(t, "related.002", payload["related_id"])
	assert.Equal(t, "test.subject", payload["subject"])
	assert.Equal(t, "rule_engine", payload["source"])
	assert.NotEmpty(t, payload["timestamp"])

	// Verify properties are included
	props, ok := payload["properties"].(map[string]any)
	require.True(t, ok, "properties should be a map")
	assert.Equal(t, "custom_value", props["custom_field"])
	assert.Equal(t, float64(1), props["priority"]) // JSON numbers are float64
}

// TestExecutePublish_SubstitutesPropertyTemplates pins ADR-039's reject-shape
// invariant: `properties.call_id = "$message.call_id"` (and similar
// `$message.*` / `$entity.*` tokens) must resolve from ExecutionContext
// before publish. Pre-fix, the literal template string reached the wire
// and broke the agentic-loop verdict dispatcher's call-id demux for the
// canonical ADR-039 reject pattern (`publish` action + `deny`). Non-string
// property values (numbers, bools, nested maps) must pass through
// unchanged — the shallow-only contract matches docs/operations/17.
func TestExecutePublish_SubstitutesPropertyTemplates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)
	ec := &ExecutionContext{
		EntityID: "c360.platform.test.svc.entity.001",
		MessageData: map[string]any{
			"loop_id":   "loop-abc",
			"call_id":   "call-001",
			"tool_name": "bash",
		},
	}
	action := Action{
		Type:    ActionTypePublish,
		Subject: "agent.toolcall.rejected.$message.loop_id.$message.call_id",
		Properties: map[string]any{
			"decision":  "rejected",
			"call_id":   "$message.call_id",
			"loop_id":   "$message.loop_id",
			"tool_name": "$message.tool_name",
			"reason":    "writes outside worktree blocked",
			"priority":  3,    // non-string survives unchanged
			"sticky":    true, // non-string survives unchanged
		},
	}

	require.NoError(t, executor.executePublish(ctx, action, ec))
	require.Len(t, mock.published, 1, "publish must fire exactly once")

	got := mock.published[0]
	assert.Equal(t,
		"agent.toolcall.rejected.loop-abc.call-001",
		got.subject,
		"subject $message.* tokens must substitute (existing behaviour)")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(got.data, &payload))

	props, ok := payload["properties"].(map[string]any)
	require.True(t, ok, "properties must be a map")
	assert.Equal(t, "rejected", props["decision"], "static string passes through unchanged")
	assert.Equal(t, "call-001", props["call_id"],
		"$message.call_id MUST resolve — VerdictPayload.EffectiveCallID falls back to this field for publish-action verdicts")
	assert.Equal(t, "loop-abc", props["loop_id"], "$message.loop_id must resolve")
	assert.Equal(t, "bash", props["tool_name"], "$message.tool_name must resolve")
	assert.Equal(t, "writes outside worktree blocked", props["reason"], "static string passes through")
	assert.Equal(t, float64(3), props["priority"], "non-string number survives unchanged (JSON unmarshals to float64)")
	assert.Equal(t, true, props["sticky"], "non-string bool survives unchanged")
}

// TestExecutePublish_NilPropertiesNoOp pins that a publish action with no
// Properties block doesn't panic and emits a nil/empty properties map
// downstream. Guard against the substituteStringProperties helper
// trying to range over a nil map after Bug 2's fix.
func TestExecutePublish_NilPropertiesNoOp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)
	ec := &ExecutionContext{EntityID: "entity.001"}
	action := Action{
		Type:    ActionTypePublish,
		Subject: "test.subject",
	}

	require.NoError(t, executor.executePublish(ctx, action, ec))
	require.Len(t, mock.published, 1)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(mock.published[0].data, &payload))
	// JSON null deserialises to nil interface; either nil or empty map is fine.
	if v, ok := payload["properties"]; ok && v != nil {
		_, isMap := v.(map[string]any)
		assert.True(t, isMap, "properties when present must be a map, got %T", v)
	}
}

// T043: Test Publish action without publisher (no-op)
func TestAction_Publish_NoPublisher(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	executor := NewActionExecutor(nil) // No publisher configured

	action := Action{
		Type:    ActionTypePublish,
		Subject: "test.subject",
	}

	// Should not error, just log and return
	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
	require.NoError(t, err)
}

// T044: Test Publish action error handling
func TestAction_Publish_ErrorHandling(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	expectedErr := assert.AnError
	mock := &mockPublisher{err: expectedErr}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublish,
		Subject: "test.subject",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish to test.subject")
}

// mockTripleMutator implements TripleMutator interface for testing
type mockTripleMutator struct {
	addedTriples   []message.Triple
	addedRuleIDs   []string
	removedTriples []struct {
		subject   string
		predicate string
	}
	removedRuleIDs []string
	addErr         error
	removeErr      error

	// replaceOwnedCalls captures every ReplaceOwned invocation (ADR-056
	// Decision 3) so tests can assert on the owner identity, target entity,
	// predicate, and the constructed objects (including the clear sub-case,
	// where objects is empty). replaceOwnedErr lets a test force the mutator
	// to fail (e.g. simulating ErrorCodeEntityNotFound).
	replaceOwnedCalls []replaceOwnedCall
	replaceOwnedErr   error
}

// replaceOwnedCall records the arguments of one ReplaceOwned invocation.
type replaceOwnedCall struct {
	ruleID    string
	owner     string
	entityID  string
	predicate string
	objects   []message.Triple
}

func (m *mockTripleMutator) AddTriple(_ context.Context, ruleID string, triple message.Triple) (uint64, error) {
	if m.addErr != nil {
		return 0, m.addErr
	}
	m.addedTriples = append(m.addedTriples, triple)
	m.addedRuleIDs = append(m.addedRuleIDs, ruleID)
	return uint64(len(m.addedTriples)), nil
}

func (m *mockTripleMutator) RemoveTriple(_ context.Context, ruleID, subject, predicate string) (uint64, error) {
	if m.removeErr != nil {
		return 0, m.removeErr
	}
	m.removedTriples = append(m.removedTriples, struct {
		subject   string
		predicate string
	}{subject, predicate})
	m.removedRuleIDs = append(m.removedRuleIDs, ruleID)
	return 1, nil
}

func (m *mockTripleMutator) ReplaceOwned(_ context.Context, ruleID, owner, entityID, predicate string, objects []message.Triple) (uint64, error) {
	if m.replaceOwnedErr != nil {
		return 0, m.replaceOwnedErr
	}
	m.replaceOwnedCalls = append(m.replaceOwnedCalls, replaceOwnedCall{
		ruleID:    ruleID,
		owner:     owner,
		entityID:  entityID,
		predicate: predicate,
		objects:   objects,
	})
	return uint64(len(m.replaceOwnedCalls)), nil
}

// T045: Test Action UpdateTriple - updates a triple (remove + add)
func TestAction_UpdateTriple(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name          string
		action        Action
		entityID      string
		relatedID     string
		wantPredicate string
		wantObject    string
		wantErr       bool
		errMsg        string
	}{
		{
			name: "update status triple",
			action: Action{
				Type:      ActionTypeUpdateTriple,
				Predicate: "status.battery",
				Object:    "low",
			},
			entityID:      "c360.platform.robotics.mav1.drone.001",
			wantPredicate: "status.battery",
			wantObject:    "low",
			wantErr:       false,
		},
		{
			name: "update with variable substitution",
			action: Action{
				Type:      ActionTypeUpdateTriple,
				Predicate: "fleet.membership",
				Object:    "$related.id",
			},
			entityID:      "c360.platform.robotics.mav1.drone.001",
			relatedID:     "c360.platform.fleet.alpha",
			wantPredicate: "fleet.membership",
			wantObject:    "c360.platform.fleet.alpha",
			wantErr:       false,
		},
		{
			name: "update with TTL",
			action: Action{
				Type:      ActionTypeUpdateTriple,
				Predicate: "alert.status",
				Object:    "active",
				TTL:       "5m",
			},
			entityID:      "c360.platform.robotics.mav1.drone.001",
			wantPredicate: "alert.status",
			wantObject:    "active",
			wantErr:       false,
		},
		{
			name: "missing predicate should fail",
			action: Action{
				Type:   ActionTypeUpdateTriple,
				Object: "test.value",
			},
			entityID: "c360.platform.robotics.mav1.drone.001",
			wantErr:  true,
			errMsg:   "predicate is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockTripleMutator{}
			executor := NewActionExecutorWithMutator(nil, mock)

			err := executor.Execute(ctx, tt.action, &ExecutionContext{EntityID: tt.entityID, RelatedID: tt.relatedID})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)

				// Verify remove was called
				require.Len(t, mock.removedTriples, 1, "should have removed one triple")
				assert.Equal(t, tt.entityID, mock.removedTriples[0].subject)
				assert.Equal(t, tt.wantPredicate, mock.removedTriples[0].predicate)

				// Verify add was called
				require.Len(t, mock.addedTriples, 1, "should have added one triple")
				assert.Equal(t, tt.entityID, mock.addedTriples[0].Subject)
				assert.Equal(t, tt.wantPredicate, mock.addedTriples[0].Predicate)
				assert.Equal(t, tt.wantObject, mock.addedTriples[0].Object)

				// Verify TTL if specified
				if tt.action.TTL != "" {
					assert.NotNil(t, mock.addedTriples[0].ExpiresAt, "Triple should have expiration")
				}
			}
		})
	}
}

// T046: Test UpdateTriple without mutator (no-op)
func TestAction_UpdateTriple_NoMutator(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	executor := NewActionExecutor(nil) // No mutator configured

	action := Action{
		Type:      ActionTypeUpdateTriple,
		Predicate: "test.predicate",
		Object:    "test.value",
	}

	// Should not error, just log and return
	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
	require.NoError(t, err)
}

// T047: Test UpdateTriple continues even if remove fails (triple may not exist)
func TestAction_UpdateTriple_RemoveFailsContinues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mock := &mockTripleMutator{
		removeErr: assert.AnError, // Simulate remove failure
	}
	executor := NewActionExecutorWithMutator(nil, mock)

	action := Action{
		Type:      ActionTypeUpdateTriple,
		Predicate: "test.predicate",
		Object:    "test.value",
	}

	// Should still succeed - add should still be called
	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
	require.NoError(t, err)

	// Add should still have been called
	require.Len(t, mock.addedTriples, 1, "should have added triple even if remove failed")
}

// T048: Test UpdateTriple fails if add fails
func TestAction_UpdateTriple_AddFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mock := &mockTripleMutator{
		addErr: assert.AnError,
	}
	executor := NewActionExecutorWithMutator(nil, mock)

	action := Action{
		Type:      ActionTypeUpdateTriple,
		Predicate: "test.predicate",
		Object:    "test.value",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "add updated triple")
}

// T049: Test Action PublishAgent - triggers an agentic loop
func TestAction_PublishAgent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name        string
		action      Action
		entityID    string
		relatedID   string
		wantSubject string
		wantErr     bool
		errMsg      string
	}{
		{
			name: "publish agent task with general role",
			action: Action{
				Type:    ActionTypePublishAgent,
				Subject: "agent.task.anomaly",
				Role:    "general",
				Model:   "mock-model",
				Prompt:  "Analyze anomaly for entity $entity.id",
			},
			entityID:    "c360.platform.sensor.temp.001",
			wantSubject: "agent.task.anomaly",
			wantErr:     false,
		},
		{
			name: "publish agent task with architect role",
			action: Action{
				Type:    ActionTypePublishAgent,
				Subject: "agent.task.design",
				Role:    "architect",
				Model:   "gpt-4",
				Prompt:  "Design solution for $entity.id",
			},
			entityID:    "c360.platform.system.001",
			wantSubject: "agent.task.design",
			wantErr:     false,
		},
		{
			name: "publish agent task with variable substitution in subject",
			action: Action{
				Type:    ActionTypePublishAgent,
				Subject: "agent.task.$entity.id",
				Role:    "general",
				Model:   "mock-model",
				Prompt:  "Analyze $entity.id",
			},
			entityID:    "sensor-001",
			wantSubject: "agent.task.sensor-001",
			wantErr:     false,
		},
		{
			name: "missing subject should fail",
			action: Action{
				Type:   ActionTypePublishAgent,
				Role:   "general",
				Model:  "mock-model",
				Prompt: "Test prompt",
			},
			entityID: "entity.001",
			wantErr:  true,
			errMsg:   "subject is required",
		},
		{
			name: "missing role should fail",
			action: Action{
				Type:    ActionTypePublishAgent,
				Subject: "agent.task.test",
				Model:   "mock-model",
				Prompt:  "Test prompt",
			},
			entityID: "entity.001",
			wantErr:  true,
			errMsg:   "role is required",
		},
		{
			name: "missing model should fail",
			action: Action{
				Type:    ActionTypePublishAgent,
				Subject: "agent.task.test",
				Role:    "general",
				Prompt:  "Test prompt",
			},
			entityID: "entity.001",
			wantErr:  true,
			errMsg:   "model is required",
		},
		{
			name: "missing prompt should fail",
			action: Action{
				Type:    ActionTypePublishAgent,
				Subject: "agent.task.test",
				Role:    "general",
				Model:   "mock-model",
			},
			entityID: "entity.001",
			wantErr:  true,
			errMsg:   "prompt is required",
		},
		{
			name: "custom role accepted (no closed-set validation)",
			action: Action{
				Type:    ActionTypePublishAgent,
				Subject: "agent.task.test",
				Role:    "ops",
				Model:   "mock-model",
				Prompt:  "Test prompt",
			},
			entityID:    "entity.001",
			wantErr:     false,
			wantSubject: "agent.task.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockPublisher{}
			executor := NewActionExecutorFull(nil, nil, mock)

			err := executor.Execute(ctx, tt.action, &ExecutionContext{EntityID: tt.entityID, RelatedID: tt.relatedID})

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				require.Len(t, mock.published, 1, "should have published one message")
				assert.Equal(t, tt.wantSubject, mock.published[0].subject)
			}
		})
	}
}

// T050: Test PublishAgent payload format (TaskMessage)
func TestAction_PublishAgent_PayloadFormat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.test",
		Role:    "general",
		Model:   "mock-model",
		Prompt:  "Analyze entity $entity.id in location $related.id",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "sensor.temp.001", RelatedID: "warehouse.zone.A"})
	require.NoError(t, err)
	require.Len(t, mock.published, 1)

	// Parse the published payload (BaseMessage envelope)
	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok, "expected *agentic.TaskMessage, got %T", baseMsg.Payload())

	// Verify TaskMessage fields
	assert.NotEmpty(t, task.TaskID, "task_id should be set")
	assert.Contains(t, task.TaskID, "rule-", "task_id should start with 'rule-'")
	assert.Equal(t, "general", task.Role)
	assert.Equal(t, "mock-model", task.Model)
	assert.Equal(t, "Analyze entity sensor.temp.001 in location warehouse.zone.A", task.Prompt)
}

// T050b: Test PublishAgent resolves action.Tools → TaskMessage.Tools via the
// supplied tool registry. Unknown names are dropped rather than failing the
// spawn.
func TestAction_PublishAgent_ToolsResolved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	toolName := "test_action_publish_agent_resolver"
	reg := agentictools.NewExecutorRegistry()
	if err := reg.RegisterTool(toolName, &stubToolExecutor{name: toolName}); err != nil {
		t.Fatalf("register test tool: %v", err)
	}

	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)
	executor.SetToolRegistry(reg)
	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.test",
		Role:    "researcher",
		Model:   "mock-model",
		Prompt:  "p",
		Tools:   []string{toolName, "tool_that_does_not_exist"},
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"})
	require.NoError(t, err)
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	// Known name resolved; unknown name silently dropped.
	require.Len(t, task.Tools, 1, "only the known tool should survive resolution")
	assert.Equal(t, toolName, task.Tools[0].Name)
}

// T050c: Test PublishAgent with empty action.Tools leaves TaskMessage.Tools
// unset — preserves the pre-existing fall-through to global discovery inside
// agentic-loop.
func TestAction_PublishAgent_EmptyToolsLeavesUnset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.test",
		Role:    "general",
		Model:   "mock-model",
		Prompt:  "p",
		// Tools omitted
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)
	assert.Empty(t, task.Tools, "empty action.Tools should leave TaskMessage.Tools unset")
}

// TestAction_PublishAgent_ActionAllowlist verifies that
// action.ActionAllowlist threads onto TaskMessage.Metadata under
// agentic.MetadataKeyDecideActionAllowlist as a []any (the JSON wire
// shape). Empty/nil leaves Metadata unset for that key.
//
// The smoke-#7 wedge motivated this: a planner LLM hallucinated an
// out-of-vocabulary action ("fan_out" instead of the persona's
// "planned"). With this allowlist plumbed through, the decide
// executor will reject and let the LLM correct.
func TestAction_PublishAgent_ActionAllowlist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:            ActionTypePublishAgent,
		Subject:         "agent.task.test",
		Role:            "dev-via-spec-planner",
		Model:           "mock-model",
		Prompt:          "p",
		ActionAllowlist: []string{"planned", "needs_clarification"},
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	require.NotNil(t, task.Metadata, "Metadata should be initialised when ActionAllowlist is set")
	rawAllowlist, ok := task.Metadata[agentic.MetadataKeyDecideActionAllowlist]
	require.True(t, ok, "MetadataKeyDecideActionAllowlist should be set")

	// The metadata round-trips through JSON as []any (the BaseMessage
	// wire shape). The decide executor's coerceAllowlist handles the
	// type-erasure on the read side. We assert the wire shape here so
	// future refactors don't accidentally break the producer/consumer
	// contract.
	allowlist, ok := rawAllowlist.([]any)
	require.True(t, ok, "expected []any after JSON round-trip; got %T", rawAllowlist)
	require.Len(t, allowlist, 2)
	assert.Equal(t, "planned", allowlist[0])
	assert.Equal(t, "needs_clarification", allowlist[1])
}

// TestAction_PublishAgent_ResponseFormat verifies that
// action.ResponseFormat threads onto TaskMessage.ResponseFormat as a
// pointer pass-through. ADR-034. Both helpers (NewJSONSchemaFormat,
// NewJSONObjectFormat) round-trip through the BaseMessage envelope.
func TestAction_PublishAgent_ResponseFormat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	rf := agentic.NewJSONSchemaFormat("decision", map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"action": map[string]any{"type": "string"},
		},
		"required": []any{"action"},
	})

	action := Action{
		Type:           ActionTypePublishAgent,
		Subject:        "agent.task.test",
		Role:           "planner",
		Model:          "mock-model",
		Prompt:         "p",
		ResponseFormat: rf,
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	require.NotNil(t, task.ResponseFormat, "ResponseFormat should round-trip onto TaskMessage")
	assert.Equal(t, agentic.ResponseFormatJSONSchema, task.ResponseFormat.Type)
	assert.Equal(t, "decision", task.ResponseFormat.Name)
	assert.True(t, task.ResponseFormat.Strict, "NewJSONSchemaFormat should produce strict-mode")
	require.NotNil(t, task.ResponseFormat.Schema)
	assert.Equal(t, "object", task.ResponseFormat.Schema["type"])
}

// TestAction_PublishAgent_EmptyResponseFormat verifies that an unset
// ResponseFormat leaves TaskMessage.ResponseFormat nil (back-compat:
// existing flows that don't opt in keep their pre-ADR-034 tool-calling
// behaviour).
func TestAction_PublishAgent_EmptyResponseFormat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.test",
		Role:    "general",
		Model:   "mock-model",
		Prompt:  "p",
		// ResponseFormat omitted
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	assert.Nil(t, task.ResponseFormat, "ResponseFormat should remain nil when action.ResponseFormat unset")
}

// TestAction_PublishAgent_ToolChoice verifies that action.ToolChoice
// threads onto TaskMessage.ToolChoice as a pointer pass-through.
// ADR-023 + #132. Both Mode "required" and Mode "function" round-trip
// through the BaseMessage envelope.
func TestAction_PublishAgent_ToolChoice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tc   *agentic.ToolChoice
	}{
		{
			name: "required",
			tc:   &agentic.ToolChoice{Mode: "required"},
		},
		{
			name: "function",
			tc:   &agentic.ToolChoice{Mode: "function", FunctionName: "decide"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			mock := &mockPublisher{}
			executor := NewActionExecutorFull(nil, nil, mock)

			action := Action{
				Type:       ActionTypePublishAgent,
				Subject:    "agent.task.test",
				Role:       "researcher",
				Model:      "mock-model",
				Prompt:     "p",
				ToolChoice: tt.tc,
			}

			require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
			require.Len(t, mock.published, 1)

			baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
			require.NoError(t, err)
			task, ok := baseMsg.Payload().(*agentic.TaskMessage)
			require.True(t, ok)

			require.NotNil(t, task.ToolChoice, "ToolChoice should round-trip onto TaskMessage")
			assert.Equal(t, tt.tc.Mode, task.ToolChoice.Mode)
			assert.Equal(t, tt.tc.FunctionName, task.ToolChoice.FunctionName)
		})
	}
}

// TestAction_PublishAgent_EmptyToolChoice verifies that an unset
// ToolChoice leaves TaskMessage.ToolChoice nil (back-compat: existing
// flows that don't opt in keep model-decides "auto" behaviour).
func TestAction_PublishAgent_EmptyToolChoice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.test",
		Role:    "general",
		Model:   "mock-model",
		Prompt:  "p",
		// ToolChoice omitted
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	assert.Nil(t, task.ToolChoice, "ToolChoice should remain nil when action.ToolChoice unset")
}

// --- #134 / ADR-046 Phase 1: for_each iteration tests ---

// TestAction_PublishAgent_ForEach_DispatchesPerItem verifies the core
// fan-out contract: a publish_agent with for_each over a list-typed
// triple dispatches one TaskMessage per item with $<for_each_var>
// bound to the current value. Each TaskMessage carries the
// substituted prompt + a distinct task ID.
func TestAction_PublishAgent_ForEach_DispatchesPerItem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	// Trigger entity carries the subtopics list as a JSON-encoded
	// triple Object — the wire shape the decide tool emits.
	ec := &ExecutionContext{
		EntityID: "acme.ops.robot.gcs.coordinator.001",
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{
					Predicate: "coordinator.decision.subtopics",
					Object:    `["hydraulics","pneumatics","electrics"]`,
				},
			},
		},
	}

	action := Action{
		Type:       ActionTypePublishAgent,
		Subject:    "agent.task.investigator",
		Role:       "researcher-investigate",
		Model:      "mock-model",
		Prompt:     "Investigate subtopic: $subtopic",
		ForEach:    "$entity.triple.coordinator.decision.subtopics",
		ForEachVar: "subtopic",
	}

	require.NoError(t, executor.Execute(ctx, action, ec))
	require.Len(t, mock.published, 3, "one TaskMessage per subtopic")

	decoder := newActionsTestDecoder(t)
	seen := make([]string, 0, 3)
	for _, msg := range mock.published {
		baseMsg, err := decoder.Decode(msg.data)
		require.NoError(t, err)
		task, ok := baseMsg.Payload().(*agentic.TaskMessage)
		require.True(t, ok)
		seen = append(seen, task.Prompt)
	}
	assert.Equal(t, []string{
		"Investigate subtopic: hydraulics",
		"Investigate subtopic: pneumatics",
		"Investigate subtopic: electrics",
	}, seen, "prompt must reflect per-iteration substitution")
}

// TestAction_PublishAgent_ForEach_EmptyListNoDispatch verifies the
// degenerate case: an empty list (decomposer found nothing) produces
// zero dispatches and no error. Lets rule authors write
// for_each-driven flows without special-casing the empty path.
func TestAction_PublishAgent_ForEach_EmptyListNoDispatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	ec := &ExecutionContext{
		EntityID: "e.1",
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: "coordinator.decision.subtopics", Object: `[]`},
			},
		},
	}

	action := Action{
		Type:       ActionTypePublishAgent,
		Subject:    "agent.task.x",
		Role:       "researcher",
		Model:      "mock-model",
		Prompt:     "p $subtopic",
		ForEach:    "$entity.triple.coordinator.decision.subtopics",
		ForEachVar: "subtopic",
	}

	require.NoError(t, executor.Execute(ctx, action, ec))
	assert.Empty(t, mock.published, "empty list should produce no dispatches")
}

// TestAction_PublishAgent_ForEach_MissingVarErrors verifies the
// authoring guard: setting ForEach without ForEachVar is a hard
// error (template substitution wouldn't have anything to bind to).
func TestAction_PublishAgent_ForEach_MissingVarErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.x",
		Role:    "researcher",
		Model:   "mock-model",
		Prompt:  "p",
		ForEach: "$entity.triple.subtopics",
		// ForEachVar deliberately omitted
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "for_each_var is required")
}

// TestAction_PublishAgent_ForEach_NonListDegeneratesToSingle verifies
// that a ForEach reference pointing at a non-list (scalar string
// predicate, missing triple) degenerates to a single dispatch with
// the iter-var unbound. The unresolved $<varName> in the Prompt
// surfaces via the standard unresolved-template warning — author
// error stays loud, doesn't silently no-op.
func TestAction_PublishAgent_ForEach_NonListDegeneratesToSingle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	ec := &ExecutionContext{
		EntityID: "e.1",
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: "agent.role", Object: "researcher"},
			},
		},
	}

	action := Action{
		Type:       ActionTypePublishAgent,
		Subject:    "agent.task.x",
		Role:       "researcher",
		Model:      "mock-model",
		Prompt:     "investigate $x",            // intentionally references unbound iter-var
		ForEach:    "$entity.triple.agent.role", // scalar, not list
		ForEachVar: "x",
	}

	require.NoError(t, executor.Execute(ctx, action, ec))
	require.Len(t, mock.published, 1, "non-list reference degenerates to single dispatch")

	// Verify the literal $<varName> token reached the dispatched
	// prompt — that's the contract for "author error stays loud."
	// Without an overlay binding, substitution leaves $x verbatim,
	// the unresolved-template warning fires, and the operator sees
	// the predicate-name typo (or whatever caused the non-list
	// resolution) in the dispatched task body.
	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)
	assert.Equal(t, "investigate $x", task.Prompt,
		"iter-var must remain unbound on degenerate path so the unresolved-template warning surfaces the author error")
}

// TestAction_PublishAgent_RelatedLoops verifies that
// action.RelatedLoops threads onto TaskMessage.Metadata under
// agentic.MetadataKeyRelatedLoops as a map[string]any (the JSON wire
// shape after BaseMessage round-trip). String-to-string by design.
//
// Use case: dev-via-spec architect needs the researcher's loop ID
// for harness selection (semteams smoke #8 run-2); challenger
// cross-grounding back to planner; ops-agent / ADR-033 chain_id
// stability.
func TestAction_PublishAgent_RelatedLoops(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.architect",
		Role:    "architect",
		Model:   "mock-model",
		Prompt:  "p",
		RelatedLoops: map[string]string{
			"researcher": "loop-research-abc",
			"planner":    "loop-plan-xyz",
		},
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	require.NotNil(t, task.Metadata, "Metadata should be initialised when RelatedLoops is set")
	rawLineage, ok := task.Metadata[agentic.MetadataKeyRelatedLoops]
	require.True(t, ok, "MetadataKeyRelatedLoops should be set")

	// JSON round-trip turns map[string]string into map[string]any.
	// Each value is a Go string, just typed as any — readers coerce.
	lineage, ok := rawLineage.(map[string]any)
	require.True(t, ok, "expected map[string]any after JSON round-trip; got %T", rawLineage)
	assert.Equal(t, "loop-research-abc", lineage["researcher"])
	assert.Equal(t, "loop-plan-xyz", lineage["planner"])
}

// TestAction_PublishAgent_RelatedLoops_VariableSubstitution verifies
// that substitution tokens in RelatedLoops values resolve at execute
// time. The load-bearing case: rule configs declare
// `"researcher": "$entity.triple.research_loop_id"` (or any
// supported substitution token) and the resolved ID flows onto the
// Metadata.
func TestAction_PublishAgent_RelatedLoops_VariableSubstitution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.architect",
		Role:    "architect",
		Model:   "mock-model",
		Prompt:  "p",
		RelatedLoops: map[string]string{
			"researcher": "$entity.id",
		},
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "loop-research-from-entity"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	rawLineage := task.Metadata[agentic.MetadataKeyRelatedLoops]
	lineage, ok := rawLineage.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "loop-research-from-entity", lineage["researcher"],
		"$entity.id should substitute to ExecutionContext.EntityID")
}

// TestAction_PublishAgent_EmptyRelatedLoops verifies that an unset
// RelatedLoops leaves Metadata's lineage key unset (back-compat:
// existing flows that don't opt in see no Metadata change).
func TestAction_PublishAgent_EmptyRelatedLoops(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.test",
		Role:    "general",
		Model:   "mock-model",
		Prompt:  "p",
		// RelatedLoops omitted
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	if task.Metadata != nil {
		_, has := task.Metadata[agentic.MetadataKeyRelatedLoops]
		assert.False(t, has, "no related_loops field should be set on Metadata when RelatedLoops is empty")
	}
}

// --- gh#354: publish_agent.properties → TaskMessage.Metadata ---
//
// These tests lock the rule-side half of the round-trip: author-supplied
// `properties` reach the dispatched TaskMessage.Metadata. The consumer
// half — TaskMessage.Metadata filled onto every spawned ToolCall.Metadata
// with no-clobber semantics — is locked in the agentic-loop package by
// TestHandleTask_MetadataCachedAndPropagated (proves arbitrary domain keys
// like tenant_id/domain reach the published tool.execute payload.metadata,
// the identical mechanism deliverable_type rides). Together they form the
// rule property → TaskMessage.Metadata → ToolCall.Metadata chain gh#354
// asks for. Each test drives its own package's production wire.

// TestAction_PublishAgent_Properties_StampedToMetadata is the headline
// gh#354 case: a domain key in `properties` reaches TaskMessage.Metadata
// (after the production BaseMessage round-trip) so a tool keying off
// ToolCall.Metadata["deliverable_type"] selects its deterministic
// validator under rule dispatch, at parity with component dispatch.
func TestAction_PublishAgent_Properties_StampedToMetadata(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.submit",
		Role:    "requirements-author",
		Model:   "mock-model",
		Prompt:  "p",
		Properties: map[string]any{
			"deliverable_type": "requirements",
		},
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	require.NotNil(t, task.Metadata, "Metadata should be initialised when properties carry domain keys")
	assert.Equal(t, "requirements", task.Metadata["deliverable_type"],
		"author-supplied properties key must reach TaskMessage.Metadata")
}

// TestAction_PublishAgent_Properties_VariableSubstitution verifies that
// string property values resolve substitution tokens at execute time, so
// configs can carry `properties: {origin: "$entity.id"}` and the resolved
// value flows onto the Metadata.
func TestAction_PublishAgent_Properties_VariableSubstitution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.submit",
		Role:    "general",
		Model:   "mock-model",
		Prompt:  "p",
		Properties: map[string]any{
			"origin": "$entity.id",
		},
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "acme.ops.robot.gcs.coordinator.001"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	assert.Equal(t, "acme.ops.robot.gcs.coordinator.001", task.Metadata["origin"],
		"$entity.id should substitute in property values")
}

// TestAction_PublishAgent_Properties_ForEachIterVar verifies that
// property substitution is iter-var aware: a for_each dispatch can vary a
// metadata key per item (e.g. tag each spawned investigator with its
// subtopic). This is the value-add of stamping after substitution rather
// than carrying the literal template.
func TestAction_PublishAgent_Properties_ForEachIterVar(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	ec := &ExecutionContext{
		EntityID: "acme.ops.robot.gcs.coordinator.001",
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{
					Predicate: "coordinator.decision.subtopics",
					Object:    `["hydraulics","pneumatics"]`,
				},
			},
		},
	}

	action := Action{
		Type:       ActionTypePublishAgent,
		Subject:    "agent.task.investigator",
		Role:       "investigator",
		Model:      "mock-model",
		Prompt:     "Investigate $subtopic",
		ForEach:    "$entity.triple.coordinator.decision.subtopics",
		ForEachVar: "subtopic",
		Properties: map[string]any{
			"subtopic": "$subtopic",
		},
	}

	require.NoError(t, executor.Execute(ctx, action, ec))
	require.Len(t, mock.published, 2)

	decoder := newActionsTestDecoder(t)
	seen := make([]string, 0, 2)
	for _, msg := range mock.published {
		baseMsg, err := decoder.Decode(msg.data)
		require.NoError(t, err)
		task, ok := baseMsg.Payload().(*agentic.TaskMessage)
		require.True(t, ok)
		got, ok := task.Metadata["subtopic"].(string)
		require.True(t, ok, "subtopic metadata should be a string")
		seen = append(seen, got)
	}
	assert.ElementsMatch(t, []string{"hydraulics", "pneumatics"}, seen,
		"each spawn's metadata.subtopic must reflect its iter-var binding")
}

// TestAction_PublishAgent_Properties_NonStringPassThrough verifies the
// shallow-only contract: non-string property values are carried unchanged
// (numbers survive the JSON round-trip as float64; bools as bool). Only
// top-level strings are substituted.
func TestAction_PublishAgent_Properties_NonStringPassThrough(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.submit",
		Role:    "general",
		Model:   "mock-model",
		Prompt:  "p",
		Properties: map[string]any{
			"priority": 7,
			"strict":   true,
		},
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	assert.Equal(t, float64(7), task.Metadata["priority"], "numbers round-trip as float64")
	assert.Equal(t, true, task.Metadata["strict"], "bools pass through unchanged")
}

// TestAction_PublishAgent_Properties_ReservedKeysSkipped verifies that an
// author cannot overwrite framework-reserved `agent.*` metadata via
// properties: a property attempting to set the decide allowlist is
// dropped, while the action's own ActionAllowlist remains authoritative.
// This is the no-clobber guarantee from the acceptance criteria.
func TestAction_PublishAgent_Properties_ReservedKeysSkipped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:            ActionTypePublishAgent,
		Subject:         "agent.task.coordinator",
		Role:            "coordinator",
		Model:           "mock-model",
		Prompt:          "p",
		ActionAllowlist: []string{"investigate", "synthesize"},
		Properties: map[string]any{
			// Hostile author trying to widen the decide gate via properties.
			// The framework ALSO writes this key (from ActionAllowlist), so
			// stamp-ordering alone would protect it — this asserts the outcome.
			agentic.MetadataKeyDecideActionAllowlist: []any{"exfiltrate"},
			// A reserved key the framework does NOT write to task.Metadata on
			// this path (run association is a struct field, not metadata). If
			// the isReservedTaskMetadataKey skip were removed, ordering would
			// NOT save this — it must be dropped purely by the skip. This makes
			// the skip load-bearing in the test, independent of write-order.
			agentic.MetadataKeyRunID: "should-be-dropped",
			// And a benign domain key alongside it.
			"deliverable_type": "plan",
		},
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	// The framework allowlist (from action.ActionAllowlist) wins; the
	// property's attempt to override it is ignored.
	rawAllowlist, ok := task.Metadata[agentic.MetadataKeyDecideActionAllowlist].([]any)
	require.True(t, ok, "decide allowlist must remain the framework-set []any")
	assert.ElementsMatch(t, []any{"investigate", "synthesize"}, rawAllowlist,
		"author properties must not overwrite the framework decide allowlist")
	// The reserved run-id key the framework never writes here must be absent —
	// proves the skip drops reserved keys, not just write-ordering.
	_, hasRunID := task.Metadata[agentic.MetadataKeyRunID]
	assert.False(t, hasRunID,
		"reserved agent.* key must be dropped by the skip even when the framework doesn't write it")
	// The benign domain key still flows through.
	assert.Equal(t, "plan", task.Metadata["deliverable_type"],
		"non-reserved property keys are unaffected by the reserved-key skip")
}

// TestAction_PublishAgent_EmptyProperties_NoMetadataChange verifies
// back-compat: a publish_agent action with no properties leaves Metadata
// exactly as the framework writes it (nil here, since no other
// metadata-stamping field is set) — opt-in, no surprise keys.
func TestAction_PublishAgent_EmptyProperties_NoMetadataChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.test",
		Role:    "general",
		Model:   "mock-model",
		Prompt:  "p",
		// Properties omitted
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	assert.Nil(t, task.Metadata, "no properties (and no other metadata field) leaves Metadata unset")
}

// TestAction_PublishAgent_ParentLoopIDFromLoopEntity asserts that when a
// publish_agent action fires on a loop-execution-shaped trigger entity, the
// resulting TaskMessage carries task.ParentLoopID extracted from the
// trigger entity's loop_id segment. This gives rule-fanned spawns the
// same parent linkage that depth-tracked subagent spawns already have via
// the SetParentLoopID path, so product code (semteams chain consumers per
// ADR-038, future cross-arc personas) can derive chain anchors by walking
// agent.loop.parent without per-rule lineage threading.
func TestAction_PublishAgent_ParentLoopIDFromLoopEntity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.architect",
		Role:    "architect",
		Model:   "mock-model",
		Prompt:  "p",
	}

	// Trigger entity is a loop execution — parent linkage should appear.
	loopEntityID := "c360.ops.agent.agentic-loop.execution.loop-research-abc"
	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: loopEntityID}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)
	assert.Equal(t, "loop-research-abc", task.ParentLoopID,
		"ParentLoopID should be set to the trigger loop's UUID when the entity matches LoopExecutionEntityID shape")
}

// TestAction_PublishAgent_NonLoopTriggerLeavesParentLoopIDUnset asserts the
// negative case: a publish_agent action triggered by a non-loop entity
// (telemetry, ops finding, chain entity, model endpoint) leaves
// ParentLoopID empty. Back-compat for every rule today that fires on
// non-loop triggers — they continue to spawn root-of-chain loops.
func TestAction_PublishAgent_NonLoopTriggerLeavesParentLoopIDUnset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name     string
		entityID string
	}{
		{"telemetry entity", "c360.ops.robotics.gcs.drone.001"},
		{"model endpoint", "c360.ops.agent.model-registry.endpoint.claude-sonnet"},
		{"trajectory step", "c360.ops.agent.agentic-loop.step.loop-1-0"},
		{"chain execution", "c360.ops.agent.chain.execution.chain-abc"},
		{"non-canonical entity ID", "e.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockPublisher{}
			executor := NewActionExecutorFull(nil, nil, mock)
			action := Action{
				Type:    ActionTypePublishAgent,
				Subject: "agent.task.test",
				Role:    "general",
				Model:   "mock-model",
				Prompt:  "p",
			}
			require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: tt.entityID}))
			require.Len(t, mock.published, 1)

			baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
			require.NoError(t, err)
			task, ok := baseMsg.Payload().(*agentic.TaskMessage)
			require.True(t, ok)
			assert.Equal(t, "", task.ParentLoopID,
				"ParentLoopID should remain unset when trigger entity is not a loop execution (got entity %q)", tt.entityID)
		})
	}
}

// TestAction_PublishAgent_EmptyActionAllowlist verifies that an unset
// ActionAllowlist leaves Metadata's allowlist key unset (back-compat:
// existing flows that don't opt in keep their pre-F2 free-form decide
// behaviour).
func TestAction_PublishAgent_EmptyActionAllowlist(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.test",
		Role:    "general",
		Model:   "mock-model",
		Prompt:  "p",
		// ActionAllowlist omitted
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	if task.Metadata != nil {
		_, has := task.Metadata[agentic.MetadataKeyDecideActionAllowlist]
		assert.False(t, has, "no allowlist field should be set on Metadata when ActionAllowlist is empty")
	}
}

// stubToolExecutor is a minimal ToolExecutor for the global-registry test.
// It does no work; resolveToolNames only reads the ToolDefinition it exposes.
type stubToolExecutor struct{ name string }

func (s *stubToolExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name:        s.name,
		Description: "stub for rule action test",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}}
}

func (s *stubToolExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{CallID: call.ID, Content: "stub"}, nil
}

// T051: Test PublishAgent without publisher (no-op)
func TestAction_PublishAgent_NoPublisher(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	executor := NewActionExecutor(nil) // No publisher configured

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.test",
		Role:    "general",
		Model:   "mock-model",
		Prompt:  "Test prompt",
	}

	// Should not error, just log and return
	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
	require.NoError(t, err)
}

// T052: Test PublishAgent error handling
func TestAction_PublishAgent_ErrorHandling(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	expectedErr := assert.AnError
	mock := &mockPublisher{err: expectedErr}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.test",
		Role:    "general",
		Model:   "mock-model",
		Prompt:  "Test prompt",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish agent task to agent.task.test")
}

// T053: Test ActionTypePublishAgent constant
func TestActionConstant_PublishAgent(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "publish_agent", ActionTypePublishAgent)
}

// Test PublishAgent with WorkflowSlug and WorkflowStep fields
func TestAction_PublishAgent_WorkflowFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:         ActionTypePublishAgent,
		Subject:      "agent.task.qualifier",
		Role:         "qualifier",
		Model:        "mock-model",
		Prompt:       "Qualify issue for $entity.id",
		WorkflowSlug: "github-issue-to-pr",
		WorkflowStep: "qualify",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "c360.github.repo.myrepo.workflow.42"})
	require.NoError(t, err)
	require.Len(t, mock.published, 1)

	// Parse the published TaskMessage
	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	require.NoError(t, err)

	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok, "expected *agentic.TaskMessage, got %T", baseMsg.Payload())

	assert.Equal(t, "github-issue-to-pr", task.WorkflowSlug)
	assert.Equal(t, "qualify", task.WorkflowStep)
	assert.Equal(t, "qualifier", task.Role)
	assert.Equal(t, "Qualify issue for c360.github.repo.myrepo.workflow.42", task.Prompt)
}

// Test PublishAgent WorkflowSlug/WorkflowStep with variable substitution
func TestAction_PublishAgent_WorkflowFieldsVariableSubstitution(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:         ActionTypePublishAgent,
		Subject:      "agent.task.develop",
		Role:         "developer",
		Model:        "mock-model",
		Prompt:       "Develop fix",
		WorkflowSlug: "github-issue-to-pr",
		WorkflowStep: "develop",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
	require.NoError(t, err)
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	require.NoError(t, err)

	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)
	assert.Equal(t, "github-issue-to-pr", task.WorkflowSlug)
	assert.Equal(t, "develop", task.WorkflowStep)
}

// Test PublishAgent without WorkflowSlug/WorkflowStep (backwards compatible)
func TestAction_PublishAgent_NoWorkflowFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.general",
		Role:    "general",
		Model:   "mock-model",
		Prompt:  "General task",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
	require.NoError(t, err)
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	require.NoError(t, err)

	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)
	assert.Empty(t, task.WorkflowSlug)
	assert.Empty(t, task.WorkflowStep)
}

// Test qualifier and developer roles are valid (added for github-pr-workflow)
func TestAction_PublishAgent_QualifierDeveloperRoles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	for _, role := range []string{"qualifier", "developer"} {
		t.Run(role, func(t *testing.T) {
			mock := &mockPublisher{}
			executor := NewActionExecutorFull(nil, nil, mock)

			action := Action{
				Type:    ActionTypePublishAgent,
				Subject: "agent.task.test",
				Role:    role,
				Model:   "mock-model",
				Prompt:  "Test prompt",
			}

			err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
			require.NoError(t, err, "role %q should be valid", role)
			require.Len(t, mock.published, 1)
		})
	}
}

// T054: Test extended role validation (ADR-018)
func TestAction_PublishAgent_ExtendedRoles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name    string
		role    string
		wantErr bool
	}{
		{name: "general role", role: "general", wantErr: false},
		{name: "architect role", role: "architect", wantErr: false},
		{name: "editor role", role: "editor", wantErr: false},
		{name: "reviewer role", role: "reviewer", wantErr: false},
		{name: "fixer role", role: "fixer", wantErr: false},
		{name: "custom role (no closed-set validation)", role: "researcher", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockPublisher{}
			executor := NewActionExecutorFull(nil, nil, mock)

			action := Action{
				Type:    ActionTypePublishAgent,
				Subject: "agent.task.test",
				Role:    tt.role,
				Model:   "mock-model",
				Prompt:  "Test prompt",
			}

			err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestAction_PublishAgent_WritesSpawnedTaskTriple verifies that publish_agent
// writes the generated taskID back to the entity as a rule.spawned_task triple
// so downstream rules can reference it via $entity.triple.rule.spawned_task.
// This closes Gap 3: without it, the taskID exists only in the published
// TaskMessage and is invisible to the rest of the rule engine.
func TestAction_PublishAgent_WritesSpawnedTaskTriple(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockPub := &mockPublisher{}
	mockMut := &mockTripleMutator{}
	executor := NewActionExecutorFull(nil, mockMut, mockPub)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.test",
		Role:    "researcher",
		Model:   "mock-model",
		Prompt:  "investigate",
	}

	entityID := "org.platform.domain.system.type.001"
	ec := &ExecutionContext{
		EntityID: entityID,
		State:    &MatchState{RuleID: "research-rule"},
	}

	err := executor.Execute(ctx, action, ec)
	require.NoError(t, err)

	// Task was published with a generated taskID.
	require.Len(t, mockPub.published, 1)
	env, err := newActionsTestDecoder(t).Decode(mockPub.published[0].data)
	require.NoError(t, err)
	task, ok := env.Payload().(*agentic.TaskMessage)
	require.True(t, ok)
	require.NotEmpty(t, task.TaskID)

	// The same taskID must have been persisted as a triple against the entity.
	require.Len(t, mockMut.addedTriples, 1,
		"publish_agent should add a rule.spawned_task triple")
	assert.Equal(t, entityID, mockMut.addedTriples[0].Subject)
	assert.Equal(t, "rule.spawned_task", mockMut.addedTriples[0].Predicate)
	assert.Equal(t, task.TaskID, mockMut.addedTriples[0].Object)

	// The triple write is tracked against the originating rule so the
	// rule will not re-trigger on its own write.
	require.Len(t, mockMut.addedRuleIDs, 1)
	assert.Equal(t, "research-rule", mockMut.addedRuleIDs[0])
}

// TestAction_PublishAgent_NoMutatorSkipsTriple verifies that publish_agent
// still succeeds if no triple mutator is configured (e.g. graph integration
// disabled). The spawned_task triple is simply skipped.
func TestAction_PublishAgent_NoMutatorSkipsTriple(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockPub := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mockPub)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.test",
		Role:    "researcher",
		Model:   "mock-model",
		Prompt:  "go",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
	require.NoError(t, err)
	require.Len(t, mockPub.published, 1)
}

// TestAction_PublishAgent_NoPublisherSkipsTriple verifies that when there is
// no publisher, the spawned_task triple is also not written. Writing the
// triple when the task was never published would leave stale tracking state.
func TestAction_PublishAgent_NoPublisherSkipsTriple(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockMut := &mockTripleMutator{}
	executor := NewActionExecutorFull(nil, mockMut, nil)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.test",
		Role:    "researcher",
		Model:   "mock-model",
		Prompt:  "go",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
	require.NoError(t, err)
	assert.Empty(t, mockMut.addedTriples,
		"no triple should be written when publish was skipped")
}

// T055: Test ExecutionContext.SubstituteVariables covers entity IDs, state fields, and entity triples
func TestExecutionContext_SubstituteVariables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ec       *ExecutionContext
		template string
		want     string
	}{
		{
			name:     "substitute entity.id",
			ec:       &ExecutionContext{EntityID: "c360.platform.sensor.temp.001"},
			template: "Entity: $entity.id",
			want:     "Entity: c360.platform.sensor.temp.001",
		},
		{
			name:     "substitute related.id",
			ec:       &ExecutionContext{EntityID: "drone.001", RelatedID: "zone.A"},
			template: "Related: $related.id",
			want:     "Related: zone.A",
		},
		{
			name: "substitute state.iteration",
			ec: &ExecutionContext{
				EntityID: "entity.001",
				State:    &MatchState{Iteration: 3, MaxIterations: 10},
			},
			template: "Iter: $state.iteration of $state.max_iterations",
			want:     "Iter: 3 of 10",
		},
		{
			name: "substitute entity triple predicate",
			ec: &ExecutionContext{
				EntityID: "entity.001",
				Entity: &gtypes.EntityState{
					ID: "entity.001",
					Triples: []message.Triple{
						{Subject: "entity.001", Predicate: "agent.role", Object: "architect"},
						{Subject: "entity.001", Predicate: "status.battery", Object: "low"},
					},
				},
			},
			template: "Role: $entity.triple.agent.role, Battery: $entity.triple.status.battery",
			want:     "Role: architect, Battery: low",
		},
		{
			name:     "no substitution needed",
			ec:       &ExecutionContext{EntityID: "entity.001"},
			template: "static.content",
			want:     "static.content",
		},
		{
			name:     "empty related.id substitutes empty string",
			ec:       &ExecutionContext{EntityID: "entity.001", RelatedID: ""},
			template: "Related: $related.id",
			want:     "Related: ",
		},
		{
			name:     "nil state skips state substitutions",
			ec:       &ExecutionContext{EntityID: "entity.001"},
			template: "Iter: $state.iteration",
			want:     "Iter: $state.iteration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.ec.SubstituteVariables(tt.template)
			assert.Equal(t, tt.want, result)
		})
	}
}

// TestExecutionContext_SubstituteVariables_WarnsOnUnresolved verifies that
// any $entity/$related/$state token that survives substitution triggers a
// warning log. This is the regression guard for the deep-research e2e
// silent-pass bug where $entity.triple.agent.loop.task reached NATS KV as
// a literal and caused "invalid key" errors hours down the debug path.
func TestExecutionContext_SubstituteVariables_WarnsOnUnresolved(t *testing.T) {
	// Capture slog output. Not t.Parallel() — slog.SetDefault mutates
	// package-level state.
	prev := slog.Default()
	var buf strings.Builder
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	tests := []struct {
		name          string
		ec            *ExecutionContext
		template      string
		wantLeftovers []string // substrings expected in the warning
	}{
		{
			name:          "triple predicate missing from entity",
			ec:            &ExecutionContext{EntityID: "entity.001", Entity: &gtypes.EntityState{ID: "entity.001"}},
			template:      "key.$entity.triple.agent.loop.task",
			wantLeftovers: []string{"$entity.triple.agent.loop.task"},
		},
		{
			name:          "state substitution without state struct",
			ec:            &ExecutionContext{EntityID: "entity.001"},
			template:      "iter=$state.iteration",
			wantLeftovers: []string{"$state.iteration"},
		},
		{
			name:          "multiple unresolved tokens collected",
			ec:            &ExecutionContext{EntityID: "entity.001"},
			template:      "a=$entity.triple.missing.one b=$related.id c=$state.iteration",
			wantLeftovers: []string{"$entity.triple.missing.one", "$state.iteration"},
		},
		{
			// Cron-only $schedule.* token in an expression-rule path
			// (Schedule == nil). Locks the regex extension that added
			// "schedule" to the alternation so a future refactor can't
			// silently drop the namespace.
			name:          "schedule token without schedule context",
			ec:            &ExecutionContext{EntityID: "entity.001"},
			template:      "key=$schedule.unknown_field",
			wantLeftovers: []string{"$schedule.unknown_field"},
		},
		{
			// Unknown $schedule.* field on a populated cron context —
			// the schedule shim only handles id/spec/last_fired_at, so
			// any other token survives and warns.
			name: "unknown schedule field on populated context",
			ec: &ExecutionContext{
				Schedule: &ScheduleContext{ID: "r1", Spec: "@hourly"},
			},
			template:      "missing=$schedule.attempt",
			wantLeftovers: []string{"$schedule.attempt"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()
			_ = tc.ec.SubstituteVariables(tc.template)
			logged := buf.String()
			assert.Contains(t, logged, "Unresolved template variables", "expected warning in log output:\n%s", logged)
			for _, want := range tc.wantLeftovers {
				assert.Contains(t, logged, want, "expected token %q in warning", want)
			}
		})
	}
}

// TestExecutionContext_SubstituteVariables_NoWarnOnClean verifies that a
// template whose tokens all resolve cleanly does NOT emit the warning — so
// successful substitutions stay silent in the log.
func TestExecutionContext_SubstituteVariables_NoWarnOnClean(t *testing.T) {
	prev := slog.Default()
	var buf strings.Builder
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	ec := &ExecutionContext{
		EntityID:  "entity.001",
		RelatedID: "zone.A",
		State:     &MatchState{Iteration: 2, MaxIterations: 5},
		Entity: &gtypes.EntityState{
			ID: "entity.001",
			Triples: []message.Triple{
				{Subject: "entity.001", Predicate: "agent.role", Object: "architect"},
			},
		},
	}

	got := ec.SubstituteVariables("$entity.id|$related.id|$state.iteration|$entity.triple.agent.role")
	assert.Equal(t, "entity.001|zone.A|2|architect", got)
	assert.Empty(t, buf.String(), "clean substitution should not log")
}

// mockKVWriter implements KVWriter for testing
type mockKVWriter struct {
	updates []kvWriteCall
	puts    []kvWriteCall
	data    map[string]map[string]map[string]any // bucket -> key -> value
}

type kvWriteCall struct {
	Bucket string
	Key    string
}

func newMockKVWriter() *mockKVWriter {
	return &mockKVWriter{
		data: make(map[string]map[string]map[string]any),
	}
}

func (m *mockKVWriter) UpdateJSON(_ context.Context, bucket, key string, updateFn func(current map[string]any) error) error {
	m.updates = append(m.updates, kvWriteCall{Bucket: bucket, Key: key})

	if m.data[bucket] == nil {
		m.data[bucket] = make(map[string]map[string]any)
	}
	current := m.data[bucket][key]
	if current == nil {
		current = make(map[string]any)
	}
	if err := updateFn(current); err != nil {
		return err
	}
	m.data[bucket][key] = current
	return nil
}

func (m *mockKVWriter) PutJSON(_ context.Context, bucket, key string, value map[string]any) error {
	m.puts = append(m.puts, kvWriteCall{Bucket: bucket, Key: key})

	if m.data[bucket] == nil {
		m.data[bucket] = make(map[string]map[string]any)
	}
	m.data[bucket][key] = value
	return nil
}

func TestAction_UpdateKV_Merge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	kv := newMockKVWriter()
	// Seed existing data
	kv.data["PLAN_STATES"] = map[string]map[string]any{
		"my-plan": {"status": "created", "owner": "alice"},
	}

	executor := NewActionExecutorComplete(nil, nil, nil, kv)

	action := Action{
		Type:   ActionTypeUpdateKV,
		Bucket: "PLAN_STATES",
		Key:    "my-plan",
		Payload: map[string]any{
			"status":     "drafting",
			"updated_by": "rule_engine",
		},
		Merge: true,
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "plan.001"})
	require.NoError(t, err)

	// Verify merge: existing "owner" preserved, "status" updated, "updated_by" added
	result := kv.data["PLAN_STATES"]["my-plan"]
	assert.Equal(t, "drafting", result["status"])
	assert.Equal(t, "alice", result["owner"])
	assert.Equal(t, "rule_engine", result["updated_by"])
	assert.Len(t, kv.updates, 1)
	assert.Equal(t, "PLAN_STATES", kv.updates[0].Bucket)
}

func TestAction_UpdateKV_Overwrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	kv := newMockKVWriter()
	executor := NewActionExecutorComplete(nil, nil, nil, kv)

	action := Action{
		Type:   ActionTypeUpdateKV,
		Bucket: "EXECUTION_STATES",
		Key:    "exec-001",
		Payload: map[string]any{
			"stage": "running",
		},
		Merge: false,
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "exec.001"})
	require.NoError(t, err)

	result := kv.data["EXECUTION_STATES"]["exec-001"]
	assert.Equal(t, "running", result["stage"])
	assert.Len(t, kv.puts, 1)
}

func TestAction_UpdateKV_VariableSubstitution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	kv := newMockKVWriter()
	executor := NewActionExecutorComplete(nil, nil, nil, kv)

	entity := &gtypes.EntityState{
		ID: "plan.001",
		Triples: []message.Triple{
			{Subject: "plan.001", Predicate: "workflow.plan.slug", Object: "my-plan"},
		},
	}

	action := Action{
		Type:   ActionTypeUpdateKV,
		Bucket: "PLAN_STATES",
		Key:    "$entity.triple.workflow.plan.slug",
		Payload: map[string]any{
			"status":     "drafting",
			"updated_at": "$now",
			"entity_id":  "$entity.id",
		},
		Merge: false,
	}

	ec := &ExecutionContext{
		EntityID: "plan.001",
		Entity:   entity,
	}

	err := executor.Execute(ctx, action, ec)
	require.NoError(t, err)

	// Key should be substituted
	assert.Contains(t, kv.data["PLAN_STATES"], "my-plan")

	result := kv.data["PLAN_STATES"]["my-plan"]
	assert.Equal(t, "drafting", result["status"])
	assert.Equal(t, "plan.001", result["entity_id"])
	// $now should be substituted to an RFC3339 timestamp
	nowStr, ok := result["updated_at"].(string)
	require.True(t, ok, "updated_at should be a string")
	_, parseErr := time.Parse(time.RFC3339, nowStr)
	assert.NoError(t, parseErr, "updated_at should be valid RFC3339: %s", nowStr)
}

func TestAction_UpdateKV_MissingBucket(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	executor := NewActionExecutorComplete(nil, nil, nil, newMockKVWriter())

	action := Action{
		Type: ActionTypeUpdateKV,
		Key:  "some-key",
		Payload: map[string]any{
			"status": "drafting",
		},
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "plan.001"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bucket is required")
}

func TestAction_UpdateKV_MissingKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	executor := NewActionExecutorComplete(nil, nil, nil, newMockKVWriter())

	action := Action{
		Type:   ActionTypeUpdateKV,
		Bucket: "PLAN_STATES",
		Payload: map[string]any{
			"status": "drafting",
		},
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "plan.001"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key is required")
}

func TestAction_UpdateKV_NoWriter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// No kvWriter — should be a graceful no-op
	executor := NewActionExecutorFull(nil, nil, nil)

	action := Action{
		Type:   ActionTypeUpdateKV,
		Bucket: "PLAN_STATES",
		Key:    "my-plan",
		Payload: map[string]any{
			"status": "drafting",
		},
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "plan.001"})
	require.NoError(t, err)
}

func TestSubstitutePayloadVariables_Nested(t *testing.T) {
	t.Parallel()

	ec := &ExecutionContext{
		EntityID:  "plan.001",
		RelatedID: "req.001",
	}

	payload := map[string]any{
		"entity":  "$entity.id",
		"related": "$related.id",
		"count":   42,
		"nested": map[string]any{
			"inner_entity": "$entity.id",
			"flag":         true,
		},
	}

	result := substitutePayloadVariables(payload, ec)

	assert.Equal(t, "plan.001", result["entity"])
	assert.Equal(t, "req.001", result["related"])
	assert.Equal(t, 42, result["count"]) // non-string preserved
	nested := result["nested"].(map[string]any)
	assert.Equal(t, "plan.001", nested["inner_entity"])
	assert.Equal(t, true, nested["flag"]) // non-string preserved
}

func TestSubstitutePayloadVariables_ArrayValues(t *testing.T) {
	t.Parallel()

	ec := &ExecutionContext{
		EntityID:  "plan.001",
		RelatedID: "req.001",
	}

	payload := map[string]any{
		"tags":    []any{"$entity.id", "static", "$related.id"},
		"numbers": []any{1, 2, 3},
		"mixed":   []any{"$entity.id", 42, true},
	}

	result := substitutePayloadVariables(payload, ec)

	tags := result["tags"].([]any)
	assert.Equal(t, "plan.001", tags[0])
	assert.Equal(t, "static", tags[1])
	assert.Equal(t, "req.001", tags[2])

	numbers := result["numbers"].([]any)
	assert.Equal(t, 1, numbers[0]) // non-string preserved

	mixed := result["mixed"].([]any)
	assert.Equal(t, "plan.001", mixed[0])
	assert.Equal(t, 42, mixed[1])
	assert.Equal(t, true, mixed[2])
}

// --- DenyVerdict type tests ---

// TestDenyVerdict_ErrorString verifies the Error() string format.
func TestDenyVerdict_ErrorString(t *testing.T) {
	t.Parallel()

	dv := &DenyVerdict{RuleID: "r1", Reason: "no"}
	want := "rule r1 denied: no"
	if got := dv.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestDenyVerdict_ErrorsIs verifies errors.Is matching behaviour.
// Any *DenyVerdict matches ErrDenyVerdict; unrelated errors do not.
func TestDenyVerdict_ErrorsIs(t *testing.T) {
	t.Parallel()

	err := &DenyVerdict{RuleID: "r1", Reason: "no"}

	if !errors.Is(err, ErrDenyVerdict) {
		t.Error("errors.Is(&DenyVerdict{...}, ErrDenyVerdict) = false, want true")
	}
	if errors.Is(errors.New("other"), ErrDenyVerdict) {
		t.Error("errors.Is(errors.New(\"other\"), ErrDenyVerdict) = true, want false")
	}
}

// TestDenyVerdict_ErrorsAs verifies errors.As extracts fields correctly.
func TestDenyVerdict_ErrorsAs(t *testing.T) {
	t.Parallel()

	var dv *DenyVerdict
	err := &DenyVerdict{RuleID: "r1", Reason: "no"}
	if !errors.As(err, &dv) {
		t.Fatal("errors.As = false, want true")
	}
	if dv.RuleID != "r1" {
		t.Errorf("dv.RuleID = %q, want %q", dv.RuleID, "r1")
	}
	if dv.Reason != "no" {
		t.Errorf("dv.Reason = %q, want %q", dv.Reason, "no")
	}
}

// --- executeDeny tests ---

// newDenyTestEC builds an ExecutionContext with a populated MatchState (so
// RuleID() returns a non-empty value) and an optional CallerContext.
func newDenyTestEC(ruleID string, caller *CallerContext) *ExecutionContext {
	return &ExecutionContext{
		EntityID: "acme.ops.test.svc.entity.001",
		State: &MatchState{
			RuleID: ruleID,
		},
		Caller: caller,
	}
}

// TestExecuteDeny_ReturnsDenyVerdict verifies that executeDeny returns a
// *DenyVerdict carrying the expected RuleID and Reason.
func TestExecuteDeny_ReturnsDenyVerdict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	executor := NewActionExecutorWithMutator(slog.Default(), &mockTripleMutator{})
	ec := newDenyTestEC("deny-rule-1", nil)
	action := Action{Type: ActionTypeDeny, Reason: "access not allowed"}

	err := executor.executeDeny(ctx, action, ec)
	require.Error(t, err)

	var dv *DenyVerdict
	require.True(t, errors.As(err, &dv), "error should be *DenyVerdict")
	assert.Equal(t, "deny-rule-1", dv.RuleID)
	assert.Equal(t, "access not allowed", dv.Reason)
}

// TestExecuteDeny_AuditFailureDoesNotFlipDeny is the architect-flagged invariant:
// if AddTriple fails (audit write error), executeDeny MUST still return *DenyVerdict,
// not the audit error. A failing audit-write must never flip deny → allow.
func TestExecuteDeny_AuditFailureDoesNotFlipDeny(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	aud := &mockVerdictAuditor{err: errors.New("nats unavailable")}
	executor := NewActionExecutor(slog.Default())
	executor.SetVerdictAuditor(aud)
	ec := newDenyTestEC("deny-rule-audit-fail", nil)
	action := Action{Type: ActionTypeDeny, Reason: "must deny"}

	err := executor.executeDeny(ctx, action, ec)
	require.Error(t, err)

	// The error must be a *DenyVerdict, NOT the audit error.
	require.True(t, errors.Is(err, ErrDenyVerdict),
		"audit failure must not flip deny to allow: got %v", err)

	var dv *DenyVerdict
	require.True(t, errors.As(err, &dv))
	assert.Equal(t, "deny-rule-audit-fail", dv.RuleID)
	assert.Equal(t, "must deny", dv.Reason)
}

// TestExecuteDeny_EmitsVerdictAudit verifies that executeDeny emits a governance
// verdict event (ADR-055 §3a) carrying decision=deny, the rule ID, the reason,
// and the entity ID — replacing the prior rule.deny audit triple.
func TestExecuteDeny_EmitsVerdictAudit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	aud := &mockVerdictAuditor{}
	executor := NewActionExecutor(slog.Default())
	executor.SetVerdictAuditor(aud)
	ec := newDenyTestEC("deny-rule-audit", nil)
	action := Action{Type: ActionTypeDeny, Reason: "blocked by policy"}

	_ = executor.executeDeny(ctx, action, ec)

	require.Len(t, aud.emitted, 1, "EmitVerdict must be called exactly once")
	ev := aud.emitted[0]
	assert.Equal(t, governance.DecisionDeny, ev.Decision)
	assert.Equal(t, "deny-rule-audit", ev.RuleID)
	assert.Equal(t, "blocked by policy", ev.Reason)
	assert.Equal(t, "acme.ops.test.svc.entity.001", ev.EntityID)
}

// TestExecuteDeny_VariableSubstitutionInReason verifies that $caller.role and
// other template variables are substituted into the reason before it travels
// in the DenyVerdict and the emitted verdict event.
func TestExecuteDeny_VariableSubstitutionInReason(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	aud := &mockVerdictAuditor{}
	executor := NewActionExecutor(slog.Default())
	executor.SetVerdictAuditor(aud)
	ec := newDenyTestEC("deny-rule-subst", &CallerContext{ID: "alice", Role: "viewer", Org: "acme"})
	action := Action{Type: ActionTypeDeny, Reason: "caller $caller.id with role $caller.role is denied"}

	err := executor.executeDeny(ctx, action, ec)
	require.Error(t, err)

	var dv *DenyVerdict
	require.True(t, errors.As(err, &dv))
	assert.Equal(t, "caller alice with role viewer is denied", dv.Reason)

	require.Len(t, aud.emitted, 1)
	assert.Equal(t, "caller alice with role viewer is denied", aud.emitted[0].Reason)
}

// --- executeApprove tests ---
//
// Approve is asymmetric to deny: it returns nil (does NOT short-circuit
// subsequent actions), publishes a verdict payload to the configured
// Subject, and emits an approve verdict audit event (ADR-055 §3a). These
// tests pin those invariants and the parity with deny on audit-failure
// handling.

// newApproveTestEC builds an ExecutionContext suitable for approve tests.
// MessageData is populated so $message.* substitution can resolve in the
// canonical ADR-039 use case (templating verdict subjects with the
// proposed call's loop_id / call_id).
func newApproveTestEC(ruleID string, caller *CallerContext, msgData map[string]any) *ExecutionContext {
	return &ExecutionContext{
		EntityID: "acme.ops.test.svc.entity.001",
		State: &MatchState{
			RuleID: ruleID,
		},
		Caller:      caller,
		MessageData: msgData,
	}
}

// TestExecuteApprove_ReturnsNilDoesNotShortCircuit pins the asymmetry: approve
// returns nil so the action executor's Execute loop continues with later
// actions. Deny short-circuits via *DenyVerdict; approve does NOT. This
// asymmetry is the feature, not an oversight — observability/audit
// actions on the same rule firing still run after approve. See ADR-039.
func TestExecuteApprove_ReturnsNilDoesNotShortCircuit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mut := &mockTripleMutator{}
	pub := &mockPublisher{}
	executor := NewActionExecutorFull(slog.Default(), mut, pub)
	ec := newApproveTestEC("approve-rule-1", nil, nil)
	action := Action{
		Type:    ActionTypeApprove,
		Subject: "agent.toolcall.approved.loop-abc.call-001",
		Reason:  "approved by policy",
	}

	err := executor.executeApprove(ctx, action, ec)
	assert.NoError(t, err, "approve must return nil so later actions can fire")
}

// TestExecuteApprove_PublishesVerdictPayload pins that approve publishes to
// the substituted Subject with a payload carrying decision="approved" and
// the rule context. Subject substitution must resolve $message.* tokens
// so per-call verdict routing works for ADR-039 governance.
func TestExecuteApprove_PublishesVerdictPayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mut := &mockTripleMutator{}
	pub := &mockPublisher{}
	executor := NewActionExecutorFull(slog.Default(), mut, pub)
	ec := newApproveTestEC("approve-rule-pub", nil, map[string]any{
		"loop_id": "loop-abc",
		"call_id": "call-001",
	})
	action := Action{
		Type:    ActionTypeApprove,
		Subject: "agent.toolcall.approved.$message.loop_id.$message.call_id",
		Reason:  "policy permits",
	}

	err := executor.executeApprove(ctx, action, ec)
	require.NoError(t, err)

	require.Len(t, pub.published, 1, "approve must publish exactly once")
	got := pub.published[0]
	assert.Equal(t, "agent.toolcall.approved.loop-abc.call-001", got.subject,
		"subject must have $message.* substituted")

	// Wire format is core.json.v1 BaseMessage wrapping a GenericJSONPayload.
	// Unwrap to verify the verdict fields land in the Data map per the
	// NATS-uses-payload-registry discipline.
	var envelope struct {
		Type    map[string]string `json:"type"`
		Payload struct {
			Data map[string]any `json:"data"`
		} `json:"payload"`
	}
	require.NoError(t, json.Unmarshal(got.data, &envelope))
	assert.Equal(t, "core", envelope.Type["domain"], "must be core.json.v1 envelope")
	assert.Equal(t, "json", envelope.Type["category"])
	assert.Equal(t, "v1", envelope.Type["version"])
	assert.Equal(t, "approved", envelope.Payload.Data["decision"])
	assert.Equal(t, "approve-rule-pub", envelope.Payload.Data["rule_id"])
	assert.Equal(t, "policy permits", envelope.Payload.Data["reason"])
}

// TestExecuteApprove_EmitsVerdictAudit verifies approve emits a governance
// verdict event (ADR-055 §3a) with decision=approve and the substituted reason,
// in addition to (and independent of) the routing publish.
func TestExecuteApprove_EmitsVerdictAudit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	aud := &mockVerdictAuditor{}
	pub := &mockPublisher{}
	executor := NewActionExecutorFull(slog.Default(), nil, pub)
	executor.SetVerdictAuditor(aud)
	ec := newApproveTestEC("approve-rule-audit", nil, nil)
	action := Action{
		Type:    ActionTypeApprove,
		Subject: "agent.toolcall.approved.test",
		Reason:  "policy permits",
	}

	require.NoError(t, executor.executeApprove(ctx, action, ec))

	require.Len(t, aud.emitted, 1, "EmitVerdict must be called exactly once")
	ev := aud.emitted[0]
	assert.Equal(t, governance.DecisionApprove, ev.Decision)
	assert.Equal(t, "approve-rule-audit", ev.RuleID)
	assert.Equal(t, "policy permits", ev.Reason)
}

// TestExecuteApprove_AuditFailureDoesNotBlockPublish is the deny-parity
// invariant: an audit-write failure logs at Error level but MUST NOT
// prevent the publish from running. The verdict is structural — operators
// without an audit triple is a known-bad state we surface loudly, but
// downstream consumers (agentic-loop) still need to receive the verdict
// or the call would time out.
func TestExecuteApprove_AuditFailureDoesNotBlockPublish(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	aud := &mockVerdictAuditor{err: errors.New("nats unavailable")}
	pub := &mockPublisher{}
	executor := NewActionExecutorFull(slog.Default(), nil, pub)
	executor.SetVerdictAuditor(aud)
	ec := newApproveTestEC("approve-rule-audit-fail", nil, nil)
	action := Action{
		Type:    ActionTypeApprove,
		Subject: "agent.toolcall.approved.test",
		Reason:  "permit",
	}

	err := executor.executeApprove(ctx, action, ec)
	assert.NoError(t, err, "audit failure must NOT propagate; publish ran")
	require.Len(t, pub.published, 1, "publish must still fire when audit fails")
}

// TestEmitVerdictAudit_EchoesLoopAndCallID verifies the OPTIONAL identifiers are
// sourced from MessageData (the proposed-call message) when present, and left
// empty otherwise (entity-state / cron rules have no inbound call identity).
func TestEmitVerdictAudit_EchoesLoopAndCallID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	withMsg := &mockVerdictAuditor{}
	exec1 := NewActionExecutor(slog.Default())
	exec1.SetVerdictAuditor(withMsg)
	ecWith := newApproveTestEC("rule-echo", nil, map[string]any{"loop_id": "loop-abc", "call_id": "call-001"})
	exec1.emitVerdictAudit(ctx, governance.DecisionApprove, "rule-echo", "ok", ecWith)
	require.Len(t, withMsg.emitted, 1)
	assert.Equal(t, "loop-abc", withMsg.emitted[0].LoopID)
	assert.Equal(t, "call-001", withMsg.emitted[0].CallID)

	noMsg := &mockVerdictAuditor{}
	exec2 := NewActionExecutor(slog.Default())
	exec2.SetVerdictAuditor(noMsg)
	ecNone := newDenyTestEC("rule-no-msg", nil) // nil MessageData
	exec2.emitVerdictAudit(ctx, governance.DecisionDeny, "rule-no-msg", "blocked", ecNone)
	require.Len(t, noMsg.emitted, 1)
	assert.Empty(t, noMsg.emitted[0].LoopID, "no MessageData → empty loop_id")
	assert.Empty(t, noMsg.emitted[0].CallID, "no MessageData → empty call_id")
}

// TestExecuteApprove_PublishFailureReturnsError diverges from audit-failure
// handling: publish failure DOES return an error because downstream
// consumers never learned the verdict. The caller (rule processor) will
// log + retry per its action-error policy.
func TestExecuteApprove_PublishFailureReturnsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mut := &mockTripleMutator{}
	pub := &mockPublisher{err: errors.New("jetstream timeout")}
	executor := NewActionExecutorFull(slog.Default(), mut, pub)
	ec := newApproveTestEC("approve-rule-pub-fail", nil, nil)
	action := Action{
		Type:    ActionTypeApprove,
		Subject: "agent.toolcall.approved.test",
		Reason:  "permit",
	}

	err := executor.executeApprove(ctx, action, ec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish approve verdict")
}

// TestExecuteApprove_RequiresSubject rejects approve actions without a
// Subject — the verdict has nowhere to go and downstream consumers
// would time out. Fail loudly at action-execute time so operator config
// errors surface immediately rather than as mysterious timeouts.
func TestExecuteApprove_RequiresSubject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	executor := NewActionExecutorFull(slog.Default(), &mockTripleMutator{}, &mockPublisher{})
	ec := newApproveTestEC("approve-no-subject", nil, nil)
	action := Action{Type: ActionTypeApprove, Reason: "permit"}

	err := executor.executeApprove(ctx, action, ec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subject is required")
}

// TestExecuteApprove_NoPublisherIsNoOp covers the early-boot or
// publisher-not-configured scenario: approve must still complete (return
// nil) and emit the verdict audit event. The publish step is skipped without
// error so dev/test environments without a NATS publisher don't break.
func TestExecuteApprove_NoPublisherIsNoOp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	aud := &mockVerdictAuditor{}
	executor := NewActionExecutor(slog.Default())
	executor.SetVerdictAuditor(aud)
	ec := newApproveTestEC("approve-no-pub", nil, nil)
	action := Action{Type: ActionTypeApprove, Subject: "agent.toolcall.approved.x", Reason: "permit"}

	assert.NoError(t, executor.executeApprove(ctx, action, ec))
	assert.Len(t, aud.emitted, 1, "verdict audit still emitted without publisher")
}

// --- ADR-053 Pass A: RunID inheritance in executePublishAgent ---

// TestAction_PublishAgent_RunIDInheritedFromLoopEntity verifies that when the
// trigger entity is a loop execution with an agent.run triple, the spawned
// TaskMessage.RunID is set to that run ID (ADR-053 D7 inherit default).
// Drives through the production Execute entry point per
// feedback_integration_tests_must_drive_production_wire.
func TestAction_PublishAgent_RunIDInheritedFromLoopEntity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.researcher",
		Role:    "researcher",
		Model:   "mock-model",
		Prompt:  "investigate the problem",
	}

	// Build an entity state that carries agent.run = "root-loop-uuid".
	loopEntityID := "c360.ops.agent.agentic-loop.execution.loop-abc"
	entity := &gtypes.EntityState{
		ID: loopEntityID,
		Triples: []message.Triple{
			{
				Subject:   loopEntityID,
				Predicate: "agent.run",
				Object:    "root-loop-uuid",
			},
		},
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{
		EntityID: loopEntityID,
		Entity:   entity,
	}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)
	assert.Equal(t, "root-loop-uuid", task.RunID,
		"RunID must be inherited from the firing loop entity's agent.run triple (ADR-053 D7)")
	// ParentLoopID is also inherited independently (existing behaviour preserved).
	assert.Equal(t, "loop-abc", task.ParentLoopID,
		"ParentLoopID must still be inherited from the loop entity ID shape")
}

// TestAction_PublishAgent_RunIDNotInheritedWhenMissing verifies that when the
// trigger entity has no agent.run triple, the spawned TaskMessage.RunID stays
// empty — additive-only, back-compat preserved.
func TestAction_PublishAgent_RunIDNotInheritedWhenMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.researcher",
		Role:    "researcher",
		Model:   "mock-model",
		Prompt:  "investigate",
	}

	// Loop entity without agent.run triple.
	loopEntityID := "c360.ops.agent.agentic-loop.execution.loop-xyz"
	entity := &gtypes.EntityState{
		ID: loopEntityID,
		Triples: []message.Triple{
			{Subject: loopEntityID, Predicate: "agent.loop.role", Object: "researcher"},
		},
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{
		EntityID: loopEntityID,
		Entity:   entity,
	}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)
	assert.Empty(t, task.RunID,
		"RunID must be empty when the trigger entity has no agent.run triple")
}

// TestAction_PublishAgent_RunIDInheritedFromNonLoopEntityTriple verifies that
// RunID inheritance is triple-driven (not loop-shape-gated): any entity with
// an agent.run triple propagates RunID to the spawned task. This is intentional
// — a chain entity or coordinator entity acting as trigger can thread RunID
// forward. ParentLoopID remains loop-execution-shape-gated.
func TestAction_PublishAgent_RunIDInheritedFromNonLoopEntityTriple(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.researcher",
		Role:    "researcher",
		Model:   "mock-model",
		Prompt:  "investigate",
	}

	// Chain entity (not a loop execution) carrying an agent.run triple.
	chainEntityID := "c360.ops.agent.chain.execution.chain-abc"
	entity := &gtypes.EntityState{
		ID: chainEntityID,
		Triples: []message.Triple{
			{Subject: chainEntityID, Predicate: "agent.run", Object: "root-loop-uuid"},
		},
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{
		EntityID: chainEntityID,
		Entity:   entity,
	}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)
	// RunID IS inherited: triple-driven, not loop-shape-gated.
	assert.Equal(t, "root-loop-uuid", task.RunID,
		"RunID must be inherited from agent.run triple on any entity type")
	// ParentLoopID stays empty: chain entity doesn't match agentic-loop.execution shape.
	assert.Empty(t, task.ParentLoopID,
		"ParentLoopID must be empty for non-loop-execution trigger entities")
}

// --- ADR-053 D4: run_scope tests (I2) ---
//
// These tests drive publishAgentOnce through the PRODUCTION executor.Execute entry
// point (not helpers) to lock the run_scope=new/inherit/none contract.

// TestAction_PublishAgent_RunScopeNew_MintsRunAndStampsAgentRun verifies that
// run_scope=new: (1) calls Manager.Create (minting the run), (2) sets task.RunID
// to the firing loop's bare ID on the spawned child, and (3) stamps the
// agent.run triple on the firing entity via tripleMutator.
func TestAction_PublishAgent_RunScopeNew_MintsRunAndStampsAgentRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pub := &mockPublisher{}
	mutator := &mockTripleMutator{}
	// Use the fakeLifecycleManager from actions_lifecycle_test.go (same package).
	mgr := newFakeManager()

	executor := NewActionExecutorComplete(nil, mutator, pub, nil)
	executor.SetLifecycleManager(mgr)

	// Trigger entity is a loop-execution entity — the firing coordinator loop.
	firingLoopID := "coordinator-loop-uuid"
	loopEntityID := "acme.ops.agent.agentic-loop.execution." + firingLoopID

	action := Action{
		Type:     ActionTypePublishAgent,
		Subject:  "agent.task.researcher",
		Role:     "researcher",
		Model:    "mock-model",
		Prompt:   "investigate",
		RunScope: "new",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: loopEntityID})
	require.NoError(t, err)
	require.Len(t, pub.published, 1, "task must be published")

	// Decode the published task.
	baseMsg, err := newActionsTestDecoder(t).Decode(pub.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	// Child task.RunID must equal the firing loop's bare ID.
	assert.Equal(t, firingLoopID, task.RunID,
		"spawned child's RunID must equal the firing loop's bare loop-id")

	// Manager.Create must have been called (run minted in "dispatched").
	runEntityID := "acme.ops.agent.chain.execution." + firingLoopID
	_, created := mgr.entities["agent-run"][runEntityID]
	assert.True(t, created, "Manager.Create must mint the AgentRun entity")

	// agent.run triple must be stamped on the FIRING entity (ADR-053 D4 fix).
	var agentRunTriple *message.Triple
	for i := range mutator.addedTriples {
		if mutator.addedTriples[i].Subject == loopEntityID &&
			mutator.addedTriples[i].Predicate == "agent.run" {
			agentRunTriple = &mutator.addedTriples[i]
			break
		}
	}
	require.NotNil(t, agentRunTriple,
		"agent.run triple must be stamped on the FIRING entity so the root's terminal events can be resolved")
	assert.Equal(t, firingLoopID, agentRunTriple.Object,
		"agent.run triple object must be the bare firing loop ID")
}

// TestAction_PublishAgent_RunScopeNew_NonLoopEntityFallsBackToInherit verifies that
// run_scope=new on a non-loop trigger entity (no LoopIDFromExecutionEntityID match)
// logs a warning and falls back to inherit behavior.
func TestAction_PublishAgent_RunScopeNew_NonLoopEntityFallsBackToInherit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pub := &mockPublisher{}
	mgr := newFakeManager()

	executor := NewActionExecutorComplete(nil, nil, pub, nil)
	executor.SetLifecycleManager(mgr)

	// Non-loop entity (e.g. a sensor entity, not agent.*.execution.*).
	action := Action{
		Type:     ActionTypePublishAgent,
		Subject:  "agent.task.researcher",
		Role:     "researcher",
		Model:    "mock-model",
		Prompt:   "investigate",
		RunScope: "new",
	}

	// Non-loop trigger: no LoopIDFromExecutionEntityID match.
	nonLoopEntity := "acme.ops.iot.sensor.temperature.001"
	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: nonLoopEntity})
	require.NoError(t, err)
	require.Len(t, pub.published, 1, "task must still be published despite non-loop trigger")

	baseMsg, err := newActionsTestDecoder(t).Decode(pub.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	// No run is minted; RunID is empty (inherit from entity, but entity has no agent.run triple).
	assert.Empty(t, task.RunID,
		"non-loop trigger with run_scope=new and no agent.run triple must produce empty RunID")
}

// TestAction_PublishAgent_RunScopeNew_NoLifecycleManagerFallsBackToInherit verifies that
// run_scope=new with no lifecycle manager wired logs a warning and falls back to inherit.
func TestAction_PublishAgent_RunScopeNew_NoLifecycleManagerFallsBackToInherit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pub := &mockPublisher{}
	// No SetLifecycleManager call — lifecycle is nil.
	executor := NewActionExecutorComplete(nil, nil, pub, nil)

	firingLoopID := "coordinator-loop-uuid"
	loopEntityID := "acme.ops.agent.agentic-loop.execution." + firingLoopID

	action := Action{
		Type:     ActionTypePublishAgent,
		Subject:  "agent.task.researcher",
		Role:     "researcher",
		Model:    "mock-model",
		Prompt:   "investigate",
		RunScope: "new",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: loopEntityID})
	require.NoError(t, err)
	require.Len(t, pub.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(pub.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	// No manager → no mint → RunID stays empty (inherit fallback, entity has no agent.run triple).
	assert.Empty(t, task.RunID,
		"run_scope=new with nil lifecycle manager must not panic and produce empty RunID")
}

// TestAction_PublishAgent_RunScopeNone_SuppressesRunID verifies that run_scope=none
// prevents RunID propagation even when the trigger entity has an agent.run triple.
func TestAction_PublishAgent_RunScopeNone_SuppressesRunID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pub := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, pub)

	firingLoopID := "coordinator-loop-uuid"
	loopEntityID := "acme.ops.agent.agentic-loop.execution." + firingLoopID

	// Entity has an agent.run triple — but run_scope:none suppresses it.
	entity := &gtypes.EntityState{
		ID: loopEntityID,
		Triples: []message.Triple{
			{Subject: loopEntityID, Predicate: "agent.run", Object: "existing-run-id"},
		},
	}

	action := Action{
		Type:     ActionTypePublishAgent,
		Subject:  "agent.task.researcher",
		Role:     "researcher",
		Model:    "mock-model",
		Prompt:   "standalone investigate",
		RunScope: "none",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: loopEntityID, Entity: entity})
	require.NoError(t, err)
	require.Len(t, pub.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(pub.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	assert.Empty(t, task.RunID,
		"run_scope=none must suppress RunID even when the firing entity has an agent.run triple")
}

// TestAction_PublishAgent_RunScopeNew_MintsRunSuccessfully verifies run_scope=new
// on a loop entity: Manager.Create is called (minting the run) and task.RunID is set.
// This complements TestAction_PublishAgent_RunScopeNew_MintsRunAndStampsAgentRun with
// a table-style case that also confirms the fakeLifecycleManager stores the minted run.
func TestAction_PublishAgent_RunScopeNew_MintsRunSuccessfully(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pub := &mockPublisher{}
	mgr := newFakeManager()
	executor := NewActionExecutorComplete(nil, nil, pub, nil)
	executor.SetLifecycleManager(mgr)

	firingLoopID := "coord-fresh-mint"
	loopEntityID := "acme.ops.agent.agentic-loop.execution." + firingLoopID

	action := Action{
		Type:     ActionTypePublishAgent,
		Subject:  "agent.task.researcher",
		Role:     "researcher",
		Model:    "mock-model",
		Prompt:   "investigate",
		RunScope: "new",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: loopEntityID})
	require.NoError(t, err)
	require.Len(t, pub.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(pub.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)
	assert.Equal(t, firingLoopID, task.RunID,
		"run_scope=new: spawned child's RunID must equal the firing loop's bare loop-id")

	// Manager.Create must have created the run entity.
	runEntityID := "acme.ops.agent.chain.execution." + firingLoopID
	_, created := mgr.entities["agent-run"][runEntityID]
	assert.True(t, created, "Manager.Create must mint the AgentRun entity in 'dispatched' phase")
}

// TestAction_PublishAgent_FilesystemPolicy verifies that a publish_agent rule's
// filesystem_policy + scratch_paths thread onto TaskMessage.Metadata under the
// ADR-067 keys, in the JSON-wire shape the bash executor reads (gh#445). Drives
// the production path (Execute → BaseMessage → decode) and round-trips through
// agentic.FilesystemPolicyFromMetadata — the exact accessor the executor uses —
// so the test proves the rule surface is actually consumable, not just present.
func TestAction_PublishAgent_FilesystemPolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:             ActionTypePublishAgent,
		Subject:          "agent.task.planner",
		Role:             "planner",
		Model:            "mock-model",
		Prompt:           "inspect the repo",
		FilesystemPolicy: "read_only",
		ScratchPaths:     []string{".probe/", "build"},
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	require.NotNil(t, task.Metadata, "Metadata must be initialised when filesystem_policy is set")
	assert.Equal(t, "read_only", task.Metadata[agentic.MetadataKeyFilesystemPolicy])
	// scratch_paths survive the JSON round-trip as []any (each element a string).
	rawScratch, ok := task.Metadata[agentic.MetadataKeyScratchPaths].([]any)
	require.True(t, ok, "expected []any after JSON round-trip; got %T", task.Metadata[agentic.MetadataKeyScratchPaths])
	assert.Equal(t, []any{".probe/", "build"}, rawScratch)

	// The load-bearing assertion: the executor's own accessor reads it back.
	policy, scratch := agentic.FilesystemPolicyFromMetadata(task.Metadata)
	assert.True(t, agentic.IsReadOnlyPolicy(policy), "policy must resolve to read_only through the executor accessor")
	assert.Equal(t, []string{".probe/", "build"}, scratch)
}

// TestAction_PublishAgent_NoFilesystemPolicy confirms back-compat: a
// publish_agent with no policy stamps neither ADR-067 key.
func TestAction_PublishAgent_NoFilesystemPolicy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:    ActionTypePublishAgent,
		Subject: "agent.task.worker",
		Role:    "worker",
		Model:   "mock-model",
		Prompt:  "do work",
	}

	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "e.1"}))
	require.Len(t, mock.published, 1)

	baseMsg, err := newActionsTestDecoder(t).Decode(mock.published[0].data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok)

	if task.Metadata != nil {
		_, hasPolicy := task.Metadata[agentic.MetadataKeyFilesystemPolicy]
		_, hasScratch := task.Metadata[agentic.MetadataKeyScratchPaths]
		assert.False(t, hasPolicy, "no filesystem_policy must be stamped when unset")
		assert.False(t, hasScratch, "no scratch_paths must be stamped when unset")
	}
}
