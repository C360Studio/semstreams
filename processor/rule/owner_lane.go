package rule

import (
	"context"
	"errors"
	"sync"
)

// ownerLane serializes an owner's contextless mutations onto the goroutine that
// holds the owner's runtime context. The rule processor's runtime commands and
// the cron scheduler's dispatches are its two owners.
//
// Invariants (design rule-bounded-stop-fence § 4; forced by owner_lane_test.go):
//   - I1: every command or barrier appended to the lane receives exactly one
//     result — its run's, or the lane's end error.
//   - I2: an appender that observes done closed under the lane mutex never
//     appends. The lane takes its queue and closes done inside one mutex
//     section, so no append can land after the last drain.
//   - I3: a fence barrier settles after every command admitted before it, or at
//     the lane's end.
//
// A lane is armed at most once per owner lifetime by open; a lane that was
// never opened refuses submits and self-settles fences.
type ownerLane struct {
	mu     sync.Mutex
	fenced bool
	queue  []laneCommand
	wake   chan struct{}
	doneCh chan struct{}
}

type laneCommand struct {
	run    func(context.Context) error
	result chan error
}

var (
	// errLaneAdmissionClosed: the lane was never opened or has been fenced.
	errLaneAdmissionClosed = errors.New("lane admission is closed")
	// errLaneEnded: the lane's runtime has ended.
	errLaneEnded = errors.New("lane runtime has ended")
)

// open arms the lane for the owner's runtime; the owner then runs run on
// exactly one goroutine it joins.
func (l *ownerLane) open() {
	l.mu.Lock()
	l.wake = make(chan struct{}, 1)
	l.doneCh = make(chan struct{})
	l.fenced = false
	l.mu.Unlock()
}

// done closes once the lane can no longer accept an append. Nil before open.
// The only work the lane goroutine does after done closes is failing commands
// it already took, each a send on a cap-1 channel only this lane writes.
func (l *ownerLane) done() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.doneCh
}

// run executes admitted commands with ctx until ctx ends, then fails every
// command still queued with ctx's error.
func (l *ownerLane) run(ctx context.Context) {
	l.mu.Lock()
	wake, done := l.wake, l.doneCh
	l.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			l.end(done, ctx.Err())
			return
		case <-wake:
			for {
				l.mu.Lock()
				if len(l.queue) == 0 {
					l.mu.Unlock()
					break
				}
				command := l.queue[0]
				l.queue = l.queue[1:]
				l.mu.Unlock()
				command.result <- command.run(ctx)
				close(command.result)
			}
		}
	}
}

// end takes the queue and closes done in one mutex section (I2), then fails
// what it took. Closing done after Unlock would let a fence append a barrier
// between the take and the close that nothing ever drains (#1283).
func (l *ownerLane) end(done chan struct{}, err error) {
	l.mu.Lock()
	queue := l.queue
	l.queue = nil
	close(done)
	l.mu.Unlock()
	for _, command := range queue {
		command.result <- err
		close(command.result)
	}
}

// submit blocks while the owner's runtime executes run; it returns run's
// result, a refusal once the lane is fenced, or the lane's end error. It carries
// no caller deadline — the owner's bounded Stop cancels the runtime and releases
// it.
func (l *ownerLane) submit(run func(context.Context) error) error {
	command := laneCommand{run: run, result: make(chan error, 1)}
	l.mu.Lock()
	if l.fenced || l.wake == nil {
		l.mu.Unlock()
		return errLaneAdmissionClosed
	}
	select {
	case <-l.doneCh:
		l.mu.Unlock()
		return errLaneEnded
	default:
	}
	l.queue = append(l.queue, command)
	wake := l.wake
	l.mu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
	return <-command.result
}

// fence closes admission and returns a barrier that settles after every
// command admitted before it (I3), or at once when the lane never opened or
// has already ended.
func (l *ownerLane) fence() <-chan error {
	barrier := laneCommand{run: func(context.Context) error { return nil }, result: make(chan error, 1)}
	l.mu.Lock()
	l.fenced = true
	if l.doneCh == nil {
		l.mu.Unlock()
		return settledBarrier(barrier)
	}
	select {
	case <-l.doneCh:
		l.mu.Unlock()
		return settledBarrier(barrier)
	default:
	}
	l.queue = append(l.queue, barrier)
	wake := l.wake
	l.mu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
	return barrier.result
}

func settledBarrier(barrier laneCommand) <-chan error {
	barrier.result <- nil
	close(barrier.result)
	return barrier.result
}
