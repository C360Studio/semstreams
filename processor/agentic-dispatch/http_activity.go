package agenticdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/graphview"
	"github.com/nats-io/nats.go/jetstream"
)

// completeKeyPrefix marks terminal AGENT_LOOPS keys (COMPLETE_<loopID>)
// carrying completion event payloads rather than live LoopEntity state.
const completeKeyPrefix = "COMPLETE_"

// errActivityViewStart marks ensureActivityView failures at view start
// (WatchAll open) rather than bucket acquisition, so the handler keeps
// today's per-stage SSE error strings and metric labels.
var errActivityViewStart = errors.New("start activity view")

// activityRecord is the AGENT_LOOPS view's decoded value type: the canonical
// Loop wire projection, decoded exactly once per KV write regardless of how
// many SSE clients are attached (ADR-081 decode-once), plus the KV server
// write time (graphview.EntryMeta.Created == entry.Created()) — exact
// timestamp parity with the retired per-client watcher wire for both replay
// and live events.
type activityRecord struct {
	loop      Loop
	createdAt time.Time
}

type activityViewCommand struct {
	source graphview.WatcherSource
	stop   bool
	result chan activityViewResult
}

type activityViewResult struct {
	view *graphview.View[activityRecord]
	err  error
}

func (c *Component) runActivityViewControl(
	runCtx context.Context,
	commands <-chan activityViewCommand,
	done chan<- struct{},
) {
	defer close(done)
	var view *graphview.View[activityRecord]
	defer func() {
		if view != nil {
			view.Stop()
		}
	}()
	for {
		select {
		case <-runCtx.Done():
			return
		case command := <-commands:
			if command.stop {
				if view != nil {
					view.Stop()
					view = nil
				}
				command.result <- activityViewResult{}
				return
			}
			if view == nil {
				opts := append([]graphview.Option{graphview.WithHooks(c.activityViewHooks())}, c.activityViewOpts...)
				created, err := graphview.New[activityRecord](command.source, c.decodeActivityRecord, opts...)
				if err == nil {
					err = created.Start(runCtx)
				}
				if err != nil {
					if created != nil {
						created.Stop()
					}
					command.result <- activityViewResult{err: fmt.Errorf("%w: %w", errActivityViewStart, err)}
					continue
				}
				view = created
			}
			command.result <- activityViewResult{view: view}
		}
	}
}

// decodeActivityRecord is the shared view's validating DecodeFunc (G6).
// COMPLETE_<id> keys decode terminal completion payloads (loopFromCompletion);
// every other key decodes live agentic.LoopEntity state (loopFromEntity) —
// the same production projections the per-client watcher path used. keep is
// always true for well-formed payloads because today's wire forwards every
// AGENT_LOOPS key. Genuinely malformed payloads return an error and poison
// the key: they surface on the SSE error path instead of laundering through
// as data-less activity events, and heal on the next clean write (ADR-079).
func (c *Component) decodeActivityRecord(key string, value []byte, meta graphview.EntryMeta) (activityRecord, bool, error) {
	if strings.HasPrefix(key, completeKeyPrefix) {
		loop, ok := loopFromCompletion(value)
		if !ok {
			return activityRecord{}, false, fmt.Errorf("undecodable completion payload on %s", key)
		}
		return activityRecord{loop: loop, createdAt: meta.Created}, true, nil
	}
	var e agentic.LoopEntity
	if err := json.Unmarshal(value, &e); err != nil {
		return activityRecord{}, false, fmt.Errorf("undecodable loop entity on %s: %w", key, err)
	}
	return activityRecord{
		loop:      loopFromEntity(&e, c.deps.Platform.Org, c.deps.Platform.Platform),
		createdAt: meta.Created,
	}, true, nil
}

// activityViewHooks wires the shared view's telemetry into the component's
// Prometheus metrics (graph-view-subscription task 2.5). Test hooks, when
// set, run after the metric update so tests synchronize on the exact signals
// the metrics see. All callbacks are fast and non-blocking per the Hooks
// contract.
func (c *Component) activityViewHooks() graphview.Hooks {
	extra := c.activityTestHooks
	return graphview.Hooks{
		OnApply: func(key string, revision uint64) {
			c.metrics.recordActivityViewRevision(revision)
			if extra.OnApply != nil {
				extra.OnApply(key, revision)
			}
		},
		OnCaughtUp: func() {
			c.metrics.recordActivityViewCaughtUp(true)
			if extra.OnCaughtUp != nil {
				extra.OnCaughtUp()
			}
		},
		OnWatcherLost: func(err error) {
			c.metrics.recordActivityViewCaughtUp(false)
			c.metrics.recordActivityViewWatcherLost()
			if extra.OnWatcherLost != nil {
				extra.OnWatcherLost(err)
			}
		},
		OnPoison: func(key string, err error) {
			c.metrics.recordActivityViewPoison()
			if extra.OnPoison != nil {
				extra.OnPoison(key, err)
			}
		},
		OnTick: func(changed, subs int) {
			if extra.OnTick != nil {
				extra.OnTick(changed, subs)
			}
		},
		OnSubscribers: func(n int) {
			c.metrics.recordActivityViewSubscribers(n)
			if extra.OnSubscribers != nil {
				extra.OnSubscribers(n)
			}
		},
		OnFanOut: func(overwritten, maxPending int) {
			c.metrics.recordActivityViewFanOut(overwritten, maxPending)
			if extra.OnFanOut != nil {
				extra.OnFanOut(overwritten, maxPending)
			}
		},
	}
}

// ensureActivityView returns the component's ONE shared AGENT_LOOPS view,
// lazily constructing and starting it on first use. Laziness preserves the
// pre-view contract: bucket absence degrades the /activity endpoint per
// request instead of failing component boot (the unconditional-resource-
// wiring footgun) — the old handler resolved the bucket per request too.
// Lifecycle contract: this is a lazy singleton owned by the component's
// one-shot control goroutine. A request after terminal Stop receives the
// existing per-request error path and cannot rebuild lifecycle authority.
func (c *Component) ensureActivityView(ctx context.Context) (*graphview.View[activityRecord], error) {
	c.lifecycleMu.Lock()
	commands := c.activityCommands
	source := c.activityViewSource
	c.lifecycleMu.Unlock()
	if commands == nil {
		return nil, errors.New("activity view lifecycle is not running")
	}
	// Resolve the bucket handle OUTSIDE the mutex (double-checked init): a
	// hung NATS API call must not serialize first-attaches behind one caller
	// or block stopActivityView/Component.Stop on the same mutex.
	if source == nil {
		if c.natsClient == nil {
			return nil, ErrNATSClientNil
		}
		bucket, bucketErr := c.loopsBucketName()
		if bucketErr != nil {
			return nil, fmt.Errorf("resolve activity bucket: %w", bucketErr)
		}
		kv, err := c.natsClient.GetKeyValueBucket(ctx, bucket)
		if err != nil {
			return nil, fmt.Errorf("get %s bucket: %w", bucket, err)
		}
		source = kv
	}

	result := make(chan activityViewResult, 1)
	select {
	case commands <- activityViewCommand{source: source, result: result}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case got := <-result:
		return got.view, got.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// stopActivityView stops the shared view if the lazy first /activity request
// created it: every attached subscription receives the explicit terminal
// close and the single bucket watcher shuts down. Idempotent; called from
// Component.Stop.
//
// Terminal stop while HTTP routing still delivers requests cannot recreate
// the view; the endpoint degrades through its existing SSE error response.
func (c *Component) stopActivityView() {
	c.lifecycleMu.Lock()
	cancel := c.activityCancel
	done := c.activityDone
	commands := c.activityCommands
	c.lifecycleMu.Unlock()
	if cancel != nil {
		result := make(chan activityViewResult, 1)
		select {
		case commands <- activityViewCommand{stop: true, result: result}:
			<-result
		case <-done:
		}
		cancel()
	}
	if done != nil {
		<-done
	}
	c.lifecycleMu.Lock()
	if c.activityDone == done {
		c.activityCommands = nil
		c.activityDone = nil
		c.activityCancel = nil
	}
	c.lifecycleMu.Unlock()
}

// attachActivityView gates the SSE attach on view readiness (G5) bounded by
// the request context: the view never serves an unmarked partial state. A
// failed view is restarted at most once per attach — the supervisory choice
// is restart-on-next-attach, not a background retry loop: the first client
// arriving after a watcher loss pays the re-bootstrap, and an idle component
// holds no retry state. Concurrent attaches race Restart benignly — exactly
// one transitions the failed view to bootstrap, the losers' Restart error is
// ignored, and everyone waits for the same caught-up signal.
func (c *Component) attachActivityView(ctx context.Context, view *graphview.View[activityRecord]) (
	graphview.Snapshot[activityRecord], *graphview.Subscription[activityRecord], error,
) {
	err := view.WaitCaughtUp(ctx)
	if err != nil && errors.Is(err, graphview.ErrWatcherLost) {
		if rerr := view.Restart(); rerr != nil {
			c.logger.DebugContext(ctx, "activity view restart raced or failed",
				slog.String("error", rerr.Error()))
		}
		err = view.WaitCaughtUp(ctx)
	}
	if err != nil {
		return graphview.Snapshot[activityRecord]{}, nil, err
	}
	return view.SnapshotAndSubscribe(ctx)
}

// handleActivityStream streams real-time activity events via SSE, served
// from the component's ONE shared AGENT_LOOPS graph view (ADR-081) instead
// of a per-client kv.WatchAll — N connected dashboards cost one bucket
// watcher and one decode per write (the in-repo gh#579 fix). The wire
// contract is unchanged: connected → retry directive → replay → sync_complete
// → live events; failures ride the existing "error" SSE event.
func (c *Component) handleActivityStream(w http.ResponseWriter, r *http.Request) {
	ctx, requestID := c.withRequestID(w, r)
	clientID := r.Header.Get("X-Client-ID")
	if clientID == "" {
		clientID = requestID
	}

	// Setup SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		c.metrics.recordSSEError("streaming_not_supported")
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	view, err := c.ensureActivityView(ctx)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to acquire activity view",
			slog.String("request_id", requestID),
			slog.String("client_id", clientID),
			slog.String("error", err.Error()))
		if errors.Is(err, errActivityViewStart) {
			c.metrics.recordSSEError("watcher_create")
			c.sendActivityError(w, flusher, "Failed to create watcher", err)
		} else {
			c.metrics.recordSSEError("kv_bucket_access")
			c.sendActivityError(w, flusher, "Failed to access AGENT_LOOPS bucket", err)
		}
		return
	}

	snap, sub, err := c.attachActivityView(ctx, view)
	if err != nil {
		if ctx.Err() != nil {
			return // client went away while waiting for readiness
		}
		c.logger.ErrorContext(ctx, "failed to attach to activity view",
			slog.String("request_id", requestID),
			slog.String("client_id", clientID),
			slog.String("error", err.Error()))
		c.metrics.recordSSEError("watcher_create")
		c.sendActivityError(w, flusher, "Failed to create watcher", err)
		return
	}
	defer sub.Unsubscribe()

	// Track SSE connection
	c.metrics.recordSSEConnect()
	defer c.metrics.recordSSEDisconnect()

	c.logger.InfoContext(ctx, "activity SSE client connected",
		slog.String("request_id", requestID),
		slog.String("client_id", clientID),
		slog.String("remote_addr", r.RemoteAddr))

	// Send initial connected event
	c.sendActivityEvent(w, flusher, "connected", map[string]string{
		"message":   "Watching for activity events",
		"client_id": clientID,
	})
	c.metrics.recordSSEEvent("connected")

	// Send retry directive
	fmt.Fprintf(w, "retry: 5000\n\n")
	flusher.Flush()

	c.replayActivitySnapshot(ctx, w, flusher, clientID, snap)

	c.sendActivityEvent(w, flusher, "sync_complete", map[string]string{
		"message": "Initial sync complete",
	})
	c.metrics.recordSSEEvent("sync_complete")

	c.streamActivityDeltas(ctx, w, flusher, clientID, sub)
}

// replayActivitySnapshot emits the consistent-at-S snapshot as the client's
// initial replay, ordered by KV revision ascending (the closest analogue of
// the per-client watcher's chronological replay; keys tie-break for
// determinism). Poisoned keys surface on the per-key SSE error path (G6) —
// a late attacher still learns which keys are unservable.
func (c *Component) replayActivitySnapshot(ctx context.Context, w http.ResponseWriter, flusher http.Flusher,
	clientID string, snap graphview.Snapshot[activityRecord]) {
	type replayItem struct {
		key    string
		rev    uint64
		rec    activityRecord
		poison *graphview.PoisonError
	}
	items := make([]replayItem, 0, len(snap.Entries)+len(snap.Poisoned))
	for key, e := range snap.Entries {
		items = append(items, replayItem{key: key, rev: e.Revision, rec: e.Value})
	}
	for key, p := range snap.Poisoned {
		items = append(items, replayItem{key: key, rev: p.Revision, poison: p})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].rev != items[j].rev {
			return items[i].rev < items[j].rev
		}
		return items[i].key < items[j].key
	})
	for _, it := range items {
		if it.poison != nil {
			c.emitActivityPoison(w, flusher, it.key, it.poison)
			continue
		}
		c.emitActivityUpsert(ctx, w, flusher, clientID, it.key, it.rev, it.rec)
	}
}

// streamActivityDeltas is the per-client drain loop: coalesced delta batches
// from the shared view, heartbeats, and the explicit terminal signals. A
// stalled HTTP write parks only this loop (this client's request goroutine)
// — the view watcher, the projection, and every other client keep flowing,
// and this client's pending deltas coalesce last-writer-wins until it drains
// again (slowness never disconnects it).
func (c *Component) streamActivityDeltas(ctx context.Context, w http.ResponseWriter, flusher http.Flusher,
	clientID string, sub *graphview.Subscription[activityRecord]) {
	// Heartbeat ticker for connection health
	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.InfoContext(ctx, "activity SSE client disconnected",
				slog.String("client_id", clientID),
				slog.String("reason", ctx.Err().Error()))
			return

		case <-heartbeatTicker.C:
			// Send heartbeat comment to keep connection alive and detect stale connections
			fmt.Fprintf(w, ":heartbeat %d\n\n", time.Now().Unix())
			flusher.Flush()
			c.metrics.recordSSEEvent("heartbeat")

		case batch, ok := <-sub.Deltas():
			if !ok {
				err := sub.Err()
				if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					// Clean detach — the client hung up (subscription context);
					// same exit as the ctx.Done branch.
					c.logger.InfoContext(ctx, "activity SSE client disconnected",
						slog.String("client_id", clientID))
					return
				}
				// Watcher loss or view stop: fail closed — the client must
				// never keep trusting a frozen stream as live (G5).
				c.logger.WarnContext(ctx, "activity view subscription terminated",
					slog.String("client_id", clientID),
					slog.String("error", err.Error()))
				c.metrics.recordSSEError("watcher_closed")
				c.sendActivityError(w, flusher, "Watcher closed unexpectedly", err)
				return
			}
			for _, d := range batch {
				c.writeActivityDelta(ctx, w, flusher, clientID, d)
			}
		}
	}
}

// writeActivityDelta maps one coalesced view delta onto the SSE wire.
// Upserts and tombstones keep the pre-view activity-event shape (type
// derived from key/op/revision via activityEventTypeAndID); poison maps to
// the existing per-key error event without ending the connection (G6).
func (c *Component) writeActivityDelta(ctx context.Context, w http.ResponseWriter, flusher http.Flusher,
	clientID string, d graphview.Delta[activityRecord]) {
	switch d.Op {
	case graphview.DeltaPoison:
		c.emitActivityPoison(w, flusher, d.Key, d.Err)
	case graphview.DeltaDelete:
		eventType, bareLoopID := activityEventTypeAndID(d.Key, jetstream.KeyValueDelete, d.Revision)
		// Deletes carry no payload (the watcher path never decoded tombstones
		// either); Delta.Created is the delete marker's entry.Created() —
		// the same timestamp the per-client watcher wire stamped.
		c.emitActivityEvent(ctx, w, flusher, clientID, ActivityEvent{
			Type:      eventType,
			LoopID:    bareLoopID,
			Timestamp: d.Created,
		})
	default: // graphview.DeltaUpsert
		c.emitActivityUpsert(ctx, w, flusher, clientID, d.Key, d.Revision, d.Value)
	}
}

// emitActivityUpsert writes one upsert (live delta or snapshot replay entry)
// as an "activity" SSE event with the same type derivation the per-client
// watcher used.
func (c *Component) emitActivityUpsert(ctx context.Context, w http.ResponseWriter, flusher http.Flusher,
	clientID, key string, revision uint64, rec activityRecord) {
	eventType, bareLoopID := activityEventTypeAndID(key, jetstream.KeyValuePut, revision)
	loop := rec.loop // per-client copy: decoded values are shared across subscribers
	c.emitActivityEvent(ctx, w, flusher, clientID, ActivityEvent{
		Type:      eventType,
		LoopID:    bareLoopID,
		Timestamp: rec.createdAt,
		Data:      &loop,
	})
}

// emitActivityEvent marshals and writes one "activity" SSE event.
func (c *Component) emitActivityEvent(ctx context.Context, w http.ResponseWriter, flusher http.Flusher,
	clientID string, event ActivityEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to marshal activity event",
			slog.String("client_id", clientID),
			slog.String("loop_id", event.LoopID),
			slog.String("error", err.Error()))
		c.metrics.recordSSEError("marshal_event")
		return
	}

	fmt.Fprintf(w, "event: activity\ndata: %s\n\n", data)
	flusher.Flush()
	c.metrics.recordSSEEvent(event.Type)

	c.logger.DebugContext(ctx, "sent activity event",
		slog.String("client_id", clientID),
		slog.String("loop_id", event.LoopID),
		slog.String("event_type", event.Type))
}

// emitActivityPoison surfaces a poisoned AGENT_LOOPS key on the existing SSE
// error path — per-key and non-terminal: one malformed value must not kill
// the stream for every healthy loop (G6). The pre-view handler silently
// forwarded a data-less activity event for undecodable values; the poison
// contract makes the failure visible instead of laundering it. The
// view-level poison counter increments once per poisoned write via OnPoison
// regardless of client count; this per-client surfacing rides
// router_sse_errors_total.
func (c *Component) emitActivityPoison(w http.ResponseWriter, flusher http.Flusher, key string, perr error) {
	c.metrics.recordSSEError("poisoned_entry")
	c.sendActivityError(w, flusher, fmt.Sprintf("Malformed loop entry: %s", key), perr)
}

// sendActivityEvent sends an SSE event for activity stream.
func (c *Component) sendActivityEvent(w http.ResponseWriter, flusher http.Flusher, event string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		c.logger.Error("Failed to marshal activity event", slog.String("event", event), slog.String("error", err.Error()))
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
	flusher.Flush()
}

// sendActivityError sends an error event via SSE.
func (c *Component) sendActivityError(w http.ResponseWriter, flusher http.Flusher, message string, err error) {
	errorData := map[string]string{"error": message}
	if err != nil {
		errorData["details"] = err.Error()
	}
	c.sendActivityEvent(w, flusher, "error", errorData)
}
