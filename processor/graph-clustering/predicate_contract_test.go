package graphclustering

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

func TestEntityQuerierReturnsNoPartialResultWhenAnyEntityIsPoisoned(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.robotics.gcs.drone.001"
	poisonedID := "acme.ops.robotics.gcs.drone.002"
	validData, err := graph.MarshalEntityState(&graph.EntityState{
		ID: validID,
		Triples: []message.Triple{{
			Subject: validID, Predicate: "robotics.status.armed", Object: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	bucket := newMockKVBucket()
	bucket.data[validID] = validData
	bucket.data[poisonedID] = []byte(`{"id":"acme.ops.robotics.gcs.drone.002","triples":[{"subject":"acme.ops.robotics.gcs.drone.002","predicate":"legacy.predicate","object":"old"}]}`)
	querier := newKVEntityQuerier(bucket, slog.New(slog.NewTextHandler(io.Discard, nil)))

	entities, err := querier.GetEntities(context.Background(), []string{validID, poisonedID})
	if err == nil {
		t.Fatal("GetEntities error = nil, want graph-state poison")
	}
	if !errs.IsFatal(err) {
		t.Fatalf("GetEntities error = %T %v, want fatal", err, err)
	}
	if entities != nil {
		t.Fatalf("GetEntities returned partial result %#v before poison", entities)
	}
}

func TestCommunityQueriesStayBlockedAfterGraphStatePoison(t *testing.T) {
	t.Parallel()

	c := &Component{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	poison := &graph.StateContractError{Reason: graph.GraphStateReasonNoncanonicalPredicate}
	if !c.latchGraphStatePoison(poison) {
		t.Fatal("latchGraphStatePoison = false, want true")
	}

	// A later non-contract error cannot clear or replace the first bounded
	// reset reason.
	if c.latchGraphStatePoison(errors.New("later transient")) {
		t.Fatal("transient error classified as graph-state poison")
	}

	for name, query := range map[string]func() ([]byte, error){
		"community": func() ([]byte, error) { return c.handleQueryCommunityNATS(context.Background(), []byte(`{"id":"c1"}`)) },
		"members": func() ([]byte, error) {
			return c.handleQueryMembersNATS(context.Background(), []byte(`{"community_id":"c1"}`))
		},
		"entity": func() ([]byte, error) {
			return c.handleQueryEntityNATS(context.Background(), []byte(`{"entity_id":"acme.ops.robotics.gcs.drone.001"}`))
		},
		"level": func() ([]byte, error) { return c.handleQueryLevelNATS(context.Background(), []byte(`{"level":0}`)) },
	} {
		t.Run(name, func(t *testing.T) {
			result, err := query()
			if err == nil || !errs.IsFatal(err) {
				t.Fatalf("query error = %T %v, want fatal reset-required", err, err)
			}
			if result != nil {
				t.Fatalf("query result = %s, want nil", result)
			}
			var contractErr *graph.StateContractError
			if !errors.As(err, &contractErr) || contractErr.Reason != graph.GraphStateReasonNoncanonicalPredicate {
				t.Fatalf("query error = %v, want noncanonical predicate reason", err)
			}
		})
	}
}
