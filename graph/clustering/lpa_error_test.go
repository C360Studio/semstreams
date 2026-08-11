package clustering

import (
	"context"
	"errors"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"

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
	pruneCalls           int
	deleteCalls          int
	saveAttempts         []*Community
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
	s.saveAttempts = append(s.saveAttempts, community)
	if len(community.Members) >= s.failAtOrAboveMembers {
		return s.err
	}
	return s.MockCommunityStorage.SaveCommunity(ctx, community)
}

func (s *selectiveFailStorage) Prune(ctx context.Context, keep []*Community) error {
	s.pruneCalls++
	s.deleteCalls++ // Prune owns the storage-layer Delete path.
	return s.MockCommunityStorage.Prune(ctx, keep)
}

// TestLPADetector_PermanentPartialCandidateSavesSiblingsButDoesNotPrune pins
// #855's complete-candidate boundary without weakening #837: a permanently
// rejected record does not prevent writable siblings at the same level from
// persisting, but any rejected record makes the run incomplete. The detector
// must preserve the classified error, stop before higher levels, and never hand
// the partial candidate to Prune.
func TestLPADetector_PermanentPartialCandidateSavesSiblingsButDoesNotPrune(t *testing.T) {
	ctx := context.Background()
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

	backing := NewMockCommunityStorage()
	priorID := "prior-overlapping-community"
	require.NoError(t, backing.SaveCommunity(ctx, &Community{
		ID: priorID, Level: 0, Members: []string{"s1", "prior-only"},
	}))
	storage := &selectiveFailStorage{
		MockCommunityStorage: backing,
		failAtOrAboveMembers: 5,
		err:                  permanentOversizeErr(),
	}

	detector := NewLPADetector(provider, storage)
	detector.WithLevels(3)

	result, err := detector.DetectCommunities(ctx)

	require.Error(t, err, "a partial candidate must not report a complete detection pass")
	assert.True(t, errs.IsInvalid(err), "the existing permanent classification must survive wrapping")
	assert.Nil(t, result, "an incomplete candidate must not be returned as a successful partition")
	assert.Zero(t, storage.pruneCalls, "Prune must not receive an incomplete candidate")
	assert.Zero(t, storage.deleteCalls, "no prune-driven deletion path may run")

	for _, attempted := range storage.saveAttempts {
		assert.Zero(t, attempted.Level, "an incomplete lower level must not construct a higher level")
	}

	// The writable sibling did persist and was allowed to overwrite the prior
	// mapping. #855 promises no prune/deletion and no false completion, not
	// rollback or an unmixed prior projection.
	assert.NotEqual(t, priorID, backing.entityCommunity[0]["s1"],
		"a successful sibling write may overwrite an overlapping prior mapping")
	_, priorStillPresent := backing.communities[0][priorID]
	assert.True(t, priorStillPresent, "without prune the prior community record remains visible")
	_, priorOnlyStillMapped := backing.entityCommunity[0]["prior-only"]
	assert.True(t, priorOnlyStillMapped, "without prune prior-only mappings remain visible")
}

// partialMappingFailStorage models SaveCommunity's non-atomic write shape: the
// community record and an early entity mapping land before a later mapping Put
// fails. The earlier writes are intentionally retained as a mixed projection.
type partialMappingFailStorage struct {
	*MockCommunityStorage
	pruneCalls  int
	deleteCalls int
}

func (s *partialMappingFailStorage) SaveCommunity(_ context.Context, community *Community) error {
	if s.communities[community.Level] == nil {
		s.communities[community.Level] = make(map[string]*Community)
	}
	s.communities[community.Level][community.ID] = community
	if s.entityCommunity[community.Level] == nil {
		s.entityCommunity[community.Level] = make(map[string]string)
	}
	if len(community.Members) > 0 {
		s.entityCommunity[community.Level][community.Members[0]] = community.ID
	}
	return errs.WrapTransient(errors.New("nats: mapping put failed"),
		"NATSCommunityStorage", "SaveCommunity", "put entity mapping")
}

func (s *partialMappingFailStorage) Prune(ctx context.Context, keep []*Community) error {
	s.pruneCalls++
	s.deleteCalls++ // Prune owns the storage-layer Delete path.
	return s.MockCommunityStorage.Prune(ctx, keep)
}

func TestLPADetector_PartialMappingWriteDoesNotPrune(t *testing.T) {
	ctx := context.Background()
	provider := NewMockProvider()
	provider.AddEntity("A")
	provider.AddEntity("B")
	provider.AddEdge("A", "B", 1.0)

	storage := &partialMappingFailStorage{MockCommunityStorage: NewMockCommunityStorage()}
	detector := NewLPADetector(provider, storage).WithLevels(1)

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.True(t, errs.IsTransient(err), "the existing mapping-write classification must survive wrapping")
	assert.Zero(t, storage.pruneCalls, "partial mapping state must never drive prune")
	assert.Zero(t, storage.deleteCalls, "partial mapping state must never drive deletion")

	require.Len(t, storage.communities[0], 1,
		"the community record may remain after a later mapping write fails")
	require.Len(t, storage.entityCommunity[0], 1,
		"an earlier mapping may remain after a later mapping write fails")
	for entityID, communityID := range storage.entityCommunity[0] {
		assert.NotEmpty(t, entityID)
		assert.NotEmpty(t, communityID)
		assert.Contains(t, storage.communities[0], communityID)
	}
}

// TestLPADetector_TotalSaveFailureStillErrors retains the all-permanent
// classification case alongside the partial-candidate regression above. No
// rejected candidate is successful; same-level sibling attempts only determine
// which writes may remain visible before the classified error returns.
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
