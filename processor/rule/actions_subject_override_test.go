package rule

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
)

// #147 / ADR-046 Phase 1 join gap: triple-write actions now accept an
// optional Subject field. These tests cover the four contract paths
// per action: explicit literal, substitution-resolved, empty
// (back-compat to ec.EntityID), empty-after-substitution (authoring
// error).

// --- ExecuteAddTriple subject-override ---

func TestExecuteAddTriple_SubjectExplicitLiteral(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := NewActionExecutorFull(nil, mutator, nil)

	action := Action{
		Type:      ActionTypeAddTriple,
		Subject:   "acme.ops.robot.gcs.plan.42",
		Predicate: "test.gather.completed-subtopic",
		Object:    "hydraulics",
	}
	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "acme.ops.robot.gcs.gather.99"}))

	require.Len(t, mutator.addedTriples, 1)
	assert.Equal(t, "acme.ops.robot.gcs.plan.42", mutator.addedTriples[0].Subject,
		"literal Subject must override the trigger entity ID")
	assert.Equal(t, "test.gather.completed-subtopic", mutator.addedTriples[0].Predicate)
	assert.Equal(t, "hydraulics", mutator.addedTriples[0].Object)
}

func TestExecuteAddTriple_SubjectSubstitutionResolved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := NewActionExecutorFull(nil, mutator, nil)

	// Trigger entity (a child gather loop) carries a parent-loop ref
	// triple — the counter pattern stamps the completion marker on
	// that parent.
	ec := &ExecutionContext{
		EntityID: "acme.ops.robot.gcs.gather.99",
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: "agent.loop.parent", Object: "acme.ops.robot.gcs.plan.42"},
			},
		},
	}
	action := Action{
		Type:      ActionTypeAddTriple,
		Subject:   "$entity.triple.agent.loop.parent",
		Predicate: "test.gather.completed-subtopic",
		Object:    "hydraulics",
	}
	require.NoError(t, executor.Execute(ctx, action, ec))

	require.Len(t, mutator.addedTriples, 1)
	assert.Equal(t, "acme.ops.robot.gcs.plan.42", mutator.addedTriples[0].Subject,
		"$entity.triple.agent.loop.parent must resolve to the parent loop entity")
}

func TestExecuteAddTriple_SubjectEmptyDefaultsToEntityID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := NewActionExecutorFull(nil, mutator, nil)

	// Back-compat path: no Subject set → falls through to the trigger
	// entity. Every pre-#147 rule must keep its behaviour.
	action := Action{
		Type:      ActionTypeAddTriple,
		Predicate: "rule.task.spawned",
		Object:    "task-001",
	}
	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "acme.ops.robot.gcs.coordinator.7"}))

	require.Len(t, mutator.addedTriples, 1)
	assert.Equal(t, "acme.ops.robot.gcs.coordinator.7", mutator.addedTriples[0].Subject,
		"empty Subject must fall back to ec.EntityID for back-compat")
}

func TestExecuteAddTriple_SubjectResolvedEmptyIsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := NewActionExecutorFull(nil, mutator, nil)

	// Trigger entity DOESN'T carry the referenced predicate — typo or
	// race. Must return an error, NOT silently fall back to EntityID
	// (which would mask the typo).
	ec := &ExecutionContext{
		EntityID: "acme.ops.robot.gcs.gather.99",
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				// no agent.loop.parent triple
			},
		},
	}
	action := Action{
		Type:      ActionTypeAddTriple,
		Subject:   "$entity.triple.agent.loop.parent",
		Predicate: "test.gather.completed-subtopic",
		Object:    "hydraulics",
	}
	err := executor.Execute(ctx, action, ec)
	require.Error(t, err, "empty-after-substitution must error, not silently fall back to EntityID")
	assert.Contains(t, err.Error(), "unresolved template variables")
	assert.Empty(t, mutator.addedTriples, "no triple should be persisted on subject-resolution failure")
}

// --- ExecuteRemoveTriple subject-override ---

func TestExecuteRemoveTriple_SubjectOverrideTargetsNamedEntity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := NewActionExecutorFull(nil, mutator, nil)

	action := Action{
		Type:      ActionTypeRemoveTriple,
		Subject:   "acme.ops.robot.gcs.plan.42",
		Predicate: "test.gather.completed-subtopic",
	}
	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "acme.ops.robot.gcs.gather.99"}))

	require.Len(t, mutator.removedTriples, 1)
	assert.Equal(t, "acme.ops.robot.gcs.plan.42", mutator.removedTriples[0].subject)
	assert.Equal(t, "test.gather.completed-subtopic", mutator.removedTriples[0].predicate)
}

func TestExecuteRemoveTriple_SubjectEmptyDefaultsToEntityID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := NewActionExecutorFull(nil, mutator, nil)

	action := Action{
		Type:      ActionTypeRemoveTriple,
		Predicate: "agent.loop.status",
	}
	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "acme.ops.robot.gcs.coordinator.7"}))

	require.Len(t, mutator.removedTriples, 1)
	assert.Equal(t, "acme.ops.robot.gcs.coordinator.7", mutator.removedTriples[0].subject)
}

func TestExecuteRemoveTriple_SubjectResolvedEmptyIsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := NewActionExecutorFull(nil, mutator, nil)

	ec := &ExecutionContext{
		EntityID: "acme.ops.robot.gcs.gather.99",
		Entity:   &gtypes.EntityState{Triples: []message.Triple{}},
	}
	action := Action{
		Type:      ActionTypeRemoveTriple,
		Subject:   "$entity.triple.agent.loop.parent",
		Predicate: "test.gather.completed-subtopic",
	}
	require.Error(t, executor.Execute(ctx, action, ec))
	assert.Empty(t, mutator.removedTriples)
}

// --- executeUpdateTriple subject-override ---

func TestExecuteUpdateTriple_SubjectSubstitutionResolved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := NewActionExecutorFull(nil, mutator, nil)

	ec := &ExecutionContext{
		EntityID: "acme.ops.robot.gcs.gather.99",
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: "agent.loop.parent", Object: "acme.ops.robot.gcs.plan.42"},
			},
		},
	}
	action := Action{
		Type:      ActionTypeUpdateTriple,
		Subject:   "$entity.triple.agent.loop.parent",
		Predicate: "test.plan.status",
		Object:    "synthesizing",
	}
	require.NoError(t, executor.Execute(ctx, action, ec))

	// Update is remove+add; both must target the resolved entity.
	require.Len(t, mutator.removedTriples, 1)
	require.Len(t, mutator.addedTriples, 1)
	assert.Equal(t, "acme.ops.robot.gcs.plan.42", mutator.removedTriples[0].subject)
	assert.Equal(t, "acme.ops.robot.gcs.plan.42", mutator.addedTriples[0].Subject)
}

func TestExecuteUpdateTriple_SubjectEmptyDefaultsToEntityID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := NewActionExecutorFull(nil, mutator, nil)

	action := Action{
		Type:      ActionTypeUpdateTriple,
		Predicate: "agent.loop.status",
		Object:    "running",
	}
	require.NoError(t, executor.Execute(ctx, action, &ExecutionContext{EntityID: "acme.ops.robot.gcs.coordinator.7"}))

	require.Len(t, mutator.addedTriples, 1)
	assert.Equal(t, "acme.ops.robot.gcs.coordinator.7", mutator.addedTriples[0].Subject)
}

func TestExecuteUpdateTriple_SubjectResolvedEmptyIsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mutator := &mockTripleMutator{}
	executor := NewActionExecutorFull(nil, mutator, nil)

	ec := &ExecutionContext{
		EntityID: "acme.ops.robot.gcs.gather.99",
		Entity:   &gtypes.EntityState{Triples: []message.Triple{}},
	}
	action := Action{
		Type:      ActionTypeUpdateTriple,
		Subject:   "$entity.triple.agent.loop.parent",
		Predicate: "test.plan.status",
		Object:    "synthesizing",
	}
	require.Error(t, executor.Execute(ctx, action, ec))
	assert.Empty(t, mutator.addedTriples)
}

// --- resolveTripleSubject unit cases ---

// TestResolveTripleSubject_TruthTable covers the four resolution
// paths from the helper itself. Pure function over (Action, EC).
func TestResolveTripleSubject_TruthTable(t *testing.T) {
	t.Parallel()
	entityID := semantictest.EntityID(t, "test", "rule", "subject", "override", "entity", "001")
	literalID := semantictest.EntityID(t, "test", "rule", "subject", "override", "literal", "001")
	childID := semantictest.EntityID(t, "test", "rule", "subject", "override", "child", "001")
	parentID := semantictest.EntityID(t, "test", "rule", "subject", "override", "parent", "001")
	cases := []struct {
		name      string
		action    Action
		ec        *ExecutionContext
		want      string
		wantError bool
	}{
		{
			name:   "empty subject defaults to EntityID",
			action: Action{},
			ec:     &ExecutionContext{EntityID: entityID},
			want:   entityID,
		},
		{
			name:   "literal subject overrides EntityID",
			action: Action{Subject: literalID},
			ec:     &ExecutionContext{EntityID: entityID},
			want:   literalID,
		},
		{
			name:   "subject substitutes from entity triples",
			action: Action{Subject: "$entity.triple.agent.loop.parent"},
			ec: &ExecutionContext{
				EntityID: childID,
				Entity: &gtypes.EntityState{
					Triples: []message.Triple{
						{Predicate: "agent.loop.parent", Object: parentID},
					},
				},
			},
			want: parentID,
		},
		{
			name:      "subject has unresolved tokens after substitution → error",
			action:    Action{Subject: "$entity.triple.test.fixture.nonexistent"},
			ec:        &ExecutionContext{EntityID: entityID, Entity: &gtypes.EntityState{}},
			wantError: true,
		},
		{
			// Distinct branch from the unresolved-token case: substitution
			// completed cleanly but produced an empty string (e.g.
			// $entity.id against an empty EntityID — possible on
			// message-path rules that don't have an entity in scope).
			// Covers the `if resolved == ""` check that the unresolved-
			// tokens case bypasses entirely.
			name:   "subject resolves to empty string (no surviving tokens) → error",
			action: Action{Subject: "$entity.id"},
			ec:     &ExecutionContext{EntityID: ""},
			// entity-id-audit:classify intentional-malformed "" line=304 column=40 surface=go-field:ExecutionContext.EntityID entity_id_invalid:empty empty-after-substitution rejection fixture
			wantError: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveTripleSubjectContext(context.Background(), tc.action, tc.ec)
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// silence unused-import warning when only ExecuteAddTriple/Remove
// paths are exercised; time is needed for triple construction
// elsewhere in this file.
var _ = time.Now
