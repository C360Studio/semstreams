package embedding

import (
	"log/slog"
	"testing"
)

// TestWithWorkers_FloorsAtOne guards gh#620 at the library seam.
//
// graph-embedding derived its worker count as batch_size/10, so any batch_size
// under 10 produced ZERO workers by integer division. Start spawns exactly
// w.workers goroutines to drain the EMBEDDING_INDEX watcher, so zero means the
// component silently processes nothing while every health signal stays green.
// The floor lives here rather than at each call site because no caller ever
// wants a worker pool that consumes nothing.
func TestWithWorkers_FloorsAtOne(t *testing.T) {
	t.Parallel()

	for _, n := range []int{-3, 0, 1} {
		w := NewWorker(nil, nil, nil, slog.Default()).WithWorkers(n)
		if w.workers < 1 {
			t.Errorf("WithWorkers(%d) left %d workers; the watcher would never be drained", n, w.workers)
		}
	}

	if w := NewWorker(nil, nil, nil, slog.Default()).WithWorkers(7); w.workers != 7 {
		t.Errorf("WithWorkers(7) = %d, want 7 (the floor must not clamp real values)", w.workers)
	}
}
