package component

import (
	"fmt"
	"time"
)

// JetStreamPort - NATS JetStream for durable, at-least-once messaging
type JetStreamPort struct {
	// Stream configuration (for outputs)
	StreamName      string   `json:"stream_name"`              // e.g., "ENTITY_EVENTS"
	Subjects        []string `json:"subjects"`                 // e.g., ["events.graph.entity.>"]
	Storage         string   `json:"storage,omitempty"`        // "file" or "memory" when declared
	RetentionPolicy string   `json:"retention,omitempty"`      // "limits", "interest", or "work_queue" when declared
	RetentionDays   int      `json:"retention_days,omitempty"` // Declared message retention in days
	MaxSizeGB       int      `json:"max_size_gb,omitempty"`    // Declared maximum stream size in GiB
	Replicas        int      `json:"replicas,omitempty"`       // Declared replica count

	// Consumer configuration (for inputs)
	ConsumerName  string `json:"consumer_name,omitempty"`  // Durable consumer name
	DeliverPolicy string `json:"deliver_policy,omitempty"` // Declared delivery policy
	AckPolicy     string `json:"ack_policy,omitempty"`     // Declared acknowledgement policy
	MaxDeliver    int    `json:"max_deliver,omitempty"`    // Declared maximum redelivery attempts
	// AckWait is the duration the JetStream server waits for an ack before
	// redelivering. Strings are parsed via time.ParseDuration ("90s",
	// "2m", "5m"). Empty falls through to a per-component default. The
	// component-level default is a starting point; this port-level field
	// lets operators tune ack_wait for long-running consumers (LLM model
	// calls, slow tool execution) without forking the component. Per
	// docs/operations/14-timeout-chain.md: ack_wait must comfortably
	// exceed the longest legitimate per-task wallclock budget so that
	// healthy long-tail work isn't reaped before it can ack.
	AckWait string `json:"ack_wait,omitempty"`
	// HeartbeatInterval is the cadence at which the consumer goroutine
	// fires msg.InProgress() to reset the ack clock. Strings are parsed
	// via time.ParseDuration ("60s", "90s"). Empty falls through to a
	// per-component default. Should be sized comfortably below ack_wait
	// (typical 1.5x margin) so a single missed heartbeat doesn't trigger
	// redelivery. Only honored on consumers that wrap their handler in
	// natsclient.ConsumeWithHeartbeat — pure-ack consumers ignore it.
	HeartbeatInterval string `json:"heartbeat_interval,omitempty"`
	// MaxAckPending caps the number of delivered-but-unacked messages the
	// server keeps in flight for this consumer — the consumer-side backpressure
	// lever. Empty/0 leaves policy to NATS, which may inherit a stream limit,
	// apply its default, or cap it under server/account policy; -1 is unlimited
	// outstanding acknowledgements. gh#480: there was previously no
	// config path to this at all, so operators could not tune ingest backpressure.
	MaxAckPending int `json:"max_ack_pending,omitempty"`

	// Interface contract
	Interface *InterfaceContract `json:"interface,omitempty"`
}

// ResourceID returns unique identifier for JetStream ports
func (j JetStreamPort) ResourceID() string {
	if j.StreamName != "" {
		return fmt.Sprintf("jetstream:%s", j.StreamName)
	}
	// For consumers without explicit stream name
	if len(j.Subjects) > 0 {
		return fmt.Sprintf("jetstream:%s", j.Subjects[0])
	}
	return "jetstream:unknown"
}

// IsExclusive returns false as JetStream manages consumer coordination
func (j JetStreamPort) IsExclusive() bool {
	return false
}

// Kind returns the canonical port kind.
func (j JetStreamPort) Kind() PortKind {
	return PortKindJetStream
}

// ConsumerConfig holds extracted JetStream consumer configuration.
//
// Duration fields (AckWait, HeartbeatInterval) are zero-valued when the
// port-level string is empty; consumers may apply their component default.
// Invalid declarations are rejected before a ConsumerConfig is returned.
type ConsumerConfig struct {
	DeliverPolicy     string
	AckPolicy         string
	MaxDeliver        int
	AckWait           time.Duration
	HeartbeatInterval time.Duration
	MaxAckPending     int // 0 = inherited/default/capped server policy; -1 = unlimited outstanding acks (gh#480)
}

// GetConsumerConfig validates a JetStream port through the canonical facts
// projection and extracts its consumer configuration. A non-JetStream or
// invalid port returns an error rather than silently receiving defaults.
// Unset fields receive these defaults:
// - DeliverPolicy: "new" (safe default - don't replay historical messages)
// - AckPolicy: "explicit"
// - MaxDeliver: 3
// - AckWait, HeartbeatInterval: zero (caller applies per-component default)
func GetConsumerConfig(port Port) (ConsumerConfig, error) {
	facts, err := port.Facts()
	if err != nil {
		return ConsumerConfig{}, err
	}
	return consumerConfigFromFacts(facts, "new")
}

func consumerConfigFromFacts(facts PortFacts, defaultDeliverPolicy string) (ConsumerConfig, error) {
	stream, ok := facts.Stream()
	if !ok {
		return ConsumerConfig{}, fmt.Errorf("port kind %q does not declare JetStream consumer configuration", facts.Kind())
	}
	cfg := ConsumerConfig{
		DeliverPolicy: defaultDeliverPolicy,
		AckPolicy:     "explicit",
		MaxDeliver:    3,
	}
	if err := applyJetStreamConsumerConfig(&cfg, stream); err != nil {
		return ConsumerConfig{}, err
	}
	return cfg, nil
}

// applyJetStreamConsumerConfig copies non-zero StreamFacts consumer fields
// into the supplied ConsumerConfig. Centralised so both extractors stay in
// lockstep — adding a field only needs editing one place.
func applyJetStreamConsumerConfig(cfg *ConsumerConfig, stream StreamFacts) error {
	if stream.DeliverPolicy() != "" {
		cfg.DeliverPolicy = stream.DeliverPolicy()
	}
	if stream.AckPolicy() != "" {
		cfg.AckPolicy = stream.AckPolicy()
	}
	if stream.MaxDeliver() > 0 {
		cfg.MaxDeliver = stream.MaxDeliver()
	}
	// MaxAckPending: a non-zero port value (including -1 = unlimited) overrides
	// the server default. 0 stays 0 (caller leaves the server default).
	if stream.MaxAckPending() != 0 {
		cfg.MaxAckPending = stream.MaxAckPending()
	}
	if stream.AckWait() != "" {
		duration, err := time.ParseDuration(stream.AckWait())
		if err != nil {
			return fmt.Errorf("parse ack_wait: %w", err)
		}
		cfg.AckWait = duration
	}
	if stream.HeartbeatInterval() != "" {
		duration, err := time.ParseDuration(stream.HeartbeatInterval())
		if err != nil {
			return fmt.Errorf("parse heartbeat_interval: %w", err)
		}
		cfg.HeartbeatInterval = duration
	}
	return nil
}
