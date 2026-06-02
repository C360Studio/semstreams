package llmwrap

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/stretchr/testify/assert"
)

// TestNewNATSTriplePublisher_NilClient locks the contract that a nil
// natsclient.Client yields a nil TriplePublisher rather than a
// publisher that crashes on first Add. Components treat nil as "no
// graph emission" and log warn — degraded observability, not crash.
func TestNewNATSTriplePublisher_NilClient(t *testing.T) {
	pub := NewNATSTriplePublisher(nil)
	assert.Nil(t, pub, "nil client must yield nil publisher so callers can guard with a single nil check")
}

// recordingPublisher is the canonical test double components use to
// assert orchestration-triple emission in their handler tests. Kept
// here so the five component test suites share one shape.
type recordingPublisher struct {
	addCalls   []message.Triple
	batchCalls [][]message.Triple
	err        error
}

func (r *recordingPublisher) AddTriple(_ context.Context, triple message.Triple) error {
	r.addCalls = append(r.addCalls, triple)
	return r.err
}

func (r *recordingPublisher) AddTriplesBatch(_ context.Context, triples []message.Triple) error {
	r.batchCalls = append(r.batchCalls, triples)
	return r.err
}

// TestTriplePublisher_InterfaceShape pins the published interface so
// future renames or method-set changes trip the test before they reach
// component callers. Compile-time assertion via assignment — recordingPublisher
// must satisfy TriplePublisher for handler tests to use it.
func TestTriplePublisher_InterfaceShape(t *testing.T) {
	var pub TriplePublisher = &recordingPublisher{}
	assert.NotNil(t, pub)
	err := pub.AddTriple(context.Background(), message.Triple{Subject: "x", Predicate: "y", Object: "z", Timestamp: time.Now()})
	assert.NoError(t, err)
	err = pub.AddTriplesBatch(context.Background(), []message.Triple{{Subject: "x", Predicate: "y", Object: "z", Timestamp: time.Now()}})
	assert.NoError(t, err)
}
