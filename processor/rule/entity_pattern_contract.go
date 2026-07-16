package rule

import (
	"errors"
	"fmt"
	"sort"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/errs"
	entitytypes "github.com/c360studio/semstreams/pkg/types"
)

// ErrorCodeEntityWatchBucketUnsupported identifies a foreign bucket supplied
// to the rule processor's typed EntityState watcher.
const ErrorCodeEntityWatchBucketUnsupported = "entity_watch_bucket_unsupported"

var errEntityWatchBucketUnsupported = errors.New("rule EntityState watcher supports only ENTITY_STATES; typed operational-KV rule adapters require a separately designed decoder and evaluator")

func validateEntityWatchBuckets(buckets map[string][]string) error {
	for _, bucket := range sortedWatchBucketNames(buckets) {
		patterns := buckets[bucket]
		seen := make(map[string]int, len(patterns))
		for index, pattern := range patterns {
			if firstIndex, duplicate := seen[pattern]; duplicate {
				return fmt.Errorf(
					"entity_watch_buckets[%q][%d]: duplicate pattern %q (first declared at index %d)",
					bucket, index, pattern, firstIndex,
				)
			}
			seen[pattern] = index
			if err := validateEntityWatchPattern(bucket, pattern); err != nil {
				return fmt.Errorf("entity_watch_buckets[%q][%d]: %w", bucket, index, err)
			}
		}
	}
	return nil
}

func validateEntityWatchPattern(bucket, pattern string) error {
	if bucket != gtypes.BucketEntityStates {
		return unsupportedEntityWatchBucket(bucket)
	}
	if err := entitytypes.ValidateEntityIDPattern(pattern); err != nil {
		return fmt.Errorf("invalid ENTITY_STATES entity ID pattern %q: %w", pattern, err)
	}
	return nil
}

func validateDefinitionEntityPattern(def Definition) error {
	for _, bucket := range def.Entity.WatchBuckets {
		if bucket != gtypes.BucketEntityStates {
			return fmt.Errorf("rule %s entity.watch_buckets: %w", def.ID, unsupportedEntityWatchBucket(bucket))
		}
	}
	if def.Entity.Pattern == "" {
		if len(def.Entity.WatchBuckets) > 0 {
			return fmt.Errorf("rule %s entity.pattern is required when entity.watch_buckets is declared", def.ID)
		}
		return nil
	}
	if err := entitytypes.ValidateEntityIDPattern(def.Entity.Pattern); err != nil {
		return fmt.Errorf("rule %s entity.pattern for ENTITY_STATES: %w", def.ID, err)
	}
	return nil
}

func unsupportedEntityWatchBucket(bucket string) error {
	return errs.ClassifiedCodeDetail(
		errs.ErrorInvalid,
		ErrorCodeEntityWatchBucketUnsupported,
		map[string]any{
			"bucket":           bucket,
			"supported_bucket": gtypes.BucketEntityStates,
		},
		errEntityWatchBucketUnsupported,
	)
}

func parseEntityWatchBuckets(value any) (map[string][]string, error) {
	switch buckets := value.(type) {
	case map[string][]string:
		return buckets, nil
	case map[string]any:
		parsed := make(map[string][]string, len(buckets))
		keys := make([]string, 0, len(buckets))
		for bucket := range buckets {
			keys = append(keys, bucket)
		}
		sort.Strings(keys)
		for _, bucket := range keys {
			rawPatterns := buckets[bucket]
			switch patterns := rawPatterns.(type) {
			case []string:
				parsed[bucket] = patterns
			case []any:
				parsed[bucket] = make([]string, len(patterns))
				for index, rawPattern := range patterns {
					pattern, ok := rawPattern.(string)
					if !ok {
						return nil, fmt.Errorf("entity_watch_buckets[%q][%d] must be string, got %T", bucket, index, rawPattern)
					}
					parsed[bucket][index] = pattern
				}
			default:
				return nil, fmt.Errorf("entity_watch_buckets[%q] must be array of strings, got %T", bucket, rawPatterns)
			}
		}
		return parsed, nil
	default:
		return nil, fmt.Errorf("entity_watch_buckets must be an object, got %T", value)
	}
}

func sortedWatchBucketNames(buckets map[string][]string) []string {
	names := make([]string, 0, len(buckets))
	for bucket := range buckets {
		names = append(names, bucket)
	}
	sort.Strings(names)
	return names
}
