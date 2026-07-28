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

// retentionGuardedBuckets returns the framework-owned KV buckets the boot-time
// retention sweep asserts carry no lifecycle eviction (ADR-068). It is
// FrameworkOwnedBuckets() MINUS EMBEDDINGS_CACHE.
//
// EMBEDDINGS_CACHE is the one legitimately rebuildable cache in the owned set:
// bounding its capacity (a MaxBytes/DiscardNew size cap) is a legitimate
// operability policy owned by the separate bounded-storage-operability
// capability, not by this reachability-blind-retention guard. It stays a member
// of FrameworkOwnedBuckets() (still write-ownership-protected against a generic
// update_kv) but is excluded here so the retention sweep never strips a size cap
// its true owner may legitimately set.
func retentionGuardedBuckets() []string {
	owned := FrameworkOwnedBuckets()
	guarded := make([]string, 0, len(owned))
	for _, bucket := range owned {
		if bucket == BucketEmbeddingsCache {
			continue
		}
		guarded = append(guarded, bucket)
	}
	return guarded
}

// AssertOwnedBucketsClean is the boot-time sweep that keeps the framework-owned
// KV plane free of NATS lifecycle retention (ADR-068 D1; #622). It ranges
// retentionGuardedBuckets() and, for EACH bucket that exists WHEN IT RUNS, runs
// the KV reconcile-then-assert atom (natsclient.ReconcileNoLifecycleRetention):
// a foreign/legacy MaxAge or binding MaxBytes is stripped in place and WARNed; a
// genuinely unfixable retention aborts boot fast with the bucket named, rather
// than proceeding to silently expire graph state.
//
// It is invoked TWICE per boot, and the coverage guarantee is the pair, because
// a single pass can only reconcile the buckets that happen to exist at the
// moment it ranges the set:
//
//   - PRE-START belt — from service.WireOwnership, before rule evaluation or the
//     graph components' get-or-create can lean on a persisted-dirty bucket. It
//     takes down prior-boot / out-of-band dirt early.
//   - POST-START coverage — from the tail of Manager.StartAll, after every owner
//     has created its bucket and before the HTTP surface reports healthy. This
//     pass catches a bucket created dirty DURING this boot's service-start loop
//     (a create-race) that the pre-start belt necessarily skipped as absent.
//
// Each bucket is bound READ-ONLY / MUST-EXIST (never created) / SKIP-IF-ABSENT:
// a guarded bucket that does not yet exist (a tier-gated deploy that has not
// provisioned, e.g., an embedding or community index) is passed over — it cannot
// carry a foreign TTL, and its true owner creates it clean. The sweep therefore
// imposes no bucket-creation ordering and never forces a resourceless deploy to
// provision a bucket it does not use (feedback_unconditional_resource_wiring).
//
// graph-ingest additionally keeps its two at-creation asserts (ENTITY_STATES +
// redelivery guard) as create-time belt-and-suspenders.
func AssertOwnedBucketsClean(ctx context.Context, client *natsclient.Client, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		return errs.WrapInvalid(errors.New("nil NATS client"),
			"graph", "AssertOwnedBucketsClean", "NATS client is required for the owned-bucket retention sweep")
	}

	js, err := client.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "graph", "AssertOwnedBucketsClean", "get JetStream context")
	}

	for _, bucket := range retentionGuardedBuckets() {
		// Read-only / must-exist probe: GetKeyValueBucket never creates a bucket
		// and returns jetstream.ErrBucketNotFound for an absent one (exempted from
		// the shared circuit breaker). Skip-if-absent: a not-yet-provisioned
		// tier-gated bucket is passed over.
		if _, err := client.GetKeyValueBucket(ctx, bucket); err != nil {
			if errors.Is(err, jetstream.ErrBucketNotFound) {
				logger.Debug("owned-bucket retention sweep: bucket absent, skipping",
					slog.String("bucket", bucket))
				continue
			}
			return errs.WrapTransient(err, "graph", "AssertOwnedBucketsClean",
				fmt.Sprintf("probe owned bucket %q", bucket))
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
