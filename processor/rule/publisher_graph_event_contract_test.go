package rule

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"sync/atomic"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type graphPublisherSpy struct {
	coreCalls   atomic.Int64
	streamCalls atomic.Int64
}

func (spy *graphPublisherSpy) Publish(context.Context, string, []byte) error {
	spy.coreCalls.Add(1)
	return nil
}

func (spy *graphPublisherSpy) PublishToStream(context.Context, string, []byte) error {
	spy.streamCalls.Add(1)
	return nil
}

func (spy *graphPublisherSpy) calls() int64 {
	return spy.coreCalls.Load() + spy.streamCalls.Load()
}

type marshalCounter struct {
	calls *atomic.Int64
}

func (counter marshalCounter) MarshalJSON() ([]byte, error) {
	counter.calls.Add(1)
	return json.Marshal("marshaled")
}

func TestPublishGraphEventsPreflightsCompleteBatch(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		mode := "disabled"
		if enabled {
			mode = "enabled"
		}
		t.Run(mode, func(t *testing.T) {
			marshalCalls := &atomic.Int64{}
			valid := publisherContractEvent(t, map[string]any{
				"probe": marshalCounter{calls: marshalCalls},
			})
			invalid := *valid
			invalid.EntityID = "three.part.id" // entity-id-audit: intentional-invalid literal "three.part.id" reason=arity

			var typedNil *gtypes.Event
			batches := []struct {
				name   string
				events []Event
			}{
				{"invalid first", []Event{&invalid, valid}},
				{"invalid later", []Event{valid, &invalid}},
				{"typed nil", []Event{valid, typedNil}},
			}
			for _, batch := range batches {
				t.Run(batch.name, func(t *testing.T) {
					spy := &graphPublisherSpy{}
					metrics := publisherContractMetrics()
					processor := &Processor{
						config:              &Config{PackID: "publisher-contract-test", EnableGraphIntegration: enabled},
						graphEventPublisher: spy,
						metrics:             metrics,
					}
					if err := processor.publishGraphEvents(context.Background(), batch.events); err == nil {
						t.Fatal("publishGraphEvents() = nil, want error")
					}
					if got := marshalCalls.Load(); got != 0 {
						t.Fatalf("marshal calls = %d, want 0", got)
					}
					if got := spy.calls(); got != 0 {
						t.Fatalf("publisher calls = %d, want 0", got)
					}
					if got := atomic.LoadInt64(&processor.eventsPublished); got != 0 {
						t.Fatalf("published event count = %d, want 0", got)
					}
					if got := testutil.ToFloat64(metrics.eventsPublishedTotal.WithLabelValues(valid.Subject(), valid.EventType())); got != 0 {
						t.Fatalf("published event metric = %v, want 0", got)
					}
					reason := "invalid_event"
					if batch.name == "typed nil" {
						reason = "nil_event"
					}
					if got := testutil.ToFloat64(metrics.graphEventRejections.WithLabelValues(
						graphEventRejectionLane, reason,
					)); got != 1 {
						t.Fatalf("rejection metric = %v, want exactly 1", got)
					}
				})
			}
		})
	}
}

func TestPublishGraphEventsValidDisabledBatchEncodesButHasNoEmission(t *testing.T) {
	marshalCalls := &atomic.Int64{}
	valid := publisherContractEvent(t, map[string]any{
		"probe": marshalCounter{calls: marshalCalls},
	})
	spy := &graphPublisherSpy{}
	metrics := publisherContractMetrics()
	processor := &Processor{
		config:              &Config{PackID: "publisher-contract-test", EnableGraphIntegration: false},
		graphEventPublisher: spy,
		metrics:             metrics,
	}

	if err := processor.publishGraphEvents(context.Background(), []Event{valid}); err != nil {
		t.Fatalf("publishGraphEvents() error = %v", err)
	}
	if got := marshalCalls.Load(); got != 1 {
		t.Fatalf("marshal calls = %d, want 1 preflight encoding", got)
	}
	if got := spy.calls(); got != 0 {
		t.Fatalf("publisher calls = %d, want 0", got)
	}
	if got := atomic.LoadInt64(&processor.eventsPublished); got != 0 {
		t.Fatalf("published event count = %d, want 0", got)
	}
	if got := testutil.ToFloat64(metrics.eventsPublishedTotal.WithLabelValues(valid.Subject(), valid.EventType())); got != 0 {
		t.Fatalf("published event metric = %v, want 0", got)
	}
}

func TestPublishGraphEventsMarshalFailureCannotPartiallyPublish(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		mode := "disabled"
		if enabled {
			mode = "enabled"
		}
		t.Run(mode, func(t *testing.T) {
			marshalCalls := &atomic.Int64{}
			valid := publisherContractEvent(t, map[string]any{
				"probe": marshalCounter{calls: marshalCalls},
			})
			invalid := publisherContractEvent(t, map[string]any{"not_finite": math.NaN()})
			spy := &graphPublisherSpy{}
			metrics := publisherContractMetrics()
			processor := &Processor{
				config:              &Config{PackID: "publisher-contract-test", EnableGraphIntegration: enabled},
				graphEventPublisher: spy,
				metrics:             metrics,
			}

			if err := processor.publishGraphEvents(context.Background(), []Event{valid, invalid}); err == nil {
				t.Fatal("publishGraphEvents() = nil, want marshal preflight error")
			}
			if got := marshalCalls.Load(); got != 1 {
				t.Fatalf("valid prefix marshal calls = %d, want 1 before later encoding failure", got)
			}
			if got := spy.calls(); got != 0 {
				t.Fatalf("publisher calls = %d, want 0", got)
			}
			if got := atomic.LoadInt64(&processor.eventsPublished); got != 0 {
				t.Fatalf("published event count = %d, want 0", got)
			}
			if got := testutil.ToFloat64(metrics.graphEventRejections.WithLabelValues(
				graphEventRejectionLane, "marshal_error",
			)); got != 1 {
				t.Fatalf("marshal rejection metric = %v, want exactly 1", got)
			}
		})
	}
}

func TestPublishGraphEventsValidEnabledBatchEmitsAfterPreflight(t *testing.T) {
	marshalCalls := &atomic.Int64{}
	valid := publisherContractEvent(t, map[string]any{
		"probe": marshalCounter{calls: marshalCalls},
	})
	spy := &graphPublisherSpy{}
	metrics := publisherContractMetrics()
	processor := &Processor{
		config:              &Config{PackID: "publisher-contract-test", EnableGraphIntegration: true},
		graphEventPublisher: spy,
		metrics:             metrics,
	}

	if err := processor.publishGraphEvents(context.Background(), []Event{valid}); err != nil {
		t.Fatalf("publishGraphEvents() error = %v", err)
	}
	if got := marshalCalls.Load(); got != 1 {
		t.Fatalf("marshal calls = %d, want 1", got)
	}
	if got := spy.calls(); got != 1 {
		t.Fatalf("publisher calls = %d, want 1", got)
	}
	if got := atomic.LoadInt64(&processor.eventsPublished); got != 1 {
		t.Fatalf("published event count = %d, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.eventsPublishedTotal.WithLabelValues(valid.Subject(), valid.EventType())); got != 1 {
		t.Fatalf("published event metric = %v, want 1", got)
	}
}

func TestFireRuleActionsInvalidBatchHasOnlyOneRejectionSideEffect(t *testing.T) {
	valid := publisherContractEvent(t, nil)
	invalid := *valid
	invalid.EntityID = "three.part.id" // entity-id-audit: intentional-invalid literal "three.part.id" reason=arity
	spy := &graphPublisherSpy{}
	metrics := publisherContractMetrics()
	processor := &Processor{
		logger:              slog.Default(),
		config:              &Config{PackID: "publisher-contract-test", EnableGraphIntegration: true},
		graphEventPublisher: spy,
		metrics:             metrics,
	}
	rule := &publisherContractRule{events: []Event{valid, &invalid}}

	processor.fireRuleActions(
		context.Background(), "publisher-contract", false, Definition{},
		map[string]*atomic.Int64{"publisher-contract": {}}, rule, nil,
	)

	if got := spy.calls(); got != 0 {
		t.Fatalf("publisher calls = %d, want 0", got)
	}
	if got := atomic.LoadInt64(&processor.eventsPublished); got != 0 {
		t.Fatalf("published event count = %d, want 0", got)
	}
	if got := atomic.LoadInt64(&processor.rulesTriggered); got != 0 {
		t.Fatalf("triggered rule count = %d, want 0", got)
	}
	if got := atomic.LoadInt64(&processor.errorCount); got != 0 {
		t.Fatalf("generic error count = %d, want 0; rejection metric is the sole counter side effect", got)
	}
	if got := testutil.ToFloat64(metrics.graphEventRejections.WithLabelValues(
		graphEventRejectionLane, "invalid_event",
	)); got != 1 {
		t.Fatalf("rejection metric = %v, want exactly 1", got)
	}
}

type publisherContractRule struct {
	events []Event
}

func (rule *publisherContractRule) Name() string                    { return "publisher-contract" }
func (rule *publisherContractRule) Subscribe() []string             { return nil }
func (rule *publisherContractRule) Evaluate([]message.Message) bool { return true }
func (rule *publisherContractRule) ExecuteEvents([]message.Message) ([]Event, error) {
	return rule.events, nil
}

func publisherContractEvent(t testing.TB, properties map[string]any) *gtypes.Event {
	t.Helper()
	entityID := semantictest.EntityID(t, "test", "rule", "publisher", "graph", "event", "001")
	event, err := gtypes.NewEntityUpdateEvent(entityID, properties, gtypes.EventMetadata{
		RuleName:  "publisher-contract",
		Timestamp: time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
		Source:    "rule-processor",
		Reason:    "publisher contract test",
	})
	if err != nil {
		t.Fatalf("NewEntityUpdateEvent: %v", err)
	}
	return event
}

func publisherContractMetrics() *Metrics {
	return &Metrics{
		eventsPublishedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "publisher_contract_events_published_total",
		}, []string{"subject", "event_type"}),
		graphEventRejections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "publisher_contract_graph_event_rejections_total",
		}, []string{"lane", "reason"}),
	}
}
