package natsclient

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

type fakePolicyConsumeContext struct{ closed chan struct{} }

func (f *fakePolicyConsumeContext) Stop() {
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
}
func (f *fakePolicyConsumeContext) Drain()                  { f.Stop() }
func (f *fakePolicyConsumeContext) Closed() <-chan struct{} { return f.closed }

type fakeManagedPolicyConsumer struct {
	jetstream.Consumer
	info          *jetstream.ConsumerInfo
	infoErr       error
	events        *[]string
	consumeCalled bool
}

func (f *fakeManagedPolicyConsumer) Info(context.Context) (*jetstream.ConsumerInfo, error) {
	*f.events = append(*f.events, "info")
	return f.info, f.infoErr
}

func (f *fakeManagedPolicyConsumer) Consume(
	jetstream.MessageHandler,
	...jetstream.PullConsumeOpt,
) (jetstream.ConsumeContext, error) {
	f.consumeCalled = true
	*f.events = append(*f.events, "consume")
	return &fakePolicyConsumeContext{closed: make(chan struct{})}, nil
}

type capturedPolicyLog struct {
	message string
	attrs   map[string]any
}

type policyLogHandler struct {
	mu      sync.Mutex
	records []capturedPolicyLog
}

func (h *policyLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *policyLogHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *policyLogHandler) WithGroup(string) slog.Handler            { return h }
func (h *policyLogHandler) Handle(_ context.Context, record slog.Record) error {
	entry := capturedPolicyLog{message: record.Message, attrs: map[string]any{}}
	record.Attrs(func(attr slog.Attr) bool {
		entry.attrs[attr.Key] = attr.Value.Any()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, entry)
	h.mu.Unlock()
	return nil
}

func TestPortConsumerObservesBeforeDeliveryAndLogsOnce(t *testing.T) {
	events := []string{}
	consumer := &fakeManagedPolicyConsumer{events: &events, info: &jetstream.ConsumerInfo{
		Stream: "EVENTS", Name: "worker-events",
		Config: jetstream.ConsumerConfig{MaxAckPending: 5},
	}}
	logs := &policyLogHandler{}
	client := &Client{logger: slog.New(logs)}
	owner := PortConsumerContext{Component: "worker", Port: "events"}
	cfg := StreamConsumerConfig{StreamName: "EVENTS", ConsumerName: "worker-events", MaxAckPending: 5}

	identity := internalConsumerIdentity{stream: cfg.StreamName, durable: cfg.ConsumerName}
	consumeCtx, err := client.startPortConsumer(
		context.Background(), context.Background(), "ConsumeStreamWithConfig", owner, cfg,
		&guardedConsumer{Consumer: consumer}, identity, nil, func(context.Context, jetstream.Msg) {})
	if err != nil {
		t.Fatal(err)
	}
	defer consumeCtx.Stop()
	if !reflect.DeepEqual(events, []string{"info", "consume"}) {
		t.Fatalf("event order = %v, want observation before consume", events)
	}
	logs.mu.Lock()
	defer logs.mu.Unlock()
	if len(logs.records) != 1 {
		t.Fatalf("startup log records = %d, want exactly one", len(logs.records))
	}
	record := logs.records[0]
	if record.message != "JetStream consumer acknowledgement policy applied" {
		t.Fatalf("startup message = %q", record.message)
	}
	wantAttrs := map[string]any{
		"component": "worker", "port": "events", "stream": "EVENTS", "consumer": "worker-events",
		"policy_source": policySourcePort, "requested_max_ack_pending": int64(5), "effective_max_ack_pending": int64(5),
	}
	if !reflect.DeepEqual(record.attrs, wantAttrs) {
		t.Fatalf("startup attrs = %#v, want %#v", record.attrs, wantAttrs)
	}
}

func TestPortConsumerInitialInfoFailureIsTransientAndPreventsDelivery(t *testing.T) {
	events := []string{}
	cause := errors.New("consumer info unavailable")
	consumer := &fakeManagedPolicyConsumer{events: &events, infoErr: cause}
	client := &Client{logger: slog.Default()}
	owner := PortConsumerContext{Component: "worker", Port: "events"}
	cfg := StreamConsumerConfig{StreamName: "EVENTS", ConsumerName: "worker-events"}
	identity := internalConsumerIdentity{stream: cfg.StreamName, durable: cfg.ConsumerName}
	handle, err := client.startPortConsumer(
		context.Background(), context.Background(), "ConsumeStreamWithConfig", owner, cfg,
		&guardedConsumer{Consumer: consumer}, identity, nil, func(context.Context, jetstream.Msg) {})
	if !errs.IsTransient(err) || !errors.Is(err, cause) {
		t.Fatalf("error = %v, want transient preserving Info cause", err)
	}
	if handle != nil {
		t.Fatalf("handle = %v, want nil after Info failure", handle)
	}
	if consumer.consumeCalled || !reflect.DeepEqual(events, []string{"info"}) {
		t.Fatalf("delivery started after Info failure: called=%v events=%v", consumer.consumeCalled, events)
	}
}

func TestPortBackedConsumerOperationsRejectMissingOwnerBeforeIO(t *testing.T) {
	client := &Client{}
	cfg := StreamConsumerConfig{StreamName: "EVENTS", ConsumerName: "consumer", AckWait: 30}
	tests := []struct {
		name string
		call func() error
	}{
		{name: "ordinary", call: func() error {
			_, err := client.ConsumeStreamWithConfig(context.Background(), PortConsumerContext{}, cfg, nil)
			return err
		}},
		{name: "split contexts", call: func() error {
			_, err := client.ConsumeStreamWithConfigContexts(context.Background(), context.Background(), PortConsumerContext{}, cfg, nil)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errs.IsInvalid(err) {
				t.Fatalf("error = %v, want invalid owner context", err)
			}
		})
	}
}

func TestValidateObservedMaxAckPending(t *testing.T) {
	tests := []struct {
		name      string
		requested int
		effective int
		wantErr   bool
	}{
		{name: "zero accepts inherited", requested: 0, effective: 1000},
		{name: "positive exact", requested: 12, effective: 12},
		{name: "unlimited exact", requested: -1, effective: -1},
		{name: "positive mismatch", requested: 12, effective: 10, wantErr: true},
		{name: "unlimited mismatch", requested: -1, effective: 1000, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateObservedMaxAckPending(tt.requested, tt.effective)
			if tt.wantErr && !errs.IsInvalid(err) {
				t.Fatalf("error = %v, want invalid", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestClassifyConsumerPolicyError(t *testing.T) {
	for _, code := range []jetstream.ErrorCode{10121, 10082} {
		t.Run(strconv.Itoa(int(code)), func(t *testing.T) {
			apiErr := &jetstream.APIError{ErrorCode: code, Description: "rejected"}
			err := ClassifyConsumerPolicyError(apiErr, "test")
			if !errs.IsInvalid(err) || !errors.Is(err, apiErr) {
				t.Fatalf("error = %v, want invalid preserving API error", err)
			}
			var observed *jetstream.APIError
			if !errors.As(err, &observed) || observed.ErrorCode != code {
				t.Fatalf("API error not discoverable: %v", err)
			}
		})
	}

	transport := errors.New("transport unavailable")
	err := ClassifyConsumerPolicyError(transport, "test")
	if !errs.IsTransient(err) || !errors.Is(err, transport) {
		t.Fatalf("error = %v, want transient preserving cause", err)
	}
}
