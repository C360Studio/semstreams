package clustering

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetCountingProvider wraps MockProvider and counts ResetEdgeCache calls,
// implementing the edgeCacheResetter seam the detector invokes at each cycle
// boundary (gh#666).
type resetCountingProvider struct {
	*MockProvider
	resets atomic.Int64
}

func (p *resetCountingProvider) ResetEdgeCache() { p.resets.Add(1) }

// TestDetectCommunities_ResetsEdgeCachePerCycle pins the gh#666 lifetime seam: the
// detector drops the provider's per-cycle explicit-edge cache exactly once at the
// start of each DetectCommunities run, so a build-once cache cannot go stale
// across cycles. Providers without the cache are simply skipped (no interface).
func TestDetectCommunities_ResetsEdgeCachePerCycle(t *testing.T) {
	mock := NewMockProvider()
	mock.AddEntity("A")
	mock.AddEntity("B")
	mock.AddEdge("A", "B", 1.0)

	provider := &resetCountingProvider{MockProvider: mock}
	detector := NewLPADetector(provider, NewMockCommunityStorage())
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), provider.resets.Load(), "one cache reset at the cycle boundary")

	_, err = detector.DetectCommunities(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), provider.resets.Load(), "each subsequent cycle resets again")
}
