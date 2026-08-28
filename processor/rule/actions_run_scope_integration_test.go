//go:build integration

// Task 2.6 (entity-id-segment-semantics, slice B) — #1096. The rule engine
// mints runtime state under the DEPLOYMENT's own authority (deps.Platform),
// never read back from the firing entity, and it never writes to an imported
// firing loop: the run -> loop linkage lives on the LOCAL run entity as
// agent.run.origin-entity-id, and the two anchor writes on the firing loop are
// skipped deliberately and counted (ADR-102; design §C.3).
//
// This drives the assembled system: a real graph-ingest over real NATS as the
// graph.mutation.> provider, a real lifecycle.Manager minting through it, the
// real tripleMutator, and the production executor.Execute entry point. The
// recording mutator DELEGATES to the real one, so "no captured AddTriple names
// the import" and "the import's revision is unchanged" are two independent
// readings of the same fact.

package rule

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/agentrun"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

const (
	runScopeOrg      = "acme"
	runScopePlatform = "dep1"

	// runScopeImportedLoopID is a PEER deployment's own loop execution, of the
	// one family run_scope=new admits. Any other entity would take the
	// warn-and-inherit fallback and prove nothing about authority.
	runScopeImportedLoop = "foreign.dep9.agentic-loop.agent.execution.import1"
	runScopeImportedUUID = "import1"

	runScopeLocalUUID = "local1"
)

// recordingTripleMutator captures every AddTriple subject and forwards to the
// real mutator, so a test can assert both what was attempted and what landed.
type recordingTripleMutator struct {
	inner TripleMutator

	mu    sync.Mutex
	added []message.Triple
}

func (r *recordingTripleMutator) AddTriple(ctx context.Context, ruleID string, triple message.Triple) (uint64, error) {
	r.mu.Lock()
	r.added = append(r.added, triple)
	r.mu.Unlock()
	return r.inner.AddTriple(ctx, ruleID, triple)
}

func (r *recordingTripleMutator) RemoveTriple(ctx context.Context, ruleID, subject, predicate string) (uint64, error) {
	return r.inner.RemoveTriple(ctx, ruleID, subject, predicate)
}

func (r *recordingTripleMutator) subjects() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.added))
	for _, triple := range r.added {
		out = append(out, triple.Subject)
	}
	return out
}

func (r *recordingTripleMutator) snapshot() []message.Triple {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]message.Triple(nil), r.added...)
}

// runAnchorTestMetrics is a fresh, unregistered *Metrics carrying only the
// counter this file exercises — the isolation pattern of
// actionFailuresTestMetrics (action_failure_metrics_test.go).
func runAnchorTestMetrics() *Metrics {
	return &Metrics{
		runAnchorSkippedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "test_rule_run_anchor_skipped_total",
		}, []string{"reason"}),
	}
}

type runScopeHarness struct {
	ctx      context.Context
	executor *ActionExecutor
	mutator  *recordingTripleMutator
	metrics  *Metrics
	manager  *lifecycle.Manager
	bucket   *natsclient.KVStore
	pub      *mockPublisher
}

func newRunScopeHarness(t *testing.T) *runScopeHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())

	testClient := natsclient.NewTestClient(t,
		natsclient.WithKV(),
		natsclient.WithStreams(natsclient.TestStreamConfig{Name: "ENTITY", Subjects: []string{"entity.>"}}),
	)

	rawConfig, err := json.Marshal(graphingest.DefaultConfig())
	require.NoError(t, err)
	created, err := graphingest.CreateGraphIngest(rawConfig, component.Dependencies{
		NATSClient:      testClient.Client,
		PayloadRegistry: payloadbuiltins.NewTestRegistry(t),
		Platform:        component.PlatformMeta{Org: runScopeOrg, Platform: runScopePlatform},
	})
	require.NoError(t, err)
	ingest := created.(*graphingest.Component)
	require.NoError(t, ingest.Initialize())
	require.NoError(t, ingest.Start(ctx))
	require.NoError(t, testClient.GetNativeConnection().Flush())
	t.Cleanup(func() {
		_ = ingest.Stop(context.Background())
		cancel()
	})

	bucket, err := graph.EnsureCatalogBucket(ctx, testClient.Client, graph.BucketEntityStates)
	require.NoError(t, err)

	manager := lifecycle.NewManager(testClient.Client, nil)
	require.NoError(t, agentrun.Register(manager))

	tracker := &Processor{ownRevisions: make(map[ruleRevKey]map[uint64]time.Time)}
	mutator := &recordingTripleMutator{inner: newTripleMutator(testClient.Client, tracker)}
	pub := &mockPublisher{}

	executor := NewActionExecutorComplete(nil, mutator, pub, nil)
	executor.SetLifecycleManager(manager)
	executor.setPlatform(component.PlatformMeta{Org: runScopeOrg, Platform: runScopePlatform})
	metrics := runAnchorTestMetrics()
	executor.setMetrics(metrics)

	return &runScopeHarness{
		ctx: ctx, executor: executor, mutator: mutator, metrics: metrics,
		manager: manager, bucket: testClient.Client.NewKVStore(bucket), pub: pub,
	}
}

// seedImportedLoop writes the peer's loop execution straight into
// ENTITY_STATES. It is a FIXTURE for an already-mirrored import: this
// harness declares no import lane, and graph-ingest would (correctly)
// refuse the foreign subject on a local one. The import path itself is
// proven by TestImportLaneAcceptsForeignRejectsLocalClaim.
func (h *runScopeHarness) seedImportedLoop(t *testing.T) uint64 {
	t.Helper()
	encoded, err := graph.MarshalEntityState(&graph.EntityState{
		ID:          runScopeImportedLoop,
		MessageType: agentic.LoopExecutionMessageType(),
		Version:     1,
		UpdatedAt:   time.Now(),
	})
	require.NoError(t, err)
	revision, err := h.bucket.Create(h.ctx, runScopeImportedLoop, encoded)
	require.NoError(t, err)
	return revision
}

func (h *runScopeHarness) runScopeNewAction() Action {
	return Action{
		Type:     ActionTypePublishAgent,
		Subject:  "agent.task.researcher",
		Role:     "researcher",
		Model:    "mock-model",
		Prompt:   "investigate",
		RunScope: "new",
	}
}

func (h *runScopeHarness) storedTriples(t *testing.T, entityID string) []message.Triple {
	t.Helper()
	entry, err := h.bucket.Get(h.ctx, entityID)
	require.NoError(t, err)
	var state graph.EntityState
	require.NoError(t, graph.UnmarshalEntityState(entry.Value, &state))
	return state.Triples
}

func objectFor(triples []message.Triple, predicate string) (any, bool) {
	for _, triple := range triples {
		if triple.Predicate == predicate {
			return triple.Object, true
		}
	}
	return nil, false
}

// TestRunScopeNewOnImportedLoopLinksLocallyWithoutForeignWrite is #1096's
// binding case: the run is minted under acme.dep1 (NOT foreign.dep9), it
// carries agent.run.origin-entity-id naming the import, and nothing at all is
// written to the import.
func TestRunScopeNewOnImportedLoopLinksLocallyWithoutForeignWrite(t *testing.T) {
	h := newRunScopeHarness(t)
	revisionBefore := h.seedImportedLoop(t)

	err := h.executor.Execute(h.ctx, h.runScopeNewAction(), &ExecutionContext{EntityID: runScopeImportedLoop})
	require.NoError(t, err)

	// The run is minted under THIS deployment's authority, keyed by the firing
	// loop's bare id — never under foreign.dep9.
	runEntityID := agentic.ChainExecutionEntityID(runScopeOrg, runScopePlatform, runScopeImportedUUID)
	assert.Equal(t, "acme.dep1.chain.agent.execution.import1", runEntityID)

	runTriples := h.storedTriples(t, runEntityID)
	origin, ok := objectFor(runTriples, agvocab.RunOriginEntityID)
	require.True(t, ok, "the local run must carry %s so the run->loop pointer never depends on writing the loop",
		agvocab.RunOriginEntityID)
	assert.Equal(t, runScopeImportedLoop, origin)

	// Not one mutation request named the foreign subject.
	assert.NotContains(t, h.mutator.subjects(), runScopeImportedLoop,
		"no mutation request may target a foreign-authority subject, not even a rejected one")

	entry, err := h.bucket.Get(h.ctx, runScopeImportedLoop)
	require.NoError(t, err)
	assert.Equal(t, revisionBefore, entry.Revision,
		"the import is a read-only mirror; its revision must not move")

	assert.InDelta(t, 1.0, testutil.ToFloat64(
		h.metrics.runAnchorSkippedTotal.WithLabelValues(semtypes.EntityIDReasonForeignAuthority)), 0.0001,
		"the skip is COUNTED, not silent and not a rejection")
}

// TestRunScopeNewOnLocalLoopStampsAnchorAndOrigin is the other half: on a local
// firing loop the anchor pair is still stamped, and the origin predicate is set
// for a local origin too — one home for the linkage, in both cases.
func TestRunScopeNewOnLocalLoopStampsAnchorAndOrigin(t *testing.T) {
	h := newRunScopeHarness(t)

	localLoop := agentic.LoopExecutionEntityID(runScopeOrg, runScopePlatform, runScopeLocalUUID)
	encoded, err := graph.MarshalEntityState(&graph.EntityState{
		ID:          localLoop,
		MessageType: agentic.LoopExecutionMessageType(),
		Version:     1,
		UpdatedAt:   time.Now(),
	})
	require.NoError(t, err)
	_, err = h.bucket.Create(h.ctx, localLoop, encoded)
	require.NoError(t, err)

	require.NoError(t, h.executor.Execute(h.ctx, h.runScopeNewAction(), &ExecutionContext{EntityID: localLoop}))

	runEntityID := agentic.ChainExecutionEntityID(runScopeOrg, runScopePlatform, runScopeLocalUUID)
	runTriples := h.storedTriples(t, runEntityID)
	origin, ok := objectFor(runTriples, agvocab.RunOriginEntityID)
	require.True(t, ok, "a local origin gets the same predicate as an imported one")
	assert.Equal(t, localLoop, origin)

	loopTriples := h.storedTriples(t, localLoop)
	runID, ok := objectFor(loopTriples, agvocab.LoopRun)
	require.True(t, ok, "a LOCAL firing loop still receives the run anchor")
	assert.Equal(t, runScopeLocalUUID, runID)
	runEntity, ok := objectFor(loopTriples, agvocab.LoopRunEntityID)
	require.True(t, ok, "a LOCAL firing loop still receives the run entity anchor")
	assert.Equal(t, runEntityID, runEntity)

	assert.InDelta(t, 0.0, testutil.ToFloat64(
		h.metrics.runAnchorSkippedTotal.WithLabelValues(semtypes.EntityIDReasonForeignAuthority)), 0.0001,
		"nothing is skipped when the firing loop carries the deployment's own authority")

	_ = h.mutator.snapshot()
}
