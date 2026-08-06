package agentrun

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

type exactReaderFunc func(context.Context, string) (*graph.ExactEntity, error)

func (fn exactReaderFunc) ReadExactEntity(ctx context.Context, entityID string) (*graph.ExactEntity, error) {
	return fn(ctx, entityID)
}

func TestNATSLoopTripleReaderUsesExactAuthorityResult(t *testing.T) {
	const entityID = "acme.ops.agent.loop.run.001"
	reader := &NATSLoopTripleReader{reader: exactReaderFunc(func(_ context.Context, got string) (*graph.ExactEntity, error) {
		if got != entityID {
			t.Fatalf("entity ID = %q", got)
		}
		return &graph.ExactEntity{Entity: &graph.EntityState{ID: entityID, Triples: []message.Triple{{
			Subject: entityID, Predicate: "agent.loop.run", Object: "run-17",
		}}}, KVRevision: 9}, nil
	})}

	value, found, err := reader.GetLoopRunID(context.Background(), entityID)
	if err != nil || !found || value != "run-17" {
		t.Fatalf("GetLoopRunID() = %q, %v, %v", value, found, err)
	}
}

func TestNATSLoopTripleReaderMapsTypedEntityAbsenceOnly(t *testing.T) {
	const entityID = "acme.ops.agent.loop.run.002"
	reader := &NATSLoopTripleReader{reader: exactReaderFunc(func(context.Context, string) (*graph.ExactEntity, error) {
		return nil, errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeEntityNotFound, errors.New("missing"))
	})}

	value, found, err := reader.GetLoopRunID(context.Background(), entityID)
	if err != nil || found || value != "" {
		t.Fatalf("GetLoopRunID() = %q, %v, %v", value, found, err)
	}
}
