package maxdelivery

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validAdvisoryJSON = `{
  "type":"io.nats.jetstream.advisory.v1.max_deliver",
  "id":"WJpCJQ1vC4I7YyFGMjmH9h",
  "timestamp":"2026-08-12T12:00:00Z",
  "stream":"OBJECTSTORE_WRITES",
  "consumer":"objectstore-main",
  "stream_seq":42,
  "deliveries":3,
  "domain":"production"
}`

func TestDecodeAdvisory(t *testing.T) {
	t.Parallel()
	require.LessOrEqual(t, len(validAdvisoryJSON), 512,
		"retention sizing uses a conservative 512-byte representative typed advisory")

	tests := []struct {
		name    string
		subject string
		body    string
		want    advisory
		reason  decodeErrorReason
	}{
		{
			name:    "valid typed advisory",
			subject: advisorySubjectPrefix + ".OBJECTSTORE_WRITES.objectstore-main",
			body:    validAdvisoryJSON,
			want: advisory{
				Type: "io.nats.jetstream.advisory.v1.max_deliver", ID: "WJpCJQ1vC4I7YyFGMjmH9h",
				Timestamp: time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC),
				Stream:    "OBJECTSTORE_WRITES", Consumer: "objectstore-main",
				StreamSequence: 42, Deliveries: 3, Domain: "production",
			},
		},
		{
			name: "malformed JSON", subject: advisorySubjectPrefix + ".S.C",
			body: `{`, reason: decodeMalformed,
		},
		{
			name: "wrong typed event", subject: advisorySubjectPrefix + ".S.C",
			body:   `{"type":"io.nats.jetstream.advisory.v1.nak","id":"x","timestamp":"2026-08-12T12:00:00Z","stream":"S","consumer":"C","stream_seq":1,"deliveries":1}`,
			reason: decodeWrongType,
		},
		{
			name: "missing required field", subject: advisorySubjectPrefix + ".S.C",
			body:   `{"type":"io.nats.jetstream.advisory.v1.max_deliver","id":"x","timestamp":"2026-08-12T12:00:00Z","stream":"S","consumer":"C","stream_seq":0,"deliveries":1}`,
			reason: decodeMissingField,
		},
		{
			name: "subject disagrees with payload", subject: advisorySubjectPrefix + ".OTHER.C",
			body:   `{"type":"io.nats.jetstream.advisory.v1.max_deliver","id":"x","timestamp":"2026-08-12T12:00:00Z","stream":"S","consumer":"C","stream_seq":1,"deliveries":1}`,
			reason: decodeSubjectMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeAdvisory(tt.subject, []byte(tt.body))
			if tt.reason != "" {
				require.Error(t, err)
				assert.Equal(t, tt.reason, decodeReason(err))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStartRejectsNilAndCanceledContextBeforeAcquisition(t *testing.T) {
	stop, err := start(nil, nil, nil)
	require.Nil(t, stop)
	require.Error(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stop, err = start(ctx, nil, nil)
	require.Nil(t, stop)
	require.ErrorIs(t, err, context.Canceled)
}

type recordingMsg struct {
	jetstream.Msg
	subject string
	data    []byte
	acks    int
	naks    int
	ackErr  error
	nakErr  error
}

func (m *recordingMsg) Subject() string { return m.subject }
func (m *recordingMsg) Data() []byte    { return m.data }
func (m *recordingMsg) Ack() error      { m.acks++; return m.ackErr }
func (m *recordingMsg) Nak() error      { m.naks++; return m.nakErr }

type recordingTelemetry struct {
	mu               sync.Mutex
	occurrences      []advisory
	decodeErrors     []decodeErrorReason
	reportErr        error
	settlementErrors []string
}

func (r *recordingTelemetry) reportOccurrence(_ context.Context, event advisory) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.occurrences = append(r.occurrences, event)
	return r.reportErr
}

func (r *recordingTelemetry) reportDecodeError(_ context.Context, reason decodeErrorReason, _ error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.decodeErrors = append(r.decodeErrors, reason)
}

func (r *recordingTelemetry) reportSettlementError(_ context.Context, operation string, _ error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settlementErrors = append(r.settlementErrors, operation)
}

func TestHandleMessageDisposition(t *testing.T) {
	t.Parallel()

	t.Run("valid advisory acks only after telemetry succeeds", func(t *testing.T) {
		t.Parallel()
		telemetry := &recordingTelemetry{}
		msg := &recordingMsg{
			subject: advisorySubjectPrefix + ".OBJECTSTORE_WRITES.objectstore-main",
			data:    []byte(validAdvisoryJSON),
		}

		handleMessage(context.Background(), msg, telemetry)

		assert.Equal(t, 1, msg.acks)
		assert.Zero(t, msg.naks)
		require.Len(t, telemetry.occurrences, 1)
	})

	t.Run("telemetry failure naks for redelivery", func(t *testing.T) {
		t.Parallel()
		telemetry := &recordingTelemetry{reportErr: errors.New("metric sink unavailable")}
		msg := &recordingMsg{
			subject: advisorySubjectPrefix + ".OBJECTSTORE_WRITES.objectstore-main",
			data:    []byte(validAdvisoryJSON),
		}

		handleMessage(context.Background(), msg, telemetry)

		assert.Zero(t, msg.acks)
		assert.Equal(t, 1, msg.naks)
	})

	t.Run("valid advisory ack failure emits bounded settlement telemetry", func(t *testing.T) {
		t.Parallel()
		telemetry := &recordingTelemetry{}
		msg := &recordingMsg{
			subject: advisorySubjectPrefix + ".OBJECTSTORE_WRITES.objectstore-main",
			data:    []byte(validAdvisoryJSON), ackErr: errors.New("ack denied"),
		}

		handleMessage(context.Background(), msg, telemetry)

		assert.Equal(t, []string{"ack_valid"}, telemetry.settlementErrors)
	})

	t.Run("retry nak failure emits bounded settlement telemetry", func(t *testing.T) {
		t.Parallel()
		telemetry := &recordingTelemetry{reportErr: errors.New("metric sink unavailable")}
		msg := &recordingMsg{
			subject: advisorySubjectPrefix + ".OBJECTSTORE_WRITES.objectstore-main",
			data:    []byte(validAdvisoryJSON), nakErr: errors.New("nak denied"),
		}

		handleMessage(context.Background(), msg, telemetry)

		assert.Equal(t, []string{"nak_retry"}, telemetry.settlementErrors)
	})

	t.Run("malformed poison reports decoder error then acks", func(t *testing.T) {
		t.Parallel()
		telemetry := &recordingTelemetry{}
		msg := &recordingMsg{subject: advisorySubjectPrefix + ".S.C", data: []byte(`{`)}

		handleMessage(context.Background(), msg, telemetry)

		assert.Equal(t, 1, msg.acks)
		assert.Zero(t, msg.naks)
		assert.Equal(t, []decodeErrorReason{decodeMalformed}, telemetry.decodeErrors)
	})
}

func TestObserverConsumerDeclaration(t *testing.T) {
	t.Parallel()

	consumer := observerConsumerConfig()
	assert.Equal(t, captureStreamName, consumer.StreamName)
	assert.Equal(t, observerConsumerName, consumer.ConsumerName)
	assert.Equal(t, advisorySubjectPrefix+".>", consumer.FilterSubject)
	assert.Equal(t, "all", consumer.DeliverPolicy)
	assert.Equal(t, "explicit", consumer.AckPolicy)
	assert.Zero(t, consumer.MaxDeliver, "observer delivery must be unlimited to avoid recursive exhaustion")
}

func TestPrometheusOccurrenceLabelsStayBounded(t *testing.T) {
	t.Parallel()

	telemetry := &prometheusTelemetry{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		occurrences: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "test_max_delivery_exhaustions_total",
		}, []string{"domain", "stream", "consumer"}),
		decodeErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "test_max_delivery_decode_errors_total",
		}, []string{"reason"}),
	}
	require.NoError(t, telemetry.reportOccurrence(context.Background(), advisory{
		ID: "high-cardinality-id", Domain: "prod", Stream: "WRITES", Consumer: "objectstore",
		StreamSequence: 99123, Deliveries: 3,
	}))

	assert.Equal(t, float64(1), testutil.ToFloat64(
		telemetry.occurrences.WithLabelValues("prod", "WRITES", "objectstore")))
	desc := telemetry.occurrences.WithLabelValues("prod", "WRITES", "objectstore").Desc().String()
	assert.NotContains(t, desc, "high-cardinality-id")
	assert.NotContains(t, desc, "99123")
}
