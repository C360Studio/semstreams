package graphindex

import (
	"context"
	"hash/fnv"
	"sync"
)

// keyedDispatcher preserves FIFO ordering for a single entity while retaining
// concurrency across entities. A key always hashes to the same single-consumer
// lane, so an older update cannot finish after a later delete for that key.
type keyedDispatcher[T any] struct {
	key     func(T) string
	process func(context.Context, T)
	lanes   []chan T
	wg      sync.WaitGroup
	done    chan struct{}
}

func newKeyedDispatcher[T any](workers, queueSize int, key func(T) string, process func(context.Context, T)) *keyedDispatcher[T] {
	if workers < 1 {
		workers = 1
	}
	if queueSize < workers {
		queueSize = workers
	}
	laneSize := queueSize / workers
	if laneSize < 1 {
		laneSize = 1
	}
	lanes := make([]chan T, workers)
	for i := range lanes {
		lanes[i] = make(chan T, laneSize)
	}
	return &keyedDispatcher[T]{key: key, process: process, lanes: lanes}
}

func (d *keyedDispatcher[T]) Start(ctx context.Context) {
	done := make(chan struct{})
	d.done = done
	d.wg.Add(len(d.lanes))
	for _, lane := range d.lanes {
		go func(work <-chan T) {
			defer d.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case item, ok := <-work:
					if !ok {
						return
					}
					d.process(ctx, item)
				}
			}
		}(lane)
	}
	go func() {
		d.wg.Wait()
		close(done)
	}()
}

func (d *keyedDispatcher[T]) Submit(ctx context.Context, item T) error {
	h := fnv.New32a()
	_, _ = h.Write([]byte(d.key(item)))
	lane := d.lanes[int(h.Sum32())%len(d.lanes)]
	select {
	case <-ctx.Done():
		return ctx.Err()
	case lane <- item:
		return nil
	}
}
