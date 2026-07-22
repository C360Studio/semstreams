package embedding

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/storage"
)

// TestClassifyEmbedErr pins the bounded reason enum: each recognizable embedder error
// shape maps to its bucket, and an unrecognized shape defaults to embedder_error. The
// raw message is never returned — only a value from the closed enum (#613).
func TestClassifyEmbedErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"connection refused", errors.New("dial tcp 127.0.0.1:8080: connect: connection refused"), failReasonConnectionRefused},
		{"context deadline sentinel", fmt.Errorf("embedding API call failed: %w", context.DeadlineExceeded), failReasonTimeout},
		{"timeout substring", errors.New("Client.Timeout exceeded while awaiting headers"), failReasonTimeout},
		{"dimension mismatch", errors.New("embedding dimension 512 does not match index 384"), failReasonDimensionMismatch},
		{"unrecognized defaults to embedder_error", errors.New("HTTP 500 upstream error"), failReasonEmbedderError},
		{"nil defaults to embedder_error", nil, failReasonEmbedderError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyEmbedErr(tc.err); got != tc.want {
				t.Errorf("classifyEmbedErr(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestMarkFailed_PersistsBoundedReasonAndCounts proves the failure path stores the
// bounded reason on the durable record, increments the reason-labelled counter, and
// returns OutcomeFailed — while a superseded failure returns OutcomeSkipped and does
// NOT count (so failures_total{reason} stays a strict partition of errors_total).
func TestMarkFailed_PersistsBoundedReasonAndCounts(t *testing.T) {
	ctx := context.Background()
	const entityID = "acme.ops.a.b.c.1"

	t.Run("real failure persists reason, counts, returns OutcomeFailed", func(t *testing.T) {
		s := NewStorage(newMemKV(), newMemKV())
		if err := s.SavePending(ctx, entityID, "", "text", 1); err != nil {
			t.Fatalf("SavePending: %v", err)
		}
		m := &countingMetrics{}
		w := &Worker{storage: s, metrics: m, ctx: ctx, logger: discardLogger()}

		if got := w.markFailed(entityID, "generation failed: connection refused", failReasonConnectionRefused, 1); got != OutcomeFailed {
			t.Fatalf("markFailed outcome = %v, want OutcomeFailed", got)
		}
		rec, err := s.GetEmbedding(ctx, entityID)
		if err != nil || rec == nil {
			t.Fatalf("GetEmbedding: rec=%v err=%v", rec, err)
		}
		if rec.Status != StatusFailed || rec.Reason != failReasonConnectionRefused {
			t.Fatalf("durable record: status=%q reason=%q, want failed/%s", rec.Status, rec.Reason, failReasonConnectionRefused)
		}
		if rec.ErrorMsg == "" {
			t.Error("raw ErrorMsg must be retained alongside the bounded reason")
		}
		if m.failed != 1 || len(m.failedReasons) != 1 || m.failedReasons[0] != failReasonConnectionRefused {
			t.Fatalf("metrics: failed=%d reasons=%v; want 1 / [connection_refused]", m.failed, m.failedReasons)
		}
	})

	t.Run("superseded failure returns OutcomeSkipped and does not count", func(t *testing.T) {
		s := NewStorage(newMemKV(), newMemKV())
		// A newer generated outcome (revision 5) already landed.
		if err := s.SavePending(ctx, entityID, "", "text", 5); err != nil {
			t.Fatalf("SavePending: %v", err)
		}
		if err := s.SaveGenerated(ctx, entityID, []float32{1, 2, 3}, "m", 3, "h", 5); err != nil {
			t.Fatalf("SaveGenerated: %v", err)
		}
		m := &countingMetrics{}
		w := &Worker{storage: s, metrics: m, ctx: ctx, logger: discardLogger()}

		// An older-revision (3) failure is superseded.
		if got := w.markFailed(entityID, "generation failed", failReasonEmbedderError, 3); got != OutcomeSkipped {
			t.Fatalf("markFailed outcome = %v, want OutcomeSkipped for a superseded failure", got)
		}
		if m.failed != 0 || len(m.failedReasons) != 0 {
			t.Fatalf("a superseded failure must not count: failed=%d reasons=%v", m.failed, m.failedReasons)
		}
	})
}

// TestGenerationFailureClassifiedThroughWorker drives the production hop-2 path and
// proves a generation error is classified into the bounded reason on the durable
// record and reported as OutcomeFailed with that reason to the terminal callback.
func TestGenerationFailureClassifiedThroughWorker(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	index := newWatchableKV()
	s := NewStorage(index, newMemKV())

	embedder := &stubEmbedder{model: "m", dimensions: 3, generate: func([]string) ([][]float32, error) {
		return nil, errors.New("dial tcp: connect: connection refused")
	}}

	type terminal struct {
		outcome TerminalOutcome
		reason  string
	}
	done := make(chan terminal, 4)
	w := NewWorker(s, embedder, index, discardLogger()).
		WithWorkers(1).
		WithMaxSourceTextLen(4000).
		WithOnTerminal(func(_ string, _ uint64, o TerminalOutcome, r string) { done <- terminal{o, r} })

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = w.Stop() }()

	const id = "acme.ops.a.b.c.1"
	if err := s.SavePending(ctx, id, "", "some text to embed", 1); err != nil {
		t.Fatalf("SavePending: %v", err)
	}

	select {
	case tm := <-done:
		if tm.outcome != OutcomeFailed || tm.reason != failReasonConnectionRefused {
			t.Fatalf("terminal = %+v, want {OutcomeFailed, connection_refused}", tm)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for terminal outcome")
	}

	rec, err := s.GetEmbedding(ctx, id)
	if err != nil || rec == nil {
		t.Fatalf("GetEmbedding: rec=%v err=%v", rec, err)
	}
	if rec.Status != StatusFailed || rec.Reason != failReasonConnectionRefused {
		t.Fatalf("durable record status=%q reason=%q, want failed/connection_refused", rec.Status, rec.Reason)
	}
}

// TestConcurrentIdenticalContentCollapsesToOneGenerate is the #630 statement: K
// concurrent workers holding byte-identical content under the same embedder identity
// make exactly ONE embedder Generate call (process-local singleflight), and all K
// share the resulting vector.
//
// The barrier makes the collapse deterministic: the first Generate blocks on `release`
// while every peer calls generateShared, so all peers join the one in-flight call
// rather than racing to start their own. The settle window is test orchestration, not a
// correctness-timing assertion — too short a window can only report >1 Generate (a
// FALSE FAILURE), never mask a regression, so it fails safe.
func TestConcurrentIdenticalContentCollapsesToOneGenerate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var genCount atomic.Int64
	var firstOnce sync.Once
	firstIn := make(chan struct{})
	release := make(chan struct{})

	embedder := &stubEmbedder{model: "m", dimensions: 3, generate: func([]string) ([][]float32, error) {
		genCount.Add(1)
		firstOnce.Do(func() { close(firstIn) })
		<-release
		return [][]float32{{1, 2, 3}}, nil
	}}
	w := &Worker{embedder: embedder, ctx: ctx, logger: discardLogger()}

	const (
		K   = 8
		key = "shared-dedup-key"
	)
	start := make(chan struct{})
	results := make([][]float32, K)
	errsOut := make([]error, K)
	var wg sync.WaitGroup
	for i := 0; i < K; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errsOut[i] = w.generateShared(key, "identical over-window content")
		}(i)
	}
	close(start) // release all K simultaneously

	select {
	case <-firstIn:
	case <-time.After(5 * time.Second):
		t.Fatal("no Generate call arrived")
	}
	// Join window: the peers, already unblocked, reach singleflight.Do in microseconds;
	// 150ms is ~1000x that. Set generously so the collapse is observed reliably.
	time.Sleep(150 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := genCount.Load(); got != 1 {
		t.Fatalf("Generate calls = %d, want 1: concurrent byte-identical content must collapse to one call (#630)", got)
	}
	for i := 0; i < K; i++ {
		if errsOut[i] != nil {
			t.Fatalf("worker %d error: %v", i, errsOut[i])
		}
		if len(results[i]) != 3 || results[i][0] != 1 {
			t.Fatalf("worker %d got %v, want the shared [1 2 3]", i, results[i])
		}
	}
}

// TestOverCapContentDedupsAcrossLanes is the #627 regression lock (task 9.1): byte-
// identical OVER-CAP content, delivered inline to one entity and via a StorageRef to
// another, derives the SAME dedup key and the SAME embedded bytes — proving the
// increment-1 truncateAtWord unification is lane-independent. It asserts the property
// directly (no production change), so a future edit that re-splits the two lanes'
// truncation reddens here.
func TestOverCapContentDedupsAcrossLanes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const capLen = 20
	// 60 runes of the same repeated word — well over the 20-rune cap, so both lanes must
	// truncate, and both must truncate identically.
	overCap := ""
	for i := 0; i < 20; i++ {
		overCap += "lorem "
	}

	resolver := fakeResolver{stores: map[string]storage.StreamableStore{
		"objectstore": readerStore{data: overCap},
	}}
	embedder := &stubEmbedder{model: "test-model", dimensions: 3, generate: func([]string) ([][]float32, error) {
		return [][]float32{{1, 2, 3}}, nil
	}}
	w := NewWorker(NewStorage(newMemKV(), newMemKV()), embedder, newWatchableKV(), discardLogger()).
		WithMaxSourceTextLen(capLen).
		WithEmbedderType("test").
		WithStoreResolver(resolver)
	w.ctx = ctx

	inlineText, err := w.getSourceText(&Record{SourceText: overCap})
	if err != nil {
		t.Fatalf("inline getSourceText: %v", err)
	}
	offloadedText, err := w.getSourceText(&Record{StorageRef: &StorageRef{StorageInstance: "objectstore", Key: "k"}})
	if err != nil {
		t.Fatalf("offloaded getSourceText: %v", err)
	}

	if inlineText != offloadedText {
		t.Fatalf("over-cap content truncated differently across lanes:\n inline=%q\n offloaded=%q (#627)", inlineText, offloadedText)
	}
	if len(inlineText) >= len(overCap) {
		t.Fatal("test precondition: content must actually be over-cap and truncated")
	}

	id := w.embedderIdentity()
	if DedupKey(id, inlineText) != DedupKey(id, offloadedText) {
		t.Fatal("identical over-cap content must derive an identical dedup key across lanes (#627)")
	}
}
