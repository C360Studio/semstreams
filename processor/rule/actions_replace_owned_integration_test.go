//go:build integration

package rule

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/stretchr/testify/require"
)

const replaceOwnedIntegrationOwner = "rule-pack.replace-owned-integration"

type replaceOwnedIntegrationHarness struct {
	ctx         context.Context
	nats        *natsclient.Client
	registry    *ownership.Registry
	heartbeater *ownership.Heartbeater
	ingest      *graphingest.Component
	client      *projection.MutationClient
	contract    projection.Contract
}

func newReplaceOwnedIntegrationHarness(t *testing.T) *replaceOwnedIntegrationHarness {
	t.Helper()
	registerReplaceOwnedTestVocabulary(t)

	ctx, cancel := context.WithCancel(t.Context())
	testClient := natsclient.NewTestClient(
		t,
		natsclient.WithKV(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name: "ENTITY", Subjects: []string{"entity.>"},
		}),
	)
	registry, err := ownership.EnsureBuckets(
		ctx,
		testClient.Client,
		slog.Default(),
		vocabulary.InverseResolver,
	)
	require.NoError(t, err)
	heartbeater := registry.NewHeartbeater(20 * time.Millisecond)
	go heartbeater.Run(ctx)

	contract := replaceOwnedTestContracts(t)[0]
	client, err := projection.BindMutationClient(ctx, projection.MutationClientConfig{
		NATS:        testClient.Client,
		Registry:    registry,
		Heartbeater: heartbeater,
		Owner:       replaceOwnedIntegrationOwner,
		Contracts:   []projection.Contract{contract},
		Timeout:     2 * time.Second,
		Retry: natsclient.RetryConfig{
			MaxRetries:        1,
			InitialBackoff:    time.Millisecond,
			BackoffMultiplier: 1,
		},
	})
	require.NoError(t, err)

	config := graphingest.DefaultConfig()
	config.EnforceOwnerLease = true
	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)
	created, err := graphingest.CreateGraphIngest(
		rawConfig,
		component.Dependencies{NATSClient: testClient.Client},
	)
	require.NoError(t, err)
	ingest := created.(*graphingest.Component)
	require.NoError(t, ingest.Initialize())
	require.NoError(t, ingest.Start(ctx))
	require.NoError(t, testClient.GetNativeConnection().Flush())

	t.Cleanup(func() {
		_ = ingest.Stop(5 * time.Second)
		cancel()
	})
	return &replaceOwnedIntegrationHarness{
		ctx:         ctx,
		nats:        testClient.Client,
		registry:    registry,
		heartbeater: heartbeater,
		ingest:      ingest,
		client:      client,
		contract:    contract,
	}
}

func (h *replaceOwnedIntegrationHarness) createEntity(
	t *testing.T,
	entityID string,
	triples ...message.Triple,
) projection.MutationReceipt {
	t.Helper()
	for i := range triples {
		triples[i].Subject = entityID
		triples[i].Confidence = 1
	}
	receipt, err := h.client.CreateWithTriples(h.ctx, projection.CreateMutation{
		Contract: testReplaceContract,
		Entity: &graph.EntityState{
			ID: entityID,
			MessageType: message.Type{
				Domain: "test", Category: "status", Version: "v1",
			},
		},
		Triples: triples,
		Metadata: projection.MutationMetadata{
			RequestID: "create-" + entityID,
			TraceID:   "trace-" + entityID,
			Source:    "rule-integration",
			Timestamp: time.Now().UTC(),
		},
	})
	require.NoError(t, err)
	require.Equal(t, projection.CommitVerified, receipt.Commit)
	require.Positive(t, receipt.KVRevision)
	return receipt
}

func exactRuleTripleCount(entity *graph.EntityState, predicate string, object any) int {
	count := 0
	for _, triple := range entity.Triples {
		if triple.Predicate == predicate && triple.Object == object {
			count++
		}
	}
	return count
}

func TestIntegration_ReplaceOwnedActionReconcilesCompleteGroupAndTracksRevision(t *testing.T) {
	harness := newReplaceOwnedIntegrationHarness(t)
	const entityID = "acme.ops.robotics.gcs.drone.rule-001"
	harness.createEntity(
		t,
		entityID,
		message.Triple{Predicate: ownedPredicate, Object: "active"},
		message.Triple{Predicate: ownedSibling, Object: "yesterday"},
		message.Triple{Predicate: siblingPredicate, Object: "display-me"},
		message.Triple{Predicate: birthPredicate, Object: "born-once"},
	)

	tracker := &capturingRevisionTracker{}
	executor := replaceOwnedExecutor(t, harness.client, tracker)
	err := executor.Execute(
		harness.ctx,
		replaceOwnedAction(ownedPredicate, "retired"),
		&ExecutionContext{
			EntityID: entityID,
			State:    &MatchState{RuleID: "retire-rule", Iteration: 2},
		},
	)
	require.NoError(t, err)
	require.Positive(t, tracker.revision)
	require.Equal(t, "retire-rule", tracker.ruleID)
	require.Equal(t, entityID, tracker.entityID)

	entity, err := harness.client.ReadAuthoritative(harness.ctx, entityID)
	require.NoError(t, err)
	require.Equal(t, 1, exactRuleTripleCount(entity, ownedPredicate, "retired"))
	require.Zero(t, exactRuleTripleCount(entity, ownedSibling, "yesterday"),
		"omitted selected-group sibling must be deleted")
	require.Equal(t, 1, exactRuleTripleCount(entity, siblingPredicate, "display-me"),
		"sibling replace-owned group must remain isolated")
	require.Equal(t, 1, exactRuleTripleCount(entity, birthPredicate, "born-once"),
		"create-only birth predicate must remain outside replacement")
}

func TestIntegration_ReplaceOwnedActionNotFoundDoesNotVivify(t *testing.T) {
	harness := newReplaceOwnedIntegrationHarness(t)
	const entityID = "acme.ops.robotics.gcs.drone.missing"
	executor := replaceOwnedExecutor(t, harness.client, nil)

	err := executor.Execute(
		harness.ctx,
		replaceOwnedAction(ownedPredicate, "retired"),
		&ExecutionContext{EntityID: entityID, State: &MatchState{RuleID: "missing-rule"}},
	)
	var mutationErr *projection.MutationError
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, projection.MutationNotFound, mutationErr.Kind)
	require.Equal(t, graph.ErrorCodeEntityNotFound, mutationErr.Code)
	require.Equal(t, projection.CommitNotCommitted, mutationErr.Commit)

	_, readErr := harness.client.ReadAuthoritative(harness.ctx, entityID)
	require.ErrorAs(t, readErr, &mutationErr)
	require.Equal(t, projection.MutationNotFound, mutationErr.Kind)
}

func TestIntegration_ReplaceOwnedActionPreservesStaleTokenFailure(t *testing.T) {
	harness := newReplaceOwnedIntegrationHarness(t)
	const entityID = "acme.ops.robotics.gcs.drone.stale-001"
	harness.createEntity(
		t,
		entityID,
		message.Triple{Predicate: ownedPredicate, Object: "active"},
	)

	replacementRegistry, err := ownership.EnsureBuckets(
		harness.ctx,
		harness.nats,
		slog.Default(),
		vocabulary.InverseResolver,
	)
	require.NoError(t, err)
	_, err = projection.Bind(
		harness.ctx,
		replacementRegistry,
		replaceOwnedIntegrationOwner,
		harness.contract,
	)
	require.NoError(t, err)

	executor := replaceOwnedExecutor(t, harness.client, nil)
	err = executor.Execute(
		harness.ctx,
		replaceOwnedAction(ownedPredicate, "must-not-land"),
		&ExecutionContext{EntityID: entityID, State: &MatchState{RuleID: "stale-rule"}},
	)
	var mutationErr *projection.MutationError
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, projection.MutationStaleOwnerToken, mutationErr.Kind)
	require.Equal(t, graph.ErrorCodeOwnerLeaseStale, mutationErr.Code)
	require.Equal(t, errs.ErrorInvalid, mutationErr.Class)
	require.Equal(t, projection.CommitNotCommitted, mutationErr.Commit)
	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	require.True(t, errors.Is(err, classified))

	entity, readErr := harness.client.ReadAuthoritative(harness.ctx, entityID)
	require.NoError(t, readErr)
	require.Equal(t, 1, exactRuleTripleCount(entity, ownedPredicate, "active"))
	require.Zero(t, exactRuleTripleCount(entity, ownedPredicate, "must-not-land"))
}
