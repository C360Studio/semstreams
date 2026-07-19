package rule

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHealthAndDataFlowAreRaceFreeUnderConcurrentReads pins the gh#566 fix:
// Health() and DataFlow() were mutating their shared caches (rp.health,
// rp.flowMetrics) while holding only the READ lock, which admits concurrent
// holders — the ComponentManager health-publish loop and any health query
// race on those writes. Both are now pure getters; this test fails under
// -race against the old shape.
func TestHealthAndDataFlowAreRaceFreeUnderConcurrentReads(t *testing.T) {
	cfg, err := NewConfig("gh566-race")
	require.NoError(t, err)
	rp, err := NewProcessor(nil, &cfg)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				_ = rp.Health()
				_ = rp.DataFlow()
			}
		}()
	}
	wg.Wait()
}
