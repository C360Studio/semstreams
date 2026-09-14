package agenticloop

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

type approvalDeadlineWatcher struct {
	updates chan jetstream.KeyValueEntry
	stops   int
	stopErr error
}

func (w *approvalDeadlineWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *approvalDeadlineWatcher) Stop() error {
	w.stops++
	return w.stopErr
}

type approvalDeadlineBucket struct {
	*settlementBucket
	watcher  *approvalDeadlineWatcher
	watchErr error
	onWatch  func(context.Context)
	reads    []string
}

func (b *approvalDeadlineBucket) WatchAll(ctx context.Context, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	if b.onWatch != nil {
		b.onWatch(ctx)
	}
	return b.watcher, b.watchErr
}

func (b *approvalDeadlineBucket) Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error) {
	if b.watcher.stops != 1 {
		return nil, errors.New("exact authority read preceded snapshot Stop")
	}
	b.reads = append(b.reads, key)
	return b.settlementBucket.Get(ctx, key)
}

func emptyApprovalDeadlineBucket() *approvalDeadlineBucket {
	watcher := &approvalDeadlineWatcher{updates: make(chan jetstream.KeyValueEntry, 1)}
	watcher.updates <- nil
	return &approvalDeadlineBucket{watcher: watcher}
}

// spec: agentic-loop / Approval deadlines are reconstructed narrowly
func TestApprovalDeadlineSnapshotRestoresOnlyCurrentTimedPending(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	f.c.config.ApprovalTimeoutStr = "1m"
	untimed := f.entity
	untimed.ID = uuid.NewString()
	pending := *untimed.PendingApproval
	pending.Timeout = 0
	untimed.PendingApproval = &pending
	negative := f.entity
	negative.ID = uuid.NewString()
	negativePending := *negative.PendingApproval
	negativePending.Timeout = -time.Second
	negative.PendingApproval = &negativePending
	terminal := agentic.NewLoopEntity(uuid.NewString(), "finished-task", "general", "model", 3)
	terminal.State = agentic.LoopStateComplete
	f.bucket.values[untimed.ID] = settlementLoopRecord(t, untimed)
	f.bucket.values[negative.ID] = settlementLoopRecord(t, negative)
	f.bucket.values[terminal.ID] = settlementLoopRecord(t, terminal)
	missingID := uuid.NewString()
	w := &approvalDeadlineWatcher{updates: make(chan jetstream.KeyValueEntry, 9)}
	for _, key := range []string{"COMPLETE_" + f.entity.ID, f.entity.ID, untimed.ID, negative.ID, terminal.ID, missingID, f.entity.ID} {
		// Metadata is discovery, not authority: no payload is carried by this entry.
		w.updates <- settlementEntry{key: key}
	}
	w.updates <- nil
	// A live event after the snapshot marker must not become a continuing reader.
	w.updates <- settlementEntry{key: uuid.NewString()}
	b := &approvalDeadlineBucket{settlementBucket: f.bucket, watcher: w}
	f.c.loopsBucket = b
	before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
	require.Empty(t, f.c.handler.loopManager.loops)
	require.NoError(t, f.c.restoreApprovalDeadlines(t.Context()))
	require.Equal(t, 1, w.stops)
	require.Len(t, w.updates, 1)
	require.ElementsMatch(t, []string{f.entity.ID, untimed.ID, negative.ID, terminal.ID, missingID}, b.reads)
	require.Len(t, f.c.handler.loopManager.loops, 1)
	current, err := f.c.handler.GetLoop(f.entity.ID)
	require.NoError(t, err)
	require.Equal(t, f.entity, current, "replacement configuration must not rewrite the retained deadline or identity")
	require.Equal(t, before, f.bucket.values[f.entity.ID], "startup discovery is read-only")
	_, routed := f.c.handler.loopManager.GetLoopForToolCall(f.result.ExecutionID)
	require.False(t, routed, "execution reconstruction belongs to the native approval owner")
	require.Empty(t, f.c.handler.GetContextManager(f.entity.ID).GetContext())
}

// spec: agentic-loop / Approval deadlines are reconstructed narrowly
func TestApprovalDeadlineSnapshotPreservesDeadlineWhenNewApprovalsAreUntimed(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	require.Zero(t, f.c.config.ApprovalTimeout())
	w := &approvalDeadlineWatcher{updates: make(chan jetstream.KeyValueEntry, 2)}
	w.updates <- settlementEntry{key: f.entity.ID}
	w.updates <- nil
	f.c.loopsBucket = &approvalDeadlineBucket{settlementBucket: f.bucket, watcher: w}
	require.NoError(t, f.c.restoreApprovalDeadlines(t.Context()))
	current, err := f.c.handler.GetLoop(f.entity.ID)
	require.NoError(t, err)
	require.Equal(t, f.entity, current, "current configuration affects new approvals, not retained deadlines")
	require.Equal(t, 1, w.stops)
}

// spec: agentic-loop / Loop recovery is lane-specific and read-through
func TestApprovalDeadlineHydrationPreservesResponseReplayHistory(t *testing.T) {
	for _, hydrate := range []bool{false, true} {
		name := "cold response control"
		if hydrate {
			name = "response after timer hydration"
		}
		t.Run(name, func(t *testing.T) {
			f := newApprovalRecoveryFixture(t)
			if hydrate {
				w := &approvalDeadlineWatcher{updates: make(chan jetstream.KeyValueEntry, 2)}
				w.updates <- settlementEntry{key: f.entity.ID}
				w.updates <- nil
				f.c.loopsBucket = &approvalDeadlineBucket{settlementBucket: f.bucket, watcher: w}
				require.NoError(t, f.c.restoreApprovalDeadlines(t.Context()))
			}
			decision, err := f.c.handleResponseMessage(t.Context(), settlementEnvelope(t, &f.response))
			require.NoError(t, err)
			require.Equal(t, natsclient.DeliveryDecisionAck, decision)
			cm := f.c.handler.GetContextManager(f.entity.ID)
			require.NotNil(t, cm)
			want := append(append([]agentic.ChatMessage(nil), f.request.Messages...), f.response.Message)
			require.Equal(t, want, cm.GetContext(), "timer discovery must not suppress ordinary retained request reconstruction")
		})
	}
}

// spec: agentic-loop / Loop recovery is lane-specific and read-through
func TestApprovalDeadlineRestorePreservesAlreadyCorrelatedWarmHistory(t *testing.T) {
	f := newApprovalRecoveryFixture(t)
	require.NoError(t, f.c.handler.loopManager.restoreLoopFromRequest(f.entity, f.request, nil))
	cm := f.c.handler.GetContextManager(f.entity.ID)
	require.NoError(t, cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{Role: "user", Content: "new live context"}))
	before := cm.GetContext()
	require.NoError(t, f.c.handler.loopManager.restoreLoopFromRequest(f.entity, f.request, nil))
	require.Same(t, cm, f.c.handler.GetContextManager(f.entity.ID))
	require.Equal(t, before, f.c.handler.GetContextManager(f.entity.ID).GetContext())
}

// spec: agentic-loop / Approval deadlines are reconstructed narrowly
func TestApprovalDeadlineSnapshotFailureIsNotEmptySuccess(t *testing.T) {
	for _, name := range []string{"watch failure", "closed before marker", "canceled snapshot", "stop failure", "exact read failure", "malformed authority", "conflicting authority"} {
		t.Run(name, func(t *testing.T) {
			f := newApprovalRecoveryFixture(t)
			f.c.config.ApprovalTimeoutStr = "1m"
			w := &approvalDeadlineWatcher{updates: make(chan jetstream.KeyValueEntry, 2)}
			b := &approvalDeadlineBucket{settlementBucket: f.bucket, watcher: w}
			f.c.loopsBucket = b
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			wantError := ""
			wantStops := 1
			switch name {
			case "watch failure":
				b.watchErr = errors.New("snapshot unavailable")
				wantError, wantStops = "snapshot unavailable", 0
			case "closed before marker":
				close(w.updates)
				wantError = "closed before initial completion"
			case "canceled snapshot":
				b.onWatch = func(watchCtx context.Context) {
					require.Equal(t, ctx, watchCtx)
					cancel()
				}
				wantError = "context canceled"
			case "stop failure":
				b = emptyApprovalDeadlineBucket()
				w = b.watcher
				f.c.loopsBucket = b
				w.stopErr = errors.New("snapshot stop failed")
				wantError = "snapshot stop failed"
			default:
				w.updates <- settlementEntry{key: f.entity.ID}
				w.updates <- nil
				switch name {
				case "exact read failure":
					b.getErr = errors.New("authority unavailable")
					wantError = "authority unavailable"
				case "malformed authority":
					b.values[f.entity.ID] = []byte("{")
					wantError = "decode loop correlation"
				case "conflicting authority":
					other := f.entity
					other.ID = uuid.NewString()
					b.values[f.entity.ID] = settlementLoopRecord(t, other)
					wantError = "loop correlation conflict"
				}
			}
			require.ErrorContains(t, f.c.restoreApprovalDeadlines(ctx), wantError)
			require.Equal(t, wantStops, w.stops)
			require.Empty(t, f.c.handler.loopManager.loops)
		})
	}
}

// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded
func TestApprovalTimeoutSweepLeavesApplicationToNativeOwner(t *testing.T) {
	for _, name := range []string{"publication succeeds", "PubAck fails", "publisher unavailable"} {
		t.Run(name, func(t *testing.T) {
			f := newApprovalRecoveryFixture(t)
			f.entity.PendingApproval.RequestedAt = time.Now().UTC().Add(-2 * time.Hour)
			require.NoError(t, f.c.handler.loopManager.restoreToolBatch(f.entity, f.request, f.response, f.result))
			f.bucket.values[f.entity.ID] = settlementLoopRecord(t, f.entity)
			before := append([]byte(nil), f.bucket.values[f.entity.ID]...)
			var logs bytes.Buffer
			f.c.logger = slog.New(slog.NewTextHandler(&logs, nil))
			f.c.metrics = getMetrics(nil)
			failuresBefore := testutil.ToFloat64(f.c.metrics.approvalTimeoutPublishFailures)
			var published []byte
			if name == "PubAck fails" {
				f.c.natsClient = &natsclient.Client{} // Real disconnected publisher cannot receive PubAck.
			} else if name == "publication succeeds" {
				f.c.SetTestPublishHook(func(subject string, data []byte) {
					require.Equal(t, "agent.approval_response."+f.entity.ID, subject)
					published = append([]byte(nil), data...)
				})
			}
			f.c.sweepExpiredApprovals(context.Background())
			current, err := f.c.handler.GetLoop(f.entity.ID)
			require.NoError(t, err)
			require.Equal(t, f.entity, current, "the timer only publishes work; the native approval owner applies it")
			require.Equal(t, before, f.bucket.values[f.entity.ID], "timer publication must never clear durable pending state")
			if name != "publication succeeds" {
				require.Contains(t, logs.String(), "failed to publish approval response to wire")
				require.Equal(t, failuresBefore+1, testutil.ToFloat64(f.c.metrics.approvalTimeoutPublishFailures))
				require.NotContains(t, logs.String(), "auto-rejected")
				require.NotContains(t, logs.String(), "approval timeout rejection published")
				return
			}
			require.Equal(t, failuresBefore, testutil.ToFloat64(f.c.metrics.approvalTimeoutPublishFailures))
			base, err := f.c.decoder.Decode(published)
			require.NoError(t, err)
			response, ok := base.Payload().(*agentic.ApprovalResponse)
			require.True(t, ok)
			require.Equal(t, f.entity.ID, response.LoopID)
			require.Equal(t, f.entity.PendingApproval.CallID, response.CallID)
			require.Equal(t, agentic.ApprovalDecisionReject, response.Decision)
			require.Equal(t, approvalTimeoutSystemApprover, response.ApprovedBy)
			require.Equal(t, "approval timed out after 1h0m0s", response.Reason)
			// The publish hook isolates ownership; native PubAck is proven in the replacement integration.
		})
	}
}
