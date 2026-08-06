package rule

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/stretchr/testify/require"
)

const (
	testReconcileContract = "test-projection"
	testReconcileGroup    = "lifecycle"
	reconcilePredicate    = "test.status.phase"
	reconcileSibling      = "test.status.retired-at"
	siblingPredicate      = "test.display.label"
	appendPredicate       = "test.status.note"
	birthPredicate        = "test.status.created-at"
	otherPredicate        = "test.status.foreign"
	unselectedPredicate   = "test.status.unowned"
)

func registerReconcileTestVocabulary(t testing.TB) {
	t.Helper()
	vocabulary.Register(reconcilePredicate)
	vocabulary.Register(reconcileSibling)
	vocabulary.Register(siblingPredicate)
	vocabulary.Register(appendPredicate)
	vocabulary.Register(birthPredicate)
	vocabulary.Register(otherPredicate)
	vocabulary.Register(unselectedPredicate)
}

func reconcileTestContracts(t testing.TB) []projection.Contract {
	t.Helper()
	registerReconcileTestVocabulary(t)
	return []projection.Contract{{
		Name:          testReconcileContract,
		MessageType:   "test.status.v1",
		EntityPattern: "acme.ops.robotics.gcs.drone.*",
		Groups: []projection.PredicateGroup{
			{
				Name:       testReconcileGroup,
				Mode:       projection.ModeReconcile,
				Predicates: []string{reconcilePredicate, reconcileSibling},
			},
			{
				Name:       "display",
				Mode:       projection.ModeReconcile,
				Predicates: []string{siblingPredicate},
			},
			{
				Name:       "notes",
				Mode:       projection.ModeAppend,
				Predicates: []string{appendPredicate},
			},
		},
		BirthPredicates: []string{birthPredicate},
	}}
}

func reconcileAction(predicate, object string) Action {
	return Action{
		Type:               ActionTypeReconcilePredicates,
		ProjectionContract: testReconcileContract,
		ProjectionGroup:    testReconcileGroup,
		Predicate:          predicate, // predicate-audit:unrelated {"column":23,"surface":"go-field:Predicate","value":"","basis":"reviewed helper forwards a caller-supplied registered fixture predicate"}
		Object:             object,
	}
}

type capturingPredicateReconciler struct {
	requests []projection.ReconcileMutation
	receipt  projection.MutationReceipt
	err      error
}

func (replacer *capturingPredicateReconciler) Reconcile(
	_ context.Context,
	request projection.ReconcileMutation,
) (projection.MutationReceipt, error) {
	replacer.requests = append(replacer.requests, request)
	return replacer.receipt, replacer.err
}

type capturingRevisionTracker struct {
	ruleID   string
	entityID string
	revision uint64
}

func (tracker *capturingRevisionTracker) trackRuleRevision(ruleID, entityID string, revision uint64) {
	tracker.ruleID = ruleID
	tracker.entityID = entityID
	tracker.revision = revision
}

func reconcileExecutor(
	t *testing.T,
	replacer projection.PredicateReconciler,
	tracker revisionTracker,
) *ActionExecutor {
	t.Helper()
	index, err := buildProjectionTargetIndex(reconcileTestContracts(t))
	require.NoError(t, err)
	executor := NewActionExecutor(nil)
	executor.SetPredicateReconciler(replacer)
	executor.setProjectionTargets(index, tracker)
	return executor
}

func TestReconcileAuthoringRequiresExactTarget(t *testing.T) {
	t.Parallel()
	index, err := buildProjectionTargetIndex(reconcileTestContracts(t))
	require.NoError(t, err)

	tests := []struct {
		name   string
		action Action
		want   string
	}{
		{
			name:   "missing contract",
			action: Action{Type: ActionTypeReconcilePredicates, ProjectionGroup: testReconcileGroup, Predicate: reconcilePredicate},
			want:   "projection_contract is required",
		},
		{
			name:   "missing group",
			action: Action{Type: ActionTypeReconcilePredicates, ProjectionContract: testReconcileContract, Predicate: reconcilePredicate},
			want:   "projection_group is required",
		},
		{
			name: "unknown contract",
			action: Action{
				Type: ActionTypeReconcilePredicates, ProjectionContract: "unknown",
				ProjectionGroup: testReconcileGroup, Predicate: reconcilePredicate,
			},
			want: "unknown projection contract",
		},
		{
			name: "unknown or unnamed group",
			action: Action{
				Type: ActionTypeReconcilePredicates, ProjectionContract: testReconcileContract,
				ProjectionGroup: "unnamed", Predicate: reconcilePredicate,
			},
			want: "no named projection group",
		},
		{
			name: "wrong mode",
			action: Action{
				Type: ActionTypeReconcilePredicates, ProjectionContract: testReconcileContract,
				ProjectionGroup: "notes", Predicate: appendPredicate,
			},
			want: "not reconcile",
		},
		{
			name: "predicate outside selected group",
			action: Action{
				Type: ActionTypeReconcilePredicates, ProjectionContract: testReconcileContract,
				ProjectionGroup: testReconcileGroup, Predicate: siblingPredicate,
			},
			want: "outside projection contract",
		},
		{
			name: "birth predicate outside selected group",
			action: Action{
				Type: ActionTypeReconcilePredicates, ProjectionContract: testReconcileContract,
				ProjectionGroup: testReconcileGroup, Predicate: birthPredicate,
			},
			want: "outside projection contract",
		},
		{
			name: "foreign edge outside selected group",
			action: Action{
				Type: ActionTypeReconcilePredicates, ProjectionContract: testReconcileContract,
				ProjectionGroup: testReconcileGroup, Predicate: otherPredicate,
			},
			want: "outside projection contract",
		},
		{
			name: "dynamic predicate",
			action: Action{
				Type: ActionTypeReconcilePredicates, ProjectionContract: testReconcileContract,
				ProjectionGroup: testReconcileGroup, Predicate: "$message.predicate", // predicate-audit:invalid {"kind":"stored-predicate","value":"$message.predicate","reason":"arity"}
			},
			want: "must be a literal",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateReconcileAction(index, "rule-a", test.action)
			require.ErrorContains(t, err, test.want)
			require.True(t, errs.IsInvalid(err))
		})
	}

	target, err := index.resolve(testReconcileContract, testReconcileGroup, reconcilePredicate)
	require.NoError(t, err)
	require.Equal(t, []string{reconcilePredicate, reconcileSibling}, target.Predicates)
}

func TestProjectionTargetIndexCopiesContractsAndRejectsAmbiguity(t *testing.T) {
	t.Parallel()
	contracts := reconcileTestContracts(t)
	index, err := buildProjectionTargetIndex(contracts)
	require.NoError(t, err)
	contracts[0].Groups[0].Predicates[0] = unselectedPredicate
	_, err = index.resolve(testReconcileContract, testReconcileGroup, reconcilePredicate)
	require.NoError(t, err, "index must retain its copied target set")
	_, err = index.resolve(testReconcileContract, testReconcileGroup, unselectedPredicate)
	require.Error(t, err)

	duplicate := append(reconcileTestContracts(t), reconcileTestContracts(t)[0])
	_, err = buildProjectionTargetIndex(duplicate)
	require.ErrorContains(t, err, "duplicate contract")
}

func TestReconcileReconcilesSelectedCompleteGroup(t *testing.T) {
	t.Parallel()
	replacer := &capturingPredicateReconciler{}
	executor := reconcileExecutor(t, replacer, nil)
	entityID := "acme.ops.robotics.gcs.drone.001"

	require.NoError(t, executor.Execute(
		context.Background(),
		reconcileAction(reconcilePredicate, "retired"),
		&ExecutionContext{EntityID: entityID},
	))
	require.Len(t, replacer.requests, 1)
	request := replacer.requests[0]
	require.Equal(t, testReconcileContract, request.Contract)
	require.Equal(t, testReconcileGroup, request.Group)
	require.Equal(t, entityID, request.EntityID)
	require.Len(t, request.Desired, 1)
	require.Equal(t, reconcilePredicate, request.Desired[0].Predicate)
	require.Equal(t, "retired", request.Desired[0].Object)
	require.NotEmpty(t, request.Metadata.RequestID)
	require.Equal(t, "rule_engine", request.Metadata.Source)

	replacer.requests = nil
	require.NoError(t, executor.Execute(
		context.Background(),
		reconcileAction(reconcilePredicate, ""),
		&ExecutionContext{EntityID: entityID},
	))
	require.Len(t, replacer.requests, 1)
	require.Empty(t, replacer.requests[0].Desired, "raw empty object clears the entire selected named group")
}

func TestReconcileRawNonEmptySubstitutionResolvingEmptyStillReplaces(t *testing.T) {
	t.Parallel()
	replacer := &capturingPredicateReconciler{}
	executor := reconcileExecutor(t, replacer, nil)

	err := executor.Execute(
		context.Background(),
		reconcileAction(reconcilePredicate, "$message.value"),
		&ExecutionContext{
			EntityID:    "acme.ops.robotics.gcs.drone.001",
			MessageData: map[string]any{"value": ""},
		},
	)
	require.NoError(t, err)
	require.Len(t, replacer.requests, 1)
	require.Len(t, replacer.requests[0].Desired, 1,
		"raw non-empty object authors a replacement even when substitution resolves empty")
	require.Equal(t, reconcilePredicate, replacer.requests[0].Desired[0].Predicate)
	require.Equal(t, "", replacer.requests[0].Desired[0].Object)
}

func TestReconcilePreservesTypedSubstitution(t *testing.T) {
	t.Parallel()
	values := []any{7, 3.14, true, "ready", map[string]any{"nested": true}}
	for _, value := range values {
		value := value
		t.Run(reflect.TypeOf(value).String(), func(t *testing.T) {
			replacer := &capturingPredicateReconciler{}
			executor := reconcileExecutor(t, replacer, nil)
			action := reconcileAction(reconcilePredicate, "$message.value")
			err := executor.Execute(context.Background(), action, &ExecutionContext{
				EntityID:    "acme.ops.robotics.gcs.drone.001",
				MessageData: map[string]any{"value": value},
			})
			require.NoError(t, err)
			require.Equal(t, value, replacer.requests[0].Desired[0].Object)
		})
	}
}

func TestReconcileTracksReceiptAndPreservesTypedError(t *testing.T) {
	t.Parallel()
	tracker := &capturingRevisionTracker{}
	replacer := &capturingPredicateReconciler{
		receipt: projection.MutationReceipt{KVRevision: 42, Commit: projection.CommitVerified},
	}
	executor := reconcileExecutor(t, replacer, tracker)
	state := &MatchState{RuleID: "rule-a", Iteration: 3}
	entityID := "acme.ops.robotics.gcs.drone.001"
	require.NoError(t, executor.Execute(
		context.Background(),
		reconcileAction(reconcilePredicate, "retired"),
		&ExecutionContext{EntityID: entityID, State: state},
	))
	require.Equal(t, "rule-a", tracker.ruleID)
	require.Equal(t, entityID, tracker.entityID)
	require.Equal(t, uint64(42), tracker.revision)

	cause := errors.New("invalid reconcile")
	classified := errs.WrapInvalid(cause, "projection", "reconcile", "invalid predicate set")
	mutationErr := &projection.MutationError{
		Operation: projection.MutationOperationReconcile,
		Kind:      projection.MutationInvalid,
		Code:      "invalid",
		Class:     errs.ErrorInvalid,
		Commit:    projection.CommitNotCommitted,
		Err:       classified,
	}
	replacer.err = mutationErr
	err := executor.Execute(
		context.Background(),
		reconcileAction(reconcilePredicate, "retired"),
		&ExecutionContext{EntityID: entityID, State: state},
	)
	require.Error(t, err)
	var gotMutation *projection.MutationError
	require.ErrorAs(t, err, &gotMutation)
	require.Same(t, mutationErr, gotMutation)
	var gotClassified *errs.ClassifiedError
	require.ErrorAs(t, err, &gotClassified)
	require.ErrorIs(t, err, cause)
	require.Equal(t, projection.MutationInvalid, gotMutation.Kind)
	require.Equal(t, projection.CommitNotCommitted, gotMutation.Commit)
}

func TestReconcileFileAndHotReloadUseFrozenTargetIndex(t *testing.T) {
	t.Parallel()
	config := mustTestConfig(t, "reconcile-test")
	config.ProjectionContracts = reconcileTestContracts(t)
	config.InlineRules = []Definition{{
		ID: "initial", Type: "test_rule", Name: "initial", Enabled: true,
		OnEnter: []Action{reconcileAction(reconcilePredicate, "ready")},
	}}
	processor, err := NewProcessor(nil, &config)
	require.NoError(t, err)
	require.NoError(t, processor.loadRules())

	validRule := map[string]any{
		"type": "test_rule",
		"on_enter": []any{map[string]any{
			"type": ActionTypeReconcilePredicates, "projection_contract": testReconcileContract,
			"projection_group": testReconcileGroup, "predicate": reconcilePredicate, "object": "ready",
		}},
	}
	require.NoError(t, processor.ValidateConfigUpdate(map[string]any{
		"rules": map[string]any{"hot": validRule},
	}))

	validRule["on_enter"].([]any)[0].(map[string]any)["projection_group"] = "notes"
	err = processor.ValidateConfigUpdate(map[string]any{
		"rules": map[string]any{"hot": validRule},
	})
	require.ErrorContains(t, err, "not reconcile")
	for _, field := range []string{"pack_id", "projection_contracts", "projection_targets", "mutation_client"} {
		err = processor.ValidateConfigUpdate(map[string]any{field: "changed"})
		require.ErrorContains(t, err, "static")
	}
}

func TestReconcileInitialRuleSnapshotIsNotReread(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "rules.json")
	good := `[{"id":"snapshot","type":"test_rule","name":"snapshot","enabled":true,"on_enter":[{"type":"reconcile_predicates","projection_contract":"test-projection","projection_group":"lifecycle","predicate":"test.status.phase","object":"ready"}]}]`
	require.NoError(t, os.WriteFile(path, []byte(good), 0o600))
	config := mustTestConfig(t, "snapshot-pack")
	config.ProjectionContracts = reconcileTestContracts(t)
	config.RulesFiles = []string{path}
	processor, err := NewProcessor(nil, &config)
	require.NoError(t, err)
	require.NoError(t, processor.PreflightProjectionMutations())

	bad := strings.ReplaceAll(good, `"projection_group":"lifecycle"`, `"projection_group":"notes"`)
	require.NoError(t, os.WriteFile(path, []byte(bad), 0o600))
	require.NoError(t, processor.loadRules())
	require.Contains(t, processor.rules, "snapshot")
}
