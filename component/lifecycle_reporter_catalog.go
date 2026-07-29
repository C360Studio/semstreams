package component

import (
	"context"
	"log/slog"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// NewCatalogLifecycleReporter acquires the shared COMPONENT_STATUS bucket
// through the framework KV catalog seam and returns a throttled KV lifecycle
// reporter for componentName, degrading to the no-op reporter (with a WARN)
// when the bucket cannot be acquired — lifecycle reporting must never be the
// thing that fails a component's Start.
//
// This is the ONE acquisition path for COMPONENT_STATUS, replacing the 24
// per-component hand-written creates: every reporter asks for the single
// catalog shape (diagnostic / write-open / retention-unmanaged, #717), so no
// component can drift the bucket's configuration. COMPONENT_STATUS is
// deliberately NOT write-guarded — it has many cross-layer writers and zero
// production readers (only the e2e harness) — and the seam's create-or-open
// resolves the many-components-boot-concurrently race.
func NewCatalogLifecycleReporter(
	ctx context.Context, nc *natsclient.Client, componentName string, logger *slog.Logger,
) LifecycleReporter {
	if logger == nil {
		logger = slog.Default()
	}
	if nc == nil {
		logger.Warn("no NATS client; lifecycle reporting disabled",
			slog.String("component", componentName))
		return NewNoOpLifecycleReporter()
	}
	statusBucket, err := graph.EnsureCatalogBucket(ctx, nc, graph.BucketComponentStatus)
	if err != nil {
		logger.Warn("Failed to acquire COMPONENT_STATUS bucket, lifecycle reporting disabled",
			slog.String("component", componentName),
			slog.Any("error", err))
		return NewNoOpLifecycleReporter()
	}
	return NewLifecycleReporterFromConfig(LifecycleReporterConfig{
		KV:               statusBucket,
		ComponentName:    componentName,
		Logger:           logger,
		EnableThrottling: true,
	})
}
