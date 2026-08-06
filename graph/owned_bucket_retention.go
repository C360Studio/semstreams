package graph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// EnsureCatalogRetentionClean is the PRE-START LEGACY-DRIFT BACKSTOP for the
// framework KV catalog's no-lifecycle buckets (ADR-068 D1). It has exactly ONE
// honest job: a catalog bucket unused by the current composition never has its
// acquisition seam called — e.g. an EMBEDDING_INDEX left behind by a prior
// semantic deploy when booting a statistical configuration — so one boot-time
// pass over the catalog strips prior-boot/out-of-band retention dirt (or fails
// boot closed) for those inactive buckets.
//
// Everything else is the seam's job, not this pass's: every active component
// acquires its buckets through natsclient.EnsureFrameworkBucket inside its own
// Start — create-or-open, reconcile to the declared policy, verify, fail that
// Start closed — which covers prior-boot dirt AND this boot's create-races AND
// post-boot dynamic re-acquisition, at the moment of acquisition. There is no
// post-start sweep pass anymore; deleting it is safe precisely because its
// justified class (created-dirty during this boot) is reconciled at creation.
//
// Scope: only descriptors declared no-lifecycle. A bounded-ttl bucket's TTL is
// its declared contract (the seam converges to it; this pass must not strip
// it) and an unmanaged bucket carries no framework retention guarantee.
//
// Each bucket is bound READ-ONLY / MUST-EXIST (never created) / SKIP-IF-ABSENT:
// a guarded bucket that does not exist cannot carry a foreign TTL, and its true
// component creates it clean through the seam. The backstop therefore imposes no
// bucket-creation ordering and never forces a resourceless deploy to provision
// a bucket it does not use (feedback_unconditional_resource_wiring).
func EnsureCatalogRetentionClean(ctx context.Context, client *natsclient.Client, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		return errs.WrapInvalid(errors.New("nil NATS client"),
			"graph", "EnsureCatalogRetentionClean", "NATS client is required for the catalog-retention backstop")
	}

	js, err := client.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "graph", "EnsureCatalogRetentionClean", "get JetStream context")
	}

	for _, spec := range KVCatalog() {
		// The backstop covers exactly the catalog's no-lifecycle descriptors: a
		// bounded-ttl bucket's TTL is its declared contract (never stripped),
		// and an unmanaged bucket has no framework retention guarantee at all.
		if spec.Retention.Kind != natsclient.RetentionNoLifecycle {
			continue
		}
		bucket := spec.Name
		// Read-only / must-exist probe: GetKeyValueBucket never creates a bucket
		// and returns jetstream.ErrBucketNotFound for an absent one (exempted from
		// the shared circuit breaker). Skip-if-absent: a not-yet-provisioned
		// tier-gated bucket is passed over.
		if _, err := client.GetKeyValueBucket(ctx, bucket); err != nil {
			if errors.Is(err, jetstream.ErrBucketNotFound) {
				logger.Debug("catalog-retention backstop: bucket absent, skipping",
					slog.String("bucket", bucket))
				continue
			}
			return errs.WrapTransient(err, "graph", "EnsureCatalogRetentionClean",
				fmt.Sprintf("probe catalog bucket %q", bucket))
		}

		// Reconcile-then-assert on the existing bucket's backing stream. The atom
		// self-heals a foreign/legacy retention config and fails closed (fatal) on
		// an unfixable one — that fatal propagates and aborts boot.
		if err := natsclient.ReconcileNoLifecycleRetention(ctx, js, bucket, logger); err != nil {
			return err
		}
	}
	return nil
}
