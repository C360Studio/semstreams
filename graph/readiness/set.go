package readiness

import (
	"context"
	"sort"
	"sync"

	"github.com/c360studio/semstreams/graph"
)

// Set folds several producers' readiness envelopes into one verdict for a consumer
// that depends on all of them.
//
// AGGREGATION IS CLIENT-SIDE AND DELIBERATELY NOT PUBLISHED. A published aggregate
// would become the one envelope that is DERIVED rather than observed, so its staleness
// would report the aggregator's liveness rather than the producers'; on any defer the
// consumer needs per-producer detail anyway; and the producer set is
// deployment-dependent (graph-ingest appears in 18 shipped component instances,
// graph-index in 8, rule in 8, graph-embedding in 4), so a framework-published
// aggregate would have to guess its own membership.
//
// The KEY LIST IS THE CONSUMER'S. There is no framework-declared "all producers" list
// and no optional-key flag — an optional key is one you did not declare. Declaring a
// key you do not depend on makes you defer on someone else's outage; omitting one you
// do depend on is the bug this whole change exists to fix, and it is visible in your
// own call site rather than buried in a framework default.
type Set struct {
	// keys is sorted at construction so the fold's first-defer answer is
	// deterministic: the same partial outage names the same key every time, which is
	// what makes a defer log comparable across restarts.
	keys     []string
	watchers map[string]*Watcher

	startOnce sync.Once
	stopOnce  sync.Once
}

// NewSet builds a Set over the given producer keys. Duplicate keys collapse; an empty
// key list yields a Set that always proceeds, which is correct — a consumer that
// declared no dependencies has nothing to wait for.
func NewSet(src BucketSource, keys []string, opts ...Option) *Set {
	unique := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k != "" {
			unique[k] = struct{}{}
		}
	}

	s := &Set{
		keys:     make([]string, 0, len(unique)),
		watchers: make(map[string]*Watcher, len(unique)),
	}
	for k := range unique {
		s.keys = append(s.keys, k)
	}
	sort.Strings(s.keys)
	for _, k := range s.keys {
		s.watchers[k] = NewWatcher(src, k, opts...)
	}
	return s
}

// Keys returns the declared producer keys in fold order.
func (s *Set) Keys() []string {
	out := make([]string, len(s.keys))
	copy(out, s.keys)
	return out
}

// Start begins watching every declared key. Safe to call once; subsequent calls are
// no-ops, matching Watcher.Start.
func (s *Set) Start(ctx context.Context) error {
	var err error
	s.startOnce.Do(func() {
		started := make([]*Watcher, 0, len(s.keys))
		for _, k := range s.keys {
			if startErr := s.watchers[k].Start(ctx); startErr != nil {
				// Unwind so a partial start does not leave orphaned goroutines.
				for _, w := range started {
					w.Stop()
				}
				err = startErr
				return
			}
			started = append(started, s.watchers[k])
		}
	})
	return err
}

// Stop cancels every watch and waits for the goroutines to exit.
func (s *Set) Stop() {
	s.stopOnce.Do(func() {
		for _, k := range s.keys {
			s.watchers[k].Stop()
		}
	})
}

// WaitForFirst blocks until every declared key has produced a first reading, or the
// context ends.
//
// It does NOT wait for readiness — only for the feeds to exist. A caller that waited
// here and then skipped the fold would be gating on transport, not on health.
func (s *Set) WaitForFirst(ctx context.Context) error {
	for _, k := range s.keys {
		if err := s.watchers[k].WaitForFirst(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Read returns the current reading for one declared key.
func (s *Set) Read(key string) (Reading, bool) {
	w, ok := s.watchers[key]
	if !ok {
		return Reading{}, false
	}
	return w.Read(), true
}

// Evaluate folds every declared key through graph.EvaluateReadinessGate and returns
// the first defer in sorted key order.
//
// It introduces NO new gate semantics and NO new defer reasons: each key is delegated
// to the single home for gate semantics, and the fold is a conjunction over the
// results. An ABSENT key needs no special case either — Watcher.Read reports
// Known=false/Fresh=false, which the gate already short-circuits to
// DeferStatusUnknown. Fail-closed on a key nobody ever published is therefore a
// property of the existing pieces, not something this type adds.
//
// On proceed the returned key is empty and the reason is DeferNone.
func (s *Set) Evaluate() (proceed bool, deferKey string, reason graph.DeferReason) {
	for _, k := range s.keys {
		r := s.watchers[k].Read()
		ok, why := graph.EvaluateReadinessGate(graph.StatusReading{
			Status: r.Status,
			Fresh:  r.Fresh,
		})
		if !ok {
			return false, k, why
		}
	}
	return true, "", graph.DeferNone
}

// FullyCovered reports the STRICTER predicate a snapshot caller needs: every declared
// producer is not merely healthy but has zero outstanding work.
//
// IT IS DELIBERATELY SEPARATE FROM Evaluate AND MUST NOT GATE ANY READ PATH. ADR-085
// banned coverage as admission control FOR READS — lag is a property of the answer,
// not a fault, and gating reads on it makes a healthy system flap unavailable under
// ordinary write load. That ADR explicitly defers the NON-read case to "that
// consumer's evidence", and gh#712 is that evidence: a parity snapshot compared two
// systems while one was still ingesting, and no health signal could have caught it
// because nothing was unhealthy.
//
// So: use this to decide WHEN TO TAKE A SNAPSHOT, never to decide whether to answer a
// query. It implies Evaluate's proceed — an unhealthy or unknown producer can never be
// covered.
func (s *Set) FullyCovered() (covered bool, pendingKey string, reason graph.DeferReason) {
	proceed, deferKey, why := s.Evaluate()
	if !proceed {
		return false, deferKey, why
	}
	for _, k := range s.keys {
		if s.watchers[k].Read().Status.Lag != 0 {
			return false, k, graph.DeferNone
		}
	}
	return true, "", graph.DeferNone
}

// Dump is a read-only projection of every declared key's local state, for an operator
// endpoint. It is NOT a verdict: baking a verdict into a framework surface would bake
// the key list into the framework, which is exactly what the client-side-fold rule
// forbids.
type Dump struct {
	Key   string `json:"key"`
	Known bool   `json:"known"`
	Fresh bool   `json:"fresh"`
	AgeMs int64  `json:"age_ms"`
	// State, Ready, Lag and BootstrapComplete are echoed from the last envelope so an
	// operator can see WHY a consumer is deferring without a second round trip.
	State             string `json:"state,omitempty"`
	Ready             bool   `json:"ready"`
	Lag               uint64 `json:"lag,omitempty"`
	BootstrapComplete bool   `json:"bootstrap_complete"`
	BootstrapScope    uint64 `json:"bootstrap_scope,omitempty"`
	// Error is the last transport/decode failure, if any. A dead feed and a
	// not-ready producer are different operator problems.
	Error string `json:"error,omitempty"`
}

// Dumps returns the per-key operator view in fold order.
func (s *Set) Dumps() []Dump {
	out := make([]Dump, 0, len(s.keys))
	for _, k := range s.keys {
		r := s.watchers[k].Read()
		d := Dump{
			Key:               k,
			Known:             r.Known,
			Fresh:             r.Fresh,
			AgeMs:             r.Age.Milliseconds(),
			State:             r.Status.State,
			Ready:             r.Status.Ready,
			Lag:               r.Status.Lag,
			BootstrapComplete: r.Status.BootstrapComplete,
			BootstrapScope:    r.Status.BootstrapScope,
		}
		if r.Err != nil {
			d.Error = r.Err.Error()
		}
		out = append(out, d)
	}
	return out
}
