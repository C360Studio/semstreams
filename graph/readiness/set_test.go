package readiness

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/nats-io/nats.go/jetstream"
)

// multiKeySource serves a distinct watch channel per key, so a Set's watchers can be
// driven independently — which is the only way to test a partial outage.
type multiKeySource struct {
	channels map[string]chan jetstream.KeyValueEntry
}

func newMultiKeySource(keys ...string) *multiKeySource {
	s := &multiKeySource{channels: make(map[string]chan jetstream.KeyValueEntry, len(keys))}
	for _, k := range keys {
		s.channels[k] = make(chan jetstream.KeyValueEntry, 8)
	}
	return s
}

func (s *multiKeySource) GetKeyValueBucket(_ context.Context, _ string) (jetstream.KeyValue, error) {
	return &multiKeyBucket{src: s}, nil
}

type multiKeyBucket struct {
	jetstream.KeyValue
	src *multiKeySource
}

func (b *multiKeyBucket) Watch(
	_ context.Context, key string, _ ...jetstream.WatchOpt,
) (jetstream.KeyWatcher, error) {
	ch, ok := b.src.channels[key]
	if !ok {
		ch = make(chan jetstream.KeyValueEntry, 8)
		b.src.channels[key] = ch
	}
	return &fakeWatcher{updates: ch}, nil
}

// publish pushes an envelope onto one key's watch channel.
func (s *multiKeySource) publish(t *testing.T, key string, status graph.IndexStatusResponse) {
	t.Helper()
	s.channels[key] <- fakeEntry{
		key:   key,
		value: statusJSON(t, status),
		op:    jetstream.KeyValuePut,
	}
}

func readyEnvelope() graph.IndexStatusResponse {
	return graph.IndexStatusResponse{
		Ready:             true,
		State:             graph.IndexStateReady,
		BootstrapComplete: true,
	}
}

// startSet builds and starts a Set with a manual clock so freshness is deterministic.
func startSet(t *testing.T, src BucketSource, keys []string) (*Set, *testClock) {
	t.Helper()
	clock := &testClock{t: time.Now()}
	s := NewSet(src, keys, withClock(clock.now), WithLogger(discardLogger()))
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(s.Stop)
	return s, clock
}

// waitFor polls until cond holds. Used instead of a sleep so the test synchronizes on
// the watcher having APPLIED an update.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// waitForKnown blocks until every named key has received at least one envelope.
//
// Waiting on `Evaluate() == false` would be a NO-OP: an unpublished Set already
// defers, so the wait would return before any update was applied and the assertion
// would read the pre-publish state. That mistake cost two failing tests here.
func waitForKnown(t *testing.T, s *Set, keys ...string) {
	t.Helper()
	waitFor(t, "keys to become known", func() bool {
		known := make(map[string]bool)
		for _, d := range s.Dumps() {
			known[d.Key] = d.Known
		}
		for _, k := range keys {
			if !known[k] {
				return false
			}
		}
		return true
	})
}

// waitForProceed blocks until the fold proceeds.
func waitForProceed(t *testing.T, s *Set) {
	t.Helper()
	waitFor(t, "fold to proceed", func() bool {
		return s.evaluate().OK
	})
}

// TestSet_AbsentKeyFailsClosedWithNoNewSemantics is task 5.2. A declared key nobody
// ever published must defer, and it must do so through the EXISTING pieces: the
// watcher reports Known=false/Fresh=false and the gate short-circuits that to
// DeferStatusUnknown. The Set adds no special case, so this is a property of the
// composition rather than new behavior to maintain.
func TestSet_AbsentKeyFailsClosedWithNoNewSemantics(t *testing.T) {
	src := newMultiKeySource(KeyGraphIngest, KeyRule)
	s, _ := startSet(t, src, []string{KeyGraphIngest, KeyRule})

	v := s.evaluate()
	if v.OK {
		t.Fatal("proceeded with no envelope ever published")
	}
	if v.Reason != graph.DeferStatusUnknown {
		t.Errorf("reason = %q, want %q", v.Reason, graph.DeferStatusUnknown)
	}
	if v.Key != KeyGraphIngest {
		t.Errorf("defer key = %q, want the first key in sorted order (%q)", v.Key, KeyGraphIngest)
	}
}

// TestSet_ProceedsOnlyWhenEveryDeclaredKeyIsReady is the conjunction. One lagging
// producer holds the whole consumer back — which is the entire reason a consumer
// declares more than one key.
func TestSet_ProceedsOnlyWhenEveryDeclaredKeyIsReady(t *testing.T) {
	src := newMultiKeySource(KeyGraphIngest, KeyRule)
	s, _ := startSet(t, src, []string{KeyGraphIngest, KeyRule})

	src.publish(t, KeyGraphIngest, readyEnvelope())
	// rule is still building.
	src.publish(t, KeyRule, graph.IndexStatusResponse{State: graph.IndexStateBuilding})

	waitForKnown(t, s, KeyGraphIngest, KeyRule)

	v := s.evaluate()
	if v.Key != KeyRule {
		t.Errorf("defer key = %q, want %q", v.Key, KeyRule)
	}
	if v.Reason != graph.DeferBootstrapIncomplete {
		t.Errorf("reason = %q, want %q", v.Reason, graph.DeferBootstrapIncomplete)
	}

	src.publish(t, KeyRule, readyEnvelope())
	waitForProceed(t, s)
}

// TestSet_DeferKeyIsDeterministic pins the sorted fold order. The same partial outage
// must name the same key every run, or a defer log is not comparable across restarts.
func TestSet_DeferKeyIsDeterministic(t *testing.T) {
	src := newMultiKeySource(KeyRule, KeyGraphIngest, KeyGraphIndex)
	// Declared in deliberately unsorted order.
	s, _ := startSet(t, src, []string{KeyRule, KeyGraphIndex, KeyGraphIngest})

	// Every key is unknown, so the first in SORTED order must be reported, not the
	// first declared.
	if v := s.evaluate(); v.Key != KeyGraphIndex {
		t.Errorf("defer key = %q, want %q (sorted first)", v.Key, KeyGraphIndex)
	}
}

// TestSet_HardStopAndUnknownAreDistinct keeps a broken producer distinguishable from
// a dead feed — different operator problems, and conflating them cost three
// investigation cycles once already (gh#590).
func TestSet_HardStopAndUnknownAreDistinct(t *testing.T) {
	src := newMultiKeySource(KeyGraphIngest)
	s, clock := startSet(t, src, []string{KeyGraphIngest})

	src.publish(t, KeyGraphIngest, graph.IndexStatusResponse{
		State:             graph.IndexStateDegraded,
		BootstrapComplete: true,
	})
	waitForKnown(t, s, KeyGraphIngest)
	if v := s.evaluate(); v.Reason != graph.DeferHardStop {
		t.Errorf("degraded producer: reason = %q, want %q", v.Reason, graph.DeferHardStop)
	}

	// Now let the feed go stale: same producer, but the consumer can no longer vouch
	// for the reading.
	src.publish(t, KeyGraphIngest, readyEnvelope())
	waitForProceed(t, s)
	clock.advance(FreshnessWindow(DefaultHeartbeat) + time.Second)

	if v := s.evaluate(); v.Reason != graph.DeferStatusUnknown {
		t.Errorf("stale feed: reason = %q, want %q", v.Reason, graph.DeferStatusUnknown)
	}
}

// TestSet_EmptyKeyListProceeds covers the consumer that declared no dependencies. It
// has nothing to wait for, so deferring would be inventing a dependency it never
// asked for.
func TestSet_EmptyKeyListProceeds(t *testing.T) {
	s := NewSet(newMultiKeySource(), nil)
	v := s.evaluate()
	if !v.OK || v.Key != "" || v.Reason != graph.DeferNone {
		t.Errorf("got %+v, want {OK:true}", v)
	}
}

// TestSet_DuplicateKeysCollapse keeps a consumer that names the same producer twice
// from folding it twice.
func TestSet_DuplicateKeysCollapse(t *testing.T) {
	s := NewSet(newMultiKeySource(KeyGraphIngest), []string{KeyGraphIngest, KeyGraphIngest, ""})
	if got := s.Dumps(); len(got) != 1 || got[0].Key != KeyGraphIngest {
		t.Errorf("dumps = %+v, want exactly one for %s", got, KeyGraphIngest)
	}
}

// TestSet_FullyCoveredIsStricterThanProceed is task 5.3. Coverage is the snapshot
// predicate: healthy is not enough, every producer must also have drained. It must
// never be used to gate a read path (ADR-085), and the separation of the two methods
// is what keeps that visible at every call site.
func TestSet_FullyCoveredIsStricterThanProceed(t *testing.T) {
	src := newMultiKeySource(KeyGraphIngest, KeyRule)
	s, _ := startSet(t, src, []string{KeyGraphIngest, KeyRule})

	// Healthy and bootstrapped, but graph-ingest still has a backlog.
	behind := readyEnvelope()
	behind.Lag = 42
	src.publish(t, KeyGraphIngest, behind)
	src.publish(t, KeyRule, readyEnvelope())

	waitForKnown(t, s, KeyGraphIngest, KeyRule)
	waitForProceed(t, s) // the HEALTH gate proceeds — lag is not a fault

	v := s.FullyCovered()
	if v.OK {
		t.Error("reported fully covered with 42 messages outstanding")
	}
	if v.Key != KeyGraphIngest {
		t.Errorf("pending key = %q, want %q", v.Key, KeyGraphIngest)
	}

	drained := readyEnvelope()
	src.publish(t, KeyGraphIngest, drained)

	waitFor(t, "fully covered after the backlog drained", func() bool {
		return s.FullyCovered().OK
	})
}

// TestSet_FullyCoveredImpliesProceed pins the ordering: an unhealthy or unknown
// producer can never be reported as covered.
func TestSet_FullyCoveredImpliesProceed(t *testing.T) {
	src := newMultiKeySource(KeyGraphIngest)
	s, _ := startSet(t, src, []string{KeyGraphIngest})

	// Never published: unknown.
	v := s.FullyCovered()
	if v.OK {
		t.Error("unknown producer reported as covered")
	}
	if v.Reason != graph.DeferStatusUnknown {
		t.Errorf("reason = %q, want %q", v.Reason, graph.DeferStatusUnknown)
	}
}

// TestSet_DumpsAreReadOnlyAndPerKey pins the operator surface: per-key local facts,
// no verdict. A verdict here would bake the key list into the framework, which the
// client-side-fold rule forbids.
func TestSet_DumpsAreReadOnlyAndPerKey(t *testing.T) {
	src := newMultiKeySource(KeyGraphIngest, KeyRule)
	s, _ := startSet(t, src, []string{KeyGraphIngest, KeyRule})

	behind := readyEnvelope()
	behind.Lag = 7
	behind.BootstrapScope = 900
	src.publish(t, KeyGraphIngest, behind)
	waitForKnown(t, s, KeyGraphIngest)

	dumps := s.Dumps()
	if len(dumps) != 2 {
		t.Fatalf("got %d dumps, want 2", len(dumps))
	}
	if dumps[0].Key != KeyGraphIngest || dumps[1].Key != KeyRule {
		t.Errorf("dumps not in fold order: %+v", dumps)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		dumps = s.Dumps()
		if dumps[0].Known {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !dumps[0].Known || dumps[0].Lag != 7 || dumps[0].BootstrapScope != 900 {
		t.Errorf("graph-ingest dump did not echo the envelope: %+v", dumps[0])
	}
	if dumps[1].Known {
		t.Errorf("rule was never published; dump must report unknown: %+v", dumps[1])
	}
}
