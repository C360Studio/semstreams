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

// NewDurableHandler builds the stateless settlement callback used by a durable
// consumer. The caller starts consumption through a canonical handle-returning
// operation and owns that exact native handle. ConsumeWithHeartbeat remains the
// sole owner of Ack, Nak, Term, InProgress, cancellation, and synchronous work
// completion semantics.
func NewDurableHandler(
	cfg StreamConsumerConfig,
	heartbeat time.Duration,
	work func(context.Context, []byte) error,
) (func(context.Context, jetstream.Msg), error) {
	if work == nil {
		return nil, fmt.Errorf("durable work handler is required")
	}
	if err := validateDurableHeartbeat(cfg, heartbeat); err != nil {
		return nil, err
	}
	streamName := cfg.StreamName
	consumerName := cfg.ConsumerName
	return func(mctx context.Context, msg jetstream.Msg) {
		data := msg.Data()
		// ConsumeWithHeartbeat owns Ack/Nak; a returned error is already handled
		// (nak'd) — log for operator visibility only.
		if err := ConsumeWithHeartbeat(mctx, msg, heartbeat, func(wctx context.Context) error {
			return work(wctx, data)
		}); err != nil {
			slog.Warn("ConsumeDurable handler error",
				slog.String("stream", streamName),
				slog.String("consumer", consumerName),
				slog.String("error", err.Error()))
		}
	}, nil
}

func validateDurableHeartbeat(cfg StreamConsumerConfig, heartbeat time.Duration) error {
	if heartbeat <= 0 {
		return fmt.Errorf("heartbeat interval must be positive, got %s", heartbeat)
	}
	effectiveAckWait := cfg.AckWait
	if len(cfg.BackOff) > 0 {
		for index, interval := range cfg.BackOff {
			if interval <= 0 {
				return fmt.Errorf("back_off[%d] must be positive, got %s", index, interval)
			}
			if index == 0 || interval < effectiveAckWait {
				effectiveAckWait = interval
			}
		}
	} else if effectiveAckWait <= 0 {
		effectiveAckWait = defaultConsumerAckWait
	}
	ceiling := effectiveAckWait / 2
	if heartbeat > ceiling {
		return fmt.Errorf(
			"heartbeat interval %s exceeds computed ceiling %s for effective acknowledgement wait %s",
			heartbeat, ceiling, effectiveAckWait)
	}
	return nil
}
