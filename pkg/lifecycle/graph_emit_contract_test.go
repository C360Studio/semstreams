package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/projection"
)

func TestValidateMutationResponseEntityRejectsPoison(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidEntityID := "bad"
	entity := &graph.EntityState{ID: validID, Triples: []message.Triple{{
		Subject: validID, Predicate: "test.state.target", Object: invalidEntityID, Datatype: message.EntityReferenceDatatype,
	}}}

	err := validateMutationResponseEntity(entity)
	if err == nil || !graph.IsStateContractError(err) {
		t.Fatalf("error = %T %v, want graph state reset contract", err, err)
	}
	if err := validateMutationResponseEntity(nil); err != nil {
		t.Fatalf("nil degraded response entity error = %v", err)
	}
}

type deleteFaultRequester struct {
	calls    int
	response []byte
	err      error
}

func (r *deleteFaultRequester) RequestClassified(context.Context, string, []byte, time.Duration) ([]byte, error) {
	r.calls++
	return r.response, r.err
}

func (*deleteFaultRequester) RequestWithRetryClassified(
	context.Context,
	string,
	[]byte,
	time.Duration,
	natsclient.RetryConfig,
) ([]byte, error) {
	panic("conditional delete must not use retry transport")
}

func TestDeleteAmbiguousReplyReturnsCommitUnknownWithoutRetry(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response []byte
		err      error
	}{
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "malformed response", response: []byte(`{"outcome":`)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requester := &deleteFaultRequester{response: tt.response, err: tt.err}
			emitter := &graphEmitterNATS{client: requester, timeout: time.Second}
			_, err := emitter.delete(context.Background(), &graph.DeleteEntityRequest{
				EntityID: "acme.ops.test.system.widget.001", ExpectedRevision: 9,
			})
			var mutationErr *projection.MutationError
			if !errors.As(err, &mutationErr) ||
				mutationErr.Operation != projection.MutationOperationDelete ||
				mutationErr.Kind != projection.MutationCommitUnknown ||
				mutationErr.Commit != projection.CommitUnknown {
				t.Fatalf("error = %#v, want delete commit_unknown", mutationErr)
			}
			if requester.calls != 1 {
				t.Fatalf("calls = %d, want one", requester.calls)
			}
		})
	}
}

// entity-id-audit:classify intentional-malformed "bad" line=14 column=21 surface=go-assignment:invalidEntityID entity_id_invalid:arity lifecycle mutation reply reference poison fixture
