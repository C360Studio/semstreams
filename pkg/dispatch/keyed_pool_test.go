package dispatch

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/metric"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stopCtx returns a context with a generous timeout for graceful Stop.
func stopCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// keysOnDistinctLanes returns `count` keys that each hash to a distinct
// lane in p, so a concurrency test can guarantee genuine parallelism
// rather than relying on hash luck. White-box: uses the pool's own
// laneFor so the mapping matches routing exactly.
func keysOnDistinctLanes(p *KeyedPool[string], count int) []string {
	seen := make(map[int]string, count)
	for i := 0; len(seen) < count; i++ {
		k := fmt.Sprintf("key-%d", i)
		lane := p.laneFor(k)
		if _, ok := seen[lane]; !ok {
			seen[lane] = k
		}
	}
	out := make([]string, 0, count)
	for _, k := range seen {
		out = append(out, k)
	}
	return out
}

func validKeyedConfig() KeyedConfig[string] {
	return KeyedConfig[string]{
		Lanes:      2,
		QueueDepth: 4,
		KeyOf:      func(s string) string { return s },
		Process:    func(context.Context, int, string) error { return nil },
	}
}

func TestKeyedPool_ProcessUsesConstructorContext(t *testing.T) {
	type contextKey struct{}
	runCtx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "run-value"))
	received := make(chan context.Context, 1)
	processDone := make(chan struct{})

	p, err := NewKeyedPool(runCtx, KeyedConfig[string]{
		Lanes:      1,
		QueueDepth: 1,
		KeyOf:      func(string) string { return "key" },
		Process: func(ctx context.Context, _ int, _ string) error {
			received <- ctx
			<-ctx.Done()
			close(processDone)
			return ctx.Err()
		},
	}, KeyedDeps{})
	require.NoError(t, err)
	require.NoError(t, p.Submit("work"))

	select {
	case got := <-received:
		if got != runCtx {
			t.Fatal("Process did not receive the exact pool constructor context")
		}
		require.Equal(t, "run-value", got.Value(contextKey{}))
	case <-time.After(2 * time.Second):
		t.Fatal("Process did not receive the pool's constructor context")
	}

	cancel()
	select {
	case <-processDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Process did not observe constructor context cancellation")
	}
	require.NoError(t, p.Stop(stopCtx(t)))
}

func TestKeyedPool_StopDeadlineCanResumeConstructorOwnedDrain(t *testing.T) {
	processEntered := make(chan struct{})
	releaseProcess := make(chan struct{})
	p, err := NewKeyedPool(context.Background(), KeyedConfig[string]{
		Lanes: 1, QueueDepth: 1,
		KeyOf: func(string) string { return "key" },
		Process: func(context.Context, int, string) error {
			close(processEntered)
			<-releaseProcess
			return nil
		},
	}, KeyedDeps{})
	require.NoError(t, err)
	require.NoError(t, p.Submit("work"))
	<-processEntered

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	cancelFirst()
	require.ErrorIs(t, p.Stop(firstCtx), context.Canceled)
	close(releaseProcess)
	require.NoError(t, p.Stop(context.Background()))
}

// --- Config validation (task 1.1) ---

func TestKeyedPool_RejectsNilContext(t *testing.T) {
	_, err := NewKeyedPool[string](nil, validKeyedConfig(), KeyedDeps{})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestKeyedPool_RejectsBadConfig(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*KeyedConfig[string])
	}{
		{"zero lanes", func(c *KeyedConfig[string]) { c.Lanes = 0 }},
		{"negative lanes", func(c *KeyedConfig[string]) { c.Lanes = -1 }},
		{"zero queue depth", func(c *KeyedConfig[string]) { c.QueueDepth = 0 }},
		{"nil KeyOf", func(c *KeyedConfig[string]) { c.KeyOf = nil }},
		{"nil Process", func(c *KeyedConfig[string]) { c.Process = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validKeyedConfig()
			tc.mutate(&cfg)
			_, err := NewKeyedPool(context.Background(), cfg, KeyedDeps{})
			require.ErrorIs(t, err, ErrInvalidConfig)
		})
	}
}

// --- Ordering: same key serial in submit order (task 1.5) ---

func TestKeyedPool_SameKeyOrdered(t *testing.T) {
	var mu sync.Mutex
	var got []string
	p, err := NewKeyedPool(context.Background(), KeyedConfig[string]{
		Lanes:      8,
		QueueDepth: 16,
		KeyOf:      func(string) string { return "one-key" }, // all → one lane
		Process: func(_ context.Context, _ int, s string) error {
			mu.Lock()
			got = append(got, s)
			mu.Unlock()
			return nil
		},
	}, KeyedDeps{})
	require.NoError(t, err)

	want := []string{"a", "b", "c", "d", "e"}
	for _, s := range want {
		require.NoError(t, p.Submit(s))
	}
	require.NoError(t, p.Stop(stopCtx(t)))
	assert.Equal(t, want, got, "same-key items must process in submit order")
}

// --- Same key always lands on the same lane (task 1.5 / shard-safety) ---

func TestKeyedPool_SameKeySameLane(t *testing.T) {
	var mu sync.Mutex
	lanes := make(map[int]struct{})
	p, err := NewKeyedPool(context.Background(), KeyedConfig[string]{
		Lanes:      8,
		QueueDepth: 16,
		KeyOf:      func(string) string { return "sticky" },
		Process: func(_ context.Context, lane int, _ string) error {
			mu.Lock()
			lanes[lane] = struct{}{}
			mu.Unlock()
			return nil
		},
	}, KeyedDeps{})
	require.NoError(t, err)

	// SubmitBlocking (not Submit): all 20 items key to one lane, so under load a
	// non-blocking submit could overflow the lane's bounded queue and return
	// ErrLaneFull before the single lane goroutine drains — a timing flake. The
	// backpressure form waits for capacity; the test's concern is lane affinity,
	// not queue sizing.
	for i := 0; i < 20; i++ {
		require.NoError(t, p.SubmitBlocking(context.Background(), fmt.Sprintf("item-%d", i)))
	}
	require.NoError(t, p.Stop(stopCtx(t)))
	assert.Len(t, lanes, 1, "all same-key items must run on exactly one lane")
}

// --- Concurrency: distinct keys run in parallel across lanes (task 1.5) ---

func TestKeyedPool_DifferentKeysConcurrent(t *testing.T) {
	const n = 4
	entered := make(chan struct{}, n)
	release := make(chan struct{})
	p, err := NewKeyedPool(context.Background(), KeyedConfig[string]{
		Lanes:      n,
		QueueDepth: 1,
		KeyOf:      func(s string) string { return s },
		Process: func(_ context.Context, _ int, _ string) error {
			entered <- struct{}{}
			<-release // hold the lane until every item is confirmed in-flight
			return nil
		},
	}, KeyedDeps{})
	require.NoError(t, err)

	for _, k := range keysOnDistinctLanes(p, n) {
		require.NoError(t, p.Submit(k))
	}
	// All n must be executing simultaneously (each entered before any released).
	for i := 0; i < n; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d items ran concurrently — lanes not parallel", i, n)
		}
	}
	close(release)
	require.NoError(t, p.Stop(stopCtx(t)))
}

// --- Backpressure: full lane blocks SubmitBlocking; Submit returns ErrLaneFull (task 1.5) ---

func TestKeyedPool_Backpressure(t *testing.T) {
	entered := make(chan struct{}, 1)
	block := make(chan struct{})
	p, err := NewKeyedPool(context.Background(), KeyedConfig[string]{
		Lanes:      1,
		QueueDepth: 1,
		KeyOf:      func(string) string { return "k" },
		Process: func(_ context.Context, _ int, _ string) error {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-block
			return nil
		},
	}, KeyedDeps{})
	require.NoError(t, err)

	// First item is picked up by the lane goroutine and blocks in Process.
	require.NoError(t, p.Submit("first"))
	<-entered // now the lane is busy and its queue is empty

	// Second fills the depth-1 queue; third has nowhere to go.
	require.NoError(t, p.Submit("second"))
	require.ErrorIs(t, p.Submit("third"), ErrLaneFull)

	// SubmitBlocking on the full lane blocks until its ctx expires.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, p.SubmitBlocking(ctx, "blocked"), context.DeadlineExceeded)

	close(block)
	require.NoError(t, p.Stop(stopCtx(t)))
}

// --- Graceful drain: Stop finishes buffered work (task 1.5) ---

func TestKeyedPool_StopDrains(t *testing.T) {
	var count int64
	p, err := NewKeyedPool(context.Background(), KeyedConfig[string]{
		Lanes:      4,
		QueueDepth: 256,
		KeyOf:      func(s string) string { return s },
		Process: func(_ context.Context, _ int, _ string) error {
			atomic.AddInt64(&count, 1)
			return nil
		},
	}, KeyedDeps{})
	require.NoError(t, err)

	const total = 200
	for i := 0; i < total; i++ {
		require.NoError(t, p.Submit(fmt.Sprintf("k%d", i)))
	}
	require.NoError(t, p.Stop(stopCtx(t)))
	assert.Equal(t, int64(total), atomic.LoadInt64(&count), "Stop must drain all buffered work")
	assert.Equal(t, int64(total), p.Stats().Completed)
}

// --- Stop/Submit race: an accepted submit is never stranded (Codex P1) ---

func TestKeyedPool_StopSubmitRace(t *testing.T) {
	var processed int64
	p, err := NewKeyedPool(context.Background(), KeyedConfig[string]{
		Lanes:      4,
		QueueDepth: 8,
		KeyOf:      func(s string) string { return s },
		Process: func(_ context.Context, _ int, _ string) error {
			atomic.AddInt64(&processed, 1)
			return nil
		},
	}, KeyedDeps{})
	require.NoError(t, err)

	var acceptedNil int64
	var wg sync.WaitGroup
	stop := make(chan struct{})
	started := make(chan struct{})
	var startOnce sync.Once
	const submitters = 8
	for i := 0; i < submitters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; ; j++ {
				select {
				case <-stop:
					return
				default:
				}
				// Submit returns nil (accepted), ErrLaneFull, or ErrStopped. Only
				// a nil return asserts the item WILL be processed.
				if err := p.Submit(fmt.Sprintf("k%d-%d", id, j)); err == nil {
					atomic.AddInt64(&acceptedNil, 1)
					startOnce.Do(func() { close(started) })
				}
			}
		}(i)
	}

	<-started // ensure submits are in flight, racing with Stop
	require.NoError(t, p.Stop(stopCtx(t)))
	close(stop)
	wg.Wait()

	// Contract (Codex P1): once Stop begins draining, no Submit returns success
	// unless that item is guaranteed processed before Stop completes. So every
	// nil-returning submit must have been processed — no stranded accepted work.
	assert.Equal(t, atomic.LoadInt64(&acceptedNil), atomic.LoadInt64(&processed),
		"every accepted (nil) submit must be processed before Stop completes — no stranded work")
}

// --- Submit after Stop is rejected ---

func TestKeyedPool_SubmitAfterStop(t *testing.T) {
	p, err := NewKeyedPool(context.Background(), validKeyedConfig(), KeyedDeps{})
	require.NoError(t, err)
	require.NoError(t, p.Stop(stopCtx(t)))
	require.ErrorIs(t, p.Submit("x"), ErrStopped)
	require.ErrorIs(t, p.SubmitBlocking(context.Background(), "x"), ErrStopped)
}

// --- Panic recovery: lane survives, disposition invoked (task 1.5, H2) ---

func TestKeyedPool_PanicRecovery(t *testing.T) {
	var mu sync.Mutex
	var disposed []string
	var recoveredVal any
	var processedAfter int64

	p, err := NewKeyedPool(context.Background(), KeyedConfig[string]{
		Lanes:      1,
		QueueDepth: 16,
		KeyOf:      func(string) string { return "same" }, // one lane, ordered
		Process: func(_ context.Context, _ int, s string) error {
			if s == "boom" {
				panic("kaboom")
			}
			atomic.AddInt64(&processedAfter, 1)
			return nil
		},
		OnPanic: func(s string, r any) {
			mu.Lock()
			disposed = append(disposed, s)
			recoveredVal = r
			mu.Unlock()
		},
	}, KeyedDeps{})
	require.NoError(t, err)

	require.NoError(t, p.Submit("boom"))
	require.NoError(t, p.Submit("after1"))
	require.NoError(t, p.Submit("after2"))
	require.NoError(t, p.Stop(stopCtx(t)))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"boom"}, disposed, "OnPanic must fire for the panicking item")
	assert.Equal(t, "kaboom", recoveredVal, "recovered value passed to OnPanic")
	assert.Equal(t, int64(2), atomic.LoadInt64(&processedAfter),
		"lane must survive the panic and process later same-lane items")
	assert.Equal(t, int64(3), p.Stats().Completed, "panicked item still counts as completed")
}

// --- Metrics move (task 2.2) ---

func TestKeyedPool_MetricsMove(t *testing.T) {
	reg := metric.NewMetricsRegistry()
	p, err := NewKeyedPool(context.Background(), KeyedConfig[string]{
		Lanes:      4,
		QueueDepth: 64,
		Name:       "test_pool",
		KeyOf:      func(s string) string { return s },
		Process:    func(context.Context, int, string) error { return nil },
	}, KeyedDeps{MetricsRegistry: reg})
	require.NoError(t, err)

	const total = 50
	for i := 0; i < total; i++ {
		require.NoError(t, p.Submit(fmt.Sprintf("k%d", i)))
	}
	require.NoError(t, p.Stop(stopCtx(t)))

	st := p.Stats()
	assert.Equal(t, int64(total), st.Submitted)
	assert.Equal(t, int64(total), st.Completed, "every submitted item completes")
	assert.Equal(t, int64(0), st.Inflight, "inflight returns to zero after drain")
	assert.Equal(t, int64(0), st.Dropped)

	// Confirm the Prometheus objects actually moved.
	assert.Equal(t, float64(total), counterValue(t, reg, "dispatch_submitted_total", "test_pool"))
	assert.Equal(t, float64(total), counterValue(t, reg, "dispatch_completed_total", "test_pool"))
	assert.Equal(t, uint64(total), histogramCount(t, reg, "dispatch_queue_wait_seconds", "test_pool"))
	assert.Equal(t, uint64(total), histogramCount(t, reg, "dispatch_processing_duration_seconds", "test_pool"))
}

// findMetric gathers a single metric by name + pool label from the
// registry's Prometheus output.
func findMetric(t *testing.T, reg *metric.MetricsRegistry, name, pool string) *dto.Metric {
	t.Helper()
	families, err := reg.PrometheusRegistry().Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.Metric {
			for _, l := range m.Label {
				if l.GetName() == "pool" && l.GetValue() == pool {
					return m
				}
			}
		}
	}
	t.Fatalf("metric %q{pool=%q} not found", name, pool)
	return nil
}

func counterValue(t *testing.T, reg *metric.MetricsRegistry, name, pool string) float64 {
	t.Helper()
	return findMetric(t, reg, name, pool).GetCounter().GetValue()
}

func histogramCount(t *testing.T, reg *metric.MetricsRegistry, name, pool string) uint64 {
	t.Helper()
	return findMetric(t, reg, name, pool).GetHistogram().GetSampleCount()
}
