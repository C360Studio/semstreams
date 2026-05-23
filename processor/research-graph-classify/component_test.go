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
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
)

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
	return &Component{
		config:     DefaultConfig(),
		classifier: classifier,
		retriever:  retriever,
		loops:      loops,
		logger:     quietLogger(),
	}
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
	decoder := payloadbuiltins.NewTestDecoder(t)
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
		candidates: []research.Candidate{{EntityID: "x", Tier: "0", Source: "search_graph"}},
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
		candidates: []research.Candidate{{EntityID: "x", Tier: "0", Source: "search_graph"}},
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
		candidates: []research.Candidate{{EntityID: "battery.01", Tier: "1", Source: "search_graph"}},
	}
	c := newTestComponent(loops, classifier, retriever)
	c.logger = quietLogger()

	c.handleMessage(context.Background(), "component.nl_classify.rg_x", nil)

	decoder := payloadbuiltins.NewTestDecoder(t)
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
	if ports := c.OutputPorts(); len(ports) != 0 {
		t.Errorf("OutputPorts should be empty (component emits to KV, not NATS subjects)")
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
		{"ok", Config{Ports: &component.PortConfig{Inputs: []component.PortDefinition{{Name: "in", Subject: "x"}}}}, false},
		{"negative max", Config{
			Ports:         &component.PortConfig{Inputs: []component.PortDefinition{{Name: "in", Subject: "x"}}},
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
