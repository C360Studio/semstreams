package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/pkg/projection"
)

type mutationFaultRequester struct {
	calls    int
	subject  string
	response []byte
	err      error
}

func (r *mutationFaultRequester) RequestClassified(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
	r.calls++
	r.subject = subject
	return r.response, r.err
}

func TestMutationAmbiguousReplyReturnsCommitUnknownAfterOneAttempt(t *testing.T) {
	entity := &graph.EntityState{ID: "acme.ops.test.system.widget.001"}
	operations := []struct {
		name      string
		subject   string
		operation projection.MutationOperation
		call      func(*graphEmitterNATS) error
	}{
		{
			name: "create", subject: "graph.mutation.entity.create", operation: projection.MutationOperationCreate,
			call: func(emitter *graphEmitterNATS) error {
				_, err := emitter.create(context.Background(), &graph.CreateEntityRequest{Entity: entity})
				return err
			},
		},
		{
			name: "reconcile", subject: "graph.mutation.entity.reconcile", operation: projection.MutationOperationReconcile,
			call: func(emitter *graphEmitterNATS) error {
				_, err := emitter.reconcile(context.Background(), &graph.ReconcilePredicatesRequest{
					EntityID: entity.ID, ExpectedRevision: 9, Predicates: []string{"test.state.value"},
				})
				return err
			},
		},
		{
			name: "delete", subject: "graph.mutation.entity.delete", operation: projection.MutationOperationDelete,
			call: func(emitter *graphEmitterNATS) error {
				_, err := emitter.delete(context.Background(), &graph.DeleteEntityRequest{
					EntityID: entity.ID, ExpectedRevision: 9,
				})
				return err
			},
		},
	}
	faults := []struct {
		name     string
		response []byte
		err      error
	}{
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "malformed response", response: []byte(`{"outcome":`)},
	}
	for _, operation := range operations {
		for _, fault := range faults {
			t.Run(operation.name+"/"+fault.name, func(t *testing.T) {
				requester := &mutationFaultRequester{response: fault.response, err: fault.err}
				client, newErr := graphmutation.NewClient(requester, time.Second)
				if newErr != nil {
					t.Fatalf("NewClient: %v", newErr)
				}
				emitter := &graphEmitterNATS{client: client}
				err := operation.call(emitter)
				var mutationErr *projection.MutationError
				if !errors.As(err, &mutationErr) ||
					mutationErr.Operation != operation.operation ||
					mutationErr.Kind != projection.MutationCommitUnknown ||
					mutationErr.Commit != projection.CommitUnknown {
					t.Fatalf("error = %#v, want %s commit_unknown", mutationErr, operation.name)
				}
				if requester.calls != 1 {
					t.Fatalf("calls = %d, want one", requester.calls)
				}
				if requester.subject != operation.subject {
					t.Fatalf("subject = %q, want %q", requester.subject, operation.subject)
				}
			})
		}
	}
}
