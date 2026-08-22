package agenticloop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/storage"
	"github.com/c360studio/semstreams/storage/storeregistry"
	"github.com/nats-io/nats.go/jetstream"
)

type trajectoryTestEntry struct {
	key   string
	value []byte
}

func (e trajectoryTestEntry) Bucket() string                  { return "AGENT_TRAJECTORIES" }
func (e trajectoryTestEntry) Key() string                     { return e.key }
func (e trajectoryTestEntry) Value() []byte                   { return append([]byte(nil), e.value...) }
func (e trajectoryTestEntry) Revision() uint64                { return 1 }
func (e trajectoryTestEntry) Created() time.Time              { return time.Time{} }
func (e trajectoryTestEntry) Delta() uint64                   { return 0 }
func (e trajectoryTestEntry) Operation() jetstream.KeyValueOp { return jetstream.KeyValuePut }

type trajectoryTestLister struct{ keys chan string }

func (l *trajectoryTestLister) Keys() <-chan string { return l.keys }
func (l *trajectoryTestLister) Stop() error         { return nil }

type trajectoryTestBucket struct {
	mu              sync.Mutex
	values          map[string][]byte
	created         [][]byte
	createErrBefore bool
	createErrAfter  bool
	listErr         error
	listLister      jetstream.KeyLister
	listCalls       int
	getCalls        int
	getErrKeys      map[string]error
	blockListPrefix string
	listEntered     chan struct{}
}

func (b *trajectoryTestBucket) Create(_ context.Context, key string, value []byte, _ ...jetstream.KVCreateOpt) (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.createErrBefore {
		return 0, errors.New("create unavailable")
	}
	if _, exists := b.values[key]; exists {
		return 0, jetstream.ErrKeyExists
	}
	b.values[key] = append([]byte(nil), value...)
	b.created = append(b.created, append([]byte(nil), value...))
	if b.createErrAfter {
		return 0, errors.New("lost create reply")
	}
	return 1, nil
}

func (b *trajectoryTestBucket) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.getCalls++
	if err := b.getErrKeys[key]; err != nil {
		return nil, err
	}
	value, ok := b.values[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	return trajectoryTestEntry{key: key, value: value}, nil
}

func (b *trajectoryTestBucket) ListKeysFiltered(ctx context.Context, filters ...string) (jetstream.KeyLister, error) {
	b.mu.Lock()
	b.listCalls++
	listErr := b.listErr
	listLister := b.listLister
	block := b.blockListPrefix != "" && filters[0] == b.blockListPrefix
	entered := b.listEntered
	values := make(map[string][]byte, len(b.values))
	for key, value := range b.values {
		values[key] = value
	}
	b.mu.Unlock()
	if listErr != nil {
		return nil, listErr
	}
	if listLister != nil {
		return listLister, nil
	}
	if block {
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	prefix := filters[0][:len(filters[0])-1]
	ch := make(chan string, len(values))
	for key := range values {
		if strings.HasPrefix(key, prefix) {
			ch <- key
		}
	}
	close(ch)
	return &trajectoryTestLister{keys: ch}, nil
}

type trajectoryTestStore struct {
	mu     sync.Mutex
	values map[string][]byte
	// putErrBefore rejects the write and stores NOTHING, so the lost-reply
	// re-verification finds no object and the evidence_put failure stands.
	// Distinct from putErrAfter, which stores the value and then reports a
	// lost reply — that one is recovered by re-verification and emits no
	// failure. Mirrors createErrBefore/createErrAfter on the bucket fake.
	putErrBefore bool
	putErrAfter  bool
	blockGet     bool
}

func (s *trajectoryTestStore) Put(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putErrBefore {
		return errors.New("object store rejected the write")
	}
	s.values[key] = append([]byte(nil), value...)
	if s.putErrAfter {
		return errors.New("lost put reply")
	}
	return nil
}
func (s *trajectoryTestStore) Get(ctx context.Context, key string) ([]byte, error) {
	if s.blockGet {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return nil, storage.ErrObjectNotFound
	}
	return append([]byte(nil), value...), nil
}
func (s *trajectoryTestStore) List(context.Context, string) ([]string, error) { return nil, nil }
func (s *trajectoryTestStore) Delete(context.Context, string) error           { return nil }
func (s *trajectoryTestStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	value, err := s.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(value)), nil
}

func TestTrajectoryRecorderDistinctAttemptsCanShareEvidence(t *testing.T) {
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	store := &trajectoryTestStore{values: make(map[string][]byte)}
	registry := storeregistry.New()
	if err := registry.Register("objectstore", store); err != nil {
		t.Fatal(err)
	}
	recorder := newTrajectoryRecorder(bucket, registry, "objectstore", nil)

	observation := trajectoryObservation{
		LoopID: "loop.same", Kind: agentic.TrajectoryKindToolCompleted,
		SourceKind: agentic.TrajectorySourceToolCall, SourceCorrelation: "call-1",
		CausalPhase: agentic.TrajectoryPhaseToolResult, Evidence: map[string]any{"result": "full"},
	}
	first := recorder.record(context.Background(), observation)
	second := recorder.record(context.Background(), observation)
	if !first.FactStored || !second.FactStored {
		t.Fatalf("facts not stored: %#v %#v", first, second)
	}
	if first.Fact.AttemptID == second.Fact.AttemptID || first.Key == second.Key {
		t.Fatal("redelivery reused attempt identity")
	}
	if first.Fact.EvidenceDigest != second.Fact.EvidenceDigest || first.Fact.Evidence.Key != second.Fact.Evidence.Key {
		t.Fatal("identical evidence did not share its digest-addressed object")
	}
}

func TestTrajectoryRecorderVerifiesLostRepliesWithExactBytes(t *testing.T) {
	bucket := &trajectoryTestBucket{values: make(map[string][]byte), createErrAfter: true}
	store := &trajectoryTestStore{values: make(map[string][]byte), putErrAfter: true}
	registry := storeregistry.New()
	if err := registry.Register("objectstore", store); err != nil {
		t.Fatal(err)
	}
	var failures []trajectoryAuditFailure
	recorder := newTrajectoryRecorder(bucket, registry, "objectstore", func(f trajectoryAuditFailure) { failures = append(failures, f) })

	got := recorder.record(context.Background(), trajectoryObservation{
		LoopID: "loop.lost", Kind: agentic.TrajectoryKindModelCompleted,
		CausalPhase: agentic.TrajectoryPhaseModelResult, Evidence: map[string]string{"response": "all bytes"},
	})
	if !got.FactStored || got.Fact.EvidenceCapture != agentic.TrajectoryEvidenceStored {
		t.Fatalf("lost replies were not verified as success: %#v", got)
	}
	if len(failures) != 0 {
		t.Fatalf("verified lost replies reported failures: %#v", failures)
	}
	entry, err := bucket.Get(context.Background(), got.Key)
	if err != nil || !bytes.Equal(entry.Value(), got.Bytes) {
		t.Fatalf("stored canonical bytes differ: err=%v", err)
	}
}

func TestTrajectoryRecorderMissingProviderIsVisibleAndLaterResolutionIsLazy(t *testing.T) {
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	registry := storeregistry.New()
	var failures []trajectoryAuditFailure
	recorder := newTrajectoryRecorder(bucket, registry, "objectstore", func(f trajectoryAuditFailure) {
		failures = append(failures, f)
	})
	observation := trajectoryObservation{
		LoopID: "loop-provider", Kind: agentic.TrajectoryKindToolCompleted,
		CausalPhase: agentic.TrajectoryPhaseToolResult, Evidence: map[string]string{"result": "full"},
	}

	missing := recorder.record(context.Background(), observation)
	if !missing.FactStored || missing.Fact.EvidenceCapture != agentic.TrajectoryEvidenceMissing || missing.Fact.Evidence != nil {
		t.Fatalf("missing provider fact = %#v", missing.Fact)
	}
	if len(failures) != 1 || failures[0].Stage != trajectoryStageProviderResolve {
		t.Fatalf("missing provider failures = %#v", failures)
	}

	store := &trajectoryTestStore{values: make(map[string][]byte)}
	if err := registry.Register("objectstore", store); err != nil {
		t.Fatal(err)
	}
	stored := recorder.record(context.Background(), observation)
	if !stored.FactStored || stored.Fact.EvidenceCapture != agentic.TrajectoryEvidenceStored || stored.Fact.Evidence == nil {
		t.Fatalf("later provider was not lazily resolved: %#v", stored.Fact)
	}
}

func TestTrajectoryRecorderRestartContinuesVisibleOrdinal(t *testing.T) {
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	firstRecorder := newTrajectoryRecorder(bucket, nil, "objectstore", nil)
	first := firstRecorder.record(context.Background(), trajectoryObservation{
		LoopID: "loop-restart", Kind: agentic.TrajectoryKindLoopStarted,
		CausalPhase: agentic.TrajectoryPhaseLoopStart,
	})
	secondRecorder := newTrajectoryRecorder(bucket, nil, "objectstore", nil)
	second := secondRecorder.record(context.Background(), trajectoryObservation{
		LoopID: "loop-restart", Kind: agentic.TrajectoryKindModelRequested,
		CausalPhase: agentic.TrajectoryPhaseModelRequest,
	})
	if first.Fact.AttemptOrdinal != 1 || second.Fact.AttemptOrdinal != 2 {
		t.Fatalf("restart ordinals = %d, %d; want 1, 2", first.Fact.AttemptOrdinal, second.Fact.AttemptOrdinal)
	}
}

func TestTrajectoryRecorderRestartScanFailureRetriesWithoutWriting(t *testing.T) {
	const loopID = "loop-restart-recovery"
	const priorAttempt = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	validKey, validBytes := trajectoryVisibleFact(t, loopID, loopID, priorAttempt, 7)

	tests := []struct {
		name    string
		arrange func(*trajectoryTestBucket)
		recover func(*trajectoryTestBucket)
	}{
		{
			name: "list",
			arrange: func(bucket *trajectoryTestBucket) {
				bucket.listErr = errors.New("list unavailable")
			},
			recover: func(bucket *trajectoryTestBucket) { bucket.listErr = nil },
		},
		{
			name: "get",
			arrange: func(bucket *trajectoryTestBucket) {
				bucket.getErrKeys = map[string]error{validKey: errors.New("get unavailable")}
			},
			recover: func(bucket *trajectoryTestBucket) { delete(bucket.getErrKeys, validKey) },
		},
		{
			name:    "decode",
			arrange: func(bucket *trajectoryTestBucket) { bucket.values[validKey] = []byte("{") },
			recover: func(bucket *trajectoryTestBucket) { bucket.values[validKey] = validBytes },
		},
		{
			name: "digest",
			arrange: func(bucket *trajectoryTestBucket) {
				_, bucket.values[validKey] = trajectoryVisibleFact(t, loopID, "other-loop", priorAttempt, 7)
			},
			recover: func(bucket *trajectoryTestBucket) { bucket.values[validKey] = validBytes },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket := &trajectoryTestBucket{values: map[string][]byte{validKey: validBytes}}
			tt.arrange(bucket)
			var failures []trajectoryAuditFailure
			recorder := newTrajectoryRecorder(bucket, nil, "objectstore", func(f trajectoryAuditFailure) {
				failures = append(failures, f)
			})

			failed := recorder.record(context.Background(), trajectoryObservation{
				LoopID: loopID, Kind: agentic.TrajectoryKindModelRequested,
				CausalPhase: agentic.TrajectoryPhaseModelRequest,
			})
			if failed.FactStored || len(bucket.created) != 0 {
				t.Fatalf("restart scan failure wrote a fact: result=%#v creates=%d", failed, len(bucket.created))
			}
			if len(failures) != 1 || failures[0].AttemptID == "" {
				t.Fatalf("restart scan degradation = %#v, want one failure with attempt ID", failures)
			}

			tt.recover(bucket)
			recovered := recorder.record(context.Background(), trajectoryObservation{
				LoopID: loopID, Kind: agentic.TrajectoryKindModelRequested,
				CausalPhase: agentic.TrajectoryPhaseModelRequest,
			})
			if !recovered.FactStored || recovered.Fact.AttemptOrdinal != 8 {
				t.Fatalf("recovered fact = %#v, want stored ordinal 8", recovered)
			}
			if bucket.listCalls != 2 {
				t.Fatalf("restart scan calls = %d, want retry after failure", bucket.listCalls)
			}
			if recovered.Fact.AttemptID == failures[0].AttemptID {
				t.Fatal("recovery reused the failed invocation attempt ID")
			}
		})
	}
}

func TestTrajectoryRecorderRestartScanDoesNotBlockOtherLoops(t *testing.T) {
	blockedLoop := "loop-blocked-scan"
	bucket := &trajectoryTestBucket{
		values:          make(map[string][]byte),
		blockListPrefix: agentic.TrajectoryFactPrefix(blockedLoop) + ">",
		listEntered:     make(chan struct{}, 1),
	}
	recorder := newTrajectoryRecorder(bucket, nil, "objectstore", nil)
	blockedCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	blockedDone := make(chan struct{})
	go func() {
		defer close(blockedDone)
		recorder.record(blockedCtx, trajectoryObservation{
			LoopID: blockedLoop, Kind: agentic.TrajectoryKindLoopStarted,
			CausalPhase: agentic.TrajectoryPhaseLoopStart,
		})
	}()
	select {
	case <-bucket.listEntered:
	case <-time.After(time.Second):
		t.Fatal("blocked loop never entered restart scan")
	}

	start := time.Now()
	other := recorder.record(context.Background(), trajectoryObservation{
		LoopID: "loop-independent", Kind: agentic.TrajectoryKindLoopStarted,
		CausalPhase: agentic.TrajectoryPhaseLoopStart,
	})
	if !other.FactStored {
		t.Fatalf("independent loop fact = %#v, want stored", other)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("independent loop blocked behind another loop's scan for %v", elapsed)
	}
	<-blockedDone
}

func TestTrajectoryAuditBudgetPreservesUsefulDeliveryContext(t *testing.T) {
	tests := []struct {
		name     string
		recorder func(*Component) *trajectoryRecorder
	}{
		{
			name: "stalled evidence store",
			recorder: func(c *Component) *trajectoryRecorder {
				bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
				registry := storeregistry.New()
				if err := registry.Register("objectstore", &trajectoryTestStore{values: make(map[string][]byte), blockGet: true}); err != nil {
					t.Fatal(err)
				}
				return newTrajectoryRecorder(bucket, registry, "objectstore", c.reportTrajectoryAuditFailure)
			},
		},
		{
			name: "stalled trajectory KV",
			recorder: func(c *Component) *trajectoryRecorder {
				loopID := "loop-budget"
				bucket := &trajectoryTestBucket{
					values: make(map[string][]byte), blockListPrefix: agentic.TrajectoryFactPrefix(loopID) + ">",
				}
				return newTrajectoryRecorder(bucket, nil, "objectstore", c.reportTrajectoryAuditFailure)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Component{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), started: true, startTime: time.Now()}
			c.trajectoryRecorder = tt.recorder(c)
			parent, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			msg := &inputAckMsg{}
			var transitioned atomic.Bool
			var published atomic.Bool

			err := consumeLongRunningInput(parent, msg, time.Hour, func(workCtx context.Context, _ []byte) error {
				c.recordTrajectoryObservations(workCtx, HandlerResult{trajectoryObservations: []trajectoryObservation{{
					LoopID: "loop-budget", Kind: agentic.TrajectoryKindModelCompleted,
					CausalPhase: agentic.TrajectoryPhaseModelResult, Evidence: map[string]string{"response": "full"},
				}}})
				if workCtx.Err() != nil {
					return workCtx.Err()
				}
				transitioned.Store(true)
				published.Store(true)
				return nil
			})
			if err != nil {
				t.Fatalf("useful delivery failed after audit timeout: %v", err)
			}
			if parent.Err() != nil || !transitioned.Load() || !published.Load() || !msg.acked.Load() || msg.naked.Load() {
				t.Fatalf("useful path viability: parent=%v transition=%v publish=%v ack=%v nak=%v",
					parent.Err(), transitioned.Load(), published.Load(), msg.acked.Load(), msg.naked.Load())
			}
			health := c.Health()
			if health.Healthy || health.Status != "degraded" || health.ErrorCount == 0 {
				t.Fatalf("Health() = %#v, want loud audit degradation", health)
			}
		})
	}
}

func TestTrajectoryFactBucketContractRejectsHistoryOrMaxAgeDrift(t *testing.T) {
	tests := []struct {
		name    string
		history int64
		ttl     time.Duration
		wantErr bool
	}{
		{name: "canonical", history: 1},
		{name: "history", history: 2, wantErr: true},
		{name: "TTL MaxAge", history: 1, ttl: time.Hour, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTrajectoryFactBucketContract(tt.history, tt.ttl)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateTrajectoryFactBucketContract(%d, %s) error = %v, wantErr %v",
					tt.history, tt.ttl, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(strings.ToLower(err.Error()), "wipe") {
				t.Fatalf("incompatibility error = %q, want clean-break wipe instruction", err)
			}
		})
	}
}

func trajectoryVisibleFact(t *testing.T, keyLoopID, digestLoopID, attemptID string, ordinal uint64) (string, []byte) {
	t.Helper()
	fact := agentic.TrajectoryFactV1{
		SchemaVersion: agentic.TrajectorySchemaV1,
		LoopDigest:    agentic.TrajectoryLoopDigest(digestLoopID), AttemptID: attemptID, AttemptOrdinal: ordinal,
		Kind: agentic.TrajectoryKindLoopStarted, CausalPhase: agentic.TrajectoryPhaseLoopStart,
		ObservedAt: time.Now().UTC(), EvidenceCapture: agentic.TrajectoryEvidenceNone,
	}
	encoded, err := fact.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	key, err := agentic.TrajectoryFactKey(keyLoopID, attemptID)
	if err != nil {
		t.Fatal(err)
	}
	return key, encoded
}

func TestTrajectoryRecorderEvidenceEncodeAndFactCreateFailuresStayNonBlocking(t *testing.T) {
	t.Run("evidence encode failure still creates honest missing-evidence fact", func(t *testing.T) {
		bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
		var failures []trajectoryAuditFailure
		recorder := newTrajectoryRecorder(bucket, nil, "objectstore", func(f trajectoryAuditFailure) {
			failures = append(failures, f)
		})
		got := recorder.record(context.Background(), trajectoryObservation{
			LoopID: "loop-encode", Kind: agentic.TrajectoryKindModelCompleted,
			CausalPhase: agentic.TrajectoryPhaseModelResult, Evidence: make(chan int),
		})
		if !got.FactStored || got.Fact.EvidenceCapture != agentic.TrajectoryEvidenceMissing || got.Fact.Evidence != nil {
			t.Fatalf("encode failure did not create honest fact: %#v", got)
		}
		if len(failures) != 1 || failures[0].Stage != trajectoryStageFactEncode {
			t.Fatalf("encode failures = %#v", failures)
		}
	})

	t.Run("fact create failure reports loss without manufacturing a fact", func(t *testing.T) {
		bucket := &trajectoryTestBucket{values: make(map[string][]byte), createErrBefore: true}
		var failures []trajectoryAuditFailure
		recorder := newTrajectoryRecorder(bucket, nil, "objectstore", func(f trajectoryAuditFailure) {
			failures = append(failures, f)
		})
		got := recorder.record(context.Background(), trajectoryObservation{
			LoopID: "loop-create", Kind: agentic.TrajectoryKindLoopStarted,
			CausalPhase: agentic.TrajectoryPhaseLoopStart,
		})
		if got.FactStored || len(bucket.values) != 0 {
			t.Fatalf("failed create manufactured durable state: %#v values=%v", got, bucket.values)
		}
		if len(failures) != 1 || failures[0].Stage != trajectoryStageFactCreate {
			t.Fatalf("create failures = %#v", failures)
		}
	})
}

func TestHandlerProducesFullEvidenceBeforeOperationalTruncation(t *testing.T) {
	config := DefaultConfig()
	config.ToolResultMaxBytes = 32
	handler := NewMessageHandler(config)

	taskResult, err := handler.HandleTask(context.Background(), agentic.TaskMessage{
		TaskID: "task-full-evidence",
		Role:   "researcher",
		Model:  "test-model",
		Prompt: "the complete original prompt",
		Tools:  []agentic.ToolDefinition{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := trajectoryKinds(taskResult.trajectoryObservations); !bytes.Equal(got, []byte("loop.started,model.requested")) {
		t.Fatalf("task observation kinds = %q", got)
	}
	requestEvidence, err := json.Marshal(taskResult.trajectoryObservations[1].Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(requestEvidence, []byte("the complete original prompt")) {
		t.Fatalf("model request evidence omitted full prompt: %s", requestEvidence)
	}

	loopID := taskResult.LoopID
	const callID = "call-full-evidence"
	fullContent := strings.Repeat("full-tool-result-", 20)
	request := taskResult.trajectoryObservations[1].Evidence.(agentic.AgentRequest)
	responseResult, err := handler.HandleModelResponse(context.Background(), loopID, agentic.AgentResponse{
		RequestID: request.RequestID,
		Status:    agentic.StatusToolCall,
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{
			{ID: callID, Name: "inspect", Arguments: map[string]any{"path": "/complete/path"}},
			{ID: "call-middle", Name: "second"},
			{ID: "call-after", Name: "third"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := trajectoryKinds(responseResult.trajectoryObservations); !bytes.Equal(got, []byte("model.completed,tool.requested")) {
		t.Fatalf("response observation kinds = %q", got)
	}
	toolRequestEvidence, err := json.Marshal(responseResult.trajectoryObservations[1].Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(toolRequestEvidence, []byte("/complete/path")) {
		t.Fatalf("tool request evidence omitted arguments: %s", toolRequestEvidence)
	}
	if responseResult.trajectoryObservations[1].CausalOrdinal != 1 {
		t.Fatalf("tool request source ordinal = %d, want 1", responseResult.trajectoryObservations[1].CausalOrdinal)
	}

	toolResult, err := handler.HandleToolResult(context.Background(), loopID, agentic.ToolResult{
		CallID:  callID,
		Name:    "inspect",
		Content: fullContent,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(toolResult.trajectoryObservations) == 0 || toolResult.trajectoryObservations[0].Kind != agentic.TrajectoryKindToolCompleted {
		t.Fatalf("tool observations = %#v", toolResult.trajectoryObservations)
	}
	if toolResult.trajectoryObservations[0].CausalOrdinal != 1 {
		t.Fatalf("tool source ordinal = %d, want 1", toolResult.trajectoryObservations[0].CausalOrdinal)
	}
	toolEvidence, err := json.Marshal(toolResult.trajectoryObservations[0].Evidence)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(toolEvidence, []byte(fullContent)) || !bytes.Contains(toolEvidence, []byte("/complete/path")) {
		t.Fatalf("tool completion evidence was truncated or lost dispatch arguments: %s", toolEvidence)
	}
	if !strings.Contains(toolResult.TrajectorySteps[0].ToolResult, "truncated") {
		t.Fatalf("execution result was not operationally truncated: %q", toolResult.TrajectorySteps[0].ToolResult)
	}
}

func trajectoryKinds(observations []trajectoryObservation) []byte {
	kinds := make([]string, 0, len(observations))
	for _, observation := range observations {
		kinds = append(kinds, string(observation.Kind))
	}
	return []byte(strings.Join(kinds, ","))
}

func TestResultTerminalObservationIsRecordedAfterKnownHandlerFacts(t *testing.T) {
	handler := NewMessageHandler(DefaultConfig())
	result, err := handler.HandleTask(context.Background(), agentic.TaskMessage{
		TaskID: "task-terminal-order", Role: "reviewer", Model: "test-model", Prompt: "review",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.loopManager.TransitionLoop(result.LoopID, agentic.LoopStateComplete); err != nil {
		t.Fatal(err)
	}
	result.State = agentic.LoopStateComplete
	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	component := &Component{
		handler:            handler,
		trajectoryRecorder: newTrajectoryRecorder(bucket, nil, "objectstore", func(trajectoryAuditFailure) {}),
	}

	component.recordTrajectoryObservations(context.Background(), result)
	component.recordResultTerminalObservation(context.Background(), result)

	if len(bucket.created) != 3 {
		t.Fatalf("created facts = %d, want loop, request, terminal", len(bucket.created))
	}
	kinds := make([]agentic.TrajectoryKind, 0, len(bucket.created))
	for _, encoded := range bucket.created {
		var fact agentic.TrajectoryFactV1
		if err := json.Unmarshal(encoded, &fact); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, fact.Kind)
	}
	if kinds[2] != agentic.TrajectoryKindLoopTerminal {
		t.Fatalf("fact order = %v, terminal observation was not last", kinds)
	}
}

func TestApprovalRejectionAtIterationCapRecordsTerminalBeforeAdjacentSurfaces(t *testing.T) {
	config := DefaultConfig()
	config.MaxIterations = 1
	handler := NewMessageHandler(config)
	ctx := context.Background()
	taskResult, err := handler.HandleTask(ctx, agentic.TaskMessage{
		TaskID: "task-approval-terminal", Role: "reviewer", Model: "test-model", Prompt: "review",
	})
	if err != nil {
		t.Fatal(err)
	}
	loopID := taskResult.LoopID
	const callID = "call-approval-terminal"
	if _, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: "request-approval-terminal", Status: agentic.StatusToolCall,
		Message: agentic.ChatMessage{Role: "assistant", ToolCalls: []agentic.ToolCall{{
			ID: callID, Name: "delete_rule", Arguments: map[string]any{"id": "rule-1"},
		}}},
	}); err != nil {
		t.Fatal(err)
	}
	gateResult, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID: callID, Name: "delete_rule", ErrorKind: agentic.ToolErrorPermission,
		Error: agentic.ApprovalRequiredPrefix + "requires approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gateResult.State != agentic.LoopStateAwaitingApproval {
		t.Fatalf("gate state = %q, want awaiting approval", gateResult.State)
	}
	entity, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatal(err)
	}
	if entity.Iterations == 0 {
		if err := handler.loopManager.IncrementIteration(loopID); err != nil {
			t.Fatal(err)
		}
	}

	bucket := &trajectoryTestBucket{values: make(map[string][]byte)}
	registry := payloadbuiltins.NewTestRegistry(t)
	c := &Component{
		config: config, handler: handler, decoder: message.NewDecoder(registry),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		trajectoryRecorder: newTrajectoryRecorder(bucket, nil, "objectstore", func(trajectoryAuditFailure) {}),
	}
	response := agentic.ApprovalResponse{
		LoopID: loopID, CallID: callID, Decision: agentic.ApprovalDecisionReject,
		Reason: "policy", DecidedAt: time.Now().UTC(),
	}
	envelope := message.NewBaseMessage(response.Schema(), &response, "test")
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	c.handleApprovalResponseMessage(ctx, data)

	entity, err = handler.GetLoop(loopID)
	if err != nil {
		t.Fatal(err)
	}
	if entity.State != agentic.LoopStateFailed {
		t.Fatalf("approval rejection state = %q, want failed at iteration cap", entity.State)
	}
	if len(bucket.created) < 2 {
		t.Fatalf("created facts = %d, want rejection observation and terminal", len(bucket.created))
	}
	var kinds []agentic.TrajectoryKind
	for _, encoded := range bucket.created {
		var fact agentic.TrajectoryFactV1
		if err := json.Unmarshal(encoded, &fact); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, fact.Kind)
	}
	if kinds[len(kinds)-1] != agentic.TrajectoryKindLoopTerminal {
		t.Fatalf("approval fact order = %v, terminal must be last before persistence/publication", kinds)
	}
}

func TestBoundedTrajectoryAuditStageUsesAcceptedClosedVocabulary(t *testing.T) {
	t.Parallel()

	accepted := []trajectoryAuditStage{
		trajectoryStageProviderResolve,
		trajectoryStageEvidenceGet,
		trajectoryStageEvidencePut,
		trajectoryStageEvidenceVerify,
		trajectoryStageFactEncode,
		trajectoryStageFactCreate,
		trajectoryStageFactVerify,
	}
	for _, stage := range accepted {
		if got := boundedTrajectoryAuditStage(stage); got != stage {
			t.Errorf("boundedTrajectoryAuditStage(%q) = %q, want unchanged", stage, got)
		}
	}
	if got := boundedTrajectoryAuditStage("batch_budget"); got != trajectoryStageFactEncode {
		t.Fatalf("retired batch_budget stage mapped to %q, want bounded fallback %q", got, trajectoryStageFactEncode)
	}
}

func TestTrajectoryBatchTimeoutReportsBoundedStageAndAttemptIdentity(t *testing.T) {
	t.Parallel()

	recorder := newTrajectoryRecorder(&trajectoryTestBucket{}, nil, "objectstore", nil)
	recorder.newAttemptID = func() string { return "timeoutattempt" }
	release, acquired := recorder.acquireLoopBatch(context.Background(), "loop-timeout")
	if !acquired {
		t.Fatal("precondition: acquire loop batch token")
	}
	defer release()

	var logs bytes.Buffer
	component := &Component{
		trajectoryRecorder: recorder,
		logger:             slog.New(slog.NewTextHandler(&logs, nil)),
	}
	component.recordTrajectoryBatchWithin(context.Background(), []trajectoryObservation{{
		LoopID: "loop-timeout", Kind: agentic.TrajectoryKindModelCompleted,
	}}, 10*time.Millisecond)

	logged := logs.String()
	for _, want := range []string{
		"loop_id=loop-timeout",
		"attempt_id=timeoutattempt",
		"stage=fact_create",
		"reason=timeout",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("timeout log %q does not contain %q", logged, want)
		}
	}
	count, _ := component.trajectoryAuditHealth.snapshot()
	if count != 1 {
		t.Fatalf("audit health failure count = %d, want 1", count)
	}
}
