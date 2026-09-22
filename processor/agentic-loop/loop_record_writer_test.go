package agenticloop

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
