package query

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The gh#474 readiness gate on direct INCOMING_INDEX reads moved from a
// `graph.index.query.status` request/reply to a GRAPH_STATUS KV WATCH (ADR-083). Its
// PREDICATE did not move, and this table is the proof: it enumerates the pre-change
// decision matrix (ready / not-ready / unreachable / malformed x allow_ungated_reads)
// and asserts the same proceed/defer outcome for each. It is the bit-compatibility
// baseline the migration is checked against, not a test of the new transport.
//
// It also pins the cases the transport move adds — a stale key and a deleted key, both
// shapes of "the producer died holding a Ready envelope" — to the fail-closed branch,
// where the old no-responders timeout put them.

// statusBindWaitTest is the one-shot first-delivery budget these tests give the watch,
// replacing the 5s production default. The unknown-readiness rows never receive
// anything and spend the whole budget, so a small value is what stops the table
// costing seconds per row.
//
// It is NOT as small as it could be: the happy rows also wait on it (briefly — their
// entry is pre-loaded in memory), so a budget tuned to the unknown rows would turn a
// loaded scheduler into a spurious "ready deferred" failure. A quarter second is far
// beyond any in-process channel handoff while still keeping the whole table fast.
const statusBindWaitTest = 250 * time.Millisecond

// statusEntry is a KV entry with a controllable commit time (the freshness input) and
// operation (put vs tombstone). The zero KeyValueOp is KeyValuePut, so an unset op is
// an ordinary value.
type statusEntry struct {
	value   []byte
	created time.Time
	op      jetstream.KeyValueOp
}

func (e statusEntry) Bucket() string                  { return readiness.BucketGraphStatus }
func (e statusEntry) Key() string                     { return readiness.KeyGraphIndex }
func (e statusEntry) Value() []byte                   { return e.value }
func (e statusEntry) Revision() uint64                { return 1 }
func (e statusEntry) Created() time.Time              { return e.created }
func (e statusEntry) Delta() uint64                   { return 0 }
func (e statusEntry) Operation() jetstream.KeyValueOp { return e.op }

// statusFeed is a jetstream.KeyWatcher over a pre-loaded channel that STAYS OPEN after
// delivering. Closing it would end the watch and send the production watcher into its
// rebind loop, which is a different scenario than the ones under test.
type statusFeed struct {
	updates chan jetstream.KeyValueEntry
}

func (f *statusFeed) Updates() <-chan jetstream.KeyValueEntry { return f.updates }
func (f *statusFeed) Stop() error                             { return nil }

// statusBucket embeds jetstream.KeyValue so it satisfies the interface without the
// unused methods. ONLY Watch is reached: the readiness watcher holds state rather than
// polling, so a Get here would nil-panic — the intended loud failure if that changes.
type statusBucket struct {
	jetstream.KeyValue

	// entry is delivered on bind when non-nil. A nil entry models a key the producer
	// never wrote: the watch opens fine and yields only the end-of-initial-values
	// marker, which is how an absent key reaches a watcher.
	entry jetstream.KeyValueEntry
	// err fails the Watch call itself — a backend fault, not an absent key.
	err error

	mu      sync.Mutex
	lastKey string
}

func (b *statusBucket) Watch(_ context.Context, key string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	b.mu.Lock()
	b.lastKey = key
	b.mu.Unlock()
	if b.err != nil {
		return nil, b.err
	}
	// Cap 2 so the pre-load never blocks: the current value (when there is one) plus
	// the nil end-of-initial-values marker a real watch always sends after it.
	updates := make(chan jetstream.KeyValueEntry, 2)
	if b.entry != nil {
		updates <- b.entry
	}
	updates <- nil
	return &statusFeed{updates: updates}, nil
}

func (b *statusBucket) watchedKey() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastKey
}

// statusSourceStub serves one GRAPH_STATUS bucket, or fails to open one.
type statusSourceStub struct {
	bucket jetstream.KeyValue
	err    error

	// mu guards the recorded name: the watch binds on its own goroutine, so a test
	// reading it would race the bind without this.
	mu         sync.Mutex
	lastBucket string
}

func (s *statusSourceStub) GetKeyValueBucket(_ context.Context, name string) (jetstream.KeyValue, error) {
	s.mu.Lock()
	s.lastBucket = name
	s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return s.bucket, nil
}

func (s *statusSourceStub) openedBucket() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastBucket
}

// servingStatus builds a source whose key holds the given envelope, written now.
func servingStatus(t *testing.T, st gtypes.IndexStatusResponse) readiness.BucketSource {
	t.Helper()
	raw, err := json.Marshal(st)
	require.NoError(t, err)
	return &statusSourceStub{bucket: &statusBucket{
		entry: statusEntry{value: raw, created: time.Now()}}}
}

// newGateClient builds the minimal client the gate needs. The watch runs on the
// client's LIFETIME context, so the test owns one it cancels on cleanup — otherwise a
// watcher goroutine outlives the case that started it.
func newGateClient(t *testing.T, source readiness.BucketSource, config *Config) *natsClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &natsClient{
		config:         config,
		statusSource:   source,
		watchCtx:       ctx,
		statusBindWait: statusBindWaitTest,
	}
}

func TestIndexNotReadyErr_GateMatrixIsBitCompatible(t *testing.T) {
	// Every fixture carries BootstrapComplete because every real producer does
	// (ADR-084 D2): the bit is stamped from the producer's own build latch, so an
	// envelope without it models a pre-ADR-084 producer, not a healthy one. Omitting
	// it here would silently turn each row into a bootstrap_incomplete defer and stop
	// testing what the row is named for.
	ready := gtypes.IndexStatusResponse{Ready: true, State: gtypes.IndexStateReady,
		IndexedRevision: 100, TargetRevision: 100, BootstrapComplete: true}
	building := gtypes.IndexStatusResponse{Ready: false, State: gtypes.IndexStateBuilding,
		IndexedRevision: 40, TargetRevision: 100, Lag: 60, StalenessMs: 2500, BootstrapComplete: true}
	degraded := gtypes.IndexStatusResponse{Ready: false, State: gtypes.IndexStateDegraded,
		BootstrapComplete: true}
	staleReady, err := json.Marshal(ready)
	require.NoError(t, err)

	tests := []struct {
		name string
		// newSource builds the readiness feed the gate reads. It is a constructor
		// rather than a value because each subtest runs twice (escape off and on) and
		// a watch feed is consumed by the run that binds it.
		newSource func(t *testing.T) readiness.BucketSource
		// wantDeferUngatedOff / On are the outcomes with the escape off and on.
		wantDeferUngatedOff bool
		wantDeferUngatedOn  bool
	}{
		// --- pre-change matrix, outcome-for-outcome ---
		{
			name:                "ready proceeds",
			newSource:           func(t *testing.T) readiness.BucketSource { return servingStatus(t, ready) },
			wantDeferUngatedOff: false, wantDeferUngatedOn: false,
		},
		{
			// A RECEIVED not-ready is index state, not unknown readiness: the escape
			// never applied to it before and must not now.
			name:                "explicit not-ready defers, escape does not apply",
			newSource:           func(t *testing.T) readiness.BucketSource { return servingStatus(t, building) },
			wantDeferUngatedOff: true, wantDeferUngatedOn: true,
		},
		{
			name:                "degraded defers, escape does not apply",
			newSource:           func(t *testing.T) readiness.BucketSource { return servingStatus(t, degraded) },
			wantDeferUngatedOff: true, wantDeferUngatedOn: true,
		},
		{
			// Was: no responders / request timeout. Now: no bucket.
			name: "unreachable (bucket absent) fails closed unless ungated",
			newSource: func(*testing.T) readiness.BucketSource {
				return &statusSourceStub{err: errors.New("bucket not found")}
			},
			wantDeferUngatedOff: true, wantDeferUngatedOn: false,
		},
		{
			// Was: no responders. Now: the producer never published, so the watch
			// opens and delivers nothing but the end-of-initial-values marker.
			name: "unreachable (key never published) fails closed unless ungated",
			newSource: func(*testing.T) readiness.BucketSource {
				return &statusSourceStub{bucket: &statusBucket{}}
			},
			wantDeferUngatedOff: true, wantDeferUngatedOn: false,
		},
		{
			name: "unreachable (backend fault) fails closed unless ungated",
			newSource: func(*testing.T) readiness.BucketSource {
				return &statusSourceStub{bucket: &statusBucket{err: errors.New("kv unavailable")}}
			},
			wantDeferUngatedOff: true, wantDeferUngatedOn: false,
		},
		{
			name: "malformed envelope fails closed unless ungated",
			newSource: func(*testing.T) readiness.BucketSource {
				return &statusSourceStub{bucket: &statusBucket{
					entry: statusEntry{value: []byte("{not json"), created: time.Now()}}}
			},
			wantDeferUngatedOff: true, wantDeferUngatedOn: false,
		},
		// --- the cases the transport move adds ---
		{
			// A dead producer used to mean no responders (fail closed). Its key now
			// outlives it holding Ready=true, so freshness — not the value — is what
			// keeps this on the fail-closed branch.
			name: "stale Ready key (dead producer) fails closed unless ungated",
			newSource: func(*testing.T) readiness.BucketSource {
				return &statusSourceStub{bucket: &statusBucket{entry: statusEntry{
					value:   staleReady,
					created: time.Now().Add(-readiness.FreshnessWindow(readiness.DefaultHeartbeat) - time.Second),
				}}}
			},
			wantDeferUngatedOff: true, wantDeferUngatedOn: false,
		},
		{
			// A tombstone is not evidence the producer is alive and ready, so a
			// deleted key must land on the same fail-closed branch as an absent one.
			name: "deleted key (tombstone) fails closed unless ungated",
			newSource: func(*testing.T) readiness.BucketSource {
				return &statusSourceStub{bucket: &statusBucket{entry: statusEntry{
					value: staleReady, created: time.Now(), op: jetstream.KeyValueDelete,
				}}}
			},
			wantDeferUngatedOff: true, wantDeferUngatedOn: false,
		},
		{
			name:                "nil source fails closed unless ungated",
			newSource:           func(*testing.T) readiness.BucketSource { return nil },
			wantDeferUngatedOff: true, wantDeferUngatedOn: false,
		},
	}

	for _, tt := range tests {
		for _, ungated := range []bool{false, true} {
			wantDefer := tt.wantDeferUngatedOff
			if ungated {
				wantDefer = tt.wantDeferUngatedOn
			}
			name := tt.name + "/ungated=false"
			if ungated {
				name = tt.name + "/ungated=true"
			}
			t.Run(name, func(t *testing.T) {
				qc := newGateClient(t, tt.newSource(t), &Config{AllowUngatedReads: ungated})
				err := qc.indexNotReadyErr(context.Background())
				if !wantDefer {
					require.NoError(t, err, "gate must proceed")
					return
				}
				require.Error(t, err, "gate must defer")
				var ce *errs.ClassifiedError
				require.True(t, errors.As(err, &ce), "defer must stay a classified error")
				assert.Equal(t, gtypes.ErrorCodeIndexNotReady, ce.Code,
					"the classified code is the consumer contract and must not move")
				assert.Equal(t, errs.ErrorTransient, ce.Class,
					"the class is the consumer contract and must not move")
			})
		}
	}
}

// TestIndexNotReadyErr_NilConfigFailsClosed: the escape is opt-IN. A client with no
// config at all must behave as if AllowUngatedReads were false, exactly as the
// pre-change nil-guard did.
func TestIndexNotReadyErr_NilConfigFailsClosed(t *testing.T) {
	qc := newGateClient(t, &statusSourceStub{err: errors.New("bucket not found")}, nil)
	require.Error(t, qc.indexNotReadyErr(context.Background()),
		"a nil config must not be read as permission to serve a partial index")
}

// TestIndexNotReadyErr_ReadsTheContractKey pins the bucket and key the gate watches.
// They are the cross-repo contract (`nats kv get GRAPH_STATUS graph-index`), so a
// silent rename here would leave every consumer permanently unknown — which fails
// closed, and would therefore look like a graph-index outage rather than a typo.
func TestIndexNotReadyErr_ReadsTheContractKey(t *testing.T) {
	bucket := &statusBucket{}
	src := &statusSourceStub{bucket: bucket}
	qc := newGateClient(t, src, &Config{})
	_ = qc.indexNotReadyErr(context.Background())

	// The bind happens on the watch goroutine, so wait for the coordinates rather
	// than assuming the gate's one-shot budget outlasted the scheduler.
	require.Eventually(t, func() bool {
		return src.openedBucket() == readiness.BucketGraphStatus &&
			bucket.watchedKey() == readiness.KeyGraphIndex
	}, 2*time.Second, 5*time.Millisecond,
		"gate watched %q/%q", src.openedBucket(), bucket.watchedKey())
}

// TestIndexNotReadyErr_BindsTheWatchOnce is the guard on the transport move's whole
// point: the gate holds state instead of paying a round-trip per decision (gh#590).
// A per-call bind would reintroduce exactly the per-decision cost the move removed,
// and a per-call first-delivery wait would stall every read in a deployment with no
// producer, so both are pinned here.
func TestIndexNotReadyErr_BindsTheWatchOnce(t *testing.T) {
	raw, err := json.Marshal(gtypes.IndexStatusResponse{Ready: true, State: gtypes.IndexStateReady,
		IndexedRevision: 10, TargetRevision: 10, BootstrapComplete: true})
	require.NoError(t, err)

	var binds int
	var mu sync.Mutex
	src := &countingSource{bucket: &statusBucket{entry: statusEntry{value: raw, created: time.Now()}},
		onOpen: func() { mu.Lock(); binds++; mu.Unlock() }}
	qc := newGateClient(t, src, &Config{})

	for range 5 {
		require.NoError(t, qc.indexNotReadyErr(context.Background()), "ready must proceed")
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, binds, "the watch must bind once for the client, not once per gated read")
}

// countingSource counts bucket opens so a test can prove the bind is one-shot.
type countingSource struct {
	bucket jetstream.KeyValue
	onOpen func()
}

func (s *countingSource) GetKeyValueBucket(context.Context, string) (jetstream.KeyValue, error) {
	s.onOpen()
	return s.bucket, nil
}
