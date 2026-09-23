package agenticloop

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/agentic-loop/internal/looprequest"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// barrierLoopBucket holds the FIRST compare-and-swap inside the bucket until
// the test lets it go, and announces every entry. It is how two writers of one
// loop are interleaved deterministically, with no sleep deciding the order.
type barrierLoopBucket struct {
	*recordingLoopBucket
	entered chan string
	release chan struct{}
	held    atomic.Bool
}

func (b *barrierLoopBucket) Update(
	ctx context.Context, key string, value []byte, revision uint64,
) (uint64, error) {
	b.entered <- key
	if b.held.CompareAndSwap(false, true) {
		<-b.release
	}
	return b.recordingLoopBucket.Update(ctx, key, value, revision)
}

var _ jetstream.KeyValue = (*barrierLoopBucket)(nil)

// TestTwoLanesWritingOneLoopDoNotRefuseEachOther pins the defect CI run
// 35719155514 found: a cancel signal and the tool lane's advance wrote the
// same loop in the same process, the cancel read revision N while the advance
// committed N+1, and the cancel's compare-and-swap was refused. The refusal is
// indistinguishable from a second process owning the loop, so the cancel path
// released the loop and reported unknown durability — the loop never reached
// `cancelled`, and the lane lost delivery ownership.
//
// The compare-and-swap answers "did another PROCESS write this record". It can
// only mean that if this process's own writers never race each other, which is
// what the assertion below is: while one writer is inside its swap, no other
// writer of this process may reach the bucket at all.
//
// The one-second window is a NEGATIVE assertion with no sequencing role: an
// unblocked second writer needs a lock acquisition, a map read and a marshal —
// microseconds — to reach the bucket, so a second of silence is five orders of
// magnitude of margin. Removing the serialization makes this test fail at the
// first scheduling of the second goroutine, not after the timeout.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestTwoLanesWritingOneLoopDoNotRefuseEachOther(t *testing.T) {
	c, base, loopID := carrierLoop(t)
	bucket := &barrierLoopBucket{
		recordingLoopBucket: base,
		entered:             make(chan string, 4),
		release:             make(chan struct{}),
	}
	c.loopsBucket = bucket
	var once sync.Once
	releaseFirst := func() { once.Do(func() { close(bucket.release) }) }
	t.Cleanup(releaseFirst)

	firstWriter := make(chan error, 1)
	go func() { firstWriter <- c.persistLoopState(context.Background(), loopID) }()
	require.Equal(t, loopID, <-bucket.entered,
		"the first writer must be inside its own compare-and-swap before the second starts")

	secondWriter := make(chan error, 1)
	go func() { secondWriter <- c.persistLoopState(context.Background(), loopID) }()

	select {
	case <-bucket.entered:
		t.Fatal("a second writer of this process reached the bucket while the first was still " +
			"inside its compare-and-swap: the revision read and the write are not one critical " +
			"section, so one lane's write is refused by its own process (CI run 35719155514)")
	case <-time.After(time.Second):
	}

	releaseFirst()
	require.NoError(t, <-firstWriter)
	require.NoError(t, <-secondWriter,
		"the second writer's compare-and-swap was refused by a revision its own process had moved")

	require.Equal(t, []string{loopID, loopID}, bucket.written(), "both writers must have committed")
	_, held := c.observedLoopRevision(loopID)
	require.True(t, held, "a writer that committed left no revision for the next one")
	_, err := c.handler.GetLoop(loopID)
	require.NoError(t, err,
		"a loop nobody else wrote must not be released — that is the ownership loss the CI run reported")
}

// TestAColdAdoptAndAWarmWriteOfOneLoopDoNotRefuseEachOther extends the rule
// above to the writer it did not cover.
//
// Step 0's adopting compare-and-swap was the one loop-record write outside
// loopRecordMu, and its read lived in the caller — two unlocked steps around a
// CAS. It is reachable on a loop this very process holds warm: the tool lane
// falls into the cold arm whenever the execution's routing entry has already
// been drained (GetAndClearToolResults takes toolCallToLoop with it), so a
// redelivered result can run step 0 while the carrier is mid-write. Reading
// the record before the carrier commits and swapping after it did is a refusal
// with no foreign writer behind it.
//
// One rule, stated once: every loop-record write happens under loopRecordMu
// with the read it compare-and-swaps against in the same critical section.
//
// The one-second window is a NEGATIVE assertion with no sequencing role, as in
// the test above; the started channel is what keeps it from passing vacuously
// on a goroutine that never ran.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestAColdAdoptAndAWarmWriteOfOneLoopDoNotRefuseEachOther(t *testing.T) {
	c, base, loopID := carrierLoop(t)
	first := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
	retained := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()
	// Step 0 adopts only into a record that already NAMES a request — an
	// unnamed one is the classification's requestOrderUnnamed arm and is
	// returned untouched. This test is about the lock around the adopting
	// write, so the fixture has to be a record that has one. The set-and-
	// persist below is fixture setup, like seedLoopRecord's birth write, and
	// written() is reset after it for the same reason.
	require.NoError(t, c.handler.loopManager.SetPublishedRequest(loopID, first))
	require.NoError(t, c.persistLoopState(t.Context(), loopID))
	base.resetWritten()
	c.requestEvidence = stubEvidenceReader{requestID: retained}
	bucket := &barrierLoopBucket{
		recordingLoopBucket: base,
		entered:             make(chan string, 4),
		release:             make(chan struct{}),
	}
	c.loopsBucket = bucket
	var once sync.Once
	releaseWarm := func() { once.Do(func() { close(bucket.release) }) }
	t.Cleanup(releaseWarm)

	warm := make(chan error, 1)
	go func() { warm <- c.persistLoopState(context.Background(), loopID) }()
	require.Equal(t, loopID, <-bucket.entered,
		"the warm writer must be inside its own compare-and-swap before the cold adopt starts")

	started := make(chan struct{})
	cold := make(chan error, 1)
	go func() {
		close(started)
		_, err := c.adoptNewerRetainedRequest(context.Background(), loopID)
		cold <- err
	}()
	<-started

	select {
	case <-bucket.entered:
		t.Fatal("the cold adopt reached the bucket while the warm writer was still inside its " +
			"compare-and-swap: step 0's read and write are not one critical section, so one of " +
			"the two writes is refused with no foreign process behind it")
	case <-time.After(time.Second):
	}

	releaseWarm()
	require.NoError(t, <-warm, "the warm write was refused by a revision its own process had moved")
	require.NoError(t, <-cold, "the cold adopt was refused by a revision its own process had moved")

	require.Equal(t, []string{loopID, loopID}, bucket.written(), "both writers must have committed")
	require.Equal(t, retained, decodeRecord(t, c, loopID).PublishedRequestID,
		"the adopt ran last, so the record must name what the stream retains")
}

// TestRenderingTheRecordDoesNotRaceAStoredToolResult pins the one thing the
// record lock cannot do.
//
// loopRecordMu serializes the record WRITERS of this process. It does not
// freeze the loop's in-memory state: marshalLoopRecord takes its snapshot
// through LoopManager.GetLoop, under the manager's own mutex, and marshals it
// after that mutex is released. GetLoop returned a shallow struct copy, so the
// snapshot shared PendingToolResults with the live entity — and StoreToolResult
// on the handler goroutine writes that same map. Marshalling a map another
// goroutine is writing is a data race, not a stale read.
//
// The barrier makes both goroutines start together and the repetition is what
// makes the detector's shadow history certain to hold both accesses; no sleep
// decides anything.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestRenderingTheRecordDoesNotRaceAStoredToolResult(t *testing.T) {
	c, _, loopID := carrierLoop(t)
	require.NoError(t, c.handler.loopManager.StoreToolResult(loopID,
		agentic.ToolResult{ExecutionID: "exec-seed", Name: "seed", Content: "seeded"}))

	for round := range 100 {
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err := c.marshalLoopRecord(loopID)
			errs <- err
		}()
		go func() {
			defer wg.Done()
			<-start
			errs <- c.handler.loopManager.StoreToolResult(loopID, agentic.ToolResult{
				ExecutionID: fmt.Sprintf("exec-%d", round), Name: "tool", Content: "answered",
			})
		}()
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
	}
}

// gatedEvidenceReader stands in for the request stream at the one moment the
// carrier consults it: the identity check publishResults runs before it sends a
// minted request. Held open, it IS the interval between the mint and its
// PubAck — the stream still retains only the previous request. The test closes
// the interval by making the minted request retained, which is what a PubAck
// does.
type gatedEvidenceReader struct {
	entered  chan struct{}
	release  chan struct{}
	retained atomic.Value
}

func (g *gatedEvidenceReader) ReadRetainedRequest(
	_ context.Context, _, _ string,
) ([]byte, bool, error) {
	g.entered <- struct{}{}
	<-g.release
	request := agentic.AgentRequest{
		RequestID: g.retained.Load().(string),
		LoopID:    "unused",
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "retained"}},
	}
	data, err := json.Marshal(message.NewBaseMessage(request.Schema(), &request, "test"))
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (g *gatedEvidenceReader) ReadRetainedResponse(
	_ context.Context, _, _ string,
) ([]byte, bool, error) {
	return nil, false, nil
}

var _ loopEvidenceReader = (*gatedEvidenceReader)(nil)

// TestARecordNeverNamesARequestBeforeItsPubAck is invariant I1 held against the
// lane that did not mint.
//
// PublishedRequestID used to be stamped into the shared entity at the MINT, so
// from the moment the carrier built R2 every writer of this loop could see it:
// a deferred continuation's record write on the task lane, or a tool lane's
// compare-and-swap, rendered that entity and committed a record naming R2 while
// the stream still retained only R1. MaxAckPending=1 is per consumer and
// loopRecordMu serializes the writers, not the handler mutations that precede
// them, so nothing ordered the two. A crash in that window leaves a record
// naming a request no reader can find: every later cold read takes the fatal
// older-retained-request branch, and a cold parked loop is not settleable
// (owner Codex round finding 3, #1330 Q1, 2026-09-23).
//
// The assertion is therefore about the SIBLING's write, not the carrier's. The
// carrier is held inside its own publication and the other lane commits while
// it waits; what the record may name at that instant is the whole question.
//
// spec: agentic-loop / The loop record names its outstanding request
func TestARecordNeverNamesARequestBeforeItsPubAck(t *testing.T) {
	c, bucket, loopID := carrierLoop(t)
	outstanding := looprequest.ID{LoopID: loopID, Iteration: 1, Retry: 0}.String()
	minted := looprequest.ID{LoopID: loopID, Iteration: 2, Retry: 0}.String()

	// Fixture, as the task lane's birth leaves it: the loop names R1 and the
	// record says so. written() is reset after it for the same reason
	// seedLoopRecord's is — only the two lanes' writes are under test.
	require.NoError(t, c.handler.loopManager.SetPublishedRequest(loopID, outstanding))
	require.NoError(t, c.persistLoopState(t.Context(), loopID))
	bucket.resetWritten()

	gate := &gatedEvidenceReader{entered: make(chan struct{}, 1), release: make(chan struct{})}
	gate.retained.Store(outstanding)
	c.requestEvidence = gate
	// publishResults consults the stream only when it has a client to publish
	// through; this one never connects, and never has to — the request is
	// retained by the time the gate opens, so the identity check adopts it.
	c.natsClient = unpublishableClient(t)
	var once sync.Once
	releaseGate := func() { once.Do(func() { close(gate.release) }) }
	t.Cleanup(releaseGate)

	carrier := make(chan error, 1)
	go func() {
		carrier <- c.publishThenPersistResultState(context.Background(), HandlerResult{
			LoopID: loopID,
			State:  agentic.LoopStateExecuting,
			PublishedMessages: []PublishedMessage{{
				Subject: "agent.request." + loopID,
				Data:    []byte(`{"request":true}`),
				MsgID:   minted,
			}},
		})
	}()
	<-gate.entered

	// The sibling lane: a deferred continuation admitted on the task lane
	// persists the shared entity while the carrier's request is in flight.
	require.NoError(t, c.persistLoopState(t.Context(), loopID),
		"the sibling lane's compare-and-swap was refused by its own process")
	require.Equal(t, outstanding, decodeRecord(t, c, loopID).PublishedRequestID,
		"a sibling lane committed a record naming a request whose PubAck has not landed: KV now "+
			"names a request the stream does not retain, which is the state I1 declares impossible "+
			"and which every later cold read answers with Quarantine")

	gate.retained.Store(minted)
	releaseGate()
	require.NoError(t, <-carrier)

	require.Equal(t, minted, decodeRecord(t, c, loopID).PublishedRequestID,
		"once the request is retained the record must name it — the stamp moved, it did not vanish")
	require.Equal(t, []string{loopID, loopID}, bucket.written(),
		"both lanes must have committed: closing the window must not cost the sibling its write")
}
