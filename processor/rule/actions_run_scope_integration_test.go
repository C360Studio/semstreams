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
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

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

	// runScopeImportedLoop is a PEER deployment's own loop execution, of the
	// one family run_scope=new admits. Any other entity would take the
	// warn-and-inherit fallback and prove nothing about authority. Its instance
	// segment is a canonical UUID because that is what a loop instance token is
	// (ADR-105, #1192) — publish_agent validates the substituted task, and Mint
	// refuses a non-canonical firing-loop instance.
	runScopeImportedLoop = "foreign.dep9.agentic-loop.agent.execution." + runScopeImportedUUID
	runScopeImportedUUID = "d7e8f901-2a3b-4c4d-8e5f-60718293a4b5"

	runScopeLocalUUID = "e8f90123-3b4c-4d5e-9f60-718293a4b5c6"
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

type runScopeHarness struct {
	ctx      context.Context
	executor *ActionExecutor
	mutator  *recordingTripleMutator
	metrics  *Metrics
	manager  *lifecycle.Manager
	bucket   *natsclient.KVStore
	pub      *mockPublisher
	logs     *capturingHandler
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

	logs := &capturingHandler{}
	executor := NewActionExecutorComplete(slog.New(logs), mutator, pub, nil,
		component.PlatformMeta{Org: runScopeOrg, Platform: runScopePlatform})
	executor.SetLifecycleManager(manager)
	metrics := foreignFiringSkipTestMetrics()
	executor.setMetrics(metrics)

	return &runScopeHarness{
		ctx: ctx, executor: executor, mutator: mutator, metrics: metrics,
		manager: manager, bucket: testClient.Client.NewKVStore(bucket), pub: pub, logs: logs,
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
	assert.Equal(t, "acme.dep1.chain.agent.execution."+runScopeImportedUUID, runEntityID)

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
		h.metrics.foreignFiringWritesSkippedTotal.WithLabelValues(semtypes.EntityIDReasonForeignAuthority)), 0.0001,
		"the skip is COUNTED, not silent and not a rejection")
}

// runScopeNewForEachAction fans the same run_scope=new dispatch over a list-typed
// triple on the firing entity. It exists because the counter's documented
// cardinality is a claim about the FAN-OUT, and runScopeNewAction (no ForEach)
// can only ever observe a single dispatch.
func (h *runScopeHarness) runScopeNewForEachAction() Action {
	a := h.runScopeNewAction()
	a.Prompt = "investigate $subtopic"
	a.ForEach = "$entity.triple.coordinator.decision.subtopics"
	a.ForEachVar = "subtopic"
	return a
}

// TestRunScopeNewForEachOnOneImportCountsPerDispatchNotPerEntity pins the ONE
// thing the single-dispatch tests above structurally cannot see: what the skip
// counter's unit actually is.
//
// executePublishAgent passes the SAME ExecutionContext to every `for_each`
// iteration — only the item varies — and publishAgentOnce reads entityID from
// it, so the firing entity is invariant across the fan-out. Three items on ONE
// imported loop therefore produce THREE increments for ONE declined entity.
// That is the reading operators must not get wrong: the counter counts declined
// DISPATCHES, and over a fanning rule pack it exceeds the number of distinct
// declined entities by the fan-out factor. The spec, the metric doc comment and
// the migration note all previously said "N imported entities"; this test is
// what makes that class of error fail loudly rather than archive as truth.
//
// Two mutation checks, because the scenario makes two claims:
//
//   - The COUNT is per dispatch: promote foreignFiringSkipRecorder's skipped
//     accumulator to a field on ActionExecutor (which converts per-dispatch into
//     per-action for a single Execute call). This test fails 1 != 3;
//     TestRunScopeNewOnImportedLoopLinksLocallyWithoutForeignWrite still passes,
//     because one dispatch cannot tell the two units apart.
//   - The ENTITY is invariant: derive `entityID` per iteration in
//     publishAgentOnce (`entityID += "-" + iterVarValue`), so the firing entity
//     really does vary across the fan-out. The count, the publish tally and the
//     import's revision are ALL unmoved by that — 3 is still 3 — which is why
//     the invariance clause needs its own evidence. task.RunID is the firing
//     loop's bare id, so decoding what each dispatch carried is that evidence:
//     under the mutant the three dispatches carry import1-hydraulics,
//     import1-pneumatics, import1-electrics and this test fails. Prefix or
//     substring checks on TaskID do NOT discriminate — `rule-<id>-hydraulics-<ns>`
//     still carries the `rule-<id>-` prefix.
func TestRunScopeNewForEachOnOneImportCountsPerDispatchNotPerEntity(t *testing.T) {
	h := newRunScopeHarness(t)
	revisionBefore := h.seedImportedLoop(t)

	// One firing entity, three items. The list rides the firing entity as the
	// JSON-encoded triple Object the decide tool emits.
	ec := &ExecutionContext{
		EntityID: runScopeImportedLoop,
		Entity: &graph.EntityState{
			ID: runScopeImportedLoop,
			Triples: []message.Triple{{
				Subject:   runScopeImportedLoop,
				Predicate: "coordinator.decision.subtopics",
				Object:    `["hydraulics","pneumatics","electrics"]`,
			}},
		},
	}

	require.NoError(t, h.executor.Execute(h.ctx, h.runScopeNewForEachAction(), ec))

	require.Len(t, h.pub.published, 3, "the fan-out must actually dispatch three times")

	assert.InDelta(t, 3.0, testutil.ToFloat64(
		h.metrics.foreignFiringWritesSkippedTotal.WithLabelValues(semtypes.EntityIDReasonForeignAuthority)), 0.0001,
		"one increment per DISPATCH: 3 for_each items on ONE imported firing entity is 3, not 1 — "+
			"and it is 3 declined dispatches, not 3 declined entities")

	// The invariance the scenario asserts, read off what each dispatch actually
	// carried: task.RunID is the firing loop's bare id, derived from the firing
	// entity, so three dispatches carrying the same RunID is three dispatches
	// declined for ONE entity. Decoded through the production decoder, not a
	// shape cast.
	decoder := newActionsTestDecoder(t)
	for index, published := range h.pub.published {
		baseMsg, decodeErr := decoder.Decode(published.data)
		require.NoError(t, decodeErr, "dispatch %d", index)
		task, isTask := baseMsg.Payload().(*agentic.TaskMessage)
		require.True(t, isTask, "dispatch %d: expected *agentic.TaskMessage, got %T", index, baseMsg.Payload())
		assert.Equal(t, runScopeImportedUUID, task.RunID,
			"dispatch %d must carry the SAME firing loop as every other: the fan-out varies the ITEM, "+
				"never the firing entity, which is what makes 3 increments ONE declined entity", index)
	}

	// One Info line per dispatch too, matching the counter — the requirement says
	// "ONE Info log per dispatch", and a single-dispatch test cannot see it.
	assert.Len(t, h.logs.withMessage(foreignFiringSkipLogMessage), 3,
		"the log's unit is the counter's unit: three declined dispatches, three lines")

	// The other half of the same fact: however many times it was declined, only
	// one entity was ever in play, and nothing was written to it.
	assert.NotContains(t, h.mutator.subjects(), runScopeImportedLoop,
		"no dispatch in the fan-out may target the foreign subject")
	entry, err := h.bucket.Get(h.ctx, runScopeImportedLoop)
	require.NoError(t, err)
	assert.Equal(t, revisionBefore, entry.Revision,
		"the single import is a read-only mirror across every iteration")
}

// TestForeignFiringSkipLogNamesEveryDeclinedWrite pins the operator half of the
// requirement — "ONE Info log per dispatch naming EVERY write that dispatch skipped" — which the
// counter assertions above cannot see.
//
// Under run_scope=new on an imported loop, THREE framework writes are declined
// across TWO decision points: the anchor pair before publication, and
// rule.task.spawned after it. The line was emitted at the first decision and a
// latch swallowed the second, so it named only the anchors. rule.task.spawned is
// exactly the write an operator debugging "$entity.triple.rule.spawned_task
// never fires" is looking for, and the migration note tells them this line is
// the only signal — a line that omits it sends them looking for a write that
// never happened.
//
// Mutation check: emit the line inside record (at the first declined write)
// instead of in the deferred flush. This test fails on the missing
// rule.task.spawned; every counter assertion in this file still passes, because
// the counter's unit never changed.
func TestForeignFiringSkipLogNamesEveryDeclinedWrite(t *testing.T) {
	h := newRunScopeHarness(t)
	h.seedImportedLoop(t)

	require.NoError(t, h.executor.Execute(h.ctx, h.runScopeNewAction(),
		&ExecutionContext{EntityID: runScopeImportedLoop}))
	require.Len(t, h.pub.published, 1,
		"a skip is not a rejection: the agent task still dispatches")

	lines := h.logs.withMessage(foreignFiringSkipLogMessage)
	require.Len(t, lines, 1, "one line per DISPATCH, the same unit the counter uses")
	assert.Equal(t, slog.LevelInfo, lines[0].level, "a deliberate skip is Info, not Warn or Error")

	skipped := lines[0].attrs["skipped"]
	assert.Contains(t, skipped, agvocab.LoopRun, "the run anchor was declined and must be named")
	assert.Contains(t, skipped, agvocab.LoopRunEntityID, "so was its reciprocal")
	assert.Contains(t, skipped, "rule.task.spawned",
		"the back-reference is declined on this same dispatch; naming only the anchors understates the set")
	assert.Contains(t, skipped, agvocab.RunOriginEntityID,
		"and the line says where the linkage went instead")
	assert.NotContains(t, skipped, runScopeImportedLoop,
		"the peer's identity is never logged — it is not this deployment's to publish")
	assert.Equal(t, semtypes.EntityIDReasonForeignAuthority, lines[0].attrs["reason"])
}

// TestForeignFiringSkipLogSurvivesAPublishFailure pins the OTHER reason the
// flush is deferred rather than tail-called. The anchor skips are recorded
// before the publish; rule.task.spawned is decided after it. So on a publish
// error the counter has already incremented while the dispatch returns early,
// and only the defer keeps the log's unit equal to the counter's unit — the
// equality the requirement states.
//
// Without this, a tail call placed before the success return passes the whole
// suite: every other test in this file takes the happy path, where a tail call
// and a defer are indistinguishable. An operator debugging a failed dispatch is
// exactly who needs the line, and is exactly who would not get it.
//
// Mutation check: replace `defer flushForeignSkips()` with a `flushForeignSkips()`
// call immediately before publishAgentOnce's success return. This test fails on
// the missing line; every other test in the package still passes.
func TestForeignFiringSkipLogSurvivesAPublishFailure(t *testing.T) {
	h := newRunScopeHarness(t)
	h.seedImportedLoop(t)
	h.pub.err = errors.New("jetstream unavailable")

	err := h.executor.Execute(h.ctx, h.runScopeNewAction(),
		&ExecutionContext{EntityID: runScopeImportedLoop})
	require.Error(t, err, "the publish failure must surface; this test is about what is logged on the way out")

	lines := h.logs.withMessage(foreignFiringSkipLogMessage)
	require.Len(t, lines, 1,
		"the skips recorded before the publish are still declined facts — the line is emitted on the error path too")
	assert.Contains(t, lines[0].attrs["skipped"], agvocab.LoopRun,
		"the anchor was declined before the publish was attempted, so it is named")
	assert.InDelta(t, 1.0, testutil.ToFloat64(
		h.metrics.foreignFiringWritesSkippedTotal.WithLabelValues(semtypes.EntityIDReasonForeignAuthority)), 0.0001,
		"the counter incremented on this dispatch; the log must not silently disagree with it")
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
		h.metrics.foreignFiringWritesSkippedTotal.WithLabelValues(semtypes.EntityIDReasonForeignAuthority)), 0.0001,
		"nothing is skipped when the firing loop carries the deployment's own authority")

	// The log's negative half. flush runs from a defer on EVERY dispatch, local
	// ones included, so its emptiness guard is the only thing between a local
	// dispatch and a false foreign-authority line on the surface the migration
	// guide calls the operator's signal. The counter has both halves pinned;
	// without this the log has only its positive one.
	assert.Empty(t, h.logs.withMessage(foreignFiringSkipLogMessage),
		"a dispatch that declined nothing emits no skip line — the deferred flush fires on local dispatches too")

	_ = h.mutator.snapshot()
}
