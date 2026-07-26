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
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/stretchr/testify/require"
)

const (
	testReplaceContract    = "test-projection"
	testReplaceGroup       = "lifecycle"
	ownedPredicate         = "test.status.phase"
	ownedSibling           = "test.status.retired-at"
	siblingPredicate       = "test.display.label"
	appendPredicate        = "test.status.note"
	birthPredicate         = "test.status.created-at"
	foreignPredicate       = "test.status.foreign"
	outOfEnvelopePredicate = "test.status.unowned"
)

func init() {
	for _, predicate := range []string{
		ownedPredicate,
		ownedSibling,
		siblingPredicate,
		appendPredicate,
		birthPredicate,
		foreignPredicate,
		outOfEnvelopePredicate,
	} {
		vocabulary.Register(predicate)
	}
}

func replaceOwnedTestContracts() []projection.Contract {
	return []projection.Contract{{
		Name:          testReplaceContract,
		MessageType:   "test.status.v1",
		EntityPattern: "acme.ops.robotics.gcs.drone.*",
		Groups: []projection.PredicateGroup{
			{
				Name:       testReplaceGroup,
				Mode:       ownership.ModeReplaceOwned,
				Predicates: []string{ownedPredicate, ownedSibling},
			},
			{
				Name:       "display",
				Mode:       ownership.ModeReplaceOwned,
				Predicates: []string{siblingPredicate},
			},
			{
				Name:       "notes",
				Mode:       ownership.ModeAppendEvidence,
				Predicates: []string{appendPredicate},
			},
		},
		BirthPredicates: []string{birthPredicate},
		ForeignEdges: []projection.ForeignEdge{{
			Predicate:     foreignPredicate,
			Mode:          ownership.EdgeStrict,
			TargetPattern: "acme.ops.robotics.gcs.mission.*",
		}},
	}}
}

func replaceOwnedAction(predicate, object string) Action {
	return Action{
		Type:               ActionTypeReplaceOwned,
		ProjectionContract: testReplaceContract,
		ProjectionGroup:    testReplaceGroup,
		Predicate:          predicate,
		Object:             object,
	}
}

type capturingOwnedReplacer struct {
	requests []projection.ReplaceOwnedMutation
	receipt  projection.MutationReceipt
	err      error
}

func (replacer *capturingOwnedReplacer) ReplaceOwned(
	_ context.Context,
	request projection.ReplaceOwnedMutation,
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

func replaceOwnedExecutor(
	t *testing.T,
	replacer projection.OwnedReplacer,
	tracker revisionTracker,
) *ActionExecutor {
	t.Helper()
	index, err := buildProjectionTargetIndex(replaceOwnedTestContracts())
	require.NoError(t, err)
	executor := NewActionExecutor(nil)
	executor.SetOwnedReplacer(replacer)
	executor.setProjectionTargets(index, tracker)
	return executor
}

func TestReplaceOwnedAuthoringRequiresExactTarget(t *testing.T) {
	t.Parallel()
	index, err := buildProjectionTargetIndex(replaceOwnedTestContracts())
	require.NoError(t, err)

	tests := []struct {
		name   string
		action Action
		want   string
	}{
		{
			name:   "missing contract",
			action: Action{Type: ActionTypeReplaceOwned, ProjectionGroup: testReplaceGroup, Predicate: ownedPredicate},
			want:   "projection_contract is required",
		},
		{
			name:   "missing group",
			action: Action{Type: ActionTypeReplaceOwned, ProjectionContract: testReplaceContract, Predicate: ownedPredicate},
			want:   "projection_group is required",
		},
		{
			name: "unknown contract",
			action: Action{
				Type: ActionTypeReplaceOwned, ProjectionContract: "unknown",
				ProjectionGroup: testReplaceGroup, Predicate: ownedPredicate,
			},
			want: "unknown projection contract",
		},
		{
			name: "unknown or unnamed group",
			action: Action{
				Type: ActionTypeReplaceOwned, ProjectionContract: testReplaceContract,
				ProjectionGroup: "unnamed", Predicate: ownedPredicate,
			},
			want: "no named projection group",
		},
		{
			name: "wrong mode",
			action: Action{
				Type: ActionTypeReplaceOwned, ProjectionContract: testReplaceContract,
				ProjectionGroup: "notes", Predicate: appendPredicate,
			},
			want: "not replace-owned",
		},
		{
			name: "predicate outside selected group",
			action: Action{
				Type: ActionTypeReplaceOwned, ProjectionContract: testReplaceContract,
				ProjectionGroup: testReplaceGroup, Predicate: siblingPredicate,
			},
			want: "outside projection contract",
		},
		{
			name: "dynamic predicate",
			action: Action{
				Type: ActionTypeReplaceOwned, ProjectionContract: testReplaceContract,
				ProjectionGroup: testReplaceGroup, Predicate: "$message.predicate",
			},
			want: "must be a literal",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateReplaceOwnedAction(index, "rule-a", test.action)
			require.ErrorContains(t, err, test.want)
			require.True(t, errs.IsInvalid(err))
		})
	}

	target, err := index.resolve(testReplaceContract, testReplaceGroup, ownedPredicate)
	require.NoError(t, err)
	require.Equal(t, []string{ownedPredicate, ownedSibling}, target.Predicates)
}

func TestProjectionTargetIndexCopiesContractsAndRejectsAmbiguity(t *testing.T) {
	t.Parallel()
	contracts := replaceOwnedTestContracts()
	index, err := buildProjectionTargetIndex(contracts)
	require.NoError(t, err)
	contracts[0].Groups[0].Predicates[0] = outOfEnvelopePredicate
	_, err = index.resolve(testReplaceContract, testReplaceGroup, ownedPredicate)
	require.NoError(t, err, "index must retain its copied target set")
	_, err = index.resolve(testReplaceContract, testReplaceGroup, outOfEnvelopePredicate)
	require.Error(t, err)

	duplicate := append(replaceOwnedTestContracts(), replaceOwnedTestContracts()[0])
	_, err = buildProjectionTargetIndex(duplicate)
	require.ErrorContains(t, err, "more than once")
}

func TestReplaceOwnedReconcilesSelectedCompleteGroup(t *testing.T) {
	t.Parallel()
	replacer := &capturingOwnedReplacer{}
	executor := replaceOwnedExecutor(t, replacer, nil)
	entityID := "acme.ops.robotics.gcs.drone.001"

	require.NoError(t, executor.Execute(
		context.Background(),
		replaceOwnedAction(ownedPredicate, "retired"),
		&ExecutionContext{EntityID: entityID},
	))
	require.Len(t, replacer.requests, 1)
	request := replacer.requests[0]
	require.Equal(t, testReplaceContract, request.Contract)
	require.Equal(t, testReplaceGroup, request.Group)
	require.Equal(t, entityID, request.EntityID)
	require.Len(t, request.Desired, 1)
	require.Equal(t, ownedPredicate, request.Desired[0].Predicate)
	require.Equal(t, "retired", request.Desired[0].Object)
	require.NotEmpty(t, request.Metadata.RequestID)
	require.Equal(t, "rule_engine", request.Metadata.Source)

	replacer.requests = nil
	require.NoError(t, executor.Execute(
		context.Background(),
		replaceOwnedAction(ownedPredicate, ""),
		&ExecutionContext{EntityID: entityID},
	))
	require.Len(t, replacer.requests, 1)
	require.Empty(t, replacer.requests[0].Desired, "empty object clears the complete selected group")
}

func TestReplaceOwnedPreservesTypedSubstitution(t *testing.T) {
	t.Parallel()
	values := []any{7, 3.14, true, "ready", map[string]any{"nested": true}}
	for _, value := range values {
		value := value
		t.Run(reflect.TypeOf(value).String(), func(t *testing.T) {
			replacer := &capturingOwnedReplacer{}
			executor := replaceOwnedExecutor(t, replacer, nil)
			action := replaceOwnedAction(ownedPredicate, "$message.value")
			err := executor.Execute(context.Background(), action, &ExecutionContext{
				EntityID:    "acme.ops.robotics.gcs.drone.001",
				MessageData: map[string]any{"value": value},
			})
			require.NoError(t, err)
			require.Equal(t, value, replacer.requests[0].Desired[0].Object)
		})
	}
}

func TestReplaceOwnedTracksReceiptAndPreservesTypedError(t *testing.T) {
	t.Parallel()
	tracker := &capturingRevisionTracker{}
	replacer := &capturingOwnedReplacer{
		receipt: projection.MutationReceipt{KVRevision: 42, Commit: projection.CommitVerified},
	}
	executor := replaceOwnedExecutor(t, replacer, tracker)
	state := &MatchState{RuleID: "rule-a", Iteration: 3}
	entityID := "acme.ops.robotics.gcs.drone.001"
	require.NoError(t, executor.Execute(
		context.Background(),
		replaceOwnedAction(ownedPredicate, "retired"),
		&ExecutionContext{EntityID: entityID, State: state},
	))
	require.Equal(t, "rule-a", tracker.ruleID)
	require.Equal(t, entityID, tracker.entityID)
	require.Equal(t, uint64(42), tracker.revision)

	cause := errors.New("stale lease")
	classified := errs.WrapInvalid(cause, "projection", "replace", "stale owner")
	mutationErr := &projection.MutationError{
		Operation: projection.MutationOperationReplaceOwned,
		Kind:      projection.MutationStaleOwnerToken,
		Code:      "owner_lease_stale",
		Class:     errs.ErrorInvalid,
		Commit:    projection.CommitNotCommitted,
		Err:       classified,
	}
	replacer.err = mutationErr
	err := executor.Execute(
		context.Background(),
		replaceOwnedAction(ownedPredicate, "retired"),
		&ExecutionContext{EntityID: entityID, State: state},
	)
	require.Error(t, err)
	var gotMutation *projection.MutationError
	require.ErrorAs(t, err, &gotMutation)
	require.Same(t, mutationErr, gotMutation)
	var gotClassified *errs.ClassifiedError
	require.ErrorAs(t, err, &gotClassified)
	require.ErrorIs(t, err, cause)
	require.Equal(t, projection.MutationStaleOwnerToken, gotMutation.Kind)
	require.Equal(t, projection.CommitNotCommitted, gotMutation.Commit)
}

func TestReplaceOwnedFileAndHotReloadUseFrozenTargetIndex(t *testing.T) {
	t.Parallel()
	config := mustTestConfig(t, "replace-owned-test")
	config.ProjectionContracts = replaceOwnedTestContracts()
	config.InlineRules = []Definition{{
		ID: "initial", Type: "test_rule", Name: "initial", Enabled: true,
		OnEnter: []Action{replaceOwnedAction(ownedPredicate, "ready")},
	}}
	processor, err := NewProcessor(nil, &config)
	require.NoError(t, err)
	require.NoError(t, processor.loadRules())

	validRule := map[string]any{
		"type": "test_rule",
		"on_enter": []any{map[string]any{
			"type": ActionTypeReplaceOwned, "projection_contract": testReplaceContract,
			"projection_group": testReplaceGroup, "predicate": ownedPredicate, "object": "ready",
		}},
	}
	require.NoError(t, processor.ValidateConfigUpdate(map[string]any{
		"rules": map[string]any{"hot": validRule},
	}))

	validRule["on_enter"].([]any)[0].(map[string]any)["projection_group"] = "notes"
	err = processor.ValidateConfigUpdate(map[string]any{
		"rules": map[string]any{"hot": validRule},
	})
	require.ErrorContains(t, err, "not replace-owned")
	for _, field := range []string{"pack_id", "projection_contracts", "projection_targets", "mutation_client"} {
		err = processor.ValidateConfigUpdate(map[string]any{field: "changed"})
		require.ErrorContains(t, err, "static")
	}
}

func TestReplaceOwnedInitialRuleSnapshotIsNotReread(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "rules.json")
	good := `[{"id":"snapshot","type":"test_rule","name":"snapshot","enabled":true,"on_enter":[{"type":"replace_owned","projection_contract":"test-projection","projection_group":"lifecycle","predicate":"test.status.phase","object":"ready"}]}]`
	require.NoError(t, os.WriteFile(path, []byte(good), 0o600))
	config := mustTestConfig(t, "snapshot-pack")
	config.ProjectionContracts = replaceOwnedTestContracts()
	config.RulesFiles = []string{path}
	processor, err := NewProcessor(nil, &config)
	require.NoError(t, err)
	require.NoError(t, processor.PreflightProjectionMutations())

	bad := strings.ReplaceAll(good, `"projection_group":"lifecycle"`, `"projection_group":"notes"`)
	require.NoError(t, os.WriteFile(path, []byte(bad), 0o600))
	require.NoError(t, processor.loadRules())
	require.Contains(t, processor.rules, "snapshot")
}
