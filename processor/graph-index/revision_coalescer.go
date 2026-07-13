package graphindex

import (
	"context"
	"sync"
	"time"
)

type coalescedEntity struct {
	entityID string
	revision uint64
}

// revisionCoalescer retains the greatest observed revision for every pending
// key. Keeping the revision with the key lets the ordered reconciliation work
// complete exactly the events it represents, never a newer event that arrived
// after the batch was detached.
type revisionCoalescer struct {
	mu       sync.Mutex
	pending  map[string]uint64
	callback func([]coalescedEntity)
	ticker   *time.Ticker
	done     chan struct{}
	close    chan struct{}
	once     sync.Once
}

func newRevisionCoalescer(ctx context.Context, window time.Duration, callback func([]coalescedEntity)) *revisionCoalescer {
	c := &revisionCoalescer{
		pending:  make(map[string]uint64),
		callback: callback,
		ticker:   time.NewTicker(window),
		done:     make(chan struct{}),
		close:    make(chan struct{}),
	}
	go c.run(ctx)
	return c
}

func (c *revisionCoalescer) Add(key string, revision uint64) {
	c.mu.Lock()
	if revision > c.pending[key] {
		c.pending[key] = revision
	}
	c.mu.Unlock()
}

func (c *revisionCoalescer) Remove(key string) {
	c.mu.Lock()
	delete(c.pending, key)
	c.mu.Unlock()
}

func (c *revisionCoalescer) Close() {
	c.once.Do(func() { close(c.close) })
	<-c.done
}

func (c *revisionCoalescer) run(ctx context.Context) {
	defer close(c.done)
	defer c.ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.close:
			return
		case <-c.ticker.C:
			c.fire()
		}
	}
}

func (c *revisionCoalescer) fire() {
	c.mu.Lock()
	if len(c.pending) == 0 {
		c.mu.Unlock()
		return
	}
	pending := c.pending
	c.pending = make(map[string]uint64)
	c.mu.Unlock()

	batch := make([]coalescedEntity, 0, len(pending))
	for key, revision := range pending {
		batch = append(batch, coalescedEntity{entityID: key, revision: revision})
	}
	if c.callback != nil {
		c.callback(batch)
	}
}
