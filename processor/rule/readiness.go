package rule

import (
	"context"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/pkg/errs"
)

// statusMetricsInterval is the rule processor's readiness heartbeat, matching the
// framework default so every producer shares one freshness window.
const statusMetricsInterval = readiness.DefaultHeartbeat

// entityBootstrapProgressFor returns the progress record for a managed watcher
// generation, or nil when the caller is an unmanaged test/legacy handler (no
// generation identity) or the generation has already been superseded.
func (rp *Processor) entityBootstrapProgressFor(key string, generation uint64) *entityBootstrapProgress {
	if key == "" {
		return nil
	}
	rp.entityDispatchGate.RLock()
	defer rp.entityDispatchGate.RUnlock()
	record, ok := rp.entityDispatchRecords[key]
	if !ok || record.generation != generation {
		return nil
	}
	return record.bootstrap
}

// entityBootstrapState folds the CURRENTLY-AUTHORITATIVE watcher generations into the
// two envelope facts: whether every one of them has finished its replay, and how many
// values they replayed in total.
//
// The conjunction is over the authoritative record set, so a re-added pattern's new
// generation drags BootstrapComplete back to false until its own replay finishes.
// That is deliberate and is the divergence from graph-index's process-lifetime latch:
// the watcher set is runtime-mutable, and a recreated watcher re-runs replay.
//
// ZERO CONFIGURED PATTERNS means zero watchers, which is vacuously complete with scope
// 0 — the deployment authoritatively has nothing to replay. A rule processor with no
// entity-watch patterns holds no ENTITY_STATES watcher at all (see watchEntityStates),
// and reporting it as never-bootstrapped would defer every consumer forever.
func (rp *Processor) entityBootstrapState() (complete bool, scope uint64) {
	rp.entityDispatchGate.RLock()
	defer rp.entityDispatchGate.RUnlock()

	complete = true
	for _, record := range rp.entityDispatchRecords {
		if record.bootstrap == nil {
			// A record without progress tracking cannot vouch for its own replay;
			// fail closed rather than counting it as done.
			complete = false
			continue
		}
		if !record.bootstrap.complete.Load() {
			complete = false
		}
		scope += record.bootstrap.replayed.Load()
	}
	return complete, scope
}

// computeReadinessStatus projects the rule processor's bootstrap-replay state into the
// shared envelope.
//
// Lag is always 0 and State never reports a backlog: a watcher-replay producer has no
// queue to be behind on. Its whole readiness question is "has the initial replay
// finished", which rides on BootstrapComplete. State comes from the two EXISTING
// sticky latches rather than any new state — graphStateResetRequired outranks
// graphStateGuardDegraded because a reset-required processor is not merely unhealthy,
// it has stopped evaluating.
func (rp *Processor) computeReadinessStatus() graph.IndexStatusResponse {
	complete, scope := rp.entityBootstrapState()

	switch {
	case rp.graphStateResetRequired.Load():
		return graph.IndexStatusResponse{
			State:             graph.IndexStateResetRequired,
			BootstrapComplete: complete,
			BootstrapScope:    scope,
		}
	case rp.graphStateGuardDegraded.Load():
		return graph.IndexStatusResponse{
			State:             graph.IndexStateDegraded,
			BootstrapComplete: complete,
			BootstrapScope:    scope,
		}
	}

	state := graph.IndexStateBuilding
	if complete {
		state = graph.IndexStateReady
	}
	return graph.IndexStatusResponse{
		Ready:             complete,
		State:             state,
		BootstrapComplete: complete,
		BootstrapScope:    scope,
	}
}

// statusMetricsLoop republishes the readiness envelope on the shared heartbeat, so the
// write doubles as a liveness signal.
func (rp *Processor) statusMetricsLoop(ctx context.Context) {
	rp.refreshReadinessStatus(ctx)
	ticker := time.NewTicker(rp.statusTickInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-rp.shutdown:
			return
		case <-ticker.C:
			rp.refreshReadinessStatus(ctx)
		}
	}
}

// statusTickInterval is the heartbeat period, overridable only by tests.
func (rp *Processor) statusTickInterval() time.Duration {
	if rp.statusInterval > 0 {
		return rp.statusInterval
	}
	return statusMetricsInterval
}

func (rp *Processor) refreshReadinessStatus(ctx context.Context) {
	rp.publishReadinessStatus(ctx, rp.computeReadinessStatus())
}

// publishReadinessStatus writes the envelope to the rule GRAPH_STATUS key. A failed
// write never kills the loop: the next heartbeat is the recovery path, and consumers
// already fail closed after three missed heartbeats.
func (rp *Processor) publishReadinessStatus(ctx context.Context, status graph.IndexStatusResponse) {
	if rp.statusPublisher == nil {
		return
	}
	if err := rp.statusPublisher.Publish(ctx, status); err != nil {
		if ctx.Err() != nil {
			return
		}
		rp.logger.Warn("readiness status publish failed",
			slog.String("bucket", readiness.BucketGraphStatus),
			slog.String("key", rp.statusPublisher.Key()),
			slog.Any("error", err))
	}
}

// createStatusBucket provisions the GRAPH_STATUS handle and builds the publisher.
func (rp *Processor) createStatusBucket(ctx context.Context) error {
	bucket, err := readiness.EnsureBucket(ctx, rp.natsClient)
	if err != nil {
		if ctx.Err() != nil {
			return errs.Wrap(ctx.Err(), "Processor", "createStatusBucket", "context cancelled")
		}
		return errs.Wrap(err, "Processor", "createStatusBucket",
			"KV bucket: "+readiness.BucketGraphStatus)
	}
	rp.statusPublisher = readiness.NewPublisher(bucket, readiness.KeyRule)
	return nil
}
