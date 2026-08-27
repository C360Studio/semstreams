package projection

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/vocabulary"
)

const projectionTestEntity = "acme.ops.test.system.widget.001"

type projectionRequest struct {
	subject string
	payload []byte
}

type projectionRequester struct {
	requests []projectionRequest
	handle   func(string, []byte) ([]byte, error)
}

func (p *projectionRequester) RequestClassified(
	_ context.Context,
	subject string,
	payload []byte,
	_ time.Duration,
) ([]byte, error) {
	p.requests = append(p.requests, projectionRequest{subject: subject, payload: append([]byte(nil), payload...)})
	return p.handle(subject, payload)
}

func TestCreateRejectsMissingRequiredProvenanceBeforeRequest(t *testing.T) {
	contract := projectionTestContract(t)
	tests := []struct {
		name     string
		metadata MutationMetadata
	}{
		{name: "request ID", metadata: MutationMetadata{Source: "test-projector"}},
		{name: "source", metadata: MutationMetadata{RequestID: "create-001"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requester := &projectionRequester{handle: func(string, []byte) ([]byte, error) {
				return projectionJSON(t, graph.CreateEntityResponse{
					Outcome: graph.MutationApplied, Entity: projectionTestState(), KVRevision: 1,
				}), nil
			}}
			client, err := newMutationClient(requester, []Contract{contract}, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := client.Create(context.Background(), CreateMutation{
				Contract: contract.Name,
				Entity:   projectionTestState(),
				Triples: []message.Triple{{
					Subject: projectionTestEntity, Predicate: "test.identity.name", Object: "widget",
				}},
				Metadata: test.metadata,
			})
			assertProjectionRejectedBeforeRequest(t, receipt, err, MutationOperationCreate, requester)
		})
	}
}

func TestAppendRejectsMissingRequiredProvenanceBeforeRequest(t *testing.T) {
	contract := projectionTestContract(t)
	tests := []struct {
		name     string
		metadata MutationMetadata
	}{
		{name: "request ID", metadata: MutationMetadata{Source: "test-projector"}},
		{name: "source", metadata: MutationMetadata{RequestID: "append-001"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requester := &projectionRequester{handle: func(string, []byte) ([]byte, error) {
				return projectionJSON(t, graph.AppendTriplesResponse{Results: []graph.AppendSubjectResult{{
					EntityID: projectionTestEntity, Outcome: graph.MutationApplied, KVRevision: 2,
				}}}), nil
			}}
			client, err := newMutationClient(requester, []Contract{contract}, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := client.Append(context.Background(), AppendMutation{
				Contract: contract.Name, Group: "events", EntityID: projectionTestEntity,
				Triples: []message.Triple{{
					Subject: projectionTestEntity, Predicate: "test.event.seen", Object: "one",
				}},
				Metadata: test.metadata,
			})
			assertProjectionRejectedBeforeRequest(t, receipt, err, MutationOperationAppend, requester)
		})
	}
}

func TestCreateRejectsConflictingTimestampBeforeRequest(t *testing.T) {
	contract := projectionTestContract(t)
	metadataTime := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	requester := &projectionRequester{handle: func(string, []byte) ([]byte, error) {
		return projectionJSON(t, graph.CreateEntityResponse{
			Outcome: graph.MutationApplied, Entity: projectionTestState(), KVRevision: 1,
		}), nil
	}}
	client, err := newMutationClient(requester, []Contract{contract}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.Create(context.Background(), CreateMutation{
		Contract: contract.Name,
		Entity:   projectionTestState(),
		Triples: []message.Triple{{
			Subject: projectionTestEntity, Predicate: "test.identity.name", Object: "widget",
			Timestamp: metadataTime.Add(time.Second),
		}},
		Metadata: MutationMetadata{
			RequestID: "create-timestamp-conflict", Source: "test-projector", Timestamp: metadataTime,
		},
	})
	assertProjectionRejectedBeforeRequest(t, receipt, err, MutationOperationCreate, requester)
}

func TestAppendRejectsConflictingTimestampBeforeRequest(t *testing.T) {
	contract := projectionTestContract(t)
	metadataTime := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	requester := &projectionRequester{handle: func(string, []byte) ([]byte, error) {
		return projectionJSON(t, graph.AppendTriplesResponse{Results: []graph.AppendSubjectResult{{
			EntityID: projectionTestEntity, Outcome: graph.MutationApplied, KVRevision: 2,
		}}}), nil
	}}
	client, err := newMutationClient(requester, []Contract{contract}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.Append(context.Background(), AppendMutation{
		Contract: contract.Name, Group: "events", EntityID: projectionTestEntity,
		Triples: []message.Triple{{
			Subject: projectionTestEntity, Predicate: "test.event.seen", Object: "one",
			Timestamp: metadataTime.Add(time.Second),
		}},
		Metadata: MutationMetadata{
			RequestID: "append-timestamp-conflict", Source: "test-projector", Timestamp: metadataTime,
		},
	})
	assertProjectionRejectedBeforeRequest(t, receipt, err, MutationOperationAppend, requester)
}

func TestAppendRejectsEmptyInputBeforeRequest(t *testing.T) {
	contract := projectionTestContract(t)
	requester := &projectionRequester{handle: func(string, []byte) ([]byte, error) {
		return nil, errors.New("request must not be sent")
	}}
	client, err := newMutationClient(requester, []Contract{contract}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.Append(context.Background(), AppendMutation{
		Contract: contract.Name, Group: "events", EntityID: projectionTestEntity,
		Metadata: MutationMetadata{RequestID: "append-empty", Source: "test-projector"},
	})
	assertProjectionRejectedBeforeRequest(t, receipt, err, MutationOperationAppend, requester)
}

func assertProjectionRejectedBeforeRequest(
	t *testing.T,
	receipt MutationReceipt,
	err error,
	operation MutationOperation,
	requester *projectionRequester,
) {
	t.Helper()
	var mutationErr *MutationError
	if !errors.As(err, &mutationErr) || mutationErr.Operation != operation ||
		mutationErr.Kind != MutationInvalid || mutationErr.Class != errs.ErrorInvalid ||
		receipt.Commit != CommitNotCommitted {
		t.Fatalf("receipt = %#v, error = %#v", receipt, mutationErr)
	}
	if len(requester.requests) != 0 {
		t.Fatalf("requests = %d, want 0", len(requester.requests))
	}
}

func TestReconcileReadsExactRevisionThenMakesOneMutation(t *testing.T) {
	contract := projectionTestContract(t)
	entity := projectionTestState()
	requester := &projectionRequester{handle: func(subject string, payload []byte) ([]byte, error) {
		switch subject {
		case "graph.ingest.query.entity":
			return projectionJSON(t, graph.ExactEntity{Entity: entity, KVRevision: 7}), nil
		case "graph.mutation.entity.reconcile":
			var request graph.ReconcilePredicatesRequest
			if err := json.Unmarshal(payload, &request); err != nil {
				t.Fatal(err)
			}
			if request.ExpectedRevision != 7 {
				t.Fatalf("expected revision = %d", request.ExpectedRevision)
			}
			return projectionJSON(t, graph.ReconcilePredicatesResponse{
				Outcome: graph.MutationApplied, Entity: entity, KVRevision: 8,
			}), nil
		default:
			t.Fatalf("unexpected subject %q", subject)
			return nil, nil
		}
	}}
	client, err := newMutationClient(requester, []Contract{contract}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.Reconcile(context.Background(), ReconcileMutation{
		Contract: contract.Name, Group: "state", EntityID: projectionTestEntity,
		Desired: []message.Triple{{Subject: projectionTestEntity, Predicate: "test.value.name", Object: "new"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Commit != CommitVerified || receipt.KVRevision != 8 || len(requester.requests) != 2 {
		t.Fatalf("receipt = %#v, requests = %d", receipt, len(requester.requests))
	}
}

func TestAppendDefiniteSubjectFailureIsNotCommitUnknown(t *testing.T) {
	contract := projectionTestContract(t)
	requester := &projectionRequester{handle: func(subject string, _ []byte) ([]byte, error) {
		if subject != "graph.mutation.triple.append" {
			t.Fatalf("subject = %q", subject)
		}
		return projectionJSON(t, graph.AppendTriplesResponse{Results: []graph.AppendSubjectResult{{
			EntityID: projectionTestEntity,
			Outcome:  graph.MutationFailed,
			Error:    &graph.MutationFailure{Class: "fatal", Code: graph.ErrorCodeGraphStateResetRequired},
		}}}), nil
	}}
	client, _ := newMutationClient(requester, []Contract{contract}, time.Second)
	receipt, err := client.Append(context.Background(), AppendMutation{
		Contract: contract.Name, Group: "events", EntityID: projectionTestEntity,
		Triples:  []message.Triple{{Subject: projectionTestEntity, Predicate: "test.event.seen", Object: "one"}},
		Metadata: MutationMetadata{RequestID: "append-definite-failure", Source: "test-projector"},
	})
	var mutationErr *MutationError
	if !errors.As(err, &mutationErr) || mutationErr.Kind != MutationInternal ||
		receipt.Commit != CommitNotCommitted || mutationErr.Commit != CommitNotCommitted {
		t.Fatalf("receipt = %#v, error = %#v", receipt, mutationErr)
	}
}

func TestAppendAmbiguousTransportFailureIsNotRetried(t *testing.T) {
	contract := projectionTestContract(t)
	want := errors.New("timeout after possible delivery")
	requester := &projectionRequester{handle: func(string, []byte) ([]byte, error) { return nil, want }}
	client, _ := newMutationClient(requester, []Contract{contract}, time.Second)
	receipt, err := client.Append(context.Background(), AppendMutation{
		Contract: contract.Name, Group: "events", EntityID: projectionTestEntity,
		Triples:  []message.Triple{{Subject: projectionTestEntity, Predicate: "test.event.seen", Object: "one"}},
		Metadata: MutationMetadata{RequestID: "append-ambiguous-failure", Source: "test-projector"},
	})
	var mutationErr *MutationError
	if !errors.As(err, &mutationErr) || mutationErr.Kind != MutationCommitUnknown ||
		receipt.Commit != CommitUnknown || len(requester.requests) != 1 {
		t.Fatalf("receipt = %#v, error = %#v, requests = %d", receipt, mutationErr, len(requester.requests))
	}
}

func TestRevisionMismatchRemainsDefinite(t *testing.T) {
	contract := projectionTestContract(t)
	entity := projectionTestState()
	requester := &projectionRequester{handle: func(subject string, _ []byte) ([]byte, error) {
		if subject == "graph.ingest.query.entity" {
			return projectionJSON(t, graph.ExactEntity{Entity: entity, KVRevision: 7}), nil
		}
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeRevisionMismatch, errors.New("raced"))
	}}
	client, _ := newMutationClient(requester, []Contract{contract}, time.Second)
	receipt, err := client.Reconcile(context.Background(), ReconcileMutation{
		Contract: contract.Name, Group: "state", EntityID: projectionTestEntity,
	})
	var mutationErr *MutationError
	if !errors.As(err, &mutationErr) || mutationErr.Kind != MutationRevisionConflict || receipt.Commit != CommitNotCommitted {
		t.Fatalf("receipt = %#v, error = %#v", receipt, mutationErr)
	}
	if len(requester.requests) != 2 {
		t.Fatalf("requests = %d, want exactly one exact read and one mutation", len(requester.requests))
	}
	if got := []string{requester.requests[0].subject, requester.requests[1].subject}; got[0] != "graph.ingest.query.entity" || got[1] != "graph.mutation.entity.reconcile" {
		t.Fatalf("request subjects = %v, want exact read then reconcile with no third request", got)
	}
}

func projectionTestContract(t *testing.T) Contract {
	t.Helper()
	t.Cleanup(vocabulary.SnapshotRegistry())
	vocabulary.Register("test.value.name")
	vocabulary.Register("test.event.seen")
	vocabulary.Register("test.identity.name")
	return Contract{
		Name: "test", MessageType: message.Type{Domain: "test", Category: "fixture", Version: "v1"}, EntityPattern: "*.*.test.system.widget.*",
		BirthPredicates: []string{"test.identity.name"},
		Groups: []PredicateGroup{
			{Name: "state", Mode: ModeReconcile, Predicates: []string{"test.value.name"}},
			{Name: "events", Mode: ModeAppend, Predicates: []string{"test.event.seen"}},
		},
	}
}

func projectionTestState() *graph.EntityState {
	return &graph.EntityState{
		ID: projectionTestEntity, MessageType: message.Type{Domain: "test", Category: "fixture", Version: "v1"}, Version: 1,
	}
}

func projectionJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestCreateFillsMessageTypeFromContract (owner ruling O-17): an entity with an
// empty MessageType is stamped from the bound contract before validation and
// before the request is built — the caller predicts nothing the contract holds.
func TestCreateFillsMessageTypeFromContract(t *testing.T) {
	contract := projectionTestContract(t)
	requester := &projectionRequester{handle: func(string, []byte) ([]byte, error) {
		return projectionJSON(t, graph.CreateEntityResponse{
			Outcome: graph.MutationApplied, Entity: projectionTestState(), KVRevision: 1,
		}), nil
	}}
	client, err := newMutationClient(requester, []Contract{contract}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.Create(context.Background(), CreateMutation{
		Contract: contract.Name,
		Entity:   &graph.EntityState{ID: projectionTestEntity, Version: 1},
		Triples: []message.Triple{{
			Subject: projectionTestEntity, Predicate: "test.identity.name", Object: "widget",
		}},
		Metadata: MutationMetadata{RequestID: "create-fill", Source: "test-projector"},
	})
	if err != nil {
		t.Fatalf("Create with an empty stamp: %v", err)
	}
	if receipt.Commit != CommitVerified {
		t.Fatalf("receipt = %#v, want verified commit", receipt)
	}
	if len(requester.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(requester.requests))
	}
	var sent graph.CreateEntityRequest
	if err := json.Unmarshal(requester.requests[0].payload, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Entity == nil || !sent.Entity.MessageType.Equal(contract.MessageType) {
		t.Fatalf("request entity message type = %#v, want %q from the contract", sent.Entity, contract.MessageType.Key())
	}

	t.Run("no contract type and no stamp is still rejected", func(t *testing.T) {
		untyped := contract
		untyped.MessageType = message.Type{}
		requester := &projectionRequester{handle: func(string, []byte) ([]byte, error) { return nil, nil }}
		client, err := newMutationClient(requester, []Contract{untyped}, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := client.Create(context.Background(), CreateMutation{
			Contract: untyped.Name,
			Entity:   &graph.EntityState{ID: projectionTestEntity, Version: 1},
			Triples: []message.Triple{{
				Subject: projectionTestEntity, Predicate: "test.identity.name", Object: "widget",
			}},
			Metadata: MutationMetadata{RequestID: "create-untyped", Source: "test-projector"},
		})
		assertProjectionRejectedBeforeRequest(t, receipt, err, MutationOperationCreate, requester)
	})
}

// TestCreateRejectsConflictingMessageType pins the conflict branch: a non-empty
// stamp that differs from the bound contract's key is a classified invalid
// error naming both keys, and no request is sent.
func TestCreateRejectsConflictingMessageType(t *testing.T) {
	contract := projectionTestContract(t)
	requester := &projectionRequester{handle: func(string, []byte) ([]byte, error) { return nil, nil }}
	client, err := newMutationClient(requester, []Contract{contract}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conflicting := message.Type{Domain: "test", Category: "other", Version: "v1"}
	receipt, err := client.Create(context.Background(), CreateMutation{
		Contract: contract.Name,
		Entity:   &graph.EntityState{ID: projectionTestEntity, MessageType: conflicting, Version: 1},
		Triples: []message.Triple{{
			Subject: projectionTestEntity, Predicate: "test.identity.name", Object: "widget",
		}},
		Metadata: MutationMetadata{RequestID: "create-conflict", Source: "test-projector"},
	})
	assertProjectionRejectedBeforeRequest(t, receipt, err, MutationOperationCreate, requester)
	for _, key := range []string{conflicting.Key(), contract.MessageType.Key()} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not name %s: %v", key, err)
		}
	}
}

// TestCreateFillsFromRegisteredContract (Codex round, MEDIUM): the contract
// set the composition root derives from the payload registry carries each
// contract's STRUCTURED type, so Create fills a zero stamp from it directly —
// no key is parsed. This is the path that used to fail once a registered key
// could not be split back into three parts.
func TestCreateFillsFromRegisteredContract(t *testing.T) {
	t.Cleanup(vocabulary.SnapshotRegistry())
	vocabulary.Register("test.identity.name")
	reg := payloadregistry.New()
	registered := message.Type{Domain: "test", Category: "fixture", Version: "v1"}
	if err := reg.Register(&payloadregistry.Registration{
		Domain: registered.Domain, Category: registered.Category, Version: registered.Version,
		Factory: func() any { return &struct{}{} },
		Contracts: []Contract{{
			Name: "test", EntityPattern: "*.*.test.system.widget.*",
			BirthPredicates: []string{"test.identity.name"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	requester := &projectionRequester{handle: func(string, []byte) ([]byte, error) {
		return projectionJSON(t, graph.CreateEntityResponse{
			Outcome: graph.MutationApplied, Entity: projectionTestState(), KVRevision: 1,
		}), nil
	}}
	client, err := newMutationClient(requester, reg.Contracts(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Create(context.Background(), CreateMutation{
		Contract: "test",
		Entity:   &graph.EntityState{ID: projectionTestEntity, Version: 1},
		Triples: []message.Triple{{
			Subject: projectionTestEntity, Predicate: "test.identity.name", Object: "widget",
		}},
		Metadata: MutationMetadata{RequestID: "create-registered", Source: "test-projector"},
	}); err != nil {
		t.Fatalf("Create with a zero stamp against a registry-bound contract: %v", err)
	}
	var sent graph.CreateEntityRequest
	if err := json.Unmarshal(requester.requests[0].payload, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Entity == nil || !sent.Entity.MessageType.Equal(registered) {
		t.Fatalf("request stamp = %#v, want the registered type %s", sent.Entity, registered.Key())
	}
}
