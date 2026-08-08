package researchclassify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph/query"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/types"
)

// recordingPublisher captures orchestration triples for the per-stage
// stamp assertions. Mirrors the shape in
// processor/research-graph-llmwrap/triplepub_test.go so the five
// component test suites stay consistent.
type recordingPublisher struct {
	mu     sync.Mutex
	batch  [][]message.Triple
	addErr error
}

// Create satisfies llmwrap.TriplePublisher. The classify component only appends
// onto the already-born pipeline loop entity, so this is present for interface
// conformance and is not exercised here.
func (r *recordingPublisher) Create(_ context.Context, _ string, _ message.Type, triples []message.Triple) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	dup := append([]message.Triple(nil), triples...)
	r.batch = append(r.batch, dup)
	return r.addErr
}

func (r *recordingPublisher) Append(_ context.Context, triples []message.Triple) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	dup := append([]message.Triple(nil), triples...)
	r.batch = append(r.batch, dup)
	return r.addErr
}

func (r *recordingPublisher) lastBatch() []message.Triple {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.batch) == 0 {
		return nil
	}
	return r.batch[len(r.batch)-1]
}

// fakeLoopStore replaces the natsLoopStore for handler integration
// tests. Records GetIntent calls and PutClassifierOutput /
// PutSnapshot writes so the test can assert on key + envelope shape.
type fakeLoopStore struct {
	mu sync.Mutex

	intent    *research.Intent
	intentErr error

	getCalls int

	snapshotKey      string
	snapshotEnvelope []byte
	snapshotErr      error

	classifyKey      string
	classifyEnvelope []byte
	classifyErr      error

	writeOrder []string
}

func (s *fakeLoopStore) GetIntent(_ context.Context, _ string) (*research.Intent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCalls++
	if s.intentErr != nil {
		return nil, s.intentErr
	}
	return s.intent, nil
}

func (s *fakeLoopStore) PutSnapshot(_ context.Context, loopID string, envelope []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshotKey = loopID
	s.snapshotEnvelope = append([]byte(nil), envelope...)
	s.writeOrder = append(s.writeOrder, "snapshot")
	return s.snapshotErr
}

func (s *fakeLoopStore) PutClassifierOutput(_ context.Context, loopID string, envelope []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.classifyKey = loopID
	s.classifyEnvelope = append([]byte(nil), envelope...)
	s.writeOrder = append(s.writeOrder, "classify_complete")
	return s.classifyErr
}

// newTestComponent constructs a Component with all injected
// dependencies stubbed for handler-path testing. The deps struct is
// intentionally minimal: handleMessage doesn't touch deps once Start
// has wired the injected fields, so we can leave deps.NATSClient nil.
// quietLogger returns an slog.Logger that discards output so
// handler.go's structured log lines don't pollute test output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestComponent(loops LoopStore, classifier Classifier, retriever CandidateRetriever) *Component {
	config := DefaultConfig()
	inputs, outputs := mustResolveTestPorts(config.Ports)
	return &Component{
		config:     config,
		inputs:     inputs,
		outputs:    outputs,
		classifier: classifier,
		retriever:  retriever,
		loops:      loops,
		logger:     quietLogger(),
		triplePub:  &recordingPublisher{},
		deps: component.Dependencies{
			Platform: types.PlatformMeta{Org: "acme", Platform: "ops"},
		},
	}
}

func mustResolveTestPorts(config *component.PortConfig) ([]component.Port, []component.Port) {
	inputs := make([]component.Port, len(config.Inputs))
	for index, definition := range config.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			panic(err)
		}
		inputs[index] = port
	}
	outputs := make([]component.Port, len(config.Outputs))
	for index, definition := range config.Outputs {
		port, err := definition.Resolve(component.DirectionOutput)
		if err != nil {
			panic(err)
		}
		outputs[index] = port
	}
	return inputs, outputs
}

func TestComponent_HandleMessage_HappyPath(t *testing.T) {
	loops := &fakeLoopStore{
		intent: &research.Intent{Topic: "drone hover anomalies"},
	}
	classifier := &fakeClassifier{result: &query.ClassificationResult{
		Tier:       0,
		Options:    map[string]any{"path_intent": true},
		Confidence: 1.0,
	}}
	retriever := &fakeRetriever{
		candidates: []research.Candidate{
			{EntityID: "acme.ops.robotics.gcs.drone.001", Tier: "0", Source: "search_graph", Relevance: 0.9},
		},
	}
	c := newTestComponent(loops, classifier, retriever)
	c.logger = quietLogger()

	c.handleMessage(context.Background(), "component.nl_classify.rg_test001", nil)

	if loops.classifyKey != "rg_test001" {
		t.Errorf("classify trigger key = %q, want %q", loops.classifyKey, "rg_test001")
	}
	if loops.snapshotKey != "rg_test001" {
		t.Errorf("snapshot key = %q, want %q", loops.snapshotKey, "rg_test001")
	}
	if len(loops.writeOrder) != 2 || loops.writeOrder[0] != "snapshot" || loops.writeOrder[1] != "classify_complete" {
		t.Errorf("write order = %v, want [snapshot, classify_complete]", loops.writeOrder)
	}

	// Envelope decodes through the production registry as a
	// *research.ClassifierOutput — proves the registry wiring is
	// consistent with what R1's reader will use
	// (feedback_production_decoder_round_trip_required).
	decoder := newResearchDecoder(t)
	decoded, err := decoder.Decode(loops.classifyEnvelope)
	if err != nil {
		t.Fatalf("decode classify envelope: %v", err)
	}
	out, ok := decoded.Payload().(*research.ClassifierOutput)
	if !ok {
		t.Fatalf("decoded type = %T, want *research.ClassifierOutput", decoded.Payload())
	}
	if out.Topic != "drone hover anomalies" {
		t.Errorf("topic in envelope = %q, want %q", out.Topic, "drone hover anomalies")
	}
	if out.Tier != "0" {
		t.Errorf("tier = %q, want \"0\"", out.Tier)
	}
	if len(out.Candidates) != 1 {
		t.Fatalf("candidates len = %d, want 1", len(out.Candidates))
	}
}

func TestComponent_HandleMessage_BadSubject(t *testing.T) {
	loops := &fakeLoopStore{intent: &research.Intent{Topic: "x"}}
	c := newTestComponent(loops, &fakeClassifier{}, &fakeRetriever{})
	c.logger = quietLogger()

	c.handleMessage(context.Background(), "totally.wrong.subject", nil)

	if loops.getCalls != 0 {
		t.Errorf("GetIntent called %d times for malformed subject; want 0", loops.getCalls)
	}
	if loops.classifyKey != "" {
		t.Errorf("classify write happened on malformed subject (key=%q)", loops.classifyKey)
	}
	if atomic.LoadInt64(&c.errors) == 0 {
		t.Errorf("expected an error to be counted")
	}
}

func TestComponent_HandleMessage_IntentNotFound(t *testing.T) {
	loops := &fakeLoopStore{intentErr: errIntentNotFound}
	c := newTestComponent(loops, &fakeClassifier{}, &fakeRetriever{})
	c.logger = quietLogger()

	c.handleMessage(context.Background(), "component.nl_classify.rg_test001", nil)

	if loops.classifyKey != "" {
		t.Errorf("classify write happened despite missing intent (key=%q)", loops.classifyKey)
	}
	if atomic.LoadInt64(&c.errors) == 0 {
		t.Errorf("expected an error to be counted")
	}
}

func TestComponent_HandleMessage_RetrieverFailure(t *testing.T) {
	loops := &fakeLoopStore{intent: &research.Intent{Topic: "x"}}
	classifier := &fakeClassifier{result: &query.ClassificationResult{Tier: 0}}
	retriever := &fakeRetriever{err: errors.New("nats unreachable")}
	c := newTestComponent(loops, classifier, retriever)
	c.logger = quietLogger()

	c.handleMessage(context.Background(), "component.nl_classify.rg_test001", nil)

	if loops.classifyKey != "" {
		t.Errorf("classify write should not happen when retriever fails (key=%q)", loops.classifyKey)
	}
	if atomic.LoadInt64(&c.errors) == 0 {
		t.Errorf("expected an error to be counted")
	}
}

func TestComponent_HandleMessage_SnapshotFailureDoesNotBlockTrigger(t *testing.T) {
	// PutSnapshot is best-effort: a failure there should NOT skip
	// the classify.complete trigger write — R1 still needs to fire.
	loops := &fakeLoopStore{
		intent:      &research.Intent{Topic: "x"},
		snapshotErr: errors.New("snapshot kv error"),
	}
	classifier := &fakeClassifier{result: &query.ClassificationResult{Tier: 0}}
	retriever := &fakeRetriever{
		candidates: []research.Candidate{{EntityID: semantictest.EntityID(t, "test", "research", "classify", "candidate", "entity", "001"), Tier: "0", Source: "search_graph"}},
	}
	c := newTestComponent(loops, classifier, retriever)
	c.logger = quietLogger()

	c.handleMessage(context.Background(), "component.nl_classify.rg_test001", nil)

	if loops.classifyKey == "" {
		t.Errorf("classify.complete trigger should fire even when snapshot fails")
	}
}

func TestComponent_HandleMessage_ClassifyTriggerFailureCounts(t *testing.T) {
	loops := &fakeLoopStore{
		intent:      &research.Intent{Topic: "x"},
		classifyErr: errors.New("trigger kv error"),
	}
	classifier := &fakeClassifier{result: &query.ClassificationResult{Tier: 0}}
	retriever := &fakeRetriever{
		candidates: []research.Candidate{{EntityID: semantictest.EntityID(t, "test", "research", "classify", "candidate", "entity", "002"), Tier: "0", Source: "search_graph"}},
	}
	c := newTestComponent(loops, classifier, retriever)
	c.logger = quietLogger()

	c.handleMessage(context.Background(), "component.nl_classify.rg_test001", nil)

	if atomic.LoadInt64(&c.errors) == 0 {
		t.Errorf("expected an error to be counted when trigger write fails")
	}
	if atomic.LoadInt64(&c.messagesEmitted) != 0 {
		t.Errorf("messagesEmitted should not increment on trigger failure")
	}
}

func TestComponent_EnvelopeShape_DecodesViaProductionRegistry(t *testing.T) {
	// Belt-and-suspenders for
	// feedback_production_decoder_round_trip_required: the envelope
	// must decode through the same registry production R1 will use.
	loops := &fakeLoopStore{intent: &research.Intent{Topic: "voltage"}}
	classifier := &fakeClassifier{result: &query.ClassificationResult{Tier: 1, Confidence: 0.8}}
	retriever := &fakeRetriever{
		candidates: []research.Candidate{{EntityID: semantictest.EntityID(t, "test", "research", "classify", "candidate", "battery", "01"), Tier: "1", Source: "search_graph"}},
	}
	c := newTestComponent(loops, classifier, retriever)
	c.logger = quietLogger()

	c.handleMessage(context.Background(), "component.nl_classify.rg_x", nil)

	decoder := newResearchDecoder(t)
	decoded, err := decoder.Decode(loops.classifyEnvelope)
	if err != nil {
		t.Fatalf("envelope decode failed: %v\nwire: %s", err, loops.classifyEnvelope)
	}
	if decoded.Type() != (message.Type{
		Domain:   research.Domain,
		Category: research.CategoryClassifierOutput,
		Version:  research.SchemaVersion,
	}) {
		t.Errorf("envelope type discriminator wrong: %+v", decoded.Type())
	}
	// Snapshot should be byte-equal to classify envelope (same
	// payload, separate keys for queryability vs trigger).
	if string(loops.snapshotEnvelope) != string(loops.classifyEnvelope) {
		t.Errorf("snapshot vs classify envelope diverged")
	}
}

func newResearchDecoder(t *testing.T) *message.Decoder {
	t.Helper()
	registry := payloadregistry.New()
	if err := research.RegisterPayloads(registry); err != nil {
		t.Fatalf("register research payloads: %v", err)
	}
	return message.NewDecoder(registry)
}

// TestComponent_HandleMessage_StampsOrchestrationTriples locks the
// PR 6 contract: every successful handler dispatch emits the
// research.classify.complete batch on the research-pipeline loop
// entity so R1 of the rule chain (ADR-045) can fire. Batch atomicity
// (shared Subject) is asserted in agentic/research/orchestration_test.go;
// here we lock that the handler DOES the stamp on the happy path.
func TestComponent_HandleMessage_StampsOrchestrationTriples(t *testing.T) {
	loops := &fakeLoopStore{intent: &research.Intent{Topic: "drone hover anomalies"}}
	classifier := &fakeClassifier{result: &query.ClassificationResult{Tier: 0, Confidence: 1.0}}
	retriever := &fakeRetriever{
		candidates: []research.Candidate{
			{EntityID: "acme.ops.robotics.gcs.drone.001", Tier: "0", Source: "search_graph", Relevance: 0.9},
			{EntityID: "acme.ops.robotics.gcs.drone.002", Tier: "0", Source: "search_graph", Relevance: 0.85},
		},
		degraded:       true,
		degradedReason: "semantic fallback fired",
	}
	c := newTestComponent(loops, classifier, retriever)
	pub := c.triplePub.(*recordingPublisher)

	c.handleMessage(context.Background(), "component.nl_classify.rg_test001", nil)

	batch := pub.lastBatch()
	if len(batch) != 3 {
		t.Fatalf("expected 3 orchestration triples (complete + candidate_count + degraded), got %d", len(batch))
	}

	// All triples must share the loop-execution entity ID (the 6-part
	// form derived from deps.Platform.Org/Platform and loop_id) so
	// graph-ingest's per-Subject CAS path lands them atomically.
	const wantSubject = "acme.ops.agent.agentic-loop.execution.rg_test001"
	facts := map[string]any{}
	for _, tr := range batch {
		if tr.Subject != wantSubject {
			t.Errorf("triple Subject = %q, want %q (loop-execution entity ID for atomic batch)", tr.Subject, wantSubject)
		}
		if tr.Source != research.SourceClassify {
			t.Errorf("triple Source = %q, want %q", tr.Source, research.SourceClassify)
		}
		facts[tr.Predicate] = tr.Object
	}

	if facts[research.PredicateResearchClassifyCandidateCount] != "2" {
		t.Errorf("candidate_count = %v, want \"2\"", facts[research.PredicateResearchClassifyCandidateCount])
	}
	if facts[research.PredicateResearchClassifyDegraded] != "true" {
		t.Errorf("degraded = %v, want \"true\" (retriever flagged degraded)", facts[research.PredicateResearchClassifyDegraded])
	}
	if facts[research.PredicateResearchClassifyComplete] == nil {
		t.Errorf("classify.complete triple missing")
	}
}

// TestComponent_HandleMessage_NoStampWhenPlatformMissing pins the
// degraded path: if the platform identity isn't configured (e.g.,
// test/dev environment), the loop-execution entity ID can't be
// constructed and the chain logs warn rather than crash. The KV
// envelope still lands so the per-stage queryable snapshot survives
// even without rule-engine orchestration.
func TestComponent_HandleMessage_NoStampWhenPlatformMissing(t *testing.T) {
	loops := &fakeLoopStore{intent: &research.Intent{Topic: "voltage"}}
	c := newTestComponent(loops, &fakeClassifier{}, &fakeRetriever{})
	c.deps = component.Dependencies{Platform: types.PlatformMeta{}} // empty Org/Platform
	pub := c.triplePub.(*recordingPublisher)

	c.handleMessage(context.Background(), "component.nl_classify.rg_x", nil)

	if len(pub.batch) != 0 {
		t.Errorf("expected no triple stamps when platform identity missing, got %d batches", len(pub.batch))
	}
	// But the KV envelope DID land — handler doesn't abort on
	// triple-stamp failure.
	if loops.classifyKey != "rg_x" {
		t.Errorf("classify trigger key missing despite stamp-degraded path: %q", loops.classifyKey)
	}
}

func TestDiscoverableSurface(t *testing.T) {
	loops := &fakeLoopStore{}
	c := newTestComponent(loops, &fakeClassifier{}, &fakeRetriever{})
	c.logger = quietLogger()
	c.mu.Lock()
	c.started = true
	c.startTime = time.Now().Add(-5 * time.Minute)
	c.mu.Unlock()

	meta := c.Meta()
	if meta.Name != ComponentName {
		t.Errorf("Meta.Name = %q, want %q", meta.Name, ComponentName)
	}
	if meta.Type != "processor" {
		t.Errorf("Meta.Type = %q, want processor", meta.Type)
	}

	if ports := c.InputPorts(); len(ports) == 0 {
		t.Errorf("InputPorts should have at least one port")
	}
	ports := c.OutputPorts()
	if len(ports) != 1 {
		t.Fatalf("OutputPorts = %d, want canonical mutation port", len(ports))
	}
	request, ok := ports[0].Config.(component.NATSRequestPort)
	if !ok || !ports[0].Required || request.Subject != graphmutation.SubjectFamily || request.Interface == nil ||
		request.Interface.Type != graphmutation.InterfaceType || request.Interface.Version != graphmutation.InterfaceVersion {
		t.Errorf("graph mutation output drift: %#v", ports[0])
	}

	if schema := c.ConfigSchema(); schema.Properties == nil {
		t.Errorf("ConfigSchema returned empty properties")
	}

	health := c.Health()
	if !health.Healthy {
		t.Errorf("Health should be healthy when started=true")
	}
	if health.Uptime <= 0 {
		t.Errorf("Health.Uptime = %v, want > 0", health.Uptime)
	}
}

func TestConfig_ValidateRejectsMissingPorts(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"no ports", Config{}, true},
		{"empty inputs", Config{Ports: &component.PortConfig{}}, true},
		{"ok", Config{Ports: &component.PortConfig{Inputs: []component.PortDefinition{{Name: "in", Config: component.NATSPort{Subject: "x"}}}}}, false},
		{"negative max", Config{
			Ports:         &component.PortConfig{Inputs: []component.PortDefinition{{Name: "in", Config: component.NATSPort{Subject: "x"}}}},
			MaxCandidates: -1,
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.cfg.Validate()
			if c.wantErr && err == nil {
				t.Errorf("Validate = nil, want error")
			}
			if !c.wantErr && err != nil {
				t.Errorf("Validate = %v, want nil", err)
			}
		})
	}
}

func TestConfig_ApplyDefaults(t *testing.T) {
	c := Config{}
	c.ApplyDefaults()
	if c.LoopsBucket != "AGENT_LOOPS" {
		t.Errorf("LoopsBucket = %q, want AGENT_LOOPS", c.LoopsBucket)
	}
	if c.MaxCandidates != 25 {
		t.Errorf("MaxCandidates = %d, want 25", c.MaxCandidates)
	}
	if c.ClassifyTimeout != 30*time.Second {
		t.Errorf("ClassifyTimeout = %v, want 30s", c.ClassifyTimeout)
	}
}

func TestNewProcessor_RejectsNilNATSClient(t *testing.T) {
	cfg := DefaultConfig()
	raw, _ := json.Marshal(cfg)
	_, err := NewProcessor(raw, component.Dependencies{})
	if err == nil {
		t.Errorf("expected error when NATSClient is nil")
	}
}
