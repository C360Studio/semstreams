package projection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/nats-io/nats.go"
)

const subjectQueryEntity = "graph.ingest.query.entity"

func TestMutationClientImplementsNarrowPublicCapabilities(t *testing.T) {
	t.Parallel()

	var client *MutationClient
	var _ EntityCreator = client
	var _ OwnedReplacer = client
	var _ EvidenceAppender = client
	var _ AuthoritativeReader = client

	if CommitNotCommitted == CommitUnknown ||
		CommitUnknown == CommitCommitted ||
		CommitCommitted == CommitVerified {
		t.Fatal("commit states must remain distinct")
	}

	creator := EntityCreator(client)
	if creator == nil {
		t.Fatal("typed nil client must still satisfy EntityCreator")
	}

	_ = context.Background()
}

func TestNoRespondersRetriesOnlyProvenNonDelivery(t *testing.T) {
	t.Parallel()
	request := validCreateMutation()
	entity := canonicalMutationTestEntity(request)

	tests := []struct {
		name       string
		subject    string
		success    []byte
		invoke     func(*MutationClient) (MutationReceipt, error)
		wantCommit CommitState
	}{
		{
			name:    "create",
			subject: subjectCreateWithTriples,
			success: marshalMutationTestJSON(t, graph.CreateEntityWithTriplesResponse{
				MutationResponse: graph.MutationResponse{KVRevision: 10},
				Entity:           entity,
			}),
			invoke: func(client *MutationClient) (MutationReceipt, error) {
				return client.CreateWithTriples(context.Background(), request)
			},
			wantCommit: CommitVerified,
		},
		{
			name:    "append",
			subject: subjectAddTriplesBatch,
			success: marshalMutationTestJSON(t, graph.AddTriplesBatchResponse{
				MutationResponse: graph.MutationResponse{KVRevision: 11},
				WrittenCount:     1,
			}),
			invoke: func(client *MutationClient) (MutationReceipt, error) {
				return client.AppendEvidence(context.Background(), AppendEvidenceMutation{
					Contract: request.Contract,
					EntityID: request.Entity.ID,
					Evidence: []message.Triple{{
						Subject: request.Entity.ID, Predicate: "shared.value.p", Object: "proof",
					}},
					Metadata: request.Metadata,
				})
			},
			wantCommit: CommitCommitted,
		},
		{
			name:    "replace owned",
			subject: subjectUpdateWithTriples,
			success: marshalMutationTestJSON(t, graph.UpdateEntityWithTriplesResponse{
				MutationResponse: graph.MutationResponse{KVRevision: 12},
				Entity:           entity,
			}),
			invoke: func(client *MutationClient) (MutationReceipt, error) {
				return client.ReplaceOwned(context.Background(), ReplaceOwnedMutation{
					Contract: request.Contract,
					EntityID: request.Entity.ID,
					Desired:  entity.Triples,
					Metadata: request.Metadata,
				})
			},
			wantCommit: CommitVerified,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
				test.subject: {
					{err: fmt.Errorf("startup: %w", nats.ErrNoResponders)},
					{data: test.success},
				},
			}}
			client := newMutationTestClient(t, rpc)
			client.retry.MaxRetries = 1
			receipt, err := test.invoke(client)
			if err != nil {
				t.Fatalf("invoke: %v", err)
			}
			if receipt.Commit != test.wantCommit {
				t.Fatalf("receipt = %#v, want commit %q", receipt, test.wantCommit)
			}
			if rpc.callCount() != 2 {
				t.Fatalf("calls = %#v, want exactly two mutation attempts", rpc.calls)
			}
			for _, call := range rpc.calls {
				if call.subject != test.subject || call.retry != nil {
					t.Fatalf("call = %#v, want direct scripted mutation retry", call)
				}
			}
		})
	}
}

func TestNoRespondersExhaustionIsUnavailableAndNotCommitted(t *testing.T) {
	t.Parallel()
	request := validCreateMutation()
	entity := canonicalMutationTestEntity(request)

	tests := []struct {
		name    string
		subject string
		invoke  func(*MutationClient) (MutationReceipt, error)
	}{
		{
			name: "create", subject: subjectCreateWithTriples,
			invoke: func(client *MutationClient) (MutationReceipt, error) {
				return client.CreateWithTriples(context.Background(), request)
			},
		},
		{
			name: "append", subject: subjectAddTriplesBatch,
			invoke: func(client *MutationClient) (MutationReceipt, error) {
				return client.AppendEvidence(context.Background(), AppendEvidenceMutation{
					Contract: request.Contract, EntityID: request.Entity.ID,
					Evidence: []message.Triple{{
						Subject: request.Entity.ID, Predicate: "shared.value.p", Object: "proof",
					}},
					Metadata: request.Metadata,
				})
			},
		},
		{
			name: "replace owned", subject: subjectUpdateWithTriples,
			invoke: func(client *MutationClient) (MutationReceipt, error) {
				return client.ReplaceOwned(context.Background(), ReplaceOwnedMutation{
					Contract: request.Contract, EntityID: request.Entity.ID,
					Desired: entity.Triples, Metadata: request.Metadata,
				})
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
				test.subject: {
					{err: nats.ErrNoResponders},
					{err: fmt.Errorf("still absent: %w", nats.ErrNoResponders)},
				},
			}}
			client := newMutationTestClient(t, rpc)
			client.retry.MaxRetries = 1
			receipt, err := test.invoke(client)
			var mutationErr *MutationError
			if !errors.As(err, &mutationErr) ||
				mutationErr.Kind != MutationUnavailable ||
				mutationErr.Class != errs.ErrorTransient ||
				mutationErr.Commit != CommitNotCommitted ||
				receipt.Commit != CommitNotCommitted ||
				!errors.Is(err, nats.ErrNoResponders) {
				t.Fatalf("receipt/error = %#v/%#v", receipt, mutationErr)
			}
			if rpc.callCount() != 2 {
				t.Fatalf("calls = %#v, want exhausted direct attempts", rpc.calls)
			}
		})
	}
}

func TestReadAuthoritativeMapsNoRespondersAsUnavailableNotCommitted(t *testing.T) {
	t.Parallel()
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectQueryEntity: {{err: fmt.Errorf("query absent: %w", nats.ErrNoResponders)}},
	}}
	client := newMutationTestClient(t, rpc)
	_, err := client.ReadAuthoritative(context.Background(), validCreateMutation().Entity.ID)
	var mutationErr *MutationError
	if !errors.As(err, &mutationErr) ||
		mutationErr.Kind != MutationUnavailable ||
		mutationErr.Class != errs.ErrorTransient ||
		mutationErr.Commit != CommitNotCommitted ||
		!errors.Is(err, nats.ErrNoResponders) {
		t.Fatalf("error = %#v", mutationErr)
	}
}

func TestReadAuthoritativeMapsMalformedIDAsInvalidWithoutTransport(t *testing.T) {
	t.Parallel()
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{}}
	client := newMutationTestClient(t, rpc)
	_, err := client.ReadAuthoritative(context.Background(), "not-six-parts")
	var mutationErr *MutationError
	if !errors.As(err, &mutationErr) ||
		mutationErr.Kind != MutationInvalid ||
		mutationErr.Class != errs.ErrorInvalid ||
		mutationErr.Code != graph.ErrorCodeInvalidRequest ||
		mutationErr.Commit != CommitNotCommitted {
		t.Fatalf("error = %#v", mutationErr)
	}
	if rpc.callCount() != 0 {
		t.Fatalf("invalid read made %d transport calls", rpc.callCount())
	}
}

func TestAmbiguousMutationAttemptCannotBeDowngradedByLaterFailure(t *testing.T) {
	t.Parallel()
	request := validCreateMutation()
	absent := canonicalMutationTestEntity(request)
	absent.Triples = nil

	terminalClassifiedCause := errors.New("terminal classified")
	terminalClassified := errs.ClassifiedCodeDetail(
		errs.ErrorInvalid,
		graph.ErrorCodeInvalidRequest,
		nil,
		terminalClassifiedCause,
	)
	terminalAmbiguous := errors.New("second reply lost")
	tests := []struct {
		name         string
		terminal     error
		wantTerminal error
	}{
		{
			name:         "no responders exhaustion",
			terminal:     fmt.Errorf("responder disappeared: %w", nats.ErrNoResponders),
			wantTerminal: nats.ErrNoResponders,
		},
		{
			name:         "classified rejection",
			terminal:     terminalClassified,
			wantTerminal: terminalClassifiedCause,
		},
		{
			name:         "second ambiguous failure",
			terminal:     terminalAmbiguous,
			wantTerminal: terminalAmbiguous,
		},
	}
	for _, operation := range []string{"create", "append"} {
		operation := operation
		for _, test := range tests {
			test := test
			t.Run(operation+"/"+test.name, func(t *testing.T) {
				t.Parallel()
				firstAmbiguous := errors.New("first reply lost")
				responses := map[string][]fakeRPCResult{
					subjectQueryEntity: {
						{err: errs.ClassifiedCodeDetail(
							errs.ErrorInvalid,
							graph.ErrorCodeEntityNotFound,
							nil,
							errors.New("entity absent"),
						)},
					},
				}
				if operation == "create" {
					responses[subjectCreateWithTriples] = []fakeRPCResult{
						{err: firstAmbiguous},
						{err: test.terminal},
					}
				} else {
					responses[subjectAddTriplesBatch] = []fakeRPCResult{
						{err: firstAmbiguous},
						{err: test.terminal},
					}
					responses[subjectQueryEntity] = []fakeRPCResult{
						{data: marshalMutationTestExact(t, absent)},
					}
				}
				rpc := &fakeMutationRequester{responses: responses}
				client := newMutationTestClient(t, rpc)
				client.retry.MaxRetries = 1

				var receipt MutationReceipt
				var err error
				if operation == "create" {
					receipt, err = client.CreateWithTriples(context.Background(), request)
				} else {
					receipt, err = client.AppendEvidence(
						context.Background(),
						AppendEvidenceMutation{
							Contract: request.Contract,
							EntityID: request.Entity.ID,
							Evidence: []message.Triple{{
								Subject:   request.Entity.ID,
								Predicate: "shared.value.p",
								Object:    "proof",
							}},
							Metadata: request.Metadata,
						},
					)
				}
				var mutationErr *MutationError
				if !errors.As(err, &mutationErr) ||
					mutationErr.Kind != MutationCommitUnknown ||
					mutationErr.Class != errs.ErrorTransient ||
					mutationErr.Commit != CommitUnknown ||
					receipt.Commit != CommitUnknown ||
					!errors.Is(err, firstAmbiguous) ||
					!errors.Is(err, test.wantTerminal) {
					t.Fatalf("receipt/error = %#v/%#v", receipt, mutationErr)
				}
			})
		}
	}
}

func TestAppendEvidenceAmbiguitySurvivesNonDefinitiveSuccessResponse(t *testing.T) {
	t.Parallel()
	request := validCreateMutation()
	absent := canonicalMutationTestEntity(request)
	absent.Triples = nil
	tests := []struct {
		name             string
		response         []byte
		wantQueryCount   int
		wantErrorSnippet string
	}{
		{
			name: "requested failure",
			response: marshalMutationTestJSON(t, graph.AddTriplesBatchResponse{
				FailedSubjects: map[string]string{
					request.Entity.ID: "entity rejected",
				},
			}),
			wantQueryCount:   1,
			wantErrorSnippet: "entity rejected",
		},
		{
			name: "zero written count",
			response: marshalMutationTestJSON(t, graph.AddTriplesBatchResponse{
				WrittenCount: 0,
			}),
			wantQueryCount: 1,
			// Deduplicated is absent (zero), so a zero written count is still
			// unaccounted-for and still an anomaly — the counts stay pinned.
			wantErrorSnippet: "wrote 0 and deduplicated 0 of 1",
		},
		{
			name: "excess written count",
			response: marshalMutationTestJSON(t, graph.AddTriplesBatchResponse{
				WrittenCount: 2,
			}),
			wantQueryCount:   1,
			wantErrorSnippet: "wrote 2 and deduplicated 0 of 1",
		},
		{
			name:             "malformed response",
			response:         []byte("not-json"),
			wantQueryCount:   1,
			wantErrorSnippet: "decode append response",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ambiguous := errors.New("first append reply lost")
			queries := make([]fakeRPCResult, test.wantQueryCount)
			for index := range queries {
				queries[index].data = marshalMutationTestExact(t, absent)
			}
			rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
				subjectAddTriplesBatch: {
					{err: ambiguous},
					{data: test.response},
				},
				subjectQueryEntity: queries,
			}}
			client := newMutationTestClient(t, rpc)
			client.retry.MaxRetries = 1

			receipt, err := client.AppendEvidence(
				context.Background(),
				AppendEvidenceMutation{
					Contract: request.Contract,
					EntityID: request.Entity.ID,
					Evidence: []message.Triple{{
						Subject:   request.Entity.ID,
						Predicate: "shared.value.p",
						Object:    "proof",
					}},
					Metadata: request.Metadata,
				},
			)
			var mutationErr *MutationError
			if !errors.As(err, &mutationErr) ||
				mutationErr.Kind != MutationCommitUnknown ||
				mutationErr.Class != errs.ErrorTransient ||
				mutationErr.Commit != CommitUnknown ||
				receipt.Commit != CommitUnknown ||
				!errors.Is(err, ambiguous) ||
				!strings.Contains(err.Error(), test.wantErrorSnippet) {
				t.Fatalf("receipt/error = %#v/%#v", receipt, mutationErr)
			}
			if rpc.callCount() != 2+test.wantQueryCount {
				t.Fatalf(
					"calls = %d, want two appends and %d authoritative queries",
					rpc.callCount(),
					test.wantQueryCount,
				)
			}
		})
	}
}

func TestCreateAmbiguitySurvivesDivergentReadAndMalformedLaterResponse(t *testing.T) {
	t.Parallel()
	request := validCreateMutation()
	divergent := canonicalMutationTestEntity(request)
	divergent.Triples[0].Object = "changed after the ambiguous create"
	tests := []struct {
		name      string
		creates   []fakeRPCResult
		queries   []fakeRPCResult
		wantCalls int
	}{
		{
			name:    "divergent authoritative read",
			creates: []fakeRPCResult{{err: errors.New("placeholder")}},
			queries: []fakeRPCResult{{
				data: marshalMutationTestExact(t, divergent),
			}},
			wantCalls: 2,
		},
		{
			name: "malformed later response",
			creates: []fakeRPCResult{
				{err: errors.New("placeholder")},
				{data: []byte("not-json")},
			},
			queries: []fakeRPCResult{{
				err: errs.ClassifiedCodeDetail(
					errs.ErrorInvalid,
					graph.ErrorCodeEntityNotFound,
					nil,
					errors.New("entity absent"),
				),
			}},
			wantCalls: 3,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ambiguous := errors.New("first create reply lost")
			test.creates[0].err = ambiguous
			rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
				subjectCreateWithTriples: test.creates,
				subjectQueryEntity:       test.queries,
			}}
			client := newMutationTestClient(t, rpc)
			client.retry.MaxRetries = 1

			receipt, err := client.CreateWithTriples(context.Background(), request)
			var mutationErr *MutationError
			if !errors.As(err, &mutationErr) ||
				mutationErr.Kind != MutationCommitUnknown ||
				mutationErr.Class != errs.ErrorTransient ||
				mutationErr.Commit != CommitUnknown ||
				receipt.Commit != CommitUnknown ||
				!errors.Is(err, ambiguous) {
				t.Fatalf("receipt/error = %#v/%#v", receipt, mutationErr)
			}
			if rpc.callCount() != test.wantCalls {
				t.Fatalf("calls = %d, want %d", rpc.callCount(), test.wantCalls)
			}
		})
	}
}

func TestNoRespondersOnlyCancellationRemainsNotCommitted(t *testing.T) {
	t.Parallel()
	request := validCreateMutation()
	for _, operation := range []string{"create", "append"} {
		operation := operation
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{}}
			subject := subjectCreateWithTriples
			if operation == "append" {
				subject = subjectAddTriplesBatch
			}
			rpc.responses[subject] = []fakeRPCResult{{err: nats.ErrNoResponders}}
			client := newMutationTestClient(t, rpc)
			client.retry.MaxRetries = 1
			ctx, cancel := context.WithCancel(context.Background())
			client.retryWait = func(
				waitCtx context.Context,
				_ natsclient.RetryConfig,
				_ int,
			) error {
				cancel()
				return waitCtx.Err()
			}

			var receipt MutationReceipt
			var err error
			if operation == "create" {
				receipt, err = client.CreateWithTriples(ctx, request)
			} else {
				receipt, err = client.AppendEvidence(
					ctx,
					AppendEvidenceMutation{
						Contract: request.Contract,
						EntityID: request.Entity.ID,
						Evidence: []message.Triple{{
							Subject:   request.Entity.ID,
							Predicate: "shared.value.p",
							Object:    "proof",
						}},
						Metadata: request.Metadata,
					},
				)
			}
			var mutationErr *MutationError
			if !errors.As(err, &mutationErr) ||
				mutationErr.Kind != MutationUnavailable ||
				mutationErr.Class != errs.ErrorTransient ||
				mutationErr.Commit != CommitNotCommitted ||
				receipt.Commit != CommitNotCommitted ||
				!errors.Is(err, nats.ErrNoResponders) ||
				!errors.Is(err, context.Canceled) {
				t.Fatalf("receipt/error = %#v/%#v", receipt, mutationErr)
			}
		})
	}
}

func TestRetryNormalizationAndDelayBounds(t *testing.T) {
	t.Parallel()
	defaultMax := natsclient.DefaultRetryConfig().MaxBackoff
	for _, maxBackoff := range []time.Duration{0, -time.Second} {
		normalized := normalizeRetryConfig(natsclient.RetryConfig{
			InitialBackoff:    time.Second,
			MaxBackoff:        maxBackoff,
			BackoffMultiplier: 2,
		})
		if normalized.MaxBackoff != defaultMax || normalized.MaxBackoff <= 0 {
			t.Fatalf("normalize MaxBackoff(%s) = %s, want %s", maxBackoff, normalized.MaxBackoff, defaultMax)
		}
	}

	retry := normalizeRetryConfig(natsclient.RetryConfig{
		InitialBackoff:    time.Second,
		MaxBackoff:        2 * time.Second,
		BackoffMultiplier: 2,
	})
	if delay := mutationRetryDelay(retry, int(^uint(0)>>1)); delay != retry.MaxBackoff {
		t.Fatalf("large-index delay = %s, want cap %s", delay, retry.MaxBackoff)
	}
}

func TestWaitMutationRetryZeroDelayChecksContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitMutationRetry(ctx, natsclient.RetryConfig{}, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("zero-delay wait error = %v, want context cancellation", err)
	}
}

func TestBindMutationClientValidatesBirthOnlyOwnerSubjectSafety(t *testing.T) {
	t.Parallel()
	config := MutationClientConfig{
		NATS:      &natsclient.Client{},
		Owner:     "not subject safe",
		Contracts: []Contract{birthOnlyMutationTestContract()},
	}
	client, err := BindMutationClient(context.Background(), config)
	var mutationErr *MutationError
	if client != nil ||
		!errors.As(err, &mutationErr) ||
		!errors.Is(err, ownership.ErrInvalidClaim) ||
		mutationErr.Kind != MutationInvalid ||
		mutationErr.Commit != CommitNotCommitted {
		t.Fatalf("client/error = %#v/%#v", client, mutationErr)
	}
}

func TestNewMutationClientRejectsTypedNilRequester(t *testing.T) {
	t.Parallel()
	var requester *fakeMutationRequester
	_, err := newMutationClient(
		requester,
		ownership.OwnerToken{},
		[]Contract{mutationTestContract()},
		time.Second,
		natsclient.RetryConfig{},
	)
	if err == nil || !strings.Contains(err.Error(), "mutation requester is required") {
		t.Fatalf("typed-nil requester error = %v", err)
	}
	client := &MutationClient{rpc: requester}
	_, err = client.ReadAuthoritative(
		context.Background(),
		"acme.ops.test.system.widget.001",
	)
	var mutationErr *MutationError
	if !errors.As(err, &mutationErr) || mutationErr.Kind != MutationInvalid {
		t.Fatalf("typed-nil read error = %#v", mutationErr)
	}
}

type fakeRPCResult struct {
	data         []byte
	err          error
	beforeReturn func()
}

type fakeRPCCall struct {
	subject string
	data    []byte
	retry   *natsclient.RetryConfig
}

type fakeMutationRequester struct {
	mu        sync.Mutex
	responses map[string][]fakeRPCResult
	calls     []fakeRPCCall
}

func (f *fakeMutationRequester) RequestClassified(
	_ context.Context,
	subject string,
	data []byte,
	_ time.Duration,
) ([]byte, error) {
	return f.reply(subject, data, nil)
}

func (f *fakeMutationRequester) RequestWithRetryClassified(
	_ context.Context,
	subject string,
	data []byte,
	_ time.Duration,
	retry natsclient.RetryConfig,
) ([]byte, error) {
	return f.reply(subject, data, &retry)
}

func (f *fakeMutationRequester) reply(
	subject string,
	data []byte,
	retry *natsclient.RetryConfig,
) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeRPCCall{
		subject: subject,
		data:    append([]byte(nil), data...),
		retry:   retry,
	})
	queue := f.responses[subject]
	if len(queue) == 0 {
		return nil, errors.New("unexpected request")
	}
	result := queue[0]
	f.responses[subject] = queue[1:]
	if result.beforeReturn != nil {
		result.beforeReturn()
	}
	return append([]byte(nil), result.data...), result.err
}

func (f *fakeMutationRequester) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func mutationTestContract() Contract {
	return Contract{
		Name:          "test.widget",
		MessageType:   "test.fixture.v1",
		EntityPattern: "acme.ops.test.system.widget.*",
		Groups: []PredicateGroup{
			{Mode: ownership.ModeReplaceOwned, Predicates: []string{"sensorml.process.label"}},
			{Mode: ownership.ModeAppendEvidence, Predicates: []string{"shared.value.p"}},
		},
		ForeignEdges: []ForeignEdge{{
			Predicate:     "sensorml.component.is-hosted-by",
			Mode:          ownership.EdgeNoBirthStub,
			TargetPattern: "acme.ops.test.system.child.*",
		}},
		IndexingProfile: "control",
	}
}

func multiGroupMutationTestContract() Contract {
	contract := mutationTestContract()
	contract.Name = "test.widget.multi"
	contract.BirthPredicates = []string{"sensorml.process.uid"}
	contract.Groups = []PredicateGroup{
		{
			Name: "identity", Mode: ownership.ModeReplaceOwned,
			Predicates: []string{"sensorml.process.label", "sensorml.process.description"},
		},
		{
			Name: "position", Mode: ownership.ModeReplaceOwned,
			Predicates: []string{"sensorml.process.position"},
		},
		{
			Name: "evidence", Mode: ownership.ModeAppendEvidence,
			Predicates: []string{"shared.value.p"},
		},
	}
	return contract
}

func birthOnlyMutationTestContract() Contract {
	return Contract{
		Name:            "test.widget.birth-only",
		MessageType:     "test.fixture.v1",
		EntityPattern:   "acme.ops.test.system.widget.*",
		BirthPredicates: []string{"sensorml.process.uid"},
		IndexingProfile: "control",
	}
}

func newMutationTestClient(t *testing.T, rpc *fakeMutationRequester) *MutationClient {
	t.Helper()
	client, err := newMutationClient(
		rpc,
		ownership.ExpectedOwnerToken("test-owner", "incarnation"),
		[]Contract{mutationTestContract()},
		time.Second,
		natsclient.RetryConfig{},
	)
	if err != nil {
		t.Fatalf("newMutationClient: %v", err)
	}
	return client
}

func validCreateMutation() CreateMutation {
	const entityID = "acme.ops.test.system.widget.001"
	return CreateMutation{
		Contract: "test.widget",
		Entity: &graph.EntityState{
			ID:          entityID,
			MessageType: message.Type{Domain: "test", Category: "fixture", Version: "v1"},
		},
		Triples: []message.Triple{{
			Subject: entityID, Predicate: "sensorml.process.label", Object: "Widget",
		}},
		Metadata: MutationMetadata{
			RequestID: "request-1",
			TraceID:   "trace-1",
			Source:    "projection-test",
			Timestamp: time.Date(2026, 7, 26, 10, 30, 0, 0, time.UTC),
		},
	}
}

func TestCreateWithTriplesRejectsContractViolationsBeforeTransport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*CreateMutation)
	}{
		{name: "unknown contract", mutate: func(req *CreateMutation) { req.Contract = "missing" }},
		{name: "nil entity", mutate: func(req *CreateMutation) { req.Entity = nil }},
		{name: "entity outside pattern", mutate: func(req *CreateMutation) {
			req.Entity.ID = "acme.ops.test.system.other.001"
			req.Triples[0].Subject = req.Entity.ID
		}},
		{name: "message type mismatch", mutate: func(req *CreateMutation) {
			req.Entity.MessageType.Category = "other"
		}},
		{name: "predicate outside contract", mutate: func(req *CreateMutation) {
			req.Triples[0].Predicate = "a.b.c"
		}},
		{name: "append predicate is not create-authorized", mutate: func(req *CreateMutation) {
			req.Triples[0].Predicate = "shared.value.p"
		}},
		{name: "foreign edge outside target pattern", mutate: func(req *CreateMutation) {
			req.Triples = []message.Triple{{
				Subject:   "acme.ops.test.system.other.002",
				Predicate: "sensorml.component.is-hosted-by",
				Object:    req.Entity.ID,
			}}
		}},
		{name: "declared foreign edge targets another subject", mutate: func(req *CreateMutation) {
			req.Triples = []message.Triple{{
				Subject:   "acme.ops.test.system.child.002",
				Predicate: "sensorml.component.is-hosted-by",
				Object:    req.Entity.ID,
			}}
		}},
		{name: "missing request id", mutate: func(req *CreateMutation) { req.Metadata.RequestID = "" }},
		{name: "missing source", mutate: func(req *CreateMutation) { req.Metadata.Source = "" }},
		{name: "conflicting source", mutate: func(req *CreateMutation) {
			req.Triples[0].Source = "someone-else"
		}},
		{name: "conflicting context", mutate: func(req *CreateMutation) {
			req.Triples[0].Context = "another-request"
		}},
		{name: "conflicting timestamp", mutate: func(req *CreateMutation) {
			req.Triples[0].Timestamp = req.Metadata.Timestamp.Add(time.Second)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rpc := &fakeMutationRequester{}
			client := newMutationTestClient(t, rpc)
			req := validCreateMutation()
			tt.mutate(&req)

			receipt, err := client.CreateWithTriples(context.Background(), req)
			var mutationErr *MutationError
			if !errors.As(err, &mutationErr) ||
				mutationErr.Kind != MutationInvalid ||
				mutationErr.Commit != CommitNotCommitted {
				t.Fatalf("CreateWithTriples() = receipt %#v error %T %v, want invalid/not-committed", receipt, err, err)
			}
			if receipt.Commit != CommitNotCommitted {
				t.Fatalf("receipt commit = %q, want %q", receipt.Commit, CommitNotCommitted)
			}
			if rpc.callCount() != 0 {
				t.Fatalf("validation failure published %d RPC requests", rpc.callCount())
			}
		})
	}
}

func TestMutationErrorPreservesClassifiedInspection(t *testing.T) {
	t.Parallel()
	classified := errs.ClassifiedCodeDetail(
		errs.ErrorInvalid,
		graph.ErrorCodeRevisionMismatch,
		map[string]any{"expected_revision": float64(4)},
		errors.New("revision mismatch"),
	)
	mapped := newMutationError(MutationOperationReplaceOwned, classified, CommitNotCommitted)

	var mutationErr *MutationError
	if !errors.As(mapped, &mutationErr) {
		t.Fatalf("mapped error = %T, want *MutationError", mapped)
	}
	if mutationErr.Kind != MutationRevisionConflict || mutationErr.Code != graph.ErrorCodeRevisionMismatch {
		t.Fatalf("mapped error = %#v, want revision conflict", mutationErr)
	}
	var preserved *errs.ClassifiedError
	if !errors.As(mapped, &preserved) || preserved != classified {
		t.Fatalf("errors.As classified = %#v, want original %#v", preserved, classified)
	}
	if !errors.Is(mapped, errs.ErrRevisionMismatch) {
		t.Fatal("mapped error no longer matches errs.ErrRevisionMismatch")
	}

	encoded, err := json.Marshal(mutationErr.Detail)
	if err != nil || string(encoded) != `{"expected_revision":4}` {
		t.Fatalf("detail = %s, %v", encoded, err)
	}
}

func TestMutationErrorMapsEveryGraphMutationCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code string
		want MutationErrorKind
	}{
		{graph.ErrorCodeInvalidRequest, MutationInvalid},
		{graph.ErrorCodeStructuralInvalid, MutationInvalid},
		{graph.ErrorCodeEntityNotFound, MutationNotFound},
		{graph.ErrorCodeEntityExists, MutationConflict},
		{graph.ErrorCodeRevisionMismatch, MutationRevisionConflict},
		{graph.ErrorCodeOwnerLeaseStale, MutationStaleOwnerToken},
		{graph.ErrorCodeGraphStateResetRequired, MutationInternal},
		{graph.ErrorCodeInternal, MutationInternal},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			t.Parallel()
			err := errs.ClassifiedCodeDetail(
				errs.ErrorInvalid, tt.code, nil, errors.New(tt.code),
			)
			got := newMutationError(MutationOperationCreate, err, CommitNotCommitted)
			if got.Kind != tt.want || got.Code != tt.code || got.Commit != CommitNotCommitted {
				t.Fatalf("newMutationError(%q) = %#v, want %q", tt.code, got, tt.want)
			}
		})
	}
}

func TestMutationErrorMapsInternalCodeByClass(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("internal sentinel")
	tests := []struct {
		name  string
		class errs.ErrorClass
		want  MutationErrorKind
	}{
		{name: "transient", class: errs.ErrorTransient, want: MutationUnavailable},
		{name: "fatal", class: errs.ErrorFatal, want: MutationInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			classified := errs.ClassifiedCodeDetail(
				tt.class,
				graph.ErrorCodeInternal,
				map[string]any{"component": "graph-ingest"},
				sentinel,
			)
			mapped := newMutationError(
				MutationOperationReplaceOwned,
				classified,
				CommitNotCommitted,
			)
			if mapped.Kind != tt.want || mapped.Commit != CommitNotCommitted {
				t.Fatalf("mapped = %#v, want %q/not-committed", mapped, tt.want)
			}
			var preserved *errs.ClassifiedError
			if !errors.As(mapped, &preserved) || preserved != classified {
				t.Fatalf("errors.As = %#v, want original %#v", preserved, classified)
			}
			if !errors.Is(mapped, sentinel) {
				t.Fatal("mapped internal error does not preserve sentinel")
			}
		})
	}
}

func TestMutationErrorMapsOwnerAlreadyBoundAsConflict(t *testing.T) {
	t.Parallel()
	mapped := newMutationError(
		MutationOperationBind,
		fmt.Errorf("static owner: %w", ownership.ErrOwnerAlreadyBound),
		CommitNotCommitted,
	)
	if mapped == nil ||
		mapped.Kind != MutationConflict ||
		mapped.Class != errs.ErrorInvalid ||
		mapped.Commit != CommitNotCommitted ||
		!errors.Is(mapped, ownership.ErrOwnerAlreadyBound) {
		t.Fatalf("owner-already-bound mapping = %#v", mapped)
	}
}

func TestCommitStateHelpersRejectNilCauseWithoutPanicking(t *testing.T) {
	t.Parallel()
	unknown := commitUnknown(MutationOperationCreate, nil)
	if unknown == nil ||
		unknown.Kind != MutationInternal ||
		unknown.Class != errs.ErrorFatal ||
		unknown.Commit != CommitUnknown ||
		!strings.Contains(unknown.Error(), "internal invariant") {
		t.Fatalf("commitUnknown(nil) = %#v", unknown)
	}

	receipt, err := committedUnverified(
		MutationOperationAppendEvidence,
		MutationReceipt{},
		nil,
	)
	var mutationErr *MutationError
	if receipt.Commit != CommitCommitted ||
		!errors.As(err, &mutationErr) ||
		mutationErr.Kind != MutationInternal ||
		mutationErr.Class != errs.ErrorFatal ||
		mutationErr.Commit != CommitCommitted ||
		!strings.Contains(mutationErr.Error(), "internal invariant") {
		t.Fatalf("committedUnverified(nil) = %#v/%#v", receipt, mutationErr)
	}
}

func TestMutationClientCopiesContractIndex(t *testing.T) {
	t.Parallel()
	contract := mutationTestContract()
	rpc := &fakeMutationRequester{}
	client, err := newMutationClient(
		rpc,
		ownership.ExpectedOwnerToken("test-owner", "incarnation"),
		[]Contract{contract},
		time.Second,
		natsclient.RetryConfig{},
	)
	if err != nil {
		t.Fatalf("newMutationClient: %v", err)
	}
	contract.Groups[0].Predicates[0] = "a.b.c"
	contract.ForeignEdges[0].TargetPattern = "changed"

	_, err = client.canonicalizeCreate(validCreateMutation())
	if err != nil {
		t.Fatalf("mutating caller contract changed client index: %v", err)
	}

	_, err = newMutationClient(
		rpc,
		ownership.ExpectedOwnerToken("test-owner", "incarnation"),
		[]Contract{mutationTestContract(), mutationTestContract()},
		time.Second,
		natsclient.RetryConfig{},
	)
	if !errors.Is(err, ErrInvalidContract) {
		t.Fatalf("duplicate contract error = %v, want ErrInvalidContract", err)
	}
}

func TestBindMutationClientRequiresHeartbeatForOwningContractsBeforeRegistration(t *testing.T) {
	t.Parallel()
	// A zero Registry would panic if Bind/RegisterOwner were reached. The
	// heartbeat precondition must fail after pure contract validation and before
	// that first external side effect.
	registry := &ownership.Registry{}
	client, err := BindMutationClient(context.Background(), MutationClientConfig{
		NATS:      &natsclient.Client{},
		Registry:  registry,
		Owner:     "test-owner",
		Contracts: []Contract{multiGroupMutationTestContract()},
	})
	var mutationErr *MutationError
	if client != nil ||
		!errors.As(err, &mutationErr) ||
		mutationErr.Kind != MutationInvalid ||
		mutationErr.Commit != CommitNotCommitted {
		t.Fatalf("client/error = %#v/%#v, want nil invalid/not-committed", client, mutationErr)
	}
}

func marshalMutationTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal(%T): %v", value, err)
	}
	return data
}

func marshalMutationTestExact(t *testing.T, entity *graph.EntityState) []byte {
	t.Helper()
	return marshalMutationTestJSON(t, graph.ExactEntity{Entity: entity, KVRevision: 17})
}

func canonicalMutationTestEntity(req CreateMutation) *graph.EntityState {
	entity := req.Entity.Clone()
	entity.Triples = append([]message.Triple(nil), req.Triples...)
	for index := range entity.Triples {
		entity.Triples[index].Source = req.Metadata.Source
		entity.Triples[index].Context = req.Metadata.RequestID
		entity.Triples[index].Timestamp = req.Metadata.Timestamp
	}
	return entity
}

func TestReadAuthoritativeUsesGraphIngestAndReturnsClone(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	entity := canonicalMutationTestEntity(req)
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectQueryEntity: {{data: marshalMutationTestExact(t, entity)}},
	}}
	client := newMutationTestClient(t, rpc)

	got, err := client.ReadAuthoritative(context.Background(), entity.ID)
	if err != nil {
		t.Fatalf("ReadAuthoritative: %v", err)
	}
	got.Entity.Triples[0].Object = "mutated"

	if rpc.calls[0].subject != subjectQueryEntity || rpc.calls[0].retry != nil {
		t.Fatalf("read call = %#v, want single-attempt authoritative query", rpc.calls[0])
	}
	var wire struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rpc.calls[0].data, &wire); err != nil || wire.ID != entity.ID {
		t.Fatalf("read wire = %#v, %v", wire, err)
	}
	if entity.Triples[0].Object == "mutated" {
		t.Fatal("read result aliases transport fixture")
	}
}

func TestCreateWithTriplesReadsBackAmbiguousCommitWithoutBlindRetry(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	entity := canonicalMutationTestEntity(req)
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectCreateWithTriples: {{err: errors.New("reply timeout")}},
		subjectQueryEntity:       {{data: marshalMutationTestExact(t, entity)}},
	}}
	client := newMutationTestClient(t, rpc)

	receipt, err := client.CreateWithTriples(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateWithTriples: %v", err)
	}
	if receipt.Commit != CommitVerified || receipt.Entity == nil {
		t.Fatalf("receipt = %#v, want verified entity", receipt)
	}
	if len(rpc.calls) != 2 ||
		rpc.calls[0].subject != subjectCreateWithTriples ||
		rpc.calls[1].subject != subjectQueryEntity {
		t.Fatalf("calls = %#v, want create then authoritative read", rpc.calls)
	}
	var wire graph.CreateEntityWithTriplesRequest
	if err := json.Unmarshal(rpc.calls[0].data, &wire); err != nil {
		t.Fatalf("decode create wire: %v", err)
	}
	if wire.OwnerToken != "test-owner#incarnation" ||
		wire.IndexingProfile != "control" ||
		wire.RequestID != req.Metadata.RequestID ||
		wire.Triples[0].Source != req.Metadata.Source {
		t.Fatalf("create wire = %#v", wire)
	}
}

func TestCreateWithTriplesRetriesStableRequestAfterAuthoritativeAbsence(t *testing.T) {
	t.Parallel()
	request := validCreateMutation()
	entity := canonicalMutationTestEntity(request)
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectCreateWithTriples: {
			{err: errors.New("reply lost")},
			{data: marshalMutationTestJSON(t, graph.CreateEntityWithTriplesResponse{
				MutationResponse: graph.MutationResponse{KVRevision: 15},
				Entity:           entity,
			})},
		},
		subjectQueryEntity: {{
			err: errs.ClassifiedCodeDetail(
				errs.ErrorInvalid,
				graph.ErrorCodeEntityNotFound,
				nil,
				errors.New("entity absent"),
			),
		}},
	}}
	client := newMutationTestClient(t, rpc)
	client.retry.MaxRetries = 1

	receipt, err := client.CreateWithTriples(context.Background(), request)
	if err != nil || receipt.Commit != CommitVerified {
		t.Fatalf("receipt/error = %#v/%v", receipt, err)
	}
	if len(rpc.calls) != 3 ||
		rpc.calls[0].subject != subjectCreateWithTriples ||
		rpc.calls[1].subject != subjectQueryEntity ||
		rpc.calls[2].subject != subjectCreateWithTriples {
		t.Fatalf("calls = %#v, want create/read/create", rpc.calls)
	}
	if string(rpc.calls[0].data) != string(rpc.calls[2].data) {
		t.Fatal("create retry changed canonical request bytes")
	}
}

func TestCreateWithTriplesDegradedSuccessIsReadBackNotRetried(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	entity := canonicalMutationTestEntity(req)
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectCreateWithTriples: {{
			data: marshalMutationTestJSON(t, graph.CreateEntityWithTriplesResponse{
				MutationResponse: graph.MutationResponse{Degraded: true},
			}),
		}},
		subjectQueryEntity: {{data: marshalMutationTestExact(t, entity)}},
	}}
	client := newMutationTestClient(t, rpc)

	receipt, err := client.CreateWithTriples(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateWithTriples: %v", err)
	}
	if receipt.Commit != CommitVerified || !receipt.Degraded || len(rpc.calls) != 2 {
		t.Fatalf("receipt/calls = %#v/%#v", receipt, rpc.calls)
	}
}

func TestCreateWithTriplesDegradedFailedReadIsCommittedUnverified(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	readErr := errs.ClassifiedCodeDetail(
		errs.ErrorTransient, graph.ErrorCodeInternal, nil, errors.New("read failed"),
	)
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectCreateWithTriples: {{
			data: marshalMutationTestJSON(t, graph.CreateEntityWithTriplesResponse{
				MutationResponse: graph.MutationResponse{Degraded: true, KVRevision: 9},
			}),
		}},
		subjectQueryEntity: {{err: readErr}},
	}}
	client := newMutationTestClient(t, rpc)

	receipt, err := client.CreateWithTriples(context.Background(), req)
	var mutationErr *MutationError
	if !errors.As(err, &mutationErr) ||
		mutationErr.Kind != MutationCommittedUnverified ||
		mutationErr.Commit != CommitCommitted ||
		receipt.Commit != CommitCommitted ||
		!receipt.Degraded ||
		receipt.KVRevision != 9 {
		t.Fatalf("receipt/error = %#v/%#v", receipt, mutationErr)
	}
}

func TestBirthOnlyClientBindsWithoutRegistryHeartbeatOrToken(t *testing.T) {
	t.Parallel()
	contract := birthOnlyMutationTestContract()
	client, err := BindMutationClient(context.Background(), MutationClientConfig{
		NATS:      &natsclient.Client{},
		Owner:     "birth-writer",
		Contracts: []Contract{contract},
	})
	if err != nil {
		t.Fatalf("BindMutationClient birth-only: %v", err)
	}
	if client == nil || !client.token.IsZero() {
		t.Fatalf("birth-only client/token = %#v/%q, want client with zero token", client, client.token.Wire())
	}
}

func TestCreateWithBirthPredicatesIsTokenFreeAndOwningFactsAreFenced(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	tests := []struct {
		name      string
		contract  Contract
		triples   []message.Triple
		tokenWant string
	}{
		{
			name:     "birth only",
			contract: birthOnlyMutationTestContract(),
			triples: []message.Triple{{
				Subject: req.Entity.ID, Predicate: "sensorml.process.uid", Object: "widget-001",
			}},
		},
		{
			name:     "birth plus owning",
			contract: multiGroupMutationTestContract(),
			triples: []message.Triple{
				{Subject: req.Entity.ID, Predicate: "sensorml.process.uid", Object: "widget-001"},
				{Subject: req.Entity.ID, Predicate: "sensorml.process.label", Object: "Widget"},
			},
			tokenWant: "test-owner#incarnation",
		},
		{
			name: "cas predicate is create-authorized",
			contract: Contract{
				Name: "test.widget.cas", MessageType: "test.fixture.v1",
				EntityPattern: "acme.ops.test.system.widget.*",
				Groups: []PredicateGroup{{
					Name: "phase", Mode: ownership.ModeCASTransition,
					Predicates: []string{"sensorml.process.label"},
				}},
			},
			triples: []message.Triple{{
				Subject: req.Entity.ID, Predicate: "sensorml.process.label", Object: "initial",
			}},
			tokenWant: "test-owner#incarnation",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			create := req
			create.Contract = tt.contract.Name
			create.Triples = tt.triples
			entity := create.Entity.Clone()
			entity.Triples = append([]message.Triple(nil), tt.triples...)
			for index := range entity.Triples {
				entity.Triples[index].Source = create.Metadata.Source
				entity.Triples[index].Context = create.Metadata.RequestID
				entity.Triples[index].Timestamp = create.Metadata.Timestamp
			}
			rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
				subjectCreateWithTriples: {{
					data: marshalMutationTestJSON(t, graph.CreateEntityWithTriplesResponse{Entity: entity}),
				}},
			}}
			client, err := newMutationClient(
				rpc,
				ownership.ExpectedOwnerToken("test-owner", "incarnation"),
				[]Contract{tt.contract},
				time.Second,
				natsclient.RetryConfig{},
			)
			if err != nil {
				t.Fatalf("newMutationClient: %v", err)
			}

			receipt, err := client.CreateWithTriples(context.Background(), create)
			if err != nil || receipt.Commit != CommitVerified {
				t.Fatalf("receipt/error = %#v/%v", receipt, err)
			}
			var wire graph.CreateEntityWithTriplesRequest
			if err := json.Unmarshal(rpc.calls[0].data, &wire); err != nil {
				t.Fatalf("decode create wire: %v", err)
			}
			if wire.OwnerToken != tt.tokenWant {
				t.Fatalf("owner token = %q, want %q", wire.OwnerToken, tt.tokenWant)
			}
		})
	}
}

func TestBirthPredicatesDoNotAuthorizeAppendOrReplacement(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	contract := multiGroupMutationTestContract()
	rpc := &fakeMutationRequester{}
	client, err := newMutationClient(
		rpc,
		ownership.ExpectedOwnerToken("test-owner", "incarnation"),
		[]Contract{contract},
		time.Second,
		natsclient.RetryConfig{},
	)
	if err != nil {
		t.Fatalf("newMutationClient: %v", err)
	}
	birthTriple := message.Triple{
		Subject: req.Entity.ID, Predicate: "sensorml.process.uid", Object: "immutable",
	}
	_, appendErr := client.AppendEvidence(context.Background(), AppendEvidenceMutation{
		Contract: contract.Name, EntityID: req.Entity.ID,
		Evidence: []message.Triple{birthTriple}, Metadata: req.Metadata,
	})
	_, replaceErr := client.ReplaceOwned(context.Background(), ReplaceOwnedMutation{
		Contract: contract.Name, Group: "identity", EntityID: req.Entity.ID,
		Desired: []message.Triple{birthTriple}, Metadata: req.Metadata,
	})
	for name, err := range map[string]error{"append": appendErr, "replace": replaceErr} {
		var mutationErr *MutationError
		if !errors.As(err, &mutationErr) || mutationErr.Kind != MutationInvalid {
			t.Fatalf("%s error = %#v, want invalid", name, mutationErr)
		}
	}
	if rpc.callCount() != 0 {
		t.Fatalf("birth predicate misuse published %d calls", rpc.callCount())
	}
}

func TestAppendEvidenceDegradedResponseRequiresAuthoritativeVerification(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	evidence := message.Triple{
		Subject: req.Entity.ID, Predicate: "shared.value.p", Object: "evidence",
		Source: req.Metadata.Source, Context: req.Metadata.RequestID, Timestamp: req.Metadata.Timestamp,
	}
	present := canonicalMutationTestEntity(req)
	present.Triples = []message.Triple{evidence}
	absent := canonicalMutationTestEntity(req)
	absent.Triples = nil
	tests := []struct {
		name     string
		read     fakeRPCResult
		want     CommitState
		wantKind MutationErrorKind
	}{
		{
			name: "verified",
			read: fakeRPCResult{data: marshalMutationTestExact(t, present)},
			want: CommitVerified,
		},
		{
			name: "tuple absent",
			read: fakeRPCResult{data: marshalMutationTestExact(t, absent)},
			want: CommitCommitted, wantKind: MutationCommittedUnverified,
		},
		{
			name: "read unavailable",
			read: fakeRPCResult{err: errors.New("read unavailable")},
			want: CommitCommitted, wantKind: MutationCommittedUnverified,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
				subjectAddTriplesBatch: {{
					data: marshalMutationTestJSON(t, graph.AddTriplesBatchResponse{
						MutationResponse: graph.MutationResponse{
							Degraded: true, KVRevision: 23,
						},
						WrittenCount: 1,
					}),
				}},
				subjectQueryEntity: {tt.read},
			}}
			client := newMutationTestClient(t, rpc)
			client.retry.MaxRetries = 3

			receipt, err := client.AppendEvidence(context.Background(), AppendEvidenceMutation{
				Contract: req.Contract, EntityID: req.Entity.ID,
				Evidence: []message.Triple{evidence}, Metadata: req.Metadata,
			})
			if receipt.Commit != tt.want || !receipt.Degraded || receipt.KVRevision != 23 {
				t.Fatalf("receipt = %#v, want %q degraded revision 23", receipt, tt.want)
			}
			if tt.wantKind == "" {
				if err != nil {
					t.Fatalf("AppendEvidence: %v", err)
				}
			} else {
				var mutationErr *MutationError
				if !errors.As(err, &mutationErr) ||
					mutationErr.Kind != tt.wantKind ||
					mutationErr.Commit != CommitCommitted {
					t.Fatalf("error = %#v, want %q/committed", mutationErr, tt.wantKind)
				}
			}
			if rpc.callCount() != 2 {
				t.Fatalf("calls = %d, want append plus one read and no retry", rpc.callCount())
			}
		})
	}
}

func TestCreateAndReplaceVerificationUsesCompleteTripleEquality(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	req.Triples[0].Object = map[string]any{"count": 1}
	req.Triples[0].Confidence = 0.9
	expiresAt := req.Metadata.Timestamp.Add(time.Hour)
	req.Triples[0].ExpiresAt = &expiresAt
	stored := canonicalMutationTestEntity(req)

	t.Run("semantic JSON object normalization succeeds", func(t *testing.T) {
		t.Parallel()
		rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
			subjectCreateWithTriples: {{
				data: marshalMutationTestJSON(t, graph.CreateEntityWithTriplesResponse{
					Entity: stored,
				}),
			}},
		}}
		client := newMutationTestClient(t, rpc)
		receipt, err := client.CreateWithTriples(context.Background(), req)
		if err != nil || receipt.Commit != CommitVerified {
			t.Fatalf("receipt/error = %#v/%v, want verified", receipt, err)
		}
		if rpc.callCount() != 1 {
			t.Fatalf("calls = %d, object normalization forced read-back", rpc.callCount())
		}
	})

	t.Run("create confidence mismatch", func(t *testing.T) {
		t.Parallel()
		mismatch := stored.Clone()
		mismatch.Triples[0].Confidence = 0.4
		rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
			subjectCreateWithTriples: {{
				data: marshalMutationTestJSON(t, graph.CreateEntityWithTriplesResponse{
					Entity: mismatch,
				}),
			}},
			subjectQueryEntity: {{data: marshalMutationTestExact(t, mismatch)}},
		}}
		client := newMutationTestClient(t, rpc)
		receipt, err := client.CreateWithTriples(context.Background(), req)
		var mutationErr *MutationError
		if !errors.As(err, &mutationErr) ||
			mutationErr.Kind != MutationCommittedUnverified ||
			receipt.Commit != CommitCommitted {
			t.Fatalf("receipt/error = %#v/%#v", receipt, mutationErr)
		}
	})

	t.Run("replace expiration mismatch", func(t *testing.T) {
		t.Parallel()
		mismatch := stored.Clone()
		otherExpiry := expiresAt.Add(time.Minute)
		mismatch.Triples[0].ExpiresAt = &otherExpiry
		rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
			subjectUpdateWithTriples: {{
				data: marshalMutationTestJSON(t, graph.UpdateEntityWithTriplesResponse{
					Entity: mismatch,
				}),
			}},
			subjectQueryEntity: {{data: marshalMutationTestExact(t, mismatch)}},
		}}
		client := newMutationTestClient(t, rpc)
		receipt, err := client.ReplaceOwned(context.Background(), ReplaceOwnedMutation{
			Contract: req.Contract, EntityID: req.Entity.ID,
			Desired: req.Triples, Metadata: req.Metadata,
		})
		var mutationErr *MutationError
		if !errors.As(err, &mutationErr) ||
			mutationErr.Kind != MutationCommittedUnverified ||
			receipt.Commit != CommitCommitted {
			t.Fatalf("receipt/error = %#v/%#v", receipt, mutationErr)
		}
	})
}

func TestCreateRejectsEntityEmbeddedTriplesWithoutMutatingInput(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	req.Entity.Triples = []message.Triple{{
		Subject: req.Entity.ID, Predicate: "sensorml.process.label", Object: "embedded",
	}}
	before := req.Entity.Clone()
	rpc := &fakeMutationRequester{}
	client := newMutationTestClient(t, rpc)

	receipt, err := client.CreateWithTriples(context.Background(), req)
	var mutationErr *MutationError
	if !errors.As(err, &mutationErr) ||
		mutationErr.Kind != MutationInvalid ||
		receipt.Commit != CommitNotCommitted {
		t.Fatalf("receipt/error = %#v/%#v, want invalid/not-committed", receipt, mutationErr)
	}
	if !reflect.DeepEqual(req.Entity, before) {
		t.Fatalf("CreateWithTriples mutated entity input:\n got %#v\nwant %#v", req.Entity, before)
	}
	if rpc.callCount() != 0 {
		t.Fatalf("embedded birth facts published %d requests", rpc.callCount())
	}
}

func TestCreateAndAppendContextCancellationStopsReadBack(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	for _, operation := range []string{"create", "append"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{}}
			if operation == "create" {
				rpc.responses[subjectCreateWithTriples] = []fakeRPCResult{{err: context.Canceled}}
			} else {
				rpc.responses[subjectAddTriplesBatch] = []fakeRPCResult{{err: context.Canceled}}
			}
			client := newMutationTestClient(t, rpc)
			var receipt MutationReceipt
			var err error
			if operation == "create" {
				receipt, err = client.CreateWithTriples(ctx, req)
			} else {
				receipt, err = client.AppendEvidence(ctx, AppendEvidenceMutation{
					Contract: req.Contract, EntityID: req.Entity.ID,
					Evidence: []message.Triple{{
						Subject: req.Entity.ID, Predicate: "shared.value.p", Object: "evidence",
					}},
					Metadata: req.Metadata,
				})
			}
			var mutationErr *MutationError
			if !errors.As(err, &mutationErr) ||
				mutationErr.Kind != MutationCommitUnknown ||
				receipt.Commit != CommitUnknown {
				t.Fatalf("receipt/error = %#v/%#v", receipt, mutationErr)
			}
			if rpc.callCount() != 1 {
				t.Fatalf("canceled %s made %d calls, want no read-back", operation, rpc.callCount())
			}
		})
	}
}

func TestReplaceOwnedDerivesRemovalSetAndUsesDirectClassifiedRPC(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	desired := canonicalMutationTestEntity(req).Triples
	entity := canonicalMutationTestEntity(req)
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectUpdateWithTriples: {{
			data: marshalMutationTestJSON(t, graph.UpdateEntityWithTriplesResponse{
				MutationResponse: graph.MutationResponse{KVRevision: 17},
				Entity:           entity,
			}),
		}},
	}}
	client := newMutationTestClient(t, rpc)

	receipt, err := client.ReplaceOwned(context.Background(), ReplaceOwnedMutation{
		Contract: req.Contract, EntityID: req.Entity.ID, Desired: desired, Metadata: req.Metadata,
	})
	if err != nil {
		t.Fatalf("ReplaceOwned: %v", err)
	}
	if receipt.Commit != CommitVerified || receipt.KVRevision != 17 {
		t.Fatalf("receipt = %#v", receipt)
	}
	if len(rpc.calls) != 1 || rpc.calls[0].retry != nil {
		t.Fatalf("replace calls = %#v, want direct classified RPC", rpc.calls)
	}
	var wire graph.UpdateEntityWithTriplesRequest
	if err := json.Unmarshal(rpc.calls[0].data, &wire); err != nil {
		t.Fatalf("decode replace wire: %v", err)
	}
	if len(wire.RemoveTriples) != 1 ||
		wire.RemoveTriples[0] != "sensorml.process.label" ||
		wire.ExpectedRevision != 0 ||
		wire.OwnerToken != "test-owner#incarnation" {
		t.Fatalf("replace wire = %#v", wire)
	}
}

func TestReplaceOwnedSelectsExactlyOneNamedGroupAndIgnoresSiblings(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	contract := multiGroupMutationTestContract()
	selected := message.Triple{
		Subject: req.Entity.ID, Predicate: "sensorml.process.position", Object: "POINT (1 2)",
		Source: req.Metadata.Source, Context: req.Metadata.RequestID, Timestamp: req.Metadata.Timestamp,
	}
	authoritative := canonicalMutationTestEntity(req)
	authoritative.Triples = []message.Triple{
		selected,
		{
			Subject: req.Entity.ID, Predicate: "sensorml.process.label", Object: "divergent sibling",
			Source: req.Metadata.Source, Context: req.Metadata.RequestID, Timestamp: req.Metadata.Timestamp,
		},
		{
			Subject: req.Entity.ID, Predicate: "sensorml.process.uid", Object: "immutable-birth",
			Source: req.Metadata.Source, Context: req.Metadata.RequestID, Timestamp: req.Metadata.Timestamp,
		},
	}
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectUpdateWithTriples: {{
			data: marshalMutationTestJSON(t, graph.UpdateEntityWithTriplesResponse{
				MutationResponse: graph.MutationResponse{KVRevision: 29},
				Entity:           authoritative,
			}),
		}},
	}}
	client, err := newMutationClient(
		rpc,
		ownership.ExpectedOwnerToken("test-owner", "incarnation"),
		[]Contract{contract},
		time.Second,
		natsclient.RetryConfig{},
	)
	if err != nil {
		t.Fatalf("newMutationClient: %v", err)
	}

	receipt, err := client.ReplaceOwned(context.Background(), ReplaceOwnedMutation{
		Contract: contract.Name, Group: "position", EntityID: req.Entity.ID,
		Desired: []message.Triple{selected}, Metadata: req.Metadata,
	})
	if err != nil {
		t.Fatalf("ReplaceOwned: %v", err)
	}
	if receipt.Commit != CommitVerified {
		t.Fatalf("receipt = %#v, want selected-group verification", receipt)
	}
	var wire graph.UpdateEntityWithTriplesRequest
	if err := json.Unmarshal(rpc.calls[0].data, &wire); err != nil {
		t.Fatalf("decode replace wire: %v", err)
	}
	if !reflect.DeepEqual(wire.RemoveTriples, []string{"sensorml.process.position"}) {
		t.Fatalf("removal set = %v, want selected position group only", wire.RemoveTriples)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rpc.calls[0].data, &raw); err != nil {
		t.Fatalf("decode raw replace wire: %v", err)
	}
	if _, leaked := raw["group"]; leaked {
		t.Fatal("local group selector leaked onto graph mutation wire")
	}
}

func TestReplaceOwnedSelectedGroupDeletesOmittedPredicateOnlyWithinGroup(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	contract := multiGroupMutationTestContract()
	label := message.Triple{
		Subject: req.Entity.ID, Predicate: "sensorml.process.label", Object: "Widget",
		Source: req.Metadata.Source, Context: req.Metadata.RequestID, Timestamp: req.Metadata.Timestamp,
	}
	authoritative := canonicalMutationTestEntity(req)
	authoritative.Triples = []message.Triple{
		label,
		{
			Subject: req.Entity.ID, Predicate: "sensorml.process.position", Object: "sibling-preserved",
			Source: req.Metadata.Source, Context: req.Metadata.RequestID, Timestamp: req.Metadata.Timestamp,
		},
	}
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectUpdateWithTriples: {{
			data: marshalMutationTestJSON(t, graph.UpdateEntityWithTriplesResponse{Entity: authoritative}),
		}},
	}}
	client, err := newMutationClient(
		rpc,
		ownership.ExpectedOwnerToken("test-owner", "incarnation"),
		[]Contract{contract},
		time.Second,
		natsclient.RetryConfig{},
	)
	if err != nil {
		t.Fatalf("newMutationClient: %v", err)
	}
	receipt, err := client.ReplaceOwned(context.Background(), ReplaceOwnedMutation{
		Contract: contract.Name, Group: "identity", EntityID: req.Entity.ID,
		Desired: []message.Triple{label}, Metadata: req.Metadata,
	})
	if err != nil || receipt.Commit != CommitVerified {
		t.Fatalf("receipt/error = %#v/%v", receipt, err)
	}
	var wire graph.UpdateEntityWithTriplesRequest
	if err := json.Unmarshal(rpc.calls[0].data, &wire); err != nil {
		t.Fatalf("decode replace wire: %v", err)
	}
	want := []string{"sensorml.process.description", "sensorml.process.label"}
	if !reflect.DeepEqual(wire.RemoveTriples, want) {
		t.Fatalf("removal set = %v, want full selected group %v", wire.RemoveTriples, want)
	}
}

func TestReplaceOwnedGroupSelectorValidationPrecedesTransport(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	contract := multiGroupMutationTestContract()
	tests := []struct {
		name    string
		group   string
		desired []message.Triple
	}{
		{name: "ambiguous omission"},
		{name: "unknown", group: "missing"},
		{name: "non-replace", group: "evidence"},
		{
			name: "selected group rejects sibling desired", group: "position",
			desired: []message.Triple{{
				Subject: req.Entity.ID, Predicate: "sensorml.process.label", Object: "wrong group",
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rpc := &fakeMutationRequester{}
			client, err := newMutationClient(
				rpc,
				ownership.ExpectedOwnerToken("test-owner", "incarnation"),
				[]Contract{contract},
				time.Second,
				natsclient.RetryConfig{},
			)
			if err != nil {
				t.Fatalf("newMutationClient: %v", err)
			}
			receipt, err := client.ReplaceOwned(context.Background(), ReplaceOwnedMutation{
				Contract: contract.Name, Group: tt.group, EntityID: req.Entity.ID,
				Desired: tt.desired, Metadata: req.Metadata,
			})
			var mutationErr *MutationError
			if !errors.As(err, &mutationErr) ||
				mutationErr.Kind != MutationInvalid ||
				receipt.Commit != CommitNotCommitted {
				t.Fatalf("receipt/error = %#v/%#v, want invalid/not-committed", receipt, mutationErr)
			}
			if rpc.callCount() != 0 {
				t.Fatalf("invalid selector published %d calls", rpc.callCount())
			}
		})
	}
}

func TestReplaceOwnedOmittedSelectorKeepsSingleUnnamedGroupCompatible(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	entity := canonicalMutationTestEntity(req)
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectUpdateWithTriples: {{
			data: marshalMutationTestJSON(t, graph.UpdateEntityWithTriplesResponse{Entity: entity}),
		}},
	}}
	client := newMutationTestClient(t, rpc)
	receipt, err := client.ReplaceOwned(context.Background(), ReplaceOwnedMutation{
		Contract: req.Contract, EntityID: req.Entity.ID,
		Desired: entity.Triples, Metadata: req.Metadata,
	})
	if err != nil || receipt.Commit != CommitVerified {
		t.Fatalf("receipt/error = %#v/%v, want backward-compatible unnamed selection", receipt, err)
	}
}

func TestAppendEvidenceReadsExactTupleBeforeStableRetry(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	evidence := message.Triple{
		Subject: req.Entity.ID, Predicate: "shared.value.p", Object: "evidence",
	}
	mismatch := canonicalMutationTestEntity(req)
	mismatch.Triples = []message.Triple{{
		Subject: evidence.Subject, Predicate: "shared.value.p", Object: evidence.Object,
		Source: "different", Context: req.Metadata.RequestID, Timestamp: req.Metadata.Timestamp,
	}}
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectAddTriplesBatch: {
			{err: errors.New("reply timeout")},
			{data: marshalMutationTestJSON(t, graph.AddTriplesBatchResponse{WrittenCount: 1})},
		},
		subjectQueryEntity: {{data: marshalMutationTestExact(t, mismatch)}},
	}}
	client := newMutationTestClient(t, rpc)
	client.retry.MaxRetries = 1

	receipt, err := client.AppendEvidence(context.Background(), AppendEvidenceMutation{
		Contract: req.Contract, EntityID: req.Entity.ID,
		Evidence: []message.Triple{evidence}, Metadata: req.Metadata,
	})
	if err != nil {
		t.Fatalf("AppendEvidence: %v", err)
	}
	if receipt.Commit != CommitCommitted {
		t.Fatalf("receipt = %#v", receipt)
	}
	if len(rpc.calls) != 3 ||
		rpc.calls[0].subject != subjectAddTriplesBatch ||
		rpc.calls[1].subject != subjectQueryEntity ||
		rpc.calls[2].subject != subjectAddTriplesBatch {
		t.Fatalf("calls = %#v, want append/read/append", rpc.calls)
	}
	if string(rpc.calls[0].data) != string(rpc.calls[2].data) {
		t.Fatal("append retry changed canonical request bytes")
	}
	if rpc.calls[0].retry != nil || rpc.calls[2].retry != nil {
		t.Fatal("append used blind retrying RPC")
	}
}

func TestCreateAndAppendCancellationDuringRetryBackoffIsNotCommitted(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	for _, operation := range []string{"create", "append"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			ambiguous := errors.New("reply timeout")
			rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{}}
			if operation == "create" {
				rpc.responses[subjectCreateWithTriples] = []fakeRPCResult{{err: ambiguous}}
			} else {
				rpc.responses[subjectAddTriplesBatch] = []fakeRPCResult{{err: ambiguous}}
			}
			rpc.responses[subjectQueryEntity] = []fakeRPCResult{{err: errs.ClassifiedCodeDetail(
				errs.ErrorInvalid,
				graph.ErrorCodeEntityNotFound,
				nil,
				errors.New("entity absent"),
			)}}
			client := newMutationTestClient(t, rpc)
			client.retry.MaxRetries = 1
			ctx, cancel := context.WithCancel(context.Background())
			client.retryWait = func(waitCtx context.Context, _ natsclient.RetryConfig, _ int) error {
				cancel()
				return waitCtx.Err()
			}

			var receipt MutationReceipt
			var err error
			if operation == "create" {
				receipt, err = client.CreateWithTriples(ctx, req)
			} else {
				receipt, err = client.AppendEvidence(ctx, AppendEvidenceMutation{
					Contract: req.Contract, EntityID: req.Entity.ID,
					Evidence: []message.Triple{{
						Subject: req.Entity.ID, Predicate: "shared.value.p", Object: "evidence",
					}},
					Metadata: req.Metadata,
				})
			}

			var mutationErr *MutationError
			if !errors.As(err, &mutationErr) ||
				mutationErr.Kind != MutationCommitUnknown ||
				mutationErr.Class != errs.ErrorTransient ||
				mutationErr.Commit != CommitUnknown ||
				receipt.Commit != CommitUnknown ||
				!errors.Is(err, context.Canceled) ||
				!errors.Is(err, ambiguous) {
				t.Fatalf("receipt/error = %#v/%#v, want commit-unknown cancellation", receipt, mutationErr)
			}
			if rpc.callCount() != 2 {
				t.Fatalf("calls = %d, want mutation plus absence read and no retry", rpc.callCount())
			}
		})
	}
}

func TestCreateAndAppendCancellationAtAuthoritativeAbsenceReturnIsNotCommitted(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	absent := canonicalMutationTestEntity(req)
	absent.Triples = nil
	for _, operation := range []string{"create", "append"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			ambiguous := errors.New("reply timeout")
			rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{}}
			if operation == "create" {
				rpc.responses[subjectCreateWithTriples] = []fakeRPCResult{{err: ambiguous}}
				rpc.responses[subjectQueryEntity] = []fakeRPCResult{{
					err: errs.ClassifiedCodeDetail(
						errs.ErrorInvalid,
						graph.ErrorCodeEntityNotFound,
						nil,
						errors.New("entity absent"),
					),
					beforeReturn: cancel,
				}}
			} else {
				rpc.responses[subjectAddTriplesBatch] = []fakeRPCResult{{err: ambiguous}}
				rpc.responses[subjectQueryEntity] = []fakeRPCResult{{
					data:         marshalMutationTestExact(t, absent),
					beforeReturn: cancel,
				}}
			}
			client := newMutationTestClient(t, rpc)
			client.retry.MaxRetries = 1
			client.retry.InitialBackoff = time.Hour

			var receipt MutationReceipt
			var err error
			if operation == "create" {
				receipt, err = client.CreateWithTriples(ctx, req)
			} else {
				receipt, err = client.AppendEvidence(ctx, AppendEvidenceMutation{
					Contract: req.Contract, EntityID: req.Entity.ID,
					Evidence: []message.Triple{{
						Subject: req.Entity.ID, Predicate: "shared.value.p", Object: "evidence",
					}},
					Metadata: req.Metadata,
				})
			}

			var mutationErr *MutationError
			if !errors.As(err, &mutationErr) ||
				mutationErr.Kind != MutationCommitUnknown ||
				mutationErr.Class != errs.ErrorTransient ||
				mutationErr.Commit != CommitUnknown ||
				receipt.Commit != CommitUnknown ||
				!errors.Is(err, context.Canceled) ||
				!errors.Is(err, ambiguous) {
				t.Fatalf("receipt/error = %#v/%#v, want commit-unknown cancellation", receipt, mutationErr)
			}
			if rpc.callCount() != 2 {
				t.Fatalf("calls = %d, want mutation plus definitive absence read", rpc.callCount())
			}
		})
	}
}

func TestReplaceOwnedRejectsContractWithoutReplaceOwnedPredicates(t *testing.T) {
	t.Parallel()
	contract := mutationTestContract()
	contract.Name = "test.append-only"
	contract.Groups = []PredicateGroup{{
		Mode: ownership.ModeAppendEvidence, Predicates: []string{"shared.value.p"},
	}}
	rpc := &fakeMutationRequester{}
	client, err := newMutationClient(
		rpc,
		ownership.ExpectedOwnerToken("test-owner", "incarnation"),
		[]Contract{contract},
		time.Second,
		natsclient.RetryConfig{},
	)
	if err != nil {
		t.Fatalf("newMutationClient: %v", err)
	}
	req := validCreateMutation()
	receipt, err := client.ReplaceOwned(context.Background(), ReplaceOwnedMutation{
		Contract: contract.Name, EntityID: req.Entity.ID,
	})
	var mutationErr *MutationError
	if !errors.As(err, &mutationErr) ||
		mutationErr.Kind != MutationInvalid ||
		receipt.Commit != CommitNotCommitted {
		t.Fatalf("receipt/error = %#v/%#v, want invalid/not-committed", receipt, mutationErr)
	}
	if rpc.callCount() != 0 {
		t.Fatalf("empty replacement published %d requests", rpc.callCount())
	}
}

func TestCreateRejectsAppendOnlyContractBeforeTransport(t *testing.T) {
	t.Parallel()
	contract := mutationTestContract()
	contract.Name = "test.append-only-create"
	contract.Groups = []PredicateGroup{{
		Mode: ownership.ModeAppendEvidence, Predicates: []string{"shared.value.p"},
	}}
	rpc := &fakeMutationRequester{}
	client, err := newMutationClient(
		rpc,
		ownership.ExpectedOwnerToken("test-owner", "incarnation"),
		[]Contract{contract},
		time.Second,
		natsclient.RetryConfig{},
	)
	if err != nil {
		t.Fatalf("newMutationClient: %v", err)
	}
	req := validCreateMutation()
	req.Contract = contract.Name
	req.Triples[0].Predicate = "shared.value.p"
	receipt, err := client.CreateWithTriples(context.Background(), req)
	var mutationErr *MutationError
	if !errors.As(err, &mutationErr) ||
		mutationErr.Kind != MutationInvalid ||
		receipt.Commit != CommitNotCommitted {
		t.Fatalf("receipt/error = %#v/%#v, want invalid/not-committed", receipt, mutationErr)
	}
	if rpc.callCount() != 0 {
		t.Fatalf("append-only create published %d requests", rpc.callCount())
	}
}

func TestAppendEvidenceAnomalousSuccessRequiresAuthoritativeVerification(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	evidence := message.Triple{
		Subject: req.Entity.ID, Predicate: "shared.value.p", Object: "evidence",
	}
	canonical := evidence
	canonical.Source = req.Metadata.Source
	canonical.Context = req.Metadata.RequestID
	canonical.Timestamp = req.Metadata.Timestamp
	present := canonicalMutationTestEntity(req)
	present.Triples = []message.Triple{canonical}
	absent := canonicalMutationTestEntity(req)
	absent.Triples = nil

	tests := []struct {
		name     string
		response graph.AddTriplesBatchResponse
		read     fakeRPCResult
		want     CommitState
		wantKind MutationErrorKind
	}{
		{
			name:     "written count mismatch verified present",
			response: graph.AddTriplesBatchResponse{WrittenCount: 0},
			read:     fakeRPCResult{data: marshalMutationTestExact(t, present)},
			want:     CommitVerified,
		},
		{
			name: "unrelated failed subject verified present",
			response: graph.AddTriplesBatchResponse{
				WrittenCount: 1,
				FailedSubjects: map[string]string{
					"acme.ops.test.system.other.002": "unexpected",
				},
			},
			read: fakeRPCResult{data: marshalMutationTestExact(t, present)},
			want: CommitVerified,
		},
		{
			name: "written count mismatch proven absent",
			response: graph.AddTriplesBatchResponse{
				WrittenCount: 2,
			},
			read:     fakeRPCResult{data: marshalMutationTestExact(t, absent)},
			want:     CommitNotCommitted,
			wantKind: MutationInternal,
		},
		{
			name: "partial requested subject with absent entity is not found",
			response: graph.AddTriplesBatchResponse{
				WrittenCount: 0,
				FailedSubjects: map[string]string{
					req.Entity.ID: "rejected",
				},
			},
			read: fakeRPCResult{err: errs.ClassifiedCodeDetail(
				errs.ErrorInvalid,
				graph.ErrorCodeEntityNotFound,
				nil,
				errors.New("entity absent"),
			)},
			want:     CommitNotCommitted,
			wantKind: MutationNotFound,
		},
		{
			name: "malformed mixed partial requires unavailable read",
			response: graph.AddTriplesBatchResponse{
				WrittenCount: 1,
				FailedSubjects: map[string]string{
					req.Entity.ID:                    "rejected",
					"acme.ops.test.system.other.002": "unexpected",
				},
			},
			read: fakeRPCResult{err: errors.New("read unavailable")},
			want: CommitUnknown, wantKind: MutationCommitUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			responses := map[string][]fakeRPCResult{
				subjectAddTriplesBatch: {{data: marshalMutationTestJSON(t, tt.response)}},
			}
			if tt.read.data != nil || tt.read.err != nil {
				responses[subjectQueryEntity] = []fakeRPCResult{tt.read}
			}
			rpc := &fakeMutationRequester{responses: responses}
			client := newMutationTestClient(t, rpc)

			receipt, err := client.AppendEvidence(context.Background(), AppendEvidenceMutation{
				Contract: req.Contract, EntityID: req.Entity.ID,
				Evidence: []message.Triple{evidence}, Metadata: req.Metadata,
			})
			if receipt.Commit != tt.want {
				t.Fatalf("commit = %q, want %q (err %v)", receipt.Commit, tt.want, err)
			}
			if tt.wantKind == "" {
				if err != nil {
					t.Fatalf("AppendEvidence: %v", err)
				}
			} else {
				var mutationErr *MutationError
				if !errors.As(err, &mutationErr) || mutationErr.Kind != tt.wantKind {
					t.Fatalf("error = %#v, want kind %q", mutationErr, tt.wantKind)
				}
			}
			if rpc.callCount() != 2 {
				t.Fatalf("calls = %d, want 2", rpc.callCount())
			}
		})
	}
}

func TestAppendEvidenceLostResponseWithExactTupleDoesNotRetry(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	evidence := message.Triple{
		Subject: req.Entity.ID, Predicate: "shared.value.p", Object: map[string]any{"count": 1},
		Source: req.Metadata.Source, Context: req.Metadata.RequestID, Timestamp: req.Metadata.Timestamp,
	}
	stored := canonicalMutationTestEntity(req)
	stored.Triples = []message.Triple{evidence}
	// Simulate JSON decoding's numeric normalization.
	stored.Triples[0].Object = map[string]any{"count": float64(1)}
	stored.Triples[0].Confidence = 0.25
	stored.Triples[0].Timestamp = req.Metadata.Timestamp.Add(time.Minute)
	expiresAt := req.Metadata.Timestamp.Add(2 * time.Hour)
	stored.Triples[0].ExpiresAt = &expiresAt
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectAddTriplesBatch: {{err: errors.New("reply timeout")}},
		subjectQueryEntity:     {{data: marshalMutationTestExact(t, stored)}},
	}}
	client := newMutationTestClient(t, rpc)
	client.retry.MaxRetries = 3

	receipt, err := client.AppendEvidence(context.Background(), AppendEvidenceMutation{
		Contract: req.Contract, EntityID: req.Entity.ID,
		Evidence: []message.Triple{evidence}, Metadata: req.Metadata,
	})
	if err != nil {
		t.Fatalf("AppendEvidence: %v", err)
	}
	if receipt.Commit != CommitVerified || len(rpc.calls) != 2 {
		t.Fatalf("receipt/calls = %#v/%#v, want verified without retry", receipt, rpc.calls)
	}
}

func TestReplaceAndAppendRejectWrongWriteModesBeforeTransport(t *testing.T) {
	t.Parallel()
	req := validCreateMutation()
	rpc := &fakeMutationRequester{}
	client := newMutationTestClient(t, rpc)

	_, replaceErr := client.ReplaceOwned(context.Background(), ReplaceOwnedMutation{
		Contract: req.Contract, EntityID: req.Entity.ID,
		Desired: []message.Triple{{
			Subject: req.Entity.ID, Predicate: "shared.value.p", Object: "wrong",
		}},
		Metadata: req.Metadata,
	})
	_, appendErr := client.AppendEvidence(context.Background(), AppendEvidenceMutation{
		Contract: req.Contract, EntityID: req.Entity.ID,
		Evidence: []message.Triple{{
			Subject: req.Entity.ID, Predicate: "sensorml.process.label", Object: "wrong",
		}},
		Metadata: req.Metadata,
	})
	for name, err := range map[string]error{"replace": replaceErr, "append": appendErr} {
		var mutationErr *MutationError
		if !errors.As(err, &mutationErr) || mutationErr.Kind != MutationInvalid {
			t.Fatalf("%s error = %T %v, want MutationInvalid", name, err, err)
		}
	}
	if rpc.callCount() != 0 {
		t.Fatalf("invalid writes made %d calls", rpc.callCount())
	}
}

func TestMutationClientIsSafeForConcurrentReplacement(t *testing.T) {
	t.Parallel()
	const workers = 24
	req := validCreateMutation()
	entity := canonicalMutationTestEntity(req)
	responses := make([]fakeRPCResult, workers)
	for index := range responses {
		responses[index].data = marshalMutationTestJSON(t, graph.UpdateEntityWithTriplesResponse{
			MutationResponse: graph.MutationResponse{KVRevision: uint64(index + 1)},
			Entity:           entity,
		})
	}
	rpc := &fakeMutationRequester{responses: map[string][]fakeRPCResult{
		subjectUpdateWithTriples: responses,
	}}
	client := newMutationTestClient(t, rpc)
	desired := canonicalMutationTestEntity(req).Triples

	var wg sync.WaitGroup
	errsByWorker := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := client.ReplaceOwned(context.Background(), ReplaceOwnedMutation{
				Contract: req.Contract, EntityID: req.Entity.ID,
				Desired: desired, Metadata: req.Metadata,
			})
			if err != nil {
				errsByWorker <- err
				return
			}
			if receipt.Commit != CommitVerified {
				errsByWorker <- fmt.Errorf("commit = %q", receipt.Commit)
			}
		}()
	}
	wg.Wait()
	close(errsByWorker)
	for err := range errsByWorker {
		t.Errorf("concurrent ReplaceOwned: %v", err)
	}
	if rpc.callCount() != workers {
		t.Fatalf("calls = %d, want %d", rpc.callCount(), workers)
	}
}
