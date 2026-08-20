package natsclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/pkg/errs"
)

const (
	policySourcePort      = "port"
	policySourceComponent = "component"
	policySourceServer    = "server"
)

type consumerPolicyRecord struct {
	component    string
	port         string
	stream       string
	consumer     string
	policySource string
	requested    int
	handle       consumerPolicyInfoReader
	logger       *slog.Logger
	available    bool
	active       bool
	mu           sync.Mutex
}

type consumerPolicyKey struct {
	component    string
	port         string
	stream       string
	consumer     string
	policySource string
}

type consumerPolicyInfoReader interface {
	Info(context.Context) (*jetstream.ConsumerInfo, error)
}

func (c *Client) observeInternalConsumer(
	ctx context.Context, consumer consumerPolicyInfoReader,
) (internalConsumerIdentity, error) {
	info, err := consumer.Info(ctx)
	if err != nil {
		c.recordFailure()
		return internalConsumerIdentity{}, errs.WrapTransient(
			err, "Client", "ConsumeInternalStreamWithConfig", "initial consumer observation unavailable")
	}
	return internalConsumerIdentity{stream: info.Stream, durable: info.Name}, nil
}

func validatePortConsumerContext(owner PortConsumerContext, operation string) error {
	if strings.TrimSpace(owner.Component) != "" && strings.TrimSpace(owner.Port) != "" {
		return nil
	}
	return errs.WrapInvalid(fmt.Errorf("component and port are required"),
		"Client", operation, "missing port consumer context")
}

func (r *consumerPolicyRecord) labels() []string {
	return []string{r.component, r.port, r.stream, r.consumer, r.policySource}
}

func (r *consumerPolicyRecord) key() consumerPolicyKey {
	return consumerPolicyKey{
		component: r.component, port: r.port, stream: r.stream,
		consumer: r.consumer, policySource: r.policySource,
	}
}

// ClassifyConsumerPolicyError classifies NATS policy rejections as invalid
// configuration while preserving all other failures as transient.
func ClassifyConsumerPolicyError(err error, operation string) error {
	if err == nil {
		return nil
	}
	var apiErr *jetstream.APIError
	if errors.As(err, &apiErr) && (apiErr.ErrorCode == 10121 || apiErr.ErrorCode == 10082) {
		return errs.WrapInvalid(err, "Client", operation,
			fmt.Sprintf("consumer policy rejected by NATS API error %d", apiErr.ErrorCode))
	}
	return errs.WrapTransient(err, "Client", operation, "consumer policy operation failed")
}

func validateObservedMaxAckPending(requested, effective int) error {
	if requested != 0 && requested != effective {
		return errs.WrapInvalid(
			fmt.Errorf("requested max_ack_pending %d, observed %d", requested, effective),
			"Client", "observeConsumerPolicy", "effective consumer policy mismatch")
	}
	return nil
}

func (c *Client) observePortConsumerPolicy(
	ctx context.Context,
	owner PortConsumerContext,
	finalConfig StreamConsumerConfig,
	consumer consumerPolicyInfoReader,
) (consumerPolicyKey, error) {
	info, err := consumer.Info(ctx)
	if err != nil {
		return consumerPolicyKey{}, errs.WrapTransient(err, "Client", "observeConsumerPolicy", "initial consumer info unavailable")
	}
	if err := validateObservedMaxAckPending(finalConfig.MaxAckPending, info.Config.MaxAckPending); err != nil {
		return consumerPolicyKey{}, err
	}
	source := policySourcePort
	if finalConfig.MaxAckPending == 0 {
		source = policySourceServer
	} else if owner.ComponentOwned {
		source = policySourceComponent
	}
	record := &consumerPolicyRecord{
		component: owner.Component, port: owner.Port, stream: info.Stream, consumer: info.Name,
		policySource: source, requested: finalConfig.MaxAckPending, handle: consumer, logger: c.logger,
		available: true, active: true,
	}
	key := record.key()
	c.logger.Info("JetStream consumer acknowledgement policy applied",
		slog.String("component", record.component), slog.String("port", record.port),
		slog.String("stream", record.stream), slog.String("consumer", record.consumer),
		slog.String("policy_source", record.policySource),
		slog.Int("requested_max_ack_pending", record.requested),
		slog.Int("effective_max_ack_pending", info.Config.MaxAckPending))
	if c.jsMetrics != nil {
		c.jsMetrics.trackPolicy(key, record, info.Config.MaxAckPending)
	}
	return key, nil
}

type managedPolicyConsumer interface {
	consumerPolicyInfoReader
	Consume(jetstream.MessageHandler, ...jetstream.PullConsumeOpt) (jetstream.ConsumeContext, error)
}

func (c *Client) observeAndStartManagedConsumer(
	setupCtx context.Context,
	owner PortConsumerContext,
	cfg StreamConsumerConfig,
	consumer managedPolicyConsumer,
	handler jetstream.MessageHandler,
	observePolicy bool,
) (jetstream.ConsumeContext, consumerPolicyKey, error) {
	policyKey := consumerPolicyKey{}
	var err error
	if observePolicy {
		policyKey, err = c.observePortConsumerPolicy(setupCtx, owner, cfg, consumer)
		if err != nil {
			return nil, consumerPolicyKey{}, err
		}
	}
	forget := func() {
		if c.jsMetrics != nil {
			c.jsMetrics.forgetPolicy(policyKey)
		}
	}
	if err := setupCtx.Err(); err != nil {
		forget()
		return nil, consumerPolicyKey{}, errs.WrapTransient(err, "Client", "ConsumeStreamWithConfig",
			"setup context ended before starting consumer")
	}
	consumeCtx, err := consumer.Consume(handler)
	if err != nil {
		forget()
		c.recordFailure()
		return nil, consumerPolicyKey{}, errs.WrapTransient(err, "Client", "ConsumeStreamWithConfig",
			"failed to start consuming from stream "+cfg.StreamName)
	}
	if err := setupCtx.Err(); err != nil {
		consumeCtx.Stop()
		forget()
		return nil, consumerPolicyKey{}, errs.WrapTransient(err, "Client", "ConsumeStreamWithConfig",
			"setup context ended while starting consumer")
	}
	return consumeCtx, policyKey, nil
}

// ObserveDirectPortConsumerPolicy observes an OTEL-owned direct consumer and
// returns opaque cleanup for its canonical policy record.
func (c *Client) ObserveDirectPortConsumerPolicy(
	ctx context.Context,
	owner PortConsumerContext,
	finalConfig jetstream.ConsumerConfig,
	consumer jetstream.Consumer,
) (func(), error) {
	owner.Component = strings.TrimSpace(owner.Component)
	owner.Port = strings.TrimSpace(owner.Port)
	if err := validatePortConsumerContext(owner, "ObserveDirectPortConsumerPolicy"); err != nil {
		return nil, err
	}
	key, err := c.observePortConsumerPolicy(ctx, owner, StreamConsumerConfig{MaxAckPending: finalConfig.MaxAckPending}, consumer)
	if err != nil {
		return nil, err
	}
	return func() {
		if c.jsMetrics != nil {
			c.jsMetrics.forgetPolicy(key)
		}
	}, nil
}
