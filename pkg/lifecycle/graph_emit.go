package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/projection"
)

type graphEmitter interface {
	reconcile(context.Context, *graph.ReconcilePredicatesRequest) (*graph.ReconcilePredicatesResponse, error)
	create(context.Context, *graph.CreateEntityRequest) (*graph.CreateEntityResponse, error)
	delete(context.Context, *graph.DeleteEntityRequest) (*graph.DeleteEntityResponse, error)
}

type graphEmitterNATS struct {
	client *graphmutation.Client
}

func newGraphEmitterNATS(client *natsclient.Client, timeout time.Duration) *graphEmitterNATS {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	mutationClient, _ := graphmutation.NewClient(client, timeout)
	return &graphEmitterNATS{client: mutationClient}
}

func (g *graphEmitterNATS) reconcile(
	ctx context.Context,
	request *graph.ReconcilePredicatesRequest,
) (*graph.ReconcilePredicatesResponse, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("%w: graph mutation client is unavailable", ErrEmitFailed)
	}
	response, err := g.client.Reconcile(ctx, *request)
	if err != nil {
		return nil, classifyEmitError(graphmutation.ReconcilePredicates, err)
	}
	return response, nil
}

func (g *graphEmitterNATS) create(
	ctx context.Context,
	request *graph.CreateEntityRequest,
) (*graph.CreateEntityResponse, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("%w: graph mutation client is unavailable", ErrEmitFailed)
	}
	response, err := g.client.Create(ctx, *request)
	if err != nil {
		return nil, classifyEmitError(graphmutation.CreateEntity, err)
	}
	return response, nil
}

func (g *graphEmitterNATS) delete(
	ctx context.Context,
	request *graph.DeleteEntityRequest,
) (*graph.DeleteEntityResponse, error) {
	if g == nil || g.client == nil {
		return nil, fmt.Errorf("%w: graph mutation client is unavailable", ErrEmitFailed)
	}
	response, err := g.client.Delete(ctx, *request)
	if err != nil {
		return nil, classifyEmitError(graphmutation.DeleteEntity, err)
	}
	return response, nil
}

func classifyEmitError(operation graphmutation.Operation, err error) error {
	var classified *errs.ClassifiedError
	if errors.As(err, &classified) {
		switch classified.Code {
		case graph.ErrorCodeRevisionMismatch:
			return err
		case graph.ErrorCodeEntityNotFound:
			return fmt.Errorf("%w: %s", ErrEntityNotFound, err.Error())
		case graph.ErrorCodeEntityExists:
			return fmt.Errorf("%w: %s", ErrAlreadyExists, err.Error())
		default:
			return fmt.Errorf("%w: %s: %w", ErrEmitFailed, operation, err)
		}
	}
	if natsclient.IsNoResponders(err) {
		return &projection.MutationError{
			Operation: lifecycleMutationOperation(operation), Kind: projection.MutationUnavailable,
			Class: errs.ErrorTransient, Commit: projection.CommitNotCommitted, Err: err,
		}
	}
	return &projection.MutationError{
		Operation: lifecycleMutationOperation(operation), Kind: projection.MutationCommitUnknown,
		Class: errs.Classify(err), Commit: projection.CommitUnknown, Err: err,
	}
}

func lifecycleMutationOperation(operation graphmutation.Operation) projection.MutationOperation {
	switch operation {
	case graphmutation.CreateEntity:
		return projection.MutationOperationCreate
	case graphmutation.ReconcilePredicates:
		return projection.MutationOperationReconcile
	case graphmutation.DeleteEntity:
		return projection.MutationOperationDelete
	default:
		return projection.MutationOperationAppend
	}
}

func triple(subject, predicate string, object any) message.Triple {
	return message.Triple{
		Subject: subject, Predicate: predicate, Object: object,
		Timestamp: time.Now(), Confidence: 1.0,
	}
}
