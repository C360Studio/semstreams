package ownership

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// EnsureBuckets idempotently creates the framework-owned ownership buckets and
// returns a Registry over them.
//
// Call this EAGERLY in the framework boot path, BEFORE any
// lifecycle.Manager.Register — every non-empty registration CAS-writes
// OWNER_CLAIMS, while owning registrations also heartbeat into OWNER_PRESENCE,
// so both buckets must already exist. graph-ingest's initStorage runs AFTER
// registration in every binary's wiring (NewManager → Register → service
// start), so creating these alongside ENTITY_STATES there would make every
// registration a silent no-op (the buckets would not yet exist). That is why
// this is a standalone eager call, not graph-ingest work.
//
// Bucket layout (ADR-056 Decision 2; both shapes are declared in the framework
// KV catalog, graph/kvcatalog.go, and acquired through the owner seam):
//   - OWNER_CLAIMS — the single `_registry` epoch key; History for audit, NO
//     TTL (a TTL would age out the durable epoch between deploys).
//   - OWNER_PRESENCE — heartbeat keys only for atomic registrations containing
//     replace/CAS claims; the catalog's bounded-ttl (= PresenceTTL) IS their
//     staleness backstop. An absent key means that owning entry is compactable.
//     Non-owning append/foreign-edge-only entries intentionally have no key and
//     persist. Without this TTL a crashed owning lease is never reaped and its
//     stale claim blocks every future registrant forever — so the TTL is not
//     optional, and the seam CONVERGES to it rather than stripping it.
//
// PENDING_EDGES (BucketPendingEdges) is deliberately NOT created here: its
// consumer (the Decision-4 foreign-edge buffer) is a later increment, and a
// bucket created with no reader reads as a half-wired bug.
//
// resolver is the Decision-4 inverse-gate's InverseResolver (an adapter over
// vocabulary.GetInversePredicate, wired by the caller so this package stays free
// of the vocabulary dependency). Pass nil to skip the gate (observe-only / read-
// only consumers) — RegisterOwner then warns once if a would-be-gated claim is
// registered, rather than silently admitting an unrecoverable foreign edge.
func EnsureBuckets(ctx context.Context, client *natsclient.Client, logger *slog.Logger, resolver InverseResolver) (*Registry, error) {
	if client == nil {
		return nil, fmt.Errorf("ownership: EnsureBuckets requires a non-nil NATS client")
	}
	if logger == nil {
		logger = slog.Default()
	}

	claims, err := graph.EnsureCatalogBucket(ctx, client, BucketOwnerClaims)
	if err != nil {
		return nil, fmt.Errorf("ownership: create %s bucket: %w", BucketOwnerClaims, err)
	}

	presence, err := graph.EnsureCatalogBucket(ctx, client, BucketOwnerPresence)
	if err != nil {
		return nil, fmt.Errorf("ownership: create %s bucket: %w", BucketOwnerPresence, err)
	}

	reg := NewRegistry(client.NewKVStore(claims), client.NewKVStore(presence), logger)
	reg.inverseResolver = resolver
	return reg, nil
}
