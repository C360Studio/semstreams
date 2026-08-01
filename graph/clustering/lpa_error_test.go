package clustering

import (
	"context"
	"errors"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FailingMockProvider simulates provider failures for error testing
type FailingMockProvider struct {
	failOn string // Which method to fail: "GetAllEntityIDs", "GetNeighbors", "GetEdgeWeight"
	err    error  // Error to return
}

func (m *FailingMockProvider) GetAllEntityIDs(_ context.Context) ([]string, error) {
	if m.failOn == "GetAllEntityIDs" {
		return nil, m.err
	}
	return []string{"A", "B", "C"}, nil
}

func (m *FailingMockProvider) GetNeighbors(_ context.Context, _ string, _ string) ([]string, error) {
	if m.failOn == "GetNeighbors" {
		return nil, m.err
	}
	return []string{}, nil
}

func (m *FailingMockProvider) GetEdgeWeight(_ context.Context, _ string, _ string) (float64, error) {
	if m.failOn == "GetEdgeWeight" {
		return 0, m.err
	}
	return 1.0, nil
}

// FailingMockStorage simulates storage failures for error testing
type FailingMockStorage struct {
	failOn string // Which method to fail
	err    error
}

func (m *FailingMockStorage) SaveCommunity(_ context.Context, _ *Community) error {
	if m.failOn == "SaveCommunity" {
		return m.err
	}
	return nil
}

func (m *FailingMockStorage) GetCommunity(_ context.Context, _ string) (*Community, error) {
	if m.failOn == "GetCommunity" {
		return nil, m.err
	}
	return nil, nil
}

func (m *FailingMockStorage) GetCommunitiesByLevel(_ context.Context, _ int) ([]*Community, error) {
	return []*Community{}, nil
}

func (m *FailingMockStorage) GetEntityCommunity(_ context.Context, _ string, _ int) (*Community, error) {
	return nil, nil
}

func (m *FailingMockStorage) DeleteCommunity(_ context.Context, _ string) error {
	return nil
}

func (m *FailingMockStorage) Prune(_ context.Context, _ []*Community) error {
	if m.failOn == "Prune" {
		return m.err
	}
	return nil
}

func (m *FailingMockStorage) Clear(_ context.Context) error {
	if m.failOn == "Clear" {
		return m.err
	}
	return nil
}

func (m *FailingMockStorage) GetAllCommunities(_ context.Context) ([]*Community, error) {
	if m.failOn == "GetAllCommunities" {
		return nil, m.err
	}
	return []*Community{}, nil
}

// Test provider failures
func TestLPADetector_ProviderGetAllEntityIDsError(t *testing.T) {
	provider := &FailingMockProvider{
		failOn: "GetAllEntityIDs",
		err:    errors.New("connection lost"),
	}
	storage := NewMockCommunityStorage()

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection lost")
}

func TestLPADetector_ProviderGetNeighborsError(t *testing.T) {
	provider := &FailingMockProvider{
		failOn: "GetNeighbors",
		err:    errors.New("network timeout"),
	}
	storage := NewMockCommunityStorage()

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network timeout")
}

// edgeWeightFailProvider returns a real neighbor from GetNeighbors but errors on
// GetEdgeWeight. It exists to exercise the computeNewLabel edge-weight seam:
// FailingMockProvider.GetNeighbors returns an empty set, so computeNewLabel never
// reaches GetEdgeWeight there.
type edgeWeightFailProvider struct {
	err error
}

func (p *edgeWeightFailProvider) GetAllEntityIDs(_ context.Context) ([]string, error) {
	return []string{"A", "B"}, nil
}

func (p *edgeWeightFailProvider) GetNeighbors(_ context.Context, entityID, _ string) ([]string, error) {
	// A and B are mutual neighbors, so computeNewLabel accumulates votes and calls
	// GetEdgeWeight for each.
	if entityID == "A" {
		return []string{"B"}, nil
	}
	return []string{"A"}, nil
}

func (p *edgeWeightFailProvider) GetEdgeWeight(_ context.Context, _, _ string) (float64, error) {
	return 0, p.err
}

// TestLPADetector_ProviderGetEdgeWeightErrorPropagates pins the gh#666 fail-open
// fix: now that GetEdgeWeight does topology I/O, a transient error must abort the
// cycle (retry next tick) rather than default to 1.0 — which would fabricate an
// explicit-DOMINANT edge from a KV blip and corrupt the partition.
func TestLPADetector_ProviderGetEdgeWeightErrorPropagates(t *testing.T) {
	provider := &edgeWeightFailProvider{err: errors.New("kv blip on edge weight")}
	storage := NewMockCommunityStorage()

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err, "an edge-weight error must NOT be swallowed into a fabricated 1.0")
	assert.Contains(t, err.Error(), "kv blip on edge weight")
}

// Test storage failures
// A prune failure is non-fatal by design: the partition is already persisted, so
// the index is correct and merely carries stale extras until the next cycle.
// (Replaces the old StorageClearError test — DetectCommunities no longer clears.)
func TestLPADetector_StoragePruneErrorIsNotFatal(t *testing.T) {
	provider := NewMockProvider()
	provider.AddEntity("A")
	provider.AddEntity("B")

	storage := &FailingMockStorage{
		failOn: "Prune",
		err:    errors.New("storage unavailable"),
	}

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	result, err := detector.DetectCommunities(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, result[0], "detection returns its partition even when the prune fails")
}

func TestLPADetector_StorageSaveError(t *testing.T) {
	provider := NewMockProvider()
	provider.AddEntity("A")
	provider.AddEntity("B")
	provider.AddEdge("A", "B", 1.0)

	storage := &FailingMockStorage{
		failOn: "SaveCommunity",
		err:    errors.New("storage full"),
	}

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage full")
}

// Test context cancellation
func TestLPADetector_ContextCancellation(t *testing.T) {
	provider := NewMockProvider()
	storage := NewMockCommunityStorage()

	// Create large graph to ensure multiple iterations
	for i := 0; i < 100; i++ {
		provider.AddEntity(string(rune('A' + i)))
	}

	detector := NewLPADetector(provider, storage)
	detector.WithMaxIterations(1000) // Ensure many iterations

	// Create context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context")
}

// Test nil provider/storage validation
func TestLPADetector_NilProvider(t *testing.T) {
	storage := NewMockCommunityStorage()

	detector := NewLPADetector(nil, storage)
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graphProvider is nil")
}

func TestLPADetector_NilStorage(t *testing.T) {
	provider := NewMockProvider()
	provider.AddEntity("A")

	detector := NewLPADetector(provider, nil)
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage is nil")
}

// Test input validation
func TestLPADetector_WithMaxIterations_Validation(t *testing.T) {
	provider := NewMockProvider()
	storage := NewMockCommunityStorage()

	detector := NewLPADetector(provider, storage)

	// Test negative value gets clamped to default
	detector.WithMaxIterations(-10)
	assert.Equal(t, DefaultMaxIterations, detector.maxIterations)

	// Test zero value gets clamped to default
	detector.WithMaxIterations(0)
	assert.Equal(t, DefaultMaxIterations, detector.maxIterations)

	// Test excessive value gets capped
	detector.WithMaxIterations(20000)
	assert.Equal(t, MaxIterationsLimit, detector.maxIterations)

	// Test valid value is preserved
	detector.WithMaxIterations(50)
	assert.Equal(t, 50, detector.maxIterations)
}

func TestLPADetector_WithLevels_Validation(t *testing.T) {
	provider := NewMockProvider()
	storage := NewMockCommunityStorage()

	detector := NewLPADetector(provider, storage)

	// Test negative value gets clamped to default
	detector.WithLevels(-5)
	assert.Equal(t, DefaultLevels, detector.levels)

	// Test zero value gets clamped to default
	detector.WithLevels(0)
	assert.Equal(t, DefaultLevels, detector.levels)

	// Test excessive value gets capped
	detector.WithLevels(20)
	assert.Equal(t, MaxLevelsLimit, detector.levels)

	// Test valid value is preserved
	detector.WithLevels(5)
	assert.Equal(t, 5, detector.levels)
}

// selectiveFailStorage fails SaveCommunity only for communities whose member
// count is at or above a threshold, and records everything that did save.
//
// This models gh#837 precisely: the member list is unbounded while the KV value
// carrying it is not, so ONE community can be unwritable while its siblings are
// fine. A mock that fails every save (FailingMockStorage) cannot express that
// case, and it is the case that mattered — semsource measured 2 communities
// surviving out of 11 because the first oversized one aborted the level.
type selectiveFailStorage struct {
	*MockCommunityStorage
	failAtOrAboveMembers int
	err                  error
}

// permanentOversizeErr is the error production actually returns for an
// over-ceiling community (storage.go). Tests must inject THIS shape, not a
// plain error: the detector skips only record-local PERMANENT rejections, so a
// bare errors.New would (correctly) propagate and the test would be asserting
// against a case that cannot occur.
func permanentOversizeErr() error {
	return errs.WrapInvalid(nats.ErrMaxPayload, "NATSCommunityStorage", "SaveCommunity",
		"community exceeds the NATS payload ceiling")
}

func (s *selectiveFailStorage) SaveCommunity(ctx context.Context, community *Community) error {
	if len(community.Members) >= s.failAtOrAboveMembers {
		return s.err
	}
	return s.MockCommunityStorage.SaveCommunity(ctx, community)
}

// TestLPADetector_OversizedCommunityDoesNotDiscardTheLevel pins gh#837's core
// fix: one unwritable community must not take its siblings down with it.
//
// Before the fix this returned an error and zero communities, because the
// persist loop returned on the first SaveCommunity failure and the caller
// returns on any error — so every community after it in the loop, and every
// higher level, was lost.
func TestLPADetector_OversizedCommunityDoesNotDiscardTheLevel(t *testing.T) {
	provider := NewMockProvider()
	// Two disconnected groups => two communities. One is deliberately larger.
	big := []string{"b1", "b2", "b3", "b4", "b5"}
	for i := 0; i < len(big); i++ {
		provider.AddEntity(big[i])
	}
	for i := 0; i+1 < len(big); i++ {
		provider.AddEdge(big[i], big[i+1], 1.0)
	}
	provider.AddEntity("s1")
	provider.AddEntity("s2")
	provider.AddEdge("s1", "s2", 1.0)

	storage := &selectiveFailStorage{
		MockCommunityStorage: NewMockCommunityStorage(),
		failAtOrAboveMembers: 5,
		err:                  permanentOversizeErr(),
	}

	detector := NewLPADetector(provider, storage)
	detector.WithLevels(1)

	result, err := detector.DetectCommunities(context.Background())

	// The level survives: detection succeeds and the small community persisted.
	require.NoError(t, err, "one unwritable community must not fail the whole detection pass")
	require.NotEmpty(t, result[0], "the writable community must still be returned")

	// Every returned community is one that actually persisted — the return value
	// must not advertise communities the store rejected, or a caller counts
	// communities that cannot be read back.
	for _, c := range result[0] {
		assert.Less(t, len(c.Members), 5,
			"a community that failed to save must not appear in the result")
		saved, ok := storage.communities[0][c.ID]
		assert.True(t, ok, "returned community %s was never persisted", c.ID)
		assert.NotNil(t, saved)
	}
}

// TestLPADetector_TotalSaveFailureStillErrors pins the other half of the gh#837
// decision. Degrading on a completely broken store would turn "the store is
// unreachable" into a silent "no communities found" — strictly worse than the
// abort it replaces, because zero results are indistinguishable from an empty
// graph. Partial results are useful; zero results with no error are a lie.
func TestLPADetector_TotalSaveFailureStillErrors(t *testing.T) {
	provider := NewMockProvider()
	provider.AddEntity("A")
	provider.AddEntity("B")
	provider.AddEdge("A", "B", 1.0)

	storage := &selectiveFailStorage{
		MockCommunityStorage: NewMockCommunityStorage(),
		failAtOrAboveMembers: 1, // every community fails
		err:                  permanentOversizeErr(),
	}

	detector := NewLPADetector(provider, storage)
	detector.WithLevels(1)

	_, err := detector.DetectCommunities(context.Background())
	require.Error(t, err, "a store that rejects EVERY community must still fail the pass")

	// The detector must NOT re-label the inner classification. errors.As finds
	// the outermost ClassifiedError first, so wrapping transient here would undo
	// the permanent classification storage.go establishes — one layer up, and
	// invisibly. This assertion is the detector-level half of problem 2.
	assert.False(t, errs.IsTransient(err),
		"an all-oversized failure is permanent at the DETECTOR boundary too, not just inside storage")
	assert.True(t, errs.IsInvalid(err), "inner permanent classification must survive the detector wrapper")
}

// transientFailStorage fails SaveCommunity for one named community with a
// TRANSIENT error, and records everything else normally.
type transientFailStorage struct {
	*MockCommunityStorage
	failMembersAtOrAbove int
}

func (s *transientFailStorage) SaveCommunity(ctx context.Context, community *Community) error {
	if len(community.Members) >= s.failMembersAtOrAbove {
		return errs.WrapTransient(errors.New("nats: connection lost"),
			"NATSCommunityStorage", "SaveCommunity", "put community")
	}
	return s.MockCommunityStorage.SaveCommunity(ctx, community)
}

// TestLPADetector_TransientSaveFailureDoesNotPruneOrReportSuccess is the
// regression the first draft of gh#838 was missing, and it is the one that
// mattered.
//
// Skipping EVERY save error — not just permanent ones — meant a momentary NATS
// blip produced an INCOMPLETE partition, which then drove the prune step. Prune
// derives its keep-set from the returned result and deletes prior valid keys for
// every member of the community that failed, so a transient blip became durable
// query-visible loss, reported as a successful cycle.
//
// It also broke the graph-clustering spec's non-destructive rebuild contract,
// which permits pruning precisely because "every community in the new partition
// is already persisted at that point".
func TestLPADetector_TransientSaveFailureDoesNotPruneOrReportSuccess(t *testing.T) {
	ctx := context.Background()
	provider := NewMockProvider()
	big := []string{"b1", "b2", "b3", "b4", "b5"}
	for _, e := range big {
		provider.AddEntity(e)
	}
	for i := 0; i+1 < len(big); i++ {
		provider.AddEdge(big[i], big[i+1], 1.0)
	}
	provider.AddEntity("s1")
	provider.AddEntity("s2")
	provider.AddEdge("s1", "s2", 1.0)

	backing := NewMockCommunityStorage()

	// Seed a PRIOR partition covering the members whose save will fail. This is
	// the state that must survive: it is valid, query-visible, and the only copy.
	priorID := "prior-community-holding-the-big-members"
	require.NoError(t, backing.SaveCommunity(ctx, &Community{
		ID: priorID, Level: 0, Members: big,
	}))

	storage := &transientFailStorage{MockCommunityStorage: backing, failMembersAtOrAbove: 5}
	detector := NewLPADetector(provider, storage)
	detector.WithLevels(1)

	_, err := detector.DetectCommunities(ctx)

	// 1. The cycle must FAIL, not report success — otherwise the component
	//    records activity and emits cycle-complete on an incomplete partition.
	// assert, not require: the prune damage below is a SEPARATE symptom, and a
	// failing run should show both rather than stopping at the first.
	assert.Error(t, err, "a transient save failure must fail the detection run, not be skipped")
	assert.True(t, errs.IsTransient(err), "a connection blip must stay transient so the next cycle retries it")

	// 2. The prior partition must be INTACT. This is the destructive half: if the
	//    run continued, prune would have deleted these keys because the failed
	//    community was absent from the keep-set.
	_, stillThere := backing.communities[0][priorID]
	assert.True(t, stillThere,
		"prior valid community was pruned away by an incomplete partition — the exact data loss this guards")
	for _, m := range big {
		assert.Equal(t, priorID, backing.entityCommunity[0][m],
			"prior entity->community mapping for %s was destroyed", m)
	}
}
