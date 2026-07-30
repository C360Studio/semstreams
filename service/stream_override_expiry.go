// stream_override_expiry.go REPORTS a migration override that lapsed while this
// instance was running.
//
// A stream_migration_overrides entry admits an existing unbounded stream as a
// named, time-limited bridge. Its expiry is evaluated at configuration validation
// and at stream provisioning — both boot-time — so an instance that started before
// the deadline would otherwise run indefinitely past it with nothing saying so, and
// the entire value of a bridge is that it ends.
//
// ENFORCEMENT STAYS AT BOOT, and that is a deliberate split rather than an
// omission. The stream a lapsed override admits is still working: it is unbounded,
// which is a hygiene failure, not an outage. Flipping a running fleet out of the
// load balancer simultaneously because a calendar date passed would convert that
// hygiene failure into a self-inflicted outage, and it is the same hazard this
// capability refuses elsewhere — storage pressure reports and never gates. The hard
// refusal lands at the next boot, which is when an operator can act on it anyway.
//
// So this reports, loudly and repeatedly: a WARN per lapsed bridge on every tick,
// and a gauge an alert rule can key on. Repetition is the point — a lapse that
// scrolled past once at 03:00 is not a signal.

package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/metric"
)

// overrideExpiryCheckInterval is how often a running instance re-evaluates its
// bridges. Expiry is a date, so this only needs to be fast relative to how quickly
// an operator should hear about it; a minute keeps the gauge fresh without making
// the log noisy beyond one line per lapsed bridge per minute.
const overrideExpiryCheckInterval = time.Minute

// streamOverrideExpiryReporter re-evaluates migration-override expiry against the
// LIVE configuration on an interval.
//
// It reads the live config each tick rather than a value captured at construction:
// overrides are operator configuration, an operator may extend or remove one
// without restarting, and a reporter that kept warning about a bridge that was
// already renewed would be worse than one that said nothing.
type streamOverrideExpiryReporter struct {
	configOf func() *config.Config
	logger   *slog.Logger

	// expired is 1 for a bridge whose deadline has passed, 0 while it is open.
	// Labelled by stream and owner because the remedy needs an addressee, and
	// emitted for OPEN bridges too so the series exists before it matters — an
	// alert on a series that only appears at the moment of failure cannot be tested
	// in advance.
	expired *prometheus.GaugeVec
}

func newStreamOverrideExpiryReporter(
	configOf func() *config.Config, logger *slog.Logger,
) *streamOverrideExpiryReporter {
	return &streamOverrideExpiryReporter{
		configOf: configOf,
		logger:   logger,
		expired: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "semstreams",
			Subsystem: "streams",
			Name:      "migration_override_expired",
			Help: "1 when a stream_migration_overrides bridge has passed its expiry, 0 while it is open. " +
				"Report-only on a running instance: the stream it admits keeps working and readiness is " +
				"unaffected, but the next boot REFUSES to start. Bound the stream, or declare it archival " +
				"if permanence is genuinely its contract.",
		}, []string{"stream", "owner"}),
	}
}

func (r *streamOverrideExpiryReporter) register(registrar metric.MetricsRegistrar) error {
	return registrar.RegisterGaugeVec("streams", "migration_override_expired", r.expired)
}

// run ticks until ctx is done. The first evaluation happens immediately, so an
// instance that starts with a lapsed bridge does not stay silent for a full
// interval — that case is reachable when the process boots from a cached config or
// an operator edits one post-boot.
func (r *streamOverrideExpiryReporter) run(ctx context.Context) {
	ticker := time.NewTicker(overrideExpiryCheckInterval)
	defer ticker.Stop()

	r.evaluate(time.Now())
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.evaluate(time.Now())
		}
	}
}

func (r *streamOverrideExpiryReporter) evaluate(now time.Time) {
	cfg := r.configOf()
	if cfg == nil {
		return
	}

	// Reset first so a bridge that was RENEWED or removed stops reporting. Without
	// this the gauge would latch at 1 forever and an operator who fixed the problem
	// would keep being paged for it.
	r.expired.Reset()
	for name, override := range cfg.StreamMigrationOverrides {
		r.expired.WithLabelValues(name, override.Owner).Set(0)
	}

	for _, lapsed := range config.ExpiredMigrationOverrides(cfg, now) {
		r.expired.WithLabelValues(lapsed.Stream, lapsed.Owner).Set(1)
		r.logger.Warn(
			"stream migration override has EXPIRED; the stream it admits is still unbounded and the next "+
				"boot will refuse to start",
			slog.String("stream", lapsed.Stream),
			slog.String("owner", lapsed.Owner),
			slog.String("expired", lapsed.Expires.Format(time.RFC3339)),
			slog.Duration("expired_ago", -lapsed.Remaining),
			slog.String("reason", lapsed.Reason),
			slog.String("remedy",
				"declare max_age, max_bytes and discard on the stream, or move it to archival_streams "+
					"(owner + reason) if permanence is genuinely its contract"),
		)
	}
}
