package graphgateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/c360studio/semstreams/graph/readiness"
)

// defaultReadinessPath is where the operator surface lands when the config leaves it
// empty but declares keys.
const defaultReadinessPath = "/readiness"

// joinGatewayPath joins the gateway prefix and a route path, collapsing the double
// slashes the existing route registrations each hand-roll.
func joinGatewayPath(prefix, path string) string {
	joined := prefix + path
	if joined == "" {
		return "/"
	}
	if joined[0] != '/' {
		joined = "/" + joined
	}
	for i := 0; i < len(joined)-1; i++ {
		if joined[i] == '/' && joined[i+1] == '/' {
			joined = joined[:i] + joined[i+1:]
			i--
		}
	}
	return joined
}

// startReadinessSet binds a watcher per configured producer key.
//
// The key list comes from CONFIG, never from a framework default. The producer set is
// deployment-dependent, so a framework-declared list would either make this surface
// report on producers a deployment does not run (permanently unknown, which reads as
// broken) or silently omit ones it does. An empty list means the operator did not ask
// for this surface, and no watchers are started.
func (c *Component) startReadinessSet(ctx context.Context) error {
	if len(c.config.ReadinessKeys) == 0 {
		return nil
	}
	set := readiness.NewSet(c.natsClient, c.config.ReadinessKeys,
		readiness.WithLogger(c.logger))
	// ASSIGN BEFORE checking the error. Watcher.Read reports Known=false/Fresh=false
	// for a key that never bound, so an un-started Set produces honest UNKNOWN rows —
	// which is what the caller's log promises and what the spec's
	// quiet-feed-vs-not-ready scenario needs. Dropping the Set on error would instead
	// serve an EMPTY list, defeating the one distinction the surface exists to make.
	c.mu.Lock()
	c.readinessSet = set
	c.mu.Unlock()
	return set.Start(ctx)
}

// stopReadinessSet tears the watchers down.
func (c *Component) stopReadinessSet() {
	if c.readinessSet != nil {
		c.readinessSet.Stop()
		c.readinessSet = nil
	}
}

// readinessSurfaceResponse is the wire shape of the operator surface.
//
// THERE IS NO AGGREGATE VERDICT FIELD, AND THAT IS THE CONTRACT, not an omission. The
// key list belongs to the consumer: this gateway watches whatever an operator
// configured, which is not necessarily what any given consumer depends on. Publishing
// a computed "ready: true/false" here would invent a fleet-wide verdict out of one
// deployment's configuration, and callers would gate on it. Operators read the per-key
// rows; consumers fold their own keys client-side.
type readinessSurfaceResponse struct {
	// Producers is one row per watched key, in deterministic order.
	Producers []readiness.Dump `json:"producers"`
}

// handleReadiness serves the read-only per-producer readiness view.
//
// It answers the question "what does this process currently see on GRAPH_STATUS", so a
// quiet feed is distinguishable from a not-ready producer WITHOUT correlating logs:
// `known` separates "never saw this producer" from "saw it then it went quiet",
// `fresh` separates "vouchable" from "stale", and `age_ms` says how stale.
func (c *Component) handleReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Snapshot ONCE under the lock. c.readinessSet is written by Start and cleared by
	// Stop; re-reading the field would both race those writers and open a nil-deref
	// window between the nil check and the Dumps call (Stop can land in between).
	c.mu.RLock()
	set := c.readinessSet
	c.mu.RUnlock()

	if set == nil {
		// Watches nothing: either no keys configured, or Stop has run. An empty list
		// is the honest answer — NOT an error, and NOT an implied "all ready".
		writeReadinessJSON(w, c.logger, readinessSurfaceResponse{Producers: []readiness.Dump{}})
		return
	}

	writeReadinessJSON(w, c.logger, readinessSurfaceResponse{Producers: set.Dumps()})
}

func writeReadinessJSON(w http.ResponseWriter, logger *slog.Logger, body readinessSurfaceResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(body); err != nil && logger != nil {
		logger.Warn("readiness surface encode failed", slog.Any("error", err))
	}
}
