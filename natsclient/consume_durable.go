package natsclient

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// defaultConsumerAckWait mirrors buildConsumerConfig's fallback (stream.go): a
// zero AckWait resolves to 30s on the server, so heartbeat validation must use
// the same effective value.
const defaultConsumerAckWait = 30 * time.Second

// ConsumeDurable runs a durable at-least-once consumer over a stream with a
// plain handler: func(ctx, []byte) error — acked on nil, nak-with-delay on error,
// and held past AckWait by an InProgress heartbeat while the handler runs (see
// ConsumeWithHeartbeat). The consumer NEVER handles a raw jetstream.Msg for ack
// semantics; the []byte is the raw message data and envelope decoding
// (payload-registry BaseMessage, etc.) stays ABOVE natsclient — natsclient does
// not depend on the message package.
//
// The framework owns the at-least-once ack/heartbeat/redelivery pattern here once,
// so a durable consumer (e.g. gated-DAG dispatch — ADR-070) does not re-hand-roll
// ConsumeStreamWithConfig + ConsumeWithHeartbeat + ack per call site.
//
// heartbeat MUST be safely below the effective AckWait: a heartbeat that first
// fires only after AckWait has already expired lets the server redeliver a
// still-running unit (duplicate work). ConsumeDurable ENFORCES this (heartbeat*2
// <= effective AckWait) and returns an error on a violating config, rather than
// merely documenting it (ADR-070 B3).
func (c *Client) ConsumeDurable(
	ctx context.Context,
	cfg StreamConsumerConfig,
	heartbeat time.Duration,
	handler func(context.Context, []byte) error,
) error {
	if err := validateHeartbeatBelowAckWait(heartbeat, cfg.AckWait); err != nil {
		return err
	}
	return c.ConsumeStreamWithConfig(ctx, cfg, func(mctx context.Context, msg jetstream.Msg) {
		data := msg.Data()
		// ConsumeWithHeartbeat owns Ack/Nak; a returned error is already handled
		// (nak'd) — log for operator visibility only.
		if err := ConsumeWithHeartbeat(mctx, msg, heartbeat, func(wctx context.Context) error {
			return handler(wctx, data)
		}); err != nil {
			slog.Warn("ConsumeDurable handler error",
				slog.String("stream", cfg.StreamName),
				slog.String("consumer", cfg.ConsumerName),
				slog.String("error", err.Error()))
		}
	})
}

// validateHeartbeatBelowAckWait enforces the ADR-070 B3 invariant: the heartbeat
// must be able to fire (with margin) before AckWait expires, or a live unit gets
// redelivered. A zero AckWait resolves to the 30s server default. Exported-shaped
// as a package helper so config Validate() paths can reuse it.
func validateHeartbeatBelowAckWait(heartbeat, ackWait time.Duration) error {
	if heartbeat <= 0 {
		return fmt.Errorf("heartbeat interval must be positive, got %s", heartbeat)
	}
	effAckWait := ackWait
	if effAckWait <= 0 {
		effAckWait = defaultConsumerAckWait
	}
	if heartbeat*2 > effAckWait {
		return fmt.Errorf(
			"heartbeat interval %s must be <= half of ack_wait %s (a heartbeat that first fires after ack_wait redelivers a live unit)",
			heartbeat, effAckWait)
	}
	return nil
}
