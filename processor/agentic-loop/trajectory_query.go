package agenticloop

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/agentic"
)

func maximumVisibleAttemptOrdinal(ctx context.Context, bucket trajectoryFactBucket, loopID string) (uint64, error) {
	if bucket == nil {
		return 0, nil
	}
	lister, err := bucket.ListKeysFiltered(ctx, agentic.TrajectoryFactPrefix(loopID)+">")
	if err != nil {
		return 0, fmt.Errorf("list trajectory fact prefix: %w", err)
	}
	defer lister.Stop()
	var maxOrdinal uint64
	for key := range lister.Keys() {
		entry, getErr := bucket.Get(ctx, key)
		if getErr != nil {
			return 0, fmt.Errorf("get trajectory fact %q: %w", key, getErr)
		}
		var fact agentic.TrajectoryFactV1
		if decodeErr := json.Unmarshal(entry.Value(), &fact); decodeErr != nil {
			return 0, fmt.Errorf("decode trajectory fact %q: %w", key, decodeErr)
		}
		if fact.LoopDigest != agentic.TrajectoryLoopDigest(loopID) {
			return 0, fmt.Errorf("trajectory fact %q loop digest mismatch", key)
		}
		if fact.AttemptOrdinal > maxOrdinal {
			maxOrdinal = fact.AttemptOrdinal
		}
	}
	return maxOrdinal, nil
}
