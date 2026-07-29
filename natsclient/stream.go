// Package natsclient provides JetStream stream management utilities.
package natsclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/pkg/errs"
)

// StreamConsumerConfig configures a JetStream consumer.
type StreamConsumerConfig struct {
	// StreamName is the name of the stream to consume from (required).
	StreamName string

	// ConsumerName is the durable consumer name. If empty, creates an ephemeral consumer.
	ConsumerName string

	// FilterSubject filters messages within the stream. If empty, receives all messages.
	FilterSubject string

	// DeliverPolicy determines where to start delivering messages.
	// Options: "all" (default), "last", "new", "by_start_time"
	DeliverPolicy string

	// AckPolicy determines how messages are acknowledged.
	// Options: "explicit" (default), "none", "all"
	AckPolicy string

	// MaxDeliver is the maximum number of delivery attempts (0 = unlimited).
	MaxDeliver int

	// AckWait is how long to wait for an ack before redelivery.
	// Default is 30 seconds.
	AckWait time.Duration

	// MaxAckPending limits the number of outstanding (unacknowledged) messages
	// that can be delivered to a consumer. This provides backpressure to prevent
	// overwhelming the consumer. 0 leaves it unset, so the NATS server applies its
	// default of 1000 for explicit-ack consumers; -1 means unlimited (gh#480).
	MaxAckPending int

	// AutoCreate enables automatic stream creation if it doesn't exist.
	AutoCreate bool

	// AutoCreateConfig is used when auto-creating a stream.
	// If nil, defaults are used based on FilterSubject.
	AutoCreateConfig *StreamAutoCreateConfig

	// BackOff overrides AckWait per retry attempt. Index 0 is the first retry
	// wait, index 1 is the second, and so on. The last value is used for all
	// subsequent retries. If empty, AckWait applies uniformly.
	BackOff []time.Duration

	// MessageTimeout is the context timeout for processing each message.
	// This timeout is passed to the handler and should accommodate the full
	// processing time including any downstream calls (e.g., LLM requests).
	// Default is 30 seconds if not specified.
	MessageTimeout time.Duration

	// DisableMessageTimeout keeps the handler context bound to the consumer
	// lifecycle. Use only when the handler applies its own ordinary work deadline
	// and needs to retain an in-flight delivery across that deadline.
	DisableMessageTimeout bool
}

// StreamAutoCreateConfig configures automatic stream creation.
type StreamAutoCreateConfig struct {
	// Subjects for the stream. If empty, derived from FilterSubject.
	Subjects []string

	// Storage type: "file" (default) or "memory"
	Storage string

	// Retention policy: "limits" (default), "interest", "work_queue"
	Retention string

	// MaxAge is the maximum age of messages (default 7 days).
	MaxAge time.Duration

	// Duplicates is the server-side duplicate-detection window for the
	// Nats-Msg-Id header (ADR-055 §5 "T1"). Zero leaves the NATS server
	// default (2m). Must be <= MaxAge or the server rejects creation;
	// ensureStreamForConsumer clamps it down to MaxAge when it exceeds it.
	Duplicates time.Duration

	// MaxBytes is the maximum total size (0 = unlimited).
	MaxBytes int64

	// MaxMsgs is the maximum number of messages (0 = unlimited).
	MaxMsgs int64

	// Replicas is the number of replicas (default 1).
	Replicas int
}

// DefaultStreamConfig returns default auto-create configuration.
func DefaultStreamConfig() *StreamAutoCreateConfig {
	return &StreamAutoCreateConfig{
		Storage:   "file",
		Retention: "limits",
		MaxAge:    7 * 24 * time.Hour, // 7 days
		Replicas:  1,
	}
}

// EnsureStream creates a stream if it doesn't exist, or returns the existing one.
func (c *Client) EnsureStream(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
	// Fail closed on a KV/ObjectStore backing-stream name before anything else,
	// so the refusal does not depend on connection or circuit state. This seam is
	// operator-reachable — processor/gated-dag exposes dispatch_stream as config
	// JSON and passes it straight through with a MaxAge — and while get-or-create
	// cannot restamp a LIVE bucket, on a fresh deployment it would name-squat the
	// bucket's reserved stream with a foreign TTL and the wrong subjects, which
	// the bucket's later catalog acquisition then collides with.
	if err := CheckOrdinaryStreamName(cfg.Name, "natsclient.Client.EnsureStream"); err != nil {
		return nil, errs.WrapFatal(err, "Client", "EnsureStream",
			"validate stream name "+cfg.Name)
	}

	if c.Status() == StatusCircuitOpen {
		return nil, ErrCircuitOpen
	}

	if c.Status() != StatusConnected {
		return nil, ErrNotConnected
	}

	js, err := c.JetStream()
	if err != nil {
		return nil, err
	}

	// Try to get existing stream first
	stream, err := js.Stream(ctx, cfg.Name)
	if err == nil {
		return stream, nil
	}

	// If not found, create it
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		// The bounds requirement applies HERE rather than at the top of the
		// function, because that is the difference between "this caller is about
		// to create an under-declared stream" and "this caller is binding a
		// stream someone else already created". Refusing the second would make a
		// non-owner unable to read an existing stream, which is not its call;
		// see stream_bounds.go.
		if err := CheckStreamBounds(cfg, "natsclient.Client.EnsureStream"); err != nil {
			return nil, errs.WrapFatal(err, "Client", "EnsureStream",
				"validate stream bounds for "+cfg.Name)
		}
		stream, err = js.CreateStream(ctx, cfg)
		if err != nil {
			c.recordFailure()
			c.jsMetrics.recordError("create_stream")
			return nil, errs.WrapTransient(err, "Client", "EnsureStream", "failed to create stream "+cfg.Name)
		}
		c.resetCircuit()
		c.jsMetrics.trackStream(cfg.Name, stream)
		return stream, nil
	}

	c.recordFailure()
	return nil, errs.WrapTransient(err, "Client", "EnsureStream", "failed to get stream "+cfg.Name)
}

// ConsumeStreamWithConfig creates a JetStream consumer with full configuration.
// The handler receives the raw jetstream.Msg which includes Ack(), Nak(), and Term() methods.
// Handler MUST call one of these methods to acknowledge the message.
func (c *Client) ConsumeStreamWithConfig(
	ctx context.Context,
	cfg StreamConsumerConfig,
	handler func(ctx context.Context, msg jetstream.Msg),
) error {
	return c.ConsumeStreamWithConfigContexts(ctx, ctx, cfg, handler)
}

// ConsumeStreamWithConfigContexts separates bounded setup I/O from callback
// lifetime. setupCtx governs stream lookup and consumer creation; handlerCtx is
// only the parent for delivered-message contexts after setup succeeds.
func (c *Client) ConsumeStreamWithConfigContexts(
	setupCtx context.Context,
	handlerCtx context.Context,
	cfg StreamConsumerConfig,
	handler func(ctx context.Context, msg jetstream.Msg),
) error {
	if cfg.StreamName == "" {
		return errs.WrapInvalid(
			fmt.Errorf("stream name is required"),
			"Client", "ConsumeStreamWithConfig", "missing stream name")
	}

	if c.Status() == StatusCircuitOpen {
		return ErrCircuitOpen
	}

	if c.Status() != StatusConnected {
		return ErrNotConnected
	}

	js, err := c.JetStream()
	if err != nil {
		return err
	}

	// Auto-create stream if enabled
	if cfg.AutoCreate {
		if err := c.ensureStreamForConsumer(setupCtx, js, cfg); err != nil {
			return err
		}
	}

	// Get the stream
	stream, err := js.Stream(setupCtx, cfg.StreamName)
	if err != nil {
		c.recordFailure()
		return errs.WrapTransient(err, "Client", "ConsumeStreamWithConfig",
			"failed to get stream "+cfg.StreamName)
	}

	// Stop any existing consumer context for this key BEFORE creating new one
	consumerKey := cfg.StreamName + ":" + cfg.ConsumerName
	c.consumersMu.Lock()
	if c.consumers != nil {
		if existingConsumer, exists := c.consumers[consumerKey]; exists {
			existingConsumer.Stop()
			delete(c.consumers, consumerKey)
			c.logger.Debug("Stopped existing consumer before recreation", slog.String("consumer_key", consumerKey))
		}
	}
	c.consumersMu.Unlock()

	// Build consumer configuration
	consumerCfg := c.buildConsumerConfig(cfg)

	// Create or update consumer
	consumer, err := stream.CreateOrUpdateConsumer(setupCtx, consumerCfg)
	if err != nil {
		c.recordFailure()
		return errs.WrapTransient(err, "Client", "ConsumeStreamWithConfig",
			"failed to create consumer for stream "+cfg.StreamName)
	}

	// Determine message timeout (default 30s if not specified).
	messageTimeout := cfg.MessageTimeout
	if messageTimeout <= 0 {
		messageTimeout = 30 * time.Second
	}

	// Start consuming
	if err := setupCtx.Err(); err != nil {
		return errs.WrapTransient(err, "Client", "ConsumeStreamWithConfig",
			"setup context ended before starting consumer")
	}
	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		// Extract trace from JetStream message headers
		msgCtx := handlerCtx
		if tc := ExtractTraceFromJetStream(msg.Headers()); tc != nil {
			msgCtx = ContextWithTrace(handlerCtx, tc)
		}

		msgCtx, cancel := messageHandlerContext(msgCtx, messageTimeout, cfg.DisableMessageTimeout)
		defer cancel()

		// Wrap handler with panic recovery and default Nak
		c.safeHandleMessage(msgCtx, msg, handler)
	})
	if err != nil {
		c.recordFailure()
		return errs.WrapTransient(err, "Client", "ConsumeStreamWithConfig",
			"failed to start consuming from stream "+cfg.StreamName)
	}
	if err := setupCtx.Err(); err != nil {
		consumeCtx.Stop()
		return errs.WrapTransient(err, "Client", "ConsumeStreamWithConfig",
			"setup context ended while starting consumer")
	}

	// Track consumer for JetStream metrics only after setup committed.
	if c.jsMetrics != nil {
		c.jsMetrics.trackConsumer(cfg.StreamName, consumerCfg.Durable, consumer)
	}

	// Track consumer for cleanup
	c.consumersMu.Lock()
	if c.consumers == nil {
		c.consumers = make(map[string]jetstream.ConsumeContext)
	}
	c.consumers[consumerKey] = consumeCtx
	c.consumersMu.Unlock()

	c.resetCircuit()
	return nil
}

func messageHandlerContext(parent context.Context, timeout time.Duration, disabled bool) (context.Context, context.CancelFunc) {
	if disabled {
		return parent, func() {}
	}
	return context.WithTimeout(parent, timeout)
}

// safeHandleMessage wraps the handler with panic recovery.
// If handler doesn't ack/nak/term the message, Nak is called by default.
func (c *Client) safeHandleMessage(ctx context.Context, msg jetstream.Msg, handler func(context.Context, jetstream.Msg)) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("panic in message handler", slog.Any("panic", r))
			// Nak on panic to allow redelivery
			_ = msg.Nak()
		}
	}()

	handler(ctx, msg)
}

// buildConsumerConfig converts StreamConsumerConfig to jetstream.ConsumerConfig.
func (c *Client) buildConsumerConfig(cfg StreamConsumerConfig) jetstream.ConsumerConfig {
	consumerCfg := jetstream.ConsumerConfig{}

	// Set durable name if provided
	if cfg.ConsumerName != "" {
		consumerCfg.Durable = cfg.ConsumerName
	}

	// Set filter subject
	if cfg.FilterSubject != "" {
		consumerCfg.FilterSubject = cfg.FilterSubject
	}

	// Set deliver policy
	switch cfg.DeliverPolicy {
	case "last":
		consumerCfg.DeliverPolicy = jetstream.DeliverLastPolicy
	case "last_per_subject":
		consumerCfg.DeliverPolicy = jetstream.DeliverLastPerSubjectPolicy
	case "new":
		consumerCfg.DeliverPolicy = jetstream.DeliverNewPolicy
	case "by_start_time":
		consumerCfg.DeliverPolicy = jetstream.DeliverByStartTimePolicy
	default: // "all" or empty
		consumerCfg.DeliverPolicy = jetstream.DeliverAllPolicy
	}

	// Set ack policy
	switch cfg.AckPolicy {
	case "none":
		consumerCfg.AckPolicy = jetstream.AckNonePolicy
	case "all":
		consumerCfg.AckPolicy = jetstream.AckAllPolicy
	default: // "explicit" or empty
		consumerCfg.AckPolicy = jetstream.AckExplicitPolicy
	}

	// Set max deliver
	if cfg.MaxDeliver > 0 {
		consumerCfg.MaxDeliver = cfg.MaxDeliver
	}

	// Set ack wait
	if cfg.AckWait > 0 {
		consumerCfg.AckWait = cfg.AckWait
	} else {
		consumerCfg.AckWait = 30 * time.Second // Default
	}

	// Set max ack pending for backpressure. Use != 0 (not > 0) so a -1 reaches
	// NATS as "unlimited" (gh#480) rather than being dropped to the server default;
	// 0 stays unset (server default 1000).
	if cfg.MaxAckPending != 0 {
		consumerCfg.MaxAckPending = cfg.MaxAckPending
	}

	// Set graduated backoff for retries
	if len(cfg.BackOff) > 0 {
		consumerCfg.BackOff = cfg.BackOff
	}

	return consumerCfg
}

// ensureStreamForConsumer auto-creates a stream if it doesn't exist.
func (c *Client) ensureStreamForConsumer(ctx context.Context, js jetstream.JetStream, cfg StreamConsumerConfig) error {
	// Check if stream exists
	_, err := js.Stream(ctx, cfg.StreamName)
	if err == nil {
		return nil // Stream exists
	}

	if !errors.Is(err, jetstream.ErrStreamNotFound) {
		return errs.WrapTransient(err, "Client", "ensureStreamForConsumer",
			"failed to check stream "+cfg.StreamName)
	}

	// Stream doesn't exist, create it
	autoConfig := cfg.AutoCreateConfig
	if autoConfig == nil {
		autoConfig = DefaultStreamConfig()
	}

	// Determine subjects
	subjects := autoConfig.Subjects
	if len(subjects) == 0 && cfg.FilterSubject != "" {
		// Derive subjects from filter subject
		subjects = []string{deriveStreamSubject(cfg.FilterSubject)}
	}
	if len(subjects) == 0 {
		return errs.WrapInvalid(
			fmt.Errorf("cannot auto-create stream without subjects"),
			"Client", "ensureStreamForConsumer", "no subjects for stream "+cfg.StreamName)
	}

	// Build stream config
	streamCfg := jetstream.StreamConfig{
		Name:     cfg.StreamName,
		Subjects: subjects,
		MaxAge:   autoConfig.MaxAge,
	}

	// Set storage
	switch autoConfig.Storage {
	case "memory":
		streamCfg.Storage = jetstream.MemoryStorage
	default:
		streamCfg.Storage = jetstream.FileStorage
	}

	// Set retention
	switch autoConfig.Retention {
	case "interest":
		streamCfg.Retention = jetstream.InterestPolicy
	case "work_queue":
		streamCfg.Retention = jetstream.WorkQueuePolicy
	default:
		streamCfg.Retention = jetstream.LimitsPolicy
	}

	// Set optional limits
	if autoConfig.MaxBytes > 0 {
		streamCfg.MaxBytes = autoConfig.MaxBytes
	}
	if autoConfig.MaxMsgs > 0 {
		streamCfg.MaxMsgs = autoConfig.MaxMsgs
	}
	if autoConfig.Replicas > 0 {
		streamCfg.Replicas = autoConfig.Replicas
	}
	if autoConfig.Duplicates > 0 {
		dup := autoConfig.Duplicates
		// Server rejects an explicit window > MaxAge; clamp to keep auto-create
		// from failing on a misconfigured window (mirrors config.createStream).
		if streamCfg.MaxAge > 0 && dup > streamCfg.MaxAge {
			c.logger.Warn("duplicates window exceeds max_age; clamping to max_age",
				"stream", cfg.StreamName, "duplicates", dup, "max_age", streamCfg.MaxAge)
			dup = streamCfg.MaxAge
		}
		streamCfg.Duplicates = dup
	}

	// Create the stream
	_, err = js.CreateStream(ctx, streamCfg)
	if err != nil {
		c.recordFailure()
		return errs.WrapTransient(err, "Client", "ensureStreamForConsumer",
			"failed to auto-create stream "+cfg.StreamName)
	}

	c.logger.Info("Auto-created stream", slog.String("stream", cfg.StreamName), slog.Any("subjects", subjects))
	return nil
}

// deriveStreamSubject converts a filter subject to a stream subject pattern.
// For example: "events.graph.entity.*" becomes "events.graph.entity.>"
func deriveStreamSubject(filterSubject string) string {
	// If already has >, use as-is
	if strings.HasSuffix(filterSubject, ">") {
		return filterSubject
	}

	// Replace trailing * with > for broader stream coverage
	if strings.HasSuffix(filterSubject, "*") {
		return filterSubject[:len(filterSubject)-1] + ">"
	}

	// For exact subjects, add > wildcard
	return filterSubject + ".>"
}

// PublishToStreamWithAck publishes a message to a JetStream subject with acknowledgment.
// If AutoCreate is true and the stream doesn't exist, it will be created.
// Trace context is auto-generated if not present, and propagated via NATS message headers.
func (c *Client) PublishToStreamWithAck(
	ctx context.Context,
	subject string,
	data []byte,
) (*jetstream.PubAck, error) {
	if c.Status() == StatusCircuitOpen {
		return nil, ErrCircuitOpen
	}

	if c.Status() != StatusConnected {
		return nil, ErrNotConnected
	}

	js, err := c.JetStream()
	if err != nil {
		return nil, err
	}

	// Auto-generate trace context if none exists
	if _, ok := TraceContextFromContext(ctx); !ok {
		ctx = ContextWithTrace(ctx, NewTraceContext())
	}

	// Build message with headers for trace propagation
	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
	}
	InjectTrace(ctx, msg)

	ack, err := js.PublishMsg(ctx, msg)
	if err != nil {
		c.recordFailure()
		c.jsMetrics.recordError("publish_to_stream")
		return nil, errs.WrapTransient(err, "Client", "PublishToStreamWithAck",
			"failed to publish to subject "+subject)
	}

	c.resetCircuit()
	return ack, nil
}

// StopConsumer stops a specific consumer by stream and consumer name.
// This stops the local consume context but does not delete the durable consumer from the server.
func (c *Client) StopConsumer(streamName, consumerName string) {
	c.consumersMu.Lock()
	defer c.consumersMu.Unlock()

	key := streamName + ":" + consumerName
	if consumeCtx, ok := c.consumers[key]; ok {
		consumeCtx.Stop()
		delete(c.consumers, key)
	}
}

// StopAndDeleteConsumer stops a specific consumer and deletes the durable consumer from the server.
// WARNING: This permanently removes the consumer's position. Use only for test cleanup
// or when you intentionally want to reset consumer state.
func (c *Client) StopAndDeleteConsumer(ctx context.Context, streamName, consumerName string) error {
	// First stop the local consume context
	c.StopConsumer(streamName, consumerName)

	// Then delete the durable consumer from the server
	js, err := c.JetStream()
	if err != nil {
		return err
	}

	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		return err
	}

	return stream.DeleteConsumer(ctx, consumerName)
}

// StopAllConsumers stops all active consumers.
func (c *Client) StopAllConsumers() {
	c.consumersMu.Lock()
	defer c.consumersMu.Unlock()

	for key, consumeCtx := range c.consumers {
		consumeCtx.Stop()
		delete(c.consumers, key)
	}
}
