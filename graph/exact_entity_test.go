package graph

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/pkg/errs"
)

type exactEntityRequesterFunc func(context.Context, string, []byte, time.Duration) ([]byte, error)

func (fn exactEntityRequesterFunc) RequestClassified(
	ctx context.Context,
	subject string,
	data []byte,
	timeout time.Duration,
) ([]byte, error) {
	return fn(ctx, subject, data, timeout)
}

func TestExactEntityReaderClassifiesInvalidIDBeforeTransport(t *testing.T) {
	called := false
	requester := exactEntityRequesterFunc(func(context.Context, string, []byte, time.Duration) ([]byte, error) {
		called = true
		return nil, nil
	})
	_, err := NewExactEntityReader(requester, time.Second).ReadExactEntity(context.Background(), "not-six-parts")
	if called {
		t.Fatal("invalid entity ID reached transport")
	}
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Class != errs.ErrorInvalid || classified.Code != ErrorCodeInvalidRequest {
		t.Fatalf("error = %v, want invalid/invalid_request", err)
	}
}

func TestExactEntityReaderReturnsValidatedEntityAndRevision(t *testing.T) {
	const entityID = "acme.ops.robotics.gcs.drone.001"
	requester := exactEntityRequesterFunc(func(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
		if subject != "graph.ingest.query.entity" {
			t.Fatalf("subject = %q", subject)
		}
		var request struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(data, &request); err != nil || request.ID != entityID {
			t.Fatalf("request = %s, err = %v", data, err)
		}
		return []byte(`{"entity":{"id":"` + entityID + `","version":999,"triples":[]},"kvRevision":17}`), nil
	})

	reader := NewExactEntityReader(requester, time.Second)
	exact, err := reader.ReadExactEntity(context.Background(), entityID)
	if err != nil {
		t.Fatalf("ReadExactEntity: %v", err)
	}
	if exact.Entity == nil || exact.Entity.ID != entityID || exact.KVRevision != 17 {
		t.Fatalf("exact = %#v", exact)
	}
	if exact.KVRevision == uint64(exact.Entity.Version) {
		t.Fatal("logical version used as KV revision")
	}
}

func TestExactEntityReaderRejectsZeroRevisionAndMismatchedEntity(t *testing.T) {
	const entityID = "acme.ops.robotics.gcs.drone.001"
	tests := []string{
		`{"entity":{"id":"` + entityID + `","triples":[]},"kvRevision":0}`,
		`{"entity":{"id":"acme.ops.robotics.gcs.drone.002","triples":[]},"kvRevision":2}`,
	}
	for _, response := range tests {
		requester := exactEntityRequesterFunc(func(context.Context, string, []byte, time.Duration) ([]byte, error) {
			return []byte(response), nil
		})
		if exact, err := NewExactEntityReader(requester, time.Second).ReadExactEntity(context.Background(), entityID); err == nil {
			t.Fatalf("response %s returned %#v", response, exact)
		}
	}
}
