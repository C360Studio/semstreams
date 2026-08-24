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
	// overwhelming the consumer. 0 leaves it unset, so NATS applies inherited,
	// default, or server/account-capped policy; -1 means unlimited outstanding acks (gh#480).
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

// PortConsumerContext identifies the component port that owns a consumer.
// Stream, consumer, and policy values are derived from the final configuration and NATS.
type PortConsumerContext struct {
	Component      string
	Port           string
	ComponentOwned bool
}

// StreamAutoCreateConfig configures automatic stream creation.
type StreamAutoCreateConfig struct {
	// Subjects for the stream. If empty, derived from FilterSubject.
	Subjects []string

	// Storage type: "file" (default) or "memory"
	Storage string

	// Retention policy: "limits" (default), "interest", "work_queue"
	Retention string

	// MaxAge is the maximum age of messages. REQUIRED and must be positive:
	// auto-create is stream provisioning, and an ordinary stream declares finite
	// bounds (see CheckStreamBounds). There is no framework default — one would be
	// a bound nobody chose, indistinguishable in the operator surface from one
	// somebody did, which is what the bounds requirement exists to end.
	MaxAge time.Duration

	// Duplicates is the server-side duplicate-detection window for the
	// Nats-Msg-Id header (ADR-055 §5 "T1"). Zero leaves the NATS server
	// default (2m). Must be <= MaxAge or the server rejects creation;
	// ensureStreamForConsumer clamps it down to MaxAge when it exceeds it.
	Duplicates time.Duration

	// MaxBytes is the maximum total size. REQUIRED and must be positive, for the
	// same reason as MaxAge: JetStream reads 0 and -1 alike as unlimited, so
	// neither is a declaration.
	MaxBytes int64

	// Discard is what happens at the ceiling: jetstream.DiscardOld evicts the
	// oldest, jetstream.DiscardNew refuses the write.
	//
	// It exists here so an auto-create path can carry a declaration's discard
	// policy. Without it this struct could express a bound but not what happens
	// when the bound is reached, so a caller recreating a stream from an operator's
	// declaration silently substituted DiscardOld — the zero value — for whatever
	// they chose. It cannot be REQUIRED (DiscardOld being the zero value is exactly
	// why), but it can at least be expressible.
	Discard jetstream.DiscardPolicy

	// MaxMsgs is the maximum number of messages (0 = unlimited).
	MaxMsgs int64

	// Replicas is the number of replicas (default 1).
	Replicas int
}

// DefaultStreamConfig returns the auto-create defaults for the fields that HAVE
// defaults: storage tier, retention policy and replica count.
//
// It deliberately declares NO bounds. It used to return MaxAge 7 days with
// MaxBytes unset — the exact silent framework default the bounds requirement
// removed from the configuration path, still handing out a retention window
// nobody chose and no size ceiling at all. A caller that auto-creates must state
// its own bounds; CheckStreamBounds refuses the creation otherwise.
func DefaultStreamConfig() *StreamAutoCreateConfig {
	return &StreamAutoCreateConfig{
		Storage:   "file",
		Retention: "limits",
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
		// The caller's configuration is about to be discarded, which is correct —
		// a non-owner must not restamp another owner's stream — but doing it in
		// silence is how a stream two components declare has its limits decided
		// permanently by boot order with no diagnostic on either side.
		c.reportBindDivergence(cfg, stream)
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

// reportBindDivergence logs what binding an existing stream discarded.
//
// WARN rather than Info, and on EVERY bind rather than once. The condition is not
// an error — the call succeeded and returning the existing stream is correct — but
// it means a declaration in this process's code or configuration is not in effect,
// and nothing else will ever say so. Repetition is part of the signal for the same
// reason it is on the provisioner's repair path: a divergence that reappears on
// every boot, with the observed value alternating between two processes' values, is
// contested ownership rather than one stale stream.
//
// It reports and returns. No field is written, no error is produced, and the
// caller receives the same stream it would have received before.
func (c *Client) reportBindDivergence(declared jetstream.StreamConfig, stream jetstream.Stream) {
	if c.logger == nil || stream == nil {
		return
	}
	info := stream.CachedInfo()
	if info == nil {
		// Nothing to compare against. Silence is right here: claiming a divergence
		// we could not measure would be worse than not measuring it.
		return
	}

	// An UNBOUNDED live stream is reported on its own terms, because the
	// declared-versus-observed comparison cannot see this case. That comparison
	// comes from the caller's declaration, and an under-declared caller — the one
	// the create-versus-bind split sends down this path — declares nothing to
	// compare. This fires on a property of the LIVE STREAM instead, so it has none
	// of the false-positive problem, and it is the migration signal for a stream
	// that predates the bounds requirement: creation would have been refused, and
	// binding leaves it exactly as unbounded as it was.
	if info.Config.MaxAge <= 0 || info.Config.MaxBytes <= 0 {
		c.logger.Warn(
			"bound an existing ORDINARY stream that declares no finite bounds; creating it today would be "+
				"refused, and binding it does not repair it",
			slog.String("stream", declared.Name),
			slog.String("observed_max_age", unlimitedOr(info.Config.MaxAge)),
			slog.String("observed_max_bytes", unlimitedOrBytes(info.Config.MaxBytes)),
			slog.String("remedy",
				"this is a migration condition, not contested ownership: the stream's owner should set "+
					"finite limits on it (`nats stream edit`), or declare it in configuration so the stream "+
					"provisioner reconciles it. An archive whose contract is permanence belongs in "+
					"archival_streams instead"),
		)
	}

	divergences := DiffDeclaredStream(declared, info.Config)
	if len(divergences) == 0 {
		return
	}

	c.logger.Warn(
		"bound an existing stream whose live configuration diverges from this caller's declaration; "+
			"the declaration is NOT in effect and nothing restamped the stream",
		slog.String("stream", declared.Name),
		slog.Any("divergence", DivergenceLabels(divergences)),
		slog.String("remedy",
			"if the live stream declares no bound at all this is a MIGRATION condition — its owner sets "+
				"finite limits on it, or declares it in configuration so the provisioner reconciles it. If "+
				"it declares a DIFFERENT bound, two owners are declaring one stream: give this caller its "+
				"own stream name, or agree one owner and have the others bind by name without declaring "+
				"limits they do not own"),
	)
}

// Bounds on how long a consumer setup tolerates a stream that the node serving
// the request has not applied yet. Fixed package constants in the style of the
// readiness bounds in request.go (DefaultReadinessProbeTimeout /
// DefaultReadinessBudget): sized once by the framework, never configured, never
// predicted by a caller.
//
// A clustered JetStream answers stream creation from the meta leader while every
// other node applies that assignment from the meta Raft log asynchronously. A
// process whose connection lands on a lagging node — cold start on freshly
// provisioned storage, or a node rejoining after a partition — is answered
// 404/10059 for a stream that provably exists, moments after EnsureStreams
// proved it. That window is a fact the framework can observe and the operator
// cannot, so the framework absorbs it here rather than exporting a wait-first
// method to remember or a timeout to guess.
const (
	// streamVisibilityProbeInterval spaces re-observations of the serving node's
	// own view. Metadata apply completes in milliseconds on a healthy cluster, so
	// probing is frequent enough to add no perceptible boot latency, and the
	// budget still caps the pathological case at ~100 lightweight STREAM.INFO
	// requests.
	streamVisibilityProbeInterval = 50 * time.Millisecond
	// streamVisibilityBudget bounds the TOTAL wait before an absent stream is
	// reported as absent. It matches the propagation budget the clustered test
	// fixture already uses (internal/maxdelivery: streamAssignmentBudget), so one
	// number answers "how long may a node lag the meta layer" everywhere, and it
	// stays small so a genuinely missing stream still fails boot promptly.
	streamVisibilityBudget = 5 * time.Second
)

// awaitStreamVisible resolves a stream handle, tolerating ONLY the
// not-yet-applied window above. Every other failure — permission denial,
// transport fault, cancelled caller — is the server's real answer and is
// returned on first observation, so this stays a bounded propagation tolerance
// and never becomes retry-until-green.
//
// The wait is bounded by the caller's context AND the budget, whichever ends
// first, and completes before returning: no goroutine outlives the call.
// Whichever way it ends, jetstream.ErrStreamNotFound stays reachable through
// errors.Is so the call site's existing transient wrap still classifies the
// failure as an absent stream.
func (c *Client) awaitStreamVisible(
	ctx context.Context, js jetstream.JetStream, name string,
) (jetstream.Stream, error) {
	waitCtx, cancel := context.WithTimeout(ctx, streamVisibilityBudget)
	defer cancel()

	probe := time.NewTicker(streamVisibilityProbeInterval)
	defer probe.Stop()

	started := time.Now()
	var absent error
	for {
		stream, err := js.Stream(waitCtx, name)
		switch {
		case err == nil:
			if absent != nil {
				// The only report of a wait that succeeded. Logged once on
				// completion, never per probe: a multi-second stall at boot is
				// otherwise invisible, while per-probe logging would bury it.
				c.logger.Info("stream became visible after waiting for cluster metadata propagation",
					slog.String("stream", name),
					slog.Duration("waited", time.Since(started)))
			}
			return stream, nil
		case errors.Is(err, jetstream.ErrStreamNotFound):
			absent = err
		case absent != nil && waitCtx.Err() != nil:
			// The wait ended mid-request, so this error reports that ending rather
			// than the stream. Report what every completed observation measured.
			return nil, streamNotVisible(ctx, absent)
		default:
			return nil, err
		}

		select {
		case <-waitCtx.Done():
			return nil, streamNotVisible(ctx, absent)
		case <-probe.C:
		}
	}
}

// streamNotVisible explains which bound ended the wait while keeping the absent
// classification — and, when the caller ended it, the caller's own cause —
// reachable through errors.Is.
//
// Note for callers branching on the result: when the caller's context ended the
// wait, the returned error satisfies BOTH errors.Is(err, ctx.Err()) and
// errors.Is(err, jetstream.ErrStreamNotFound), so an if/else chain over it
// decides by ORDER, not by exclusivity. Test the cancellation first if
// "shutting down" must win over "stream is absent".
func streamNotVisible(ctx context.Context, absent error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("operation context ended while waiting for stream visibility: %w",
			errors.Join(err, absent))
	}
	return fmt.Errorf("stream visibility budget %s exhausted: %w", streamVisibilityBudget, absent)
}

// ConsumeStreamWithConfig creates a port-backed JetStream consumer with full
// configuration. The caller owns the exact returned native handle and must
// Drain it and await Closed before canceling callback authority. The handler
// receives the raw jetstream.Msg and must settle it with Ack, Nak, or Term.
func (c *Client) ConsumeStreamWithConfig(
	ctx context.Context,
	owner PortConsumerContext,
	cfg StreamConsumerConfig,
	handler func(ctx context.Context, msg jetstream.Msg),
) (jetstream.ConsumeContext, error) {
	return c.consumePortStreamWithConfigContexts(
		ctx, ctx, "ConsumeStreamWithConfig", owner, cfg, handler,
	)
}

// ConsumeInternalStreamWithConfig consumes a stream for framework-internal users
// that make no JetStreamPort configuration claim. The caller owns the exact
// returned native handle and must Drain it and await Closed before canceling
// callback authority. Client.Close does not manage internal consumer children.
func (c *Client) ConsumeInternalStreamWithConfig(
	ctx context.Context,
	cfg StreamConsumerConfig,
	handler func(ctx context.Context, msg jetstream.Msg),
) (jetstream.ConsumeContext, error) {
	if ctx == nil {
		return nil, errs.WrapInvalid(errors.New("nil context"),
			"Client", "ConsumeInternalStreamWithConfig", "missing operation context")
	}
	if err := ctx.Err(); err != nil {
		return nil, errs.WrapInvalid(err,
			"Client", "ConsumeInternalStreamWithConfig", "operation context already ended")
	}
	if cfg.StreamName == "" {
		return nil, errs.WrapInvalid(errors.New("stream name is required"),
			"Client", "ConsumeInternalStreamWithConfig", "missing stream name")
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
	if cfg.AutoCreate {
		if err := c.ensureStreamForConsumer(ctx, js, cfg); err != nil {
			return nil, err
		}
	}
	stream, err := c.awaitStreamVisible(ctx, js, cfg.StreamName)
	if err != nil {
		c.recordFailure()
		return nil, errs.WrapTransient(err, "Client", "ConsumeInternalStreamWithConfig",
			"failed to get stream "+cfg.StreamName)
	}

	identity := internalConsumerIdentity{stream: cfg.StreamName, durable: cfg.ConsumerName}
	claim, err := c.reserveInternalConsumer(identity, "ConsumeInternalStreamWithConfig")
	if err != nil {
		return nil, err
	}
	releaseClaim := func() { c.releaseInternalConsumer(identity, claim) }
	committed := false
	defer func() {
		if !committed {
			releaseClaim()
		}
	}()

	consumerCfg := c.buildConsumerConfig(cfg)
	consumer, err := stream.CreateOrUpdateConsumer(ctx, consumerCfg)
	if err != nil {
		c.recordFailure()
		return nil, ClassifyConsumerPolicyError(err, "ConsumeInternalStreamWithConfig")
	}
	guarded := &guardedConsumer{Consumer: consumer}
	observed, err := c.observeInternalConsumer(ctx, guarded)
	if err != nil {
		return nil, err
	}
	if identity.durable != "" && observed != identity {
		return nil, errs.WrapInvalid(
			fmt.Errorf("configured identity %s:%s observed as %s:%s",
				identity.stream, identity.durable, observed.stream, observed.durable),
			"Client", "ConsumeInternalStreamWithConfig", "consumer identity changed during setup")
	}
	if err := ctx.Err(); err != nil {
		return nil, errs.WrapTransient(err, "Client", "ConsumeInternalStreamWithConfig",
			"setup context ended before starting consumer")
	}

	if c.jsMetrics != nil {
		c.jsMetrics.trackConsumer(observed.stream, observed.durable, guarded)
	}
	forgetObservation := func() {
		if c.jsMetrics != nil {
			c.jsMetrics.forgetConsumer(observed.stream, observed.durable)
		}
	}
	consumeCtx, err := guarded.Consume(func(msg jetstream.Msg) {
		msgCtx := ctx
		if tc := ExtractTraceFromJetStream(msg.Headers()); tc != nil {
			msgCtx = ContextWithTrace(ctx, tc)
		}
		messageTimeout := cfg.MessageTimeout
		if messageTimeout <= 0 {
			messageTimeout = 30 * time.Second
		}
		msgCtx, cancel := messageHandlerContext(msgCtx, messageTimeout, cfg.DisableMessageTimeout)
		defer cancel()
		c.safeHandleMessage(msgCtx, msg, handler)
	})
	if err != nil {
		forgetObservation()
		c.recordFailure()
		return nil, errs.WrapTransient(err, "Client", "ConsumeInternalStreamWithConfig",
			"failed to start consuming from stream "+cfg.StreamName)
	}

	committed = true
	c.resetCircuit()
	closed := consumeCtx.Closed()
	go func() {
		<-closed
		forgetObservation()
		releaseClaim()
	}()
	return consumeCtx, nil
}

type internalConsumerIdentity struct {
	stream  string
	durable string
}

type internalConsumerClaim struct{}

func (c *Client) reserveInternalConsumer(
	identity internalConsumerIdentity,
	operation string,
) (*internalConsumerClaim, error) {
	if identity.durable == "" {
		return nil, nil
	}
	c.internalClaimsMu.Lock()
	defer c.internalClaimsMu.Unlock()
	if c.internalClaims == nil {
		c.internalClaims = make(map[internalConsumerIdentity]*internalConsumerClaim)
	}
	if _, exists := c.internalClaims[identity]; exists {
		return nil, errs.WrapInvalid(
			fmt.Errorf("consumer %s:%s already has a local owner", identity.stream, identity.durable),
			"Client", operation, "duplicate local durable identity")
	}
	claim := &internalConsumerClaim{}
	c.internalClaims[identity] = claim
	return claim, nil
}

func (c *Client) startPortConsumer(
	setupCtx context.Context,
	handlerCtx context.Context,
	operation string,
	owner PortConsumerContext,
	cfg StreamConsumerConfig,
	consumer *guardedConsumer,
	identity internalConsumerIdentity,
	claim *internalConsumerClaim,
	handler func(context.Context, jetstream.Msg),
) (jetstream.ConsumeContext, error) {
	policyKey, err := c.observePortConsumerPolicy(setupCtx, owner, cfg, consumer)
	if err != nil {
		return nil, err
	}
	forgetPolicy := func() {
		if c.jsMetrics != nil {
			c.jsMetrics.forgetPolicy(policyKey)
		}
	}
	if err := setupCtx.Err(); err != nil {
		forgetPolicy()
		return nil, errs.WrapTransient(err, "Client", operation,
			"setup context ended before starting consumer")
	}
	if err := handlerCtx.Err(); err != nil {
		forgetPolicy()
		return nil, errs.WrapInvalid(err, "Client", operation,
			"handler context ended before starting consumer")
	}

	messageTimeout := cfg.MessageTimeout
	if messageTimeout <= 0 {
		messageTimeout = 30 * time.Second
	}
	consumeCtx, err := consumer.Consume(func(msg jetstream.Msg) {
		msgCtx := handlerCtx
		if tc := ExtractTraceFromJetStream(msg.Headers()); tc != nil {
			msgCtx = ContextWithTrace(handlerCtx, tc)
		}
		msgCtx, cancel := messageHandlerContext(msgCtx, messageTimeout, cfg.DisableMessageTimeout)
		defer cancel()
		c.safeHandleMessage(msgCtx, msg, handler)
	})
	if err != nil {
		forgetPolicy()
		c.recordFailure()
		return nil, errs.WrapTransient(err, "Client", operation,
			"failed to start consuming from stream "+cfg.StreamName)
	}

	if c.jsMetrics != nil {
		c.jsMetrics.trackConsumer(identity.stream, identity.durable, consumer)
	}
	forgetObservation := func() {
		if c.jsMetrics != nil {
			c.jsMetrics.forgetConsumer(identity.stream, identity.durable)
			c.jsMetrics.forgetPolicy(policyKey)
		}
	}
	c.resetCircuit()
	closed := consumeCtx.Closed()
	go func() {
		<-closed
		forgetObservation()
		c.releaseInternalConsumer(identity, claim)
	}()
	return consumeCtx, nil
}

// ConsumeStreamWithConfigContexts creates a port-backed JetStream consumer with
// separate setup and delivered-message authority. The caller owns the exact
// returned native handle and must Drain it and await Closed before canceling
// callback authority.
func (c *Client) ConsumeStreamWithConfigContexts(
	setupCtx context.Context,
	handlerCtx context.Context,
	owner PortConsumerContext,
	cfg StreamConsumerConfig,
	handler func(ctx context.Context, msg jetstream.Msg),
) (jetstream.ConsumeContext, error) {
	return c.consumePortStreamWithConfigContexts(
		setupCtx, handlerCtx, "ConsumeStreamWithConfigContexts", owner, cfg, handler,
	)
}

func (c *Client) consumePortStreamWithConfigContexts(
	setupCtx context.Context,
	handlerCtx context.Context,
	operation string,
	owner PortConsumerContext,
	cfg StreamConsumerConfig,
	handler func(ctx context.Context, msg jetstream.Msg),
) (jetstream.ConsumeContext, error) {
	if setupCtx == nil || handlerCtx == nil {
		return nil, errs.WrapInvalid(errors.New("nil context"),
			"Client", operation, "missing operation context")
	}
	if err := setupCtx.Err(); err != nil {
		return nil, errs.WrapInvalid(err, "Client", operation, "setup context already ended")
	}
	if err := handlerCtx.Err(); err != nil {
		return nil, errs.WrapInvalid(err, "Client", operation, "handler context already ended")
	}
	owner.Component = strings.TrimSpace(owner.Component)
	owner.Port = strings.TrimSpace(owner.Port)
	if err := validatePortConsumerContext(owner, operation); err != nil {
		return nil, err
	}
	if cfg.StreamName == "" {
		return nil, errs.WrapInvalid(errors.New("stream name is required"),
			"Client", operation, "missing stream name")
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
	if cfg.AutoCreate {
		if err := c.ensureStreamForConsumer(setupCtx, js, cfg); err != nil {
			return nil, err
		}
	}
	stream, err := c.awaitStreamVisible(setupCtx, js, cfg.StreamName)
	if err != nil {
		c.recordFailure()
		return nil, errs.WrapTransient(err, "Client", operation,
			"failed to get stream "+cfg.StreamName)
	}

	identity := internalConsumerIdentity{stream: cfg.StreamName, durable: cfg.ConsumerName}
	claim, err := c.reserveInternalConsumer(identity, operation)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			c.releaseInternalConsumer(identity, claim)
		}
	}()
	consumer, err := stream.CreateOrUpdateConsumer(setupCtx, c.buildConsumerConfig(cfg))
	if err != nil {
		c.recordFailure()
		return nil, ClassifyConsumerPolicyError(err, operation)
	}
	handle, err := c.startPortConsumer(
		setupCtx, handlerCtx, operation, owner, cfg,
		&guardedConsumer{Consumer: consumer}, identity, claim, handler,
	)
	if err != nil {
		return nil, err
	}
	committed = true
	return handle, nil
}

func (c *Client) releaseInternalConsumer(identity internalConsumerIdentity, claim *internalConsumerClaim) {
	if claim == nil {
		return
	}
	c.internalClaimsMu.Lock()
	if c.internalClaims[identity] == claim {
		delete(c.internalClaims, identity)
	}
	c.internalClaimsMu.Unlock()
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
	// 0 stays unset so NATS owns the inherited/default/capped result.
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

	streamCfg.Discard = autoConfig.Discard

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

	// THE SAME TWO GUARDS AS EVERY OTHER PROVISIONING SEAM. This is a third one:
	// consumer auto-create is stream CREATION, whatever it is called, and it was
	// the last unguarded route to an unbounded ordinary stream.
	//
	// The bounds guard matters most on the path that reaches here. When a caller
	// supplies an AutoCreateConfig, DefaultStreamConfig is skipped entirely, so a
	// config naming only Subjects and Storage produced MaxAge 0 and MaxBytes 0 —
	// unlimited on both. The framework's own HEALTH, METRICS and FLOWS streams are
	// memory-backed, so a NATS restart destroys them, and the next reconnect used
	// to recreate them through here with no bounds at all — silently replacing the
	// 5m/10MB the framework declares for them. The contract's own observability
	// streams were its counterexample.
	if err := CheckOrdinaryStreamName(streamCfg.Name, "natsclient consumer auto-create"); err != nil {
		return errs.WrapFatal(err, "Client", "ensureStreamForConsumer",
			"validate stream name "+streamCfg.Name)
	}
	if err := CheckStreamBounds(streamCfg, "natsclient consumer auto-create (StreamConsumerConfig.AutoCreate)"); err != nil {
		return errs.WrapFatal(err, "Client", "ensureStreamForConsumer",
			"validate stream bounds for "+streamCfg.Name)
	}

	// Create the stream
	_, err = js.CreateStream(ctx, streamCfg)
	if errors.Is(err, jetstream.ErrStreamNameAlreadyInUse) {
		// The stream exists after all: the pre-check above was answered by a node
		// that had not applied the assignment yet, or a peer created it in the
		// meantime. Binding by name is the answer, exactly as on the pre-check's
		// success path — a non-owner must not restamp a stream someone else
		// declared, and 10058 arrives precisely when this caller's config differs
		// from the live one. Returning it as transient would fail boot one seam
		// over from the window natsclient just absorbed.
		c.logger.Info("stream already exists; binding by name instead of auto-creating",
			slog.String("stream", cfg.StreamName))
		return nil
	}
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
		c.recordStreamPublishFailure(err)
		c.jsMetrics.recordError("publish_to_stream")
		return nil, errs.WrapTransient(err, "Client", "PublishToStreamWithAck",
			"failed to publish to subject "+subject)
	}

	c.resetCircuit()
	return ack, nil
}
