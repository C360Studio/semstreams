// Package rule - Tests for Rule Actions (TDD - RED Phase)
package rule

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	var baseMsg message.BaseMessage
	err = json.Unmarshal(mock.published[0].data, &baseMsg)
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
	var baseMsg message.BaseMessage
	err = json.Unmarshal(mock.published[0].data, &baseMsg)
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

	var baseMsg message.BaseMessage
	err = json.Unmarshal(mock.published[0].data, &baseMsg)
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

	var baseMsg message.BaseMessage
	err = json.Unmarshal(mock.published[0].data, &baseMsg)
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
	var env message.BaseMessage
	require.NoError(t, json.Unmarshal(mockPub.published[0].data, &env))
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

// T057: Test ActionTypeTriggerWorkflow constant
func TestActionConstant_TriggerWorkflow(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "trigger_workflow", ActionTypeTriggerWorkflow)
}

// T058: Test TriggerWorkflow action publishes to correct subject
func TestAction_TriggerWorkflow(t *testing.T) {
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
			name: "trigger workflow with workflow_id",
			action: Action{
				Type:       ActionTypeTriggerWorkflow,
				WorkflowID: "notify-technician",
			},
			entityID:    "c360.platform.sensor.temp.001",
			wantSubject: "workflow.trigger.notify-technician",
			wantErr:     false,
		},
		{
			name: "trigger workflow with context data",
			action: Action{
				Type:       ActionTypeTriggerWorkflow,
				WorkflowID: "escalate-alert",
				ContextData: map[string]any{
					"severity": "critical",
					"zone":     "cold-storage",
				},
			},
			entityID:    "c360.platform.sensor.temp.001",
			relatedID:   "c360.platform.zone.cold-storage",
			wantSubject: "workflow.trigger.escalate-alert",
			wantErr:     false,
		},
		{
			name: "missing workflow_id should fail",
			action: Action{
				Type: ActionTypeTriggerWorkflow,
			},
			entityID: "c360.platform.sensor.temp.001",
			wantErr:  true,
			errMsg:   "workflow_id is required",
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
				require.Len(t, mock.published, 1)
				assert.Equal(t, tt.wantSubject, mock.published[0].subject)
			}
		})
	}
}

// T059: Test TriggerWorkflow payload format
func TestAction_TriggerWorkflow_PayloadFormat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mock := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:       ActionTypeTriggerWorkflow,
		WorkflowID: "notify-technician",
		ContextData: map[string]any{
			"alert_type": "temperature",
		},
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "sensor.temp.001", RelatedID: "zone.cold-storage"})
	require.NoError(t, err)
	require.Len(t, mock.published, 1)

	// Parse the published BaseMessage (trigger_workflow wraps payload in BaseMessage)
	var baseMsg map[string]any
	err = json.Unmarshal(mock.published[0].data, &baseMsg)
	require.NoError(t, err)

	// Verify BaseMessage type field
	msgType, ok := baseMsg["type"].(map[string]any)
	require.True(t, ok, "type field should be a map")
	assert.Equal(t, WorkflowTriggerDomain, msgType["domain"])
	assert.Equal(t, WorkflowTriggerCategory, msgType["category"])
	assert.Equal(t, WorkflowTriggerVersion, msgType["version"])

	// Extract payload from BaseMessage
	payload, ok := baseMsg["payload"].(map[string]any)
	require.True(t, ok, "payload should be a map")

	// Verify payload fields
	assert.Equal(t, "notify-technician", payload["workflow_id"])
	assert.Equal(t, "sensor.temp.001", payload["entity_id"])
	assert.Equal(t, "zone.cold-storage", payload["related_id"])
	assert.NotEmpty(t, payload["triggered_at"])

	// Verify context data is included
	contextData, ok := payload["context"].(map[string]any)
	require.True(t, ok, "context should be a map")
	assert.Equal(t, "temperature", contextData["alert_type"])
}

// T060: Test TriggerWorkflow without publisher (no-op)
func TestAction_TriggerWorkflow_NoPublisher(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	executor := NewActionExecutor(nil) // No publisher configured

	action := Action{
		Type:       ActionTypeTriggerWorkflow,
		WorkflowID: "notify-technician",
	}

	// Should not error, just log and return
	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
	require.NoError(t, err)
}

// T061: Test TriggerWorkflow error handling
func TestAction_TriggerWorkflow_ErrorHandling(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	expectedErr := assert.AnError
	mock := &mockPublisher{err: expectedErr}
	executor := NewActionExecutorFull(nil, nil, mock)

	action := Action{
		Type:       ActionTypeTriggerWorkflow,
		WorkflowID: "notify-technician",
	}

	err := executor.Execute(ctx, action, &ExecutionContext{EntityID: "entity.001"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "publish workflow trigger to workflow.trigger.notify-technician")
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
