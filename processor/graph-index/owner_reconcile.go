package graphindex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync/atomic"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/vocabulary"
)

// reconcileOwnedRows replaces one entity owner's complete membership set. The
// filtered listing is deduplicated before diffing because NATS implements it as
// a watcher snapshot and a concurrent Put can make the same key observable more
// than once. Operations are sorted to keep retries and diagnostics deterministic.
func (c *Component) reconcileOwnedRows(
	ctx context.Context,
	indexName string,
	bucket *natsclient.KVStore,
	ownerFilter string,
	desired map[string][]byte,
	overwriteExisting bool,
) error {
	existingKeys, err := bucket.KeysByFilter(ctx, ownerFilter)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.WrapTransient(err, "Component", "reconcileOwnedRows",
			fmt.Sprintf("list %s rows with %q", indexName, ownerFilter))
	}

	existing := make(map[string]struct{}, len(existingKeys))
	for _, key := range existingKeys {
		existing[key] = struct{}{}
	}

	stale := make([]string, 0, len(existing))
	for key := range existing {
		if _, keep := desired[key]; !keep {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)

	desiredKeys := make([]string, 0, len(desired))
	for key := range desired {
		desiredKeys = append(desiredKeys, key)
	}
	sort.Strings(desiredKeys)

	var failures []error
	for _, key := range stale {
		if delErr := bucket.Delete(ctx, key); delErr != nil && !natsclient.IsKVNotFoundError(delErr) {
			atomic.AddInt64(&c.errors, 1)
			failures = append(failures, fmt.Errorf("delete %s key %q: %w", indexName, key, delErr))
			c.logger.Debug("failed to retract stale index row",
				slog.String("index", indexName), slog.String("key", key), slog.Any("error", delErr))
		}
	}
	for _, key := range desiredKeys {
		if _, alreadyStored := existing[key]; alreadyStored && !overwriteExisting {
			continue
		}
		if _, putErr := bucket.Put(ctx, key, desired[key]); putErr != nil {
			atomic.AddInt64(&c.errors, 1)
			failures = append(failures, fmt.Errorf("put %s key %q: %w", indexName, key, putErr))
			c.logger.Debug("failed to write desired index row",
				slog.String("index", indexName), slog.String("key", key), slog.Any("error", putErr))
		}
	}
	if err := errors.Join(failures...); err != nil {
		return errs.WrapTransient(errIndexWritePartial, "Component", "reconcileOwnedRows",
			fmt.Sprintf("%s owner reconciliation failed: %v", indexName, err))
	}
	return nil
}

func (c *Component) reconcilePredicateIndex(ctx context.Context, entityID string, predicates map[string]bool) error {
	desired := make(map[string][]byte, len(predicates))
	orderedPredicates := make([]string, 0, len(predicates))
	for predicate := range predicates {
		if _, err := vocabulary.ParsePredicate(predicate); err != nil {
			return errs.WrapInvalid(err, "Component", "reconcilePredicateIndex", "invalid predicate")
		}
		desired[predicateIndexKey(predicate, entityID)] = predicateIndexMarker
		orderedPredicates = append(orderedPredicates, predicate)
	}

	if err := c.reconcileOwnedRows(ctx, "predicate", c.predicateBucket,
		predicateIndexEntityFilter(entityID), desired, false); err != nil {
		return err
	}

	// Catalog and membership are one required projection while the hash layout is
	// active. Re-put every desired name so repair converges either partial order.
	sort.Strings(orderedPredicates)
	var failures []error
	for _, predicate := range orderedPredicates {
		if err := c.updatePredicateCatalog(ctx, predicate); err != nil {
			atomic.AddInt64(&c.errors, 1)
			failures = append(failures, fmt.Errorf("catalog predicate %q: %w", predicate, err))
		}
	}
	if err := errors.Join(failures...); err != nil {
		return errs.WrapTransient(errIndexWritePartial, "Component", "reconcilePredicateIndex", err.Error())
	}
	return nil
}

func (c *Component) reconcileIncomingIndex(
	ctx context.Context,
	sourceID string,
	incomingByTarget map[string][]graph.IncomingEntry,
) error {
	desired := make(map[string][]byte)
	for targetID, entries := range incomingByTarget {
		for _, entry := range entries {
			if !validateIncomingKeyInputs(targetID, sourceID, entry.Predicate, c.logger) {
				return errs.WrapInvalid(errs.ErrInvalidData, "Component", "reconcileIncomingIndex",
					fmt.Sprintf("invalid incoming membership %s -> %s (%s)", sourceID, targetID, entry.Predicate))
			}
			desired[incomingIndexKey(targetID, sourceID, entry.Predicate)] = incomingIndexMarker
		}
	}
	return c.reconcileOwnedRows(ctx, "incoming", c.incomingBucket,
		incomingIndexSourceFilter(sourceID), desired, false)
}
