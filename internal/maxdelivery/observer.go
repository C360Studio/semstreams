// Package maxdelivery owns SemStreams' durable visibility for JetStream
// MaxDeliver exhaustion occurrences.
//
// It is internal because this is framework boot plumbing, not a component or an
// adopter-facing configuration surface. NATS publishes the occurrences; the
// framework provisions one bounded ledger and one fixed durable observer.
package maxdelivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
)

const (
	// captureStreamName is framework-owned and deliberately not configurable.
	// The stream is the durable occurrence ledger, not a current parked count.
	captureStreamName = "MAX_DELIVERY_EVENTS"

	// observerConsumerName is shared by every SemStreams replica. JetStream
	// therefore queue-delivers each occurrence to one observer while retaining
	// the durable acknowledgement floor across process restarts.
	observerConsumerName = "semstreams-max-delivery-observer"

	advisorySubjectPrefix = "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES"
	advisoryType          = "io.nats.jetstream.advisory.v1.max_deliver"
)

type decodeErrorReason string

const (
	decodeMalformed       decodeErrorReason = "malformed"
	decodeWrongType       decodeErrorReason = "wrong_type"
	decodeMissingField    decodeErrorReason = "missing_field"
	decodeSubjectMismatch decodeErrorReason = "subject_mismatch"
)

type advisory struct {
	Type           string    `json:"type"`
	ID             string    `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	Stream         string    `json:"stream"`
	Consumer       string    `json:"consumer"`
	StreamSequence uint64    `json:"stream_seq"`
	Deliveries     uint64    `json:"deliveries"`
	Domain         string    `json:"domain,omitempty"`
}

type decodeError struct {
	reason decodeErrorReason
	err    error
}

func (e *decodeError) Error() string { return e.err.Error() }
func (e *decodeError) Unwrap() error { return e.err }

func decodeReason(err error) decodeErrorReason {
	var target *decodeError
	if errors.As(err, &target) {
		return target.reason
	}
	return decodeMalformed
}

func decodeAdvisory(subject string, data []byte) (advisory, error) {
	var event advisory
	if err := json.Unmarshal(data, &event); err != nil {
		return advisory{}, &decodeError{reason: decodeMalformed, err: fmt.Errorf("decode advisory JSON: %w", err)}
	}
	if event.Type != advisoryType {
		return advisory{}, &decodeError{
			reason: decodeWrongType,
			err:    fmt.Errorf("advisory type %q, want %q", event.Type, advisoryType),
		}
	}
	if event.ID == "" || event.Timestamp.IsZero() || event.Stream == "" || event.Consumer == "" ||
		event.StreamSequence == 0 || event.Deliveries == 0 {
		return advisory{}, &decodeError{
			reason: decodeMissingField,
			err: fmt.Errorf("max-delivery advisory omits a required field: id=%t timestamp=%t stream=%t "+
				"consumer=%t stream_seq=%d deliveries=%d",
				event.ID != "", !event.Timestamp.IsZero(), event.Stream != "", event.Consumer != "",
				event.StreamSequence, event.Deliveries),
		}
	}

	wantSubject := advisorySubjectPrefix + "." + event.Stream + "." + event.Consumer
	if subject != wantSubject {
		return advisory{}, &decodeError{
			reason: decodeSubjectMismatch,
			err:    fmt.Errorf("advisory subject %q disagrees with typed payload %q", subject, wantSubject),
		}
	}
	return event, nil
}

type telemetry interface {
	reportOccurrence(context.Context, advisory) error
	reportDecodeError(context.Context, decodeErrorReason, error)
	reportSettlementError(context.Context, string, error)
}

type prometheusTelemetry struct {
	logger           *slog.Logger
	occurrences      *prometheus.CounterVec
	decodeErrors     *prometheus.CounterVec
	settlementErrors *prometheus.CounterVec
}

func newPrometheusTelemetry(registry *metric.MetricsRegistry, logger *slog.Logger) (*prometheusTelemetry, error) {
	if registry == nil {
		return nil, errors.New("max-delivery observer requires a metrics registry")
	}
	if logger == nil {
		logger = slog.Default()
	}

	occurrences := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "semstreams",
		Subsystem: "nats",
		Name:      "max_delivery_exhaustions_total",
		Help:      "Durably observed JetStream MaxDeliver exhaustion occurrences.",
	}, []string{"domain", "stream", "consumer"})
	decodeErrors := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "semstreams",
		Subsystem: "nats",
		Name:      "max_delivery_advisory_decode_errors_total",
		Help:      "Malformed or unexpected events in the MaxDeliver advisory ledger.",
	}, []string{"reason"})
	settlementErrors := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "semstreams",
		Subsystem: "nats",
		Name:      "max_delivery_advisory_settlement_errors_total",
		Help:      "Failed ACK or NAK attempts by the MaxDeliver advisory observer.",
	}, []string{"operation"})

	if err := registry.RegisterCounterVec("max-delivery-observer", "exhaustions", occurrences); err != nil {
		return nil, fmt.Errorf("register max-delivery occurrence metric: %w", err)
	}
	if err := registry.RegisterCounterVec("max-delivery-observer", "decode-errors", decodeErrors); err != nil {
		return nil, fmt.Errorf("register max-delivery decoder metric: %w", err)
	}
	if err := registry.RegisterCounterVec("max-delivery-observer", "settlement-errors", settlementErrors); err != nil {
		return nil, fmt.Errorf("register max-delivery settlement metric: %w", err)
	}
	return &prometheusTelemetry{
		logger: logger, occurrences: occurrences, decodeErrors: decodeErrors, settlementErrors: settlementErrors,
	}, nil
}

func (t *prometheusTelemetry) reportSettlementError(ctx context.Context, operation string, err error) {
	t.settlementErrors.WithLabelValues(operation).Inc()
	t.logger.ErrorContext(ctx, "Failed to settle MaxDeliver advisory; durable delivery will remain pending",
		slog.String("operation", operation), slog.Any("error", err))
}

func (t *prometheusTelemetry) reportOccurrence(ctx context.Context, event advisory) error {
	// Labels name only bounded infrastructure dimensions. The occurrence ID and
	// sequence stay in the structured log, never in Prometheus labels.
	t.occurrences.WithLabelValues(event.Domain, event.Stream, event.Consumer).Inc()
	t.logger.ErrorContext(ctx, "JetStream delivery exhausted MaxDeliver and is parked",
		slog.String("advisory_id", event.ID),
		slog.Time("advisory_timestamp", event.Timestamp),
		slog.String("domain", event.Domain),
		slog.String("stream", event.Stream),
		slog.String("consumer", event.Consumer),
		slog.Uint64("stream_sequence", event.StreamSequence),
		slog.Uint64("deliveries", event.Deliveries))
	return nil
}

func (t *prometheusTelemetry) reportDecodeError(
	ctx context.Context,
	reason decodeErrorReason,
	err error,
) {
	t.decodeErrors.WithLabelValues(string(reason)).Inc()
	t.logger.ErrorContext(ctx, "Invalid event in MaxDeliver advisory ledger; acknowledging poison event",
		slog.String("reason", string(reason)), slog.Any("error", err))
}

func handleMessage(ctx context.Context, msg jetstream.Msg, telemetry telemetry) {
	event, err := decodeAdvisory(msg.Subject(), msg.Data())
	if err != nil {
		telemetry.reportDecodeError(ctx, decodeReason(err), err)
		// The stream is a server-event ledger. A malformed entry cannot become
		// valid on retry, so acknowledge it after emitting decoder telemetry.
		if ackErr := msg.Ack(); ackErr != nil {
			telemetry.reportSettlementError(ctx, "ack_poison", ackErr)
		}
		return
	}

	if err := telemetry.reportOccurrence(ctx, event); err != nil {
		// Do not advance the durable floor until the operator signal has been
		// emitted. Unlimited MaxDeliver on this observer makes the retry durable
		// without generating a recursive MAX_DELIVERIES advisory about itself.
		if nakErr := msg.Nak(); nakErr != nil {
			telemetry.reportSettlementError(ctx, "nak_retry", nakErr)
		}
		return
	}
	if ackErr := msg.Ack(); ackErr != nil {
		telemetry.reportSettlementError(ctx, "ack_valid", ackErr)
	}
}

// Start binds the fixed durable observer to the already-provisioned capture
// stream. config.StreamsManager.EnsureStreams must have succeeded earlier in
// boot. The returned
// stop function releases only this process's consume context; it deliberately
// leaves the shared durable consumer and its acknowledgement floor.
func Start(
	ctx context.Context,
	client *natsclient.Client,
	registry *metric.MetricsRegistry,
	logger *slog.Logger,
) (func(), error) {
	telemetry, err := newPrometheusTelemetry(registry, logger)
	if err != nil {
		return nil, err
	}
	return start(ctx, client, telemetry)
}

func start(ctx context.Context, client *natsclient.Client, telemetry telemetry) (func(), error) {
	if client == nil {
		return nil, errors.New("max-delivery observer requires a NATS client")
	}
	if telemetry == nil {
		return nil, errors.New("max-delivery observer requires telemetry")
	}
	cfg := observerConsumerConfig()
	if err := client.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		handleMessage(msgCtx, msg, telemetry)
	}); err != nil {
		return nil, fmt.Errorf("start MaxDeliver observer: %w", err)
	}
	return func() { client.StopConsumer(captureStreamName, observerConsumerName) }, nil
}

func observerConsumerConfig() natsclient.StreamConsumerConfig {
	return natsclient.StreamConsumerConfig{
		StreamName:    captureStreamName,
		ConsumerName:  observerConsumerName,
		FilterSubject: advisorySubjectPrefix + ".>",
		DeliverPolicy: "all",
		AckPolicy:     "explicit",
		MaxDeliver:    0,
		AutoCreate:    false,
	}
}
