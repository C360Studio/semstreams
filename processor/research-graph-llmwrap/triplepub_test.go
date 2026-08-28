package llmwrap

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/internal/semantictest"
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
	batchCalls  [][]message.Triple
	createCalls []recordedCreate
	err         error
}

// recordedCreate captures one Create call so birth tests can
// assert the entity ID, typed-origin envelope, and carried triples.
type recordedCreate struct {
	entityID string
	msgType  message.Type
	triples  []message.Triple
}

func (r *recordingPublisher) Create(_ context.Context, entityID string, msgType message.Type, triples []message.Triple) error {
	r.createCalls = append(r.createCalls, recordedCreate{entityID: entityID, msgType: msgType, triples: triples})
	return r.err
}

func (r *recordingPublisher) Append(_ context.Context, triples []message.Triple) error {
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
	entityID := semantictest.EntityID(t, "c360", "ops", "agentic-loop", "agent", "execution", "rg-x")
	err := pub.Create(context.Background(), entityID, message.Type{Domain: "agentic", Category: "loop_execution", Version: "v1"}, []message.Triple{{Subject: entityID, Predicate: semantictest.Predicate(t, "test", "publisher", "shape"), Object: "z", Timestamp: time.Now()}})
	assert.NoError(t, err)
	err = pub.Append(context.Background(), []message.Triple{{Subject: entityID, Predicate: semantictest.Predicate(t, "test", "publisher", "append"), Object: "z", Timestamp: time.Now()}})
	assert.NoError(t, err)
}

// countingLogger captures Warn calls so the birth-helper degraded/failure
// contracts can be asserted without a real slog handler.
type countingLogger struct{ warns int }

func (l *countingLogger) Warn(_ string, _ ...any) { l.warns++ }

// TestBirthLoopEntityWithTriples_NilPublisher_Degraded pins the
// observability-disabled contract: a nil publisher logs warn and returns nil
// (the kickoff is best-effort, non-fatal) rather than panicking.
func TestBirthLoopEntityWithTriples_NilPublisher_Degraded(t *testing.T) {
	lg := &countingLogger{}
	err := BirthLoopEntityWithTriples(context.Background(), nil, lg, "research_graph_tool", "rg_x", "c360.ops.agentic-loop.agent.execution.rg_x", message.Type{}, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, lg.warns, "nil publisher must log a single degraded warn")
}

// TestBirthLoopEntityWithTriples_Success births via Create rather than Append,
// carrying the typed-origin envelope and kickoff triples —
// the gh#390 fix: the FIRST write must CREATE the entity, not append to it.
func TestBirthLoopEntityWithTriples_Success(t *testing.T) {
	pub := &recordingPublisher{}
	entityID := semantictest.EntityID(t, "c360", "ops", "agentic-loop", "agent", "execution", "rg-x")
	want := message.Type{Domain: "agentic", Category: "loop_execution", Version: "v1"}
	triples := []message.Triple{{Subject: entityID, Predicate: semantictest.Predicate(t, "research", "graph", "topic"), Object: "drones", Timestamp: time.Now()}}

	err := BirthLoopEntityWithTriples(context.Background(), pub, &countingLogger{}, "research_graph_tool", "rg_x", entityID, want, triples)
	assert.NoError(t, err)

	// Must create (birth), never append, on the first write.
	assert.Len(t, pub.createCalls, 1, "kickoff must CREATE the entity, not append")
	assert.Empty(t, pub.batchCalls, "kickoff must not use the append path")
	assert.Equal(t, entityID, pub.createCalls[0].entityID)
	assert.Equal(t, want, pub.createCalls[0].msgType, "must carry the typed-origin envelope")
	assert.Equal(t, triples, pub.createCalls[0].triples)
}

// TestBirthLoopEntityWithTriples_Failure_Propagates pins that a publish failure
// is logged warn and returned (for handler-level metrics) without panicking.
func TestBirthLoopEntityWithTriples_Failure_Propagates(t *testing.T) {
	pub := &recordingPublisher{err: assert.AnError}
	lg := &countingLogger{}
	err := BirthLoopEntityWithTriples(context.Background(), pub, lg, "research_graph_tool", "rg_x", "c360.ops.agentic-loop.agent.execution.rg_x", message.Type{}, nil)
	assert.Error(t, err)
	assert.Equal(t, 1, lg.warns, "a birth failure must log a single warn")
}
