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
	semtypes "github.com/c360studio/semstreams/pkg/types"
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
	// Validate the complete filter and every desired physical key before opening
	// a lister or mutating the bucket so a
	// failed proof is side-effect free and preserves the shared classified error.
	if err := natsclient.ValidateKVWildcardFilter(ownerFilter); err != nil {
		return err
	}
	desiredKeys := make([]string, 0, len(desired))
	for key := range desired {
		desiredKeys = append(desiredKeys, key)
	}
	sort.Strings(desiredKeys)
	for _, key := range desiredKeys {
		if err := natsclient.ValidateKVLiteralKey(key); err != nil {
			return err
		}
	}

	existingKeys, err := bucket.KeysByFilter(ctx, ownerFilter)
	if c.metrics != nil {
		c.metrics.recordKVOperation("list", indexName)
		c.metrics.recordReconcileOperation(indexName, "list", err)
	}
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.WrapTransient(err, "Component", "reconcileOwnedRows",
			fmt.Sprintf("list %s rows with %q", indexName, ownerFilter))
	}

	existing := make(map[string]struct{}, len(existingKeys))
	for _, key := range existingKeys {
		if err := natsclient.ValidateKVLiteralKey(key); err != nil {
			return err
		}
		existing[key] = struct{}{}
	}

	stale := make([]string, 0, len(existing))
	for key := range existing {
		if _, keep := desired[key]; !keep {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)

	var failures []error
	for _, key := range stale {
		delErr := bucket.Delete(ctx, key)
		if natsclient.IsKVNotFoundError(delErr) {
			delErr = nil
		}
		if c.metrics != nil {
			c.metrics.recordKVOperation("delete", indexName)
			c.metrics.recordReconcileOperation(indexName, "delete", delErr)
		}
		if delErr != nil {
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
		_, putErr := bucket.Put(ctx, key, desired[key])
		if c.metrics != nil {
			c.metrics.recordKVOperation("put", indexName)
			c.metrics.recordReconcileOperation(indexName, "put", putErr)
		}
		if putErr != nil {
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
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return err
	}
	desired := make(map[string][]byte, len(predicates))
	for predicate := range predicates {
		if _, err := vocabulary.ParsePredicate(predicate); err != nil {
			return errs.WrapInvalid(err, "Component", "reconcilePredicateIndex", "invalid predicate")
		}
		desired[predicateIndexKey(predicate, entityID)] = predicateIndexMarker
	}
	return c.reconcileOwnedRows(ctx, "predicate", c.predicateBucket,
		predicateIndexEntityFilter(entityID), desired, false)
}

func (c *Component) reconcileIncomingIndex(
	ctx context.Context,
	sourceID string,
	incomingByTarget map[string][]graph.IncomingEntry,
) error {
	if err := semtypes.ValidateEntityID(sourceID); err != nil {
		return err
	}
	desired := make(map[string][]byte)
	for targetID, entries := range incomingByTarget {
		if err := semtypes.ValidateEntityID(targetID); err != nil {
			return err
		}
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
