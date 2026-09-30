package rule

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
)

// Forces ownerLane I2/I3 at the one interleaving #1283 hit: a fence that
// arrives after the lane's exit took its queue but before its goroutine
// returned. A queued command whose cap-1 result channel is already full parks
// the exit at its result send inside that window — no sleep, no production
// hook. The fence must then settle, not append a barrier nothing drains.
func TestRuleRuntimeLaneFenceAfterLastDrainSettles(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var lane ownerLane
		lane.open()
		runCtx, cancelRun := context.WithCancel(context.Background())
		laneExited := make(chan struct{})
		go func() {
			defer close(laneExited)
			lane.run(runCtx)
		}()

		// Queue without waking the lane so only its exit can take the command.
		parked := laneCommand{run: func(context.Context) error { return nil }, result: make(chan error, 1)}
		prefill := errors.New("prefilled")
		parked.result <- prefill
		lane.mu.Lock()
		lane.queue = append(lane.queue, parked)
		lane.mu.Unlock()

		cancelRun()
		synctest.Wait() // the exit is parked sending the end error to parked.result

		barrier := lane.fence()
		if got := <-parked.result; !errors.Is(got, prefill) {
			t.Fatalf("parked result = %v, want the prefilled value", got)
		}
		synctest.Wait()

		select {
		case err := <-barrier:
			if err != nil {
				t.Fatalf("barrier = %v, want nil", err)
			}
		default:
			t.Fatal("fence racing the lane's exit was orphaned: barrier never settled")
		}
		select {
		case <-lane.done():
		default:
			t.Fatal("lane done not closed after its runtime ended")
		}
		<-laneExited
		if got := <-parked.result; !errors.Is(got, context.Canceled) {
			t.Fatalf("parked command end error = %v, want context.Canceled", got)
		}
	})
}

// A processor whose Start never armed its lane (Stop on a never-started or
// failed-before-authority processor) must not hand cleanup a barrier that
// nothing will ever settle.
func TestRuleRuntimeFenceOnUnstartedLaneSelfSettles(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		processor := &Processor{}
		barrier := processor.commandLane.fence()
		select {
		case err := <-barrier:
			if err != nil {
				t.Fatalf("barrier = %v, want nil", err)
			}
		default:
			t.Fatal("fence on a never-opened lane did not self-settle")
		}
		if err := processor.submitRuntimeCommand(func(context.Context) error { return nil }); err == nil {
			t.Fatal("runtime command admitted on a never-opened lane")
		}
	})
}
