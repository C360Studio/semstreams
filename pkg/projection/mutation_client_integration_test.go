//go:build integration

package projection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	gonats "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/ownership"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/c360studio/semstreams/vocabulary"
)

const (
	integrationOwner          = "projection-integration-owner"
	integrationContractName   = "test.projection.widget"
	integrationOwnedLabel     = "test.projection.label"
	integrationOwnedState     = "test.projection.state"
	integrationEvidence       = "test.projection.evidence"
	integrationForeignFact    = "test.foreign.note"
	integrationForeignEdge    = "test.projection.link"
	integrationMessageTypeKey = "test.fixture.v1"
)

var integrationMessageType = message.Type{
	Domain: "test", Category: "fixture", Version: "v1",
}

type mutationIntegrationHarness struct {
	ctx        context.Context
	cancel     context.CancelFunc
	testClient *natsclient.TestClient
	nats       *natsclient.Client
	registry   *ownership.Registry
	component  *graphingest.Component
	client     *MutationClient
	contract   Contract
}

func TestIntegration_BindMutationClientModeMatrix(t *testing.T) {
	ctx := t.Context()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	registry, err := ownership.EnsureBuckets(ctx, testClient.Client, nil, nil)
	require.NoError(t, err)
	heartbeater := registry.NewHeartbeater(time.Hour)
	tests := []struct {
		name       string
		contract   Contract
		registered bool
		owning     bool
	}{
		{
			name: "birth",
			contract: Contract{
				Name: "client-birth", MessageType: "test.client-birth.v1",
				EntityPattern:   "c360.semconnect.systems.csapi.client-birth.*",
				BirthPredicates: []string{"sensorml.process.uid"},
			},
		},
		{
			name: "foreign edge",
			contract: Contract{
				Name: "client-foreign", MessageType: "test.client-foreign.v1",
				EntityPattern: "c360.semconnect.systems.csapi.client-foreign.*",
				ForeignEdges: []ForeignEdge{{
					Predicate: "sensorml.component.is-hosted-by", Mode: ownership.EdgeStrict,
					TargetPattern: sysPat,
				}},
			},
			registered: true,
		},
		{
			name: "append",
			contract: Contract{
				Name: "client-append", MessageType: "test.client-append.v1",
				EntityPattern: "c360.semconnect.systems.csapi.client-append.*",
				Groups: []PredicateGroup{{
					Mode: ownership.ModeAppendEvidence, Predicates: []string{"shared.value.p"},
				}},
			},
			registered: true,
		},
		{
			name: "foreign edge and append",
			contract: Contract{
				Name: "client-foreign-append", MessageType: "test.client-foreign-append.v1",
				EntityPattern: "c360.semconnect.systems.csapi.client-foreign-append.*",
				Groups: []PredicateGroup{{
					Mode: ownership.ModeAppendEvidence, Predicates: []string{"shared.value.p"},
				}},
				ForeignEdges: []ForeignEdge{{
					Predicate: "sensorml.component.is-hosted-by", Mode: ownership.EdgeStrict,
					TargetPattern: sysPat,
				}},
			},
			registered: true,
		},
		{
			name: "owning",
			contract: Contract{
				Name: "client-owning", MessageType: "test.client-owning.v1",
				EntityPattern: "c360.semconnect.systems.csapi.client-owning.*",
				Groups: []PredicateGroup{{
					Mode: ownership.ModeReplaceOwned, Predicates: []string{"test.value.p"},
				}},
			},
			registered: true,
			owning:     true,
		},
		{
			name: "mixed",
			contract: Contract{
				Name: "client-mixed", MessageType: "test.client-mixed.v1",
				EntityPattern: "c360.semconnect.systems.csapi.client-mixed.*",
				Groups: []PredicateGroup{
					{Mode: ownership.ModeCASTransition, Predicates: []string{"test.value.p"}},
					{Mode: ownership.ModeAppendEvidence, Predicates: []string{"shared.value.p"}},
				},
				ForeignEdges: []ForeignEdge{{
					Predicate: "sensorml.component.is-hosted-by", Mode: ownership.EdgeStrict,
					TargetPattern: sysPat,
				}},
			},
			registered: true,
			owning:     true,
		},
	}

	for index, test := range tests {
		owner := fmt.Sprintf("client-matrix-%d", index)
		client, bindErr := BindMutationClient(ctx, MutationClientConfig{
			NATS:        testClient.Client,
			Registry:    registry,
			Heartbeater: heartbeater,
			Owner:       owner,
			Contracts:   []Contract{test.contract},
		})
		require.NoError(t, bindErr, test.name)
		require.NotNil(t, client, test.name)
		require.Equal(t, !test.owning, client.token.IsZero(), test.name)
		require.Equal(t, test.owning, heartbeater.IsEnrolled(owner), test.name)

		repeat, repeatErr := BindMutationClient(ctx, MutationClientConfig{
			NATS:        testClient.Client,
			Registry:    registry,
			Heartbeater: heartbeater,
			Owner:       owner,
			Contracts:   []Contract{test.contract},
		})
		if test.registered {
			require.Nil(t, repeat, test.name)
			require.ErrorIs(t, repeatErr, ownership.ErrOwnerAlreadyBound, test.name)
		} else {
			require.NoError(t, repeatErr, test.name)
			require.NotNil(t, repeat, test.name)
			require.True(t, repeat.token.IsZero(), test.name)
		}
	}
}

func newMutationIntegrationHarness(t *testing.T) *mutationIntegrationHarness {
	t.Helper()

	t.Cleanup(vocabulary.SnapshotRegistry())
	vocabulary.Register(integrationOwnedLabel)
	vocabulary.Register(integrationOwnedState)
	vocabulary.Register(integrationEvidence)
	vocabulary.Register(integrationForeignFact)
	vocabulary.Register(integrationForeignEdge)

	ctx, cancel := context.WithCancel(t.Context())
	streams := []natsclient.TestStreamConfig{{
		Name: "ENTITY", Subjects: []string{"entity.>"},
	}}
	testClient := natsclient.NewTestClient(
		t,
		natsclient.WithKV(),
		natsclient.WithStreams(streams...),
	)
	registry, err := ownership.EnsureBuckets(ctx, testClient.Client, nil, nil)
	require.NoError(t, err)

	contract := mutationIntegrationContract()
	heartbeater := registry.NewHeartbeater(20 * time.Millisecond)
	go heartbeater.Run(ctx)
	client, err := BindMutationClient(ctx, MutationClientConfig{
		NATS:        testClient.Client,
		Registry:    registry,
		Heartbeater: heartbeater,
		Owner:       integrationOwner,
		Contracts:   []Contract{contract},
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
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)
	created, err := graphingest.CreateGraphIngest(
		configJSON,
		component.Dependencies{NATSClient: testClient.Client},
	)
	require.NoError(t, err)
	ingest := created.(*graphingest.Component)
	require.NoError(t, ingest.Initialize())
	require.NoError(t, ingest.Start(ctx))
	require.NoError(t, testClient.GetNativeConnection().Flush())

	harness := &mutationIntegrationHarness{
		ctx:        ctx,
		cancel:     cancel,
		testClient: testClient,
		nats:       testClient.Client,
		registry:   registry,
		component:  ingest,
		client:     client,
		contract:   contract,
	}
	t.Cleanup(func() {
		_ = ingest.Stop(5 * time.Second)
		cancel()
	})
	return harness
}

func mutationIntegrationContract() Contract {
	return Contract{
		Name:          integrationContractName,
		MessageType:   integrationMessageTypeKey,
		EntityPattern: "acme.ops.test.system.widget.*",
		Groups: []PredicateGroup{
			{
				Mode: ownership.ModeReplaceOwned,
				Predicates: []string{
					integrationOwnedLabel,
					integrationOwnedState,
				},
			},
			{
				Mode:       ownership.ModeAppendEvidence,
				Predicates: []string{integrationEvidence},
			},
		},
		ForeignEdges: []ForeignEdge{{
			Predicate:     integrationForeignEdge,
			Mode:          ownership.EdgeNoBirthStub,
			TargetPattern: "acme.ops.test.system.child.*",
		}},
		IndexingProfile: "control",
	}
}

func TestIntegration_BindMutationClientRejectsSameRegistryRebindBeforeOwnershipMutationAndReleasesFailure(
	t *testing.T,
) {
	harness := newMutationIntegrationHarness(t)
	const secondPredicate = "test.projection.second"
	vocabulary.Register("test.projection.second")

	secondContract := Contract{
		Name:          "test.projection.second",
		EntityPattern: "acme.ops.test.system.widget.*",
		Groups: []PredicateGroup{{
			Mode:       ownership.ModeReplaceOwned,
			Predicates: []string{"test.projection.second"},
		}},
	}
	heartbeater := harness.registry.NewHeartbeater(20 * time.Millisecond)
	second, err := BindMutationClient(harness.ctx, MutationClientConfig{
		NATS:        harness.nats,
		Registry:    harness.registry,
		Heartbeater: heartbeater,
		Owner:       integrationOwner,
		Contracts:   []Contract{secondContract},
	})
	require.Nil(t, second)
	require.ErrorIs(t, err, ownership.ErrOwnerAlreadyBound)
	var mutationErr *MutationError
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, MutationConflict, mutationErr.Kind)
	require.Equal(t, CommitNotCommitted, mutationErr.Commit)
	_, found, ownerErr := harness.registry.OwnerOf(
		harness.ctx,
		"acme.ops.test.system.widget.001",
		secondPredicate,
	)
	require.NoError(t, ownerErr)
	require.False(t, found, "binding conflict must precede ownership registration")

	_, err = Bind(
		harness.ctx,
		harness.registry,
		integrationOwner,
		harness.contract,
	)
	require.ErrorIs(t, err, ownership.ErrOwnerAlreadyBound)

	failedOwner := "failed-then-retried-owner"
	_, err = BindMutationClient(harness.ctx, MutationClientConfig{
		NATS:        harness.nats,
		Registry:    harness.registry,
		Heartbeater: heartbeater,
		Owner:       failedOwner,
		Contracts:   []Contract{harness.contract},
	})
	require.ErrorIs(t, err, ownership.ErrOwnershipOverlap)

	retryContract := secondContract
	retryContract.EntityPattern = "acme.ops.test.system.gadget.*"
	retried, err := BindMutationClient(harness.ctx, MutationClientConfig{
		NATS:        harness.nats,
		Registry:    harness.registry,
		Heartbeater: heartbeater,
		Owner:       failedOwner,
		Contracts:   []Contract{retryContract},
	})
	require.NoError(t, err, "failed bind must release its process-local binding state")
	require.NotNil(t, retried)
}

func TestIntegration_DirectBindAndPublicMutationClientHaveOneConcurrentWinner(t *testing.T) {
	t.Cleanup(vocabulary.SnapshotRegistry())
	vocabulary.Register(integrationOwnedLabel)
	vocabulary.Register(integrationOwnedState)
	vocabulary.Register(integrationEvidence)
	vocabulary.Register(integrationForeignFact)
	vocabulary.Register(integrationForeignEdge)

	ctx := t.Context()
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	registry, err := ownership.EnsureBuckets(ctx, testClient.Client, nil, nil)
	require.NoError(t, err)
	contract := mutationIntegrationContract()
	heartbeater := registry.NewHeartbeater(time.Hour)

	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, bindErr := Bind(ctx, registry, "mixed-path-owner", contract)
		results <- bindErr
	}()
	go func() {
		<-start
		_, bindErr := BindMutationClient(ctx, MutationClientConfig{
			NATS:        testClient.Client,
			Registry:    registry,
			Heartbeater: heartbeater,
			Owner:       "mixed-path-owner",
			Contracts:   []Contract{contract},
		})
		results <- bindErr
	}()
	close(start)

	var succeeded, rejected int
	for range 2 {
		switch result := <-results; {
		case result == nil:
			succeeded++
		case errors.Is(result, ownership.ErrOwnerAlreadyBound):
			rejected++
		default:
			t.Fatalf("mixed-path bind: %v", result)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, rejected)
	owner, found, err := registry.OwnerOf(
		ctx,
		"acme.ops.test.system.widget.001",
		integrationOwnedLabel,
	)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "mixed-path-owner", owner)
}

func TestIntegration_CreateRetriesWhenResponderAppearsAfterInitialAbsence(t *testing.T) {
	ctx := t.Context()
	testClient := natsclient.NewTestClient(t)
	request := validCreateMutation()
	contract := birthOnlyMutationTestContract()
	request.Contract = contract.Name
	request.Triples[0].Predicate = "sensorml.process.uid"
	entity := canonicalMutationTestEntity(request)
	client, err := BindMutationClient(ctx, MutationClientConfig{
		NATS:      testClient.Client,
		Owner:     "late-responder-owner",
		Contracts: []Contract{contract},
		Timeout:   500 * time.Millisecond,
		Retry: natsclient.RetryConfig{
			MaxRetries:        5,
			InitialBackoff:    30 * time.Millisecond,
			MaxBackoff:        100 * time.Millisecond,
			BackoffMultiplier: 1,
		},
	})
	require.NoError(t, err)

	var deliveries atomic.Int32
	client.retryWait = func(
		_ context.Context,
		_ natsclient.RetryConfig,
		_ int,
	) error {
		_, subscribeErr := testClient.Client.SubscribeForRequests(
			ctx,
			subjectCreateWithTriples,
			func(_ context.Context, _ []byte) ([]byte, error) {
				deliveries.Add(1)
				return json.Marshal(graph.CreateEntityWithTriplesResponse{
					MutationResponse: graph.MutationResponse{KVRevision: 1},
					Entity:           entity,
				})
			},
		)
		if subscribeErr != nil {
			return subscribeErr
		}
		return testClient.GetNativeConnection().Flush()
	}

	receipt, err := client.CreateWithTriples(ctx, request)
	require.NoError(t, err)
	require.Equal(t, CommitVerified, receipt.Commit)
	require.Positive(t, deliveries.Load(), "late responder must receive a retried request")
}

func integrationCreateMutation(entityID, label, state, requestID string) CreateMutation {
	timestamp := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	return CreateMutation{
		Contract: integrationContractName,
		Entity: &graph.EntityState{
			ID:          entityID,
			MessageType: integrationMessageType,
		},
		Triples: []message.Triple{
			{
				Subject: entityID, Predicate: integrationOwnedLabel, Object: label,
				Confidence: 1,
			},
			{
				Subject: entityID, Predicate: integrationOwnedState, Object: state,
				Confidence: 1,
			},
		},
		Metadata: MutationMetadata{
			RequestID: requestID,
			TraceID:   "trace-" + requestID,
			Source:    "projection-integration",
			Timestamp: timestamp,
		},
	}
}

func requireMutationFailure(
	t *testing.T,
	err error,
	kind MutationErrorKind,
	commit CommitState,
) *MutationError {
	t.Helper()
	require.Error(t, err)
	var mutationErr *MutationError
	require.ErrorAs(t, err, &mutationErr)
	require.Equal(t, kind, mutationErr.Kind)
	require.Equal(t, commit, mutationErr.Commit)
	return mutationErr
}

func exactTripleCount(entity *graph.EntityState, predicate string, object any) int {
	count := 0
	for _, triple := range entity.Triples {
		if triple.Predicate == predicate && fmt.Sprint(triple.Object) == fmt.Sprint(object) {
			count++
		}
	}
	return count
}

func TestIntegration_MutationClientCreateAtomicConflictAndOwnerFence(t *testing.T) {
	harness := newMutationIntegrationHarness(t)
	const entityID = "acme.ops.test.system.widget.create-001"
	request := integrationCreateMutation(entityID, "alpha", "ready", "create-001")

	receipt, err := harness.client.CreateWithTriples(harness.ctx, request)
	require.NoError(t, err)
	require.Equal(t, CommitVerified, receipt.Commit)
	require.NotNil(t, receipt.Entity)
	require.NotZero(t, receipt.KVRevision)
	require.Equal(t, 1, exactTripleCount(receipt.Entity, integrationOwnedLabel, "alpha"))
	require.Equal(t, 1, exactTripleCount(receipt.Entity, integrationOwnedState, "ready"))

	// One authoritative KV revision contains both primary-subject birth facts:
	// neither fact is observable without the other.
	entityStates, err := harness.testClient.GetKVBucket(harness.ctx, graph.BucketEntityStates)
	require.NoError(t, err)
	history, err := entityStates.History(harness.ctx, entityID)
	require.NoError(t, err)
	require.Len(t, history, 1)
	var created graph.EntityState
	require.NoError(t, graph.UnmarshalEntityState(history[0].Value(), &created))
	require.Equal(t, 1, exactTripleCount(&created, integrationOwnedLabel, "alpha"))
	require.Equal(t, 1, exactTripleCount(&created, integrationOwnedState, "ready"))

	equivalent, err := harness.client.CreateWithTriples(harness.ctx, request)
	require.NoError(t, err)
	require.Equal(t, CommitVerified, equivalent.Commit)
	require.NotNil(t, equivalent.Entity)

	divergent := integrationCreateMutation(entityID, "different", "ready", "create-002")
	divergentReceipt, err := harness.client.CreateWithTriples(harness.ctx, divergent)
	require.Equal(t, CommitNotCommitted, divergentReceipt.Commit)
	requireMutationFailure(t, err, MutationConflict, CommitNotCommitted)
	authoritative, err := harness.client.ReadAuthoritative(harness.ctx, entityID)
	require.NoError(t, err)
	require.NotZero(t, authoritative.KVRevision)
	require.Equal(t, 1, exactTripleCount(authoritative.Entity, integrationOwnedLabel, "alpha"))
	require.Zero(t, exactTripleCount(authoritative.Entity, integrationOwnedLabel, "different"))

	// A new registry incarnation for the same owner fences this immutable
	// client. It must surface owner_lease_stale and must not auto-rebind.
	replacementRegistry, err := ownership.EnsureBuckets(harness.ctx, harness.nats, nil, nil)
	require.NoError(t, err)
	_, err = Bind(harness.ctx, replacementRegistry, integrationOwner, harness.contract)
	require.NoError(t, err)

	const fencedID = "acme.ops.test.system.widget.create-fenced"
	fencedReceipt, err := harness.client.CreateWithTriples(
		harness.ctx,
		integrationCreateMutation(fencedID, "blocked", "ready", "create-fenced"),
	)
	require.Equal(t, CommitNotCommitted, fencedReceipt.Commit)
	stale := requireMutationFailure(t, err, MutationStaleOwnerToken, CommitNotCommitted)
	require.Equal(t, graph.ErrorCodeOwnerLeaseStale, stale.Code)
	_, err = harness.client.ReadAuthoritative(harness.ctx, fencedID)
	requireMutationFailure(t, err, MutationNotFound, CommitNotCommitted)
}

func TestIntegration_MutationClientCreateRejectsCrossSubjectBeforeTransport(t *testing.T) {
	harness := newMutationIntegrationHarness(t)
	observed := make(chan []byte, 4)
	subscription, err := harness.nats.Subscribe(
		harness.ctx,
		subjectCreateWithTriples,
		func(_ context.Context, msg *gonats.Msg) {
			observed <- append([]byte(nil), msg.Data...)
		},
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = subscription.Unsubscribe() })
	require.NoError(t, harness.testClient.GetNativeConnection().Flush())

	const (
		entityID = "acme.ops.test.system.widget.cross-001"
		childID  = "acme.ops.test.system.child.cross-001"
	)
	request := integrationCreateMutation(entityID, "alpha", "ready", "cross-001")
	request.Triples = append(request.Triples, message.Triple{
		Subject: childID, Predicate: integrationForeignEdge, Object: entityID,
	})
	receipt, err := harness.client.CreateWithTriples(harness.ctx, request)
	require.Equal(t, CommitNotCommitted, receipt.Commit)
	requireMutationFailure(t, err, MutationInvalid, CommitNotCommitted)

	// A sentinel from the same NATS connection is ordered after every prior
	// publish. Seeing only the sentinel proves the invalid request never crossed
	// the transport boundary.
	sentinel := []byte("cross-subject-pretransport-sentinel")
	require.NoError(t, harness.nats.Publish(harness.ctx, subjectCreateWithTriples, sentinel))
	require.NoError(t, harness.testClient.GetNativeConnection().Flush())
	select {
	case got := <-observed:
		require.True(t, bytes.Equal(sentinel, got), "unexpected mutation transport before sentinel: %s", got)
	case <-time.After(2 * time.Second):
		t.Fatal("observer did not receive ordered sentinel")
	}
	require.Empty(t, observed)

	_, err = harness.client.ReadAuthoritative(harness.ctx, entityID)
	requireMutationFailure(t, err, MutationNotFound, CommitNotCommitted)
	_, err = harness.client.ReadAuthoritative(harness.ctx, childID)
	requireMutationFailure(t, err, MutationNotFound, CommitNotCommitted)
}

func TestIntegration_MutationClientReplaceOwnedPreservesNonOwnedFactsAndFencesStaleToken(t *testing.T) {
	harness := newMutationIntegrationHarness(t)
	const entityID = "acme.ops.test.system.widget.replace-001"
	_, err := harness.client.CreateWithTriples(
		harness.ctx,
		integrationCreateMutation(entityID, "before", "remove-me", "replace-seed"),
	)
	require.NoError(t, err)

	evidenceMetadata := MutationMetadata{
		RequestID: "evidence-seed",
		Source:    "projection-integration",
		Timestamp: time.Date(2026, 7, 27, 10, 5, 0, 0, time.UTC),
	}
	_, err = harness.client.AppendEvidence(harness.ctx, AppendEvidenceMutation{
		Contract: integrationContractName,
		EntityID: entityID,
		Evidence: []message.Triple{{
			Subject: entityID, Predicate: integrationEvidence, Object: "evidence-a",
			Datatype: "string",
		}},
		Metadata: evidenceMetadata,
	})
	require.NoError(t, err)
	foreign := message.Triple{
		Subject: entityID, Predicate: integrationForeignFact, Object: "preserve-me",
		Source: "foreign-writer", Context: "foreign-context",
		Timestamp: time.Date(2026, 7, 27, 10, 6, 0, 0, time.UTC),
	}
	require.NoError(t, harness.component.AddTriple(harness.ctx, foreign))

	replaceTimestamp := time.Date(2026, 7, 27, 10, 10, 0, 0, time.UTC)
	receipt, err := harness.client.ReplaceOwned(harness.ctx, ReplaceOwnedMutation{
		Contract: integrationContractName,
		EntityID: entityID,
		Desired: []message.Triple{{
			Subject: entityID, Predicate: integrationOwnedLabel, Object: "after",
			Source: "projection-integration", Context: "replace-001",
			Timestamp: replaceTimestamp, Confidence: 0.75,
		}},
		Metadata: MutationMetadata{
			RequestID: "replace-001",
			TraceID:   "trace-replace-001",
			Source:    "projection-integration",
			Timestamp: replaceTimestamp,
		},
	})
	require.NoError(t, err)
	require.Equal(t, CommitVerified, receipt.Commit)
	require.Equal(t, 1, exactTripleCount(receipt.Entity, integrationOwnedLabel, "after"))
	require.Zero(t, exactTripleCount(receipt.Entity, integrationOwnedState, "remove-me"))
	require.Equal(t, 1, exactTripleCount(receipt.Entity, integrationEvidence, "evidence-a"))
	require.Equal(t, 1, exactTripleCount(receipt.Entity, integrationForeignFact, "preserve-me"))

	replacementRegistry, err := ownership.EnsureBuckets(harness.ctx, harness.nats, nil, nil)
	require.NoError(t, err)
	_, err = Bind(harness.ctx, replacementRegistry, integrationOwner, harness.contract)
	require.NoError(t, err)

	staleReceipt, err := harness.client.ReplaceOwned(harness.ctx, ReplaceOwnedMutation{
		Contract: integrationContractName,
		EntityID: entityID,
		Desired: []message.Triple{{
			Subject: entityID, Predicate: integrationOwnedLabel, Object: "must-not-land",
		}},
	})
	require.Equal(t, CommitNotCommitted, staleReceipt.Commit)
	stale := requireMutationFailure(t, err, MutationStaleOwnerToken, CommitNotCommitted)
	require.Equal(t, graph.ErrorCodeOwnerLeaseStale, stale.Code)

	authoritative, err := harness.client.ReadAuthoritative(harness.ctx, entityID)
	require.NoError(t, err)
	require.NotZero(t, authoritative.KVRevision)
	require.Equal(t, 1, exactTripleCount(authoritative.Entity, integrationOwnedLabel, "after"))
	require.Zero(t, exactTripleCount(authoritative.Entity, integrationOwnedLabel, "must-not-land"))
	require.Equal(t, 1, exactTripleCount(authoritative.Entity, integrationEvidence, "evidence-a"))
	require.Equal(t, 1, exactTripleCount(authoritative.Entity, integrationForeignFact, "preserve-me"))
}

type realResponseFaultRequester struct {
	delegate *natsclient.Client

	dropSubject    string
	degradeSubject string
	faultUsed      atomic.Bool
	mutationCalls  atomic.Int64
	queryCalls     atomic.Int64
}

func (r *realResponseFaultRequester) RequestClassified(
	ctx context.Context,
	subject string,
	data []byte,
	timeout time.Duration,
) ([]byte, error) {
	response, err := r.delegate.RequestClassified(ctx, subject, data, timeout)
	if subject == subjectQueryEntity {
		r.queryCalls.Add(1)
	}
	if subject == subjectCreateWithTriples || subject == subjectAddTriplesBatch {
		r.mutationCalls.Add(1)
	}
	if err != nil || !r.faultUsed.CompareAndSwap(false, true) {
		return response, err
	}
	if subject == r.dropSubject {
		return nil, errors.New("injected lost response after real graph-ingest commit")
	}
	if subject == r.degradeSubject {
		var createResponse graph.CreateEntityWithTriplesResponse
		if decodeErr := json.Unmarshal(response, &createResponse); decodeErr != nil {
			return nil, decodeErr
		}
		createResponse.Entity = nil
		createResponse.Degraded = true
		createResponse.DegradedReason = "injected post-commit read-back failure"
		return json.Marshal(createResponse)
	}
	return response, nil
}

func (r *realResponseFaultRequester) RequestWithRetryClassified(
	ctx context.Context,
	subject string,
	data []byte,
	timeout time.Duration,
	retry natsclient.RetryConfig,
) ([]byte, error) {
	return r.delegate.RequestWithRetryClassified(ctx, subject, data, timeout, retry)
}

func TestIntegration_MutationClientAppendLostResponseReadsBackWithoutDuplicate(t *testing.T) {
	harness := newMutationIntegrationHarness(t)
	const entityID = "acme.ops.test.system.widget.append-001"
	_, err := harness.client.CreateWithTriples(
		harness.ctx,
		integrationCreateMutation(entityID, "append", "ready", "append-seed"),
	)
	require.NoError(t, err)

	requester := &realResponseFaultRequester{
		delegate:    harness.nats,
		dropSubject: subjectAddTriplesBatch,
	}
	client, err := newMutationClient(
		requester,
		harness.registry.OwnerToken(integrationOwner),
		[]Contract{harness.contract},
		2*time.Second,
		natsclient.RetryConfig{MaxRetries: 1, InitialBackoff: time.Millisecond, BackoffMultiplier: 1},
	)
	require.NoError(t, err)

	receipt, err := client.AppendEvidence(harness.ctx, AppendEvidenceMutation{
		Contract: integrationContractName,
		EntityID: entityID,
		Evidence: []message.Triple{{
			Subject: entityID, Predicate: integrationEvidence, Object: "ambiguous-evidence",
			Datatype: "string",
		}},
		Metadata: MutationMetadata{
			RequestID: "append-lost-001",
			TraceID:   "trace-append-lost-001",
			Source:    "projection-integration",
			Timestamp: time.Date(2026, 7, 27, 10, 20, 0, 0, time.UTC),
		},
	})
	require.NoError(t, err)
	require.Equal(t, CommitVerified, receipt.Commit)
	require.NotNil(t, receipt.Entity)
	require.EqualValues(t, 1, requester.mutationCalls.Load(), "lost response must not trigger blind append retry")
	require.EqualValues(t, 1, requester.queryCalls.Load(), "ambiguity must use authoritative graph-ingest read-back")
	require.Equal(t, 1, exactTripleCount(receipt.Entity, integrationEvidence, "ambiguous-evidence"))

	authoritative, err := harness.client.ReadAuthoritative(harness.ctx, entityID)
	require.NoError(t, err)
	require.NotZero(t, authoritative.KVRevision)
	require.Equal(t, 1, exactTripleCount(authoritative.Entity, integrationEvidence, "ambiguous-evidence"))
}

func TestIntegration_MutationClientDegradedCreateUsesAuthoritativeReadBack(t *testing.T) {
	harness := newMutationIntegrationHarness(t)
	requester := &realResponseFaultRequester{
		delegate:       harness.nats,
		degradeSubject: subjectCreateWithTriples,
	}
	client, err := newMutationClient(
		requester,
		harness.registry.OwnerToken(integrationOwner),
		[]Contract{harness.contract},
		2*time.Second,
		natsclient.RetryConfig{},
	)
	require.NoError(t, err)

	const entityID = "acme.ops.test.system.widget.degraded-001"
	receipt, err := client.CreateWithTriples(
		harness.ctx,
		integrationCreateMutation(entityID, "degraded", "ready", "degraded-001"),
	)
	require.NoError(t, err)
	require.Equal(t, CommitVerified, receipt.Commit)
	require.True(t, receipt.Degraded)
	require.NotNil(t, receipt.Entity)
	require.EqualValues(t, 1, requester.mutationCalls.Load())
	require.EqualValues(t, 1, requester.queryCalls.Load())
	require.Equal(t, 1, exactTripleCount(receipt.Entity, integrationOwnedLabel, "degraded"))
}
