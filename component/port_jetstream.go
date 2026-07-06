package component

import (
	"fmt"
	"log/slog"
	"time"
)

// JetStreamPort - NATS JetStream for durable, at-least-once messaging
type JetStreamPort struct {
	// Stream configuration (for outputs)
	StreamName      string   `json:"stream_name"`              // e.g., "ENTITY_EVENTS"
	Subjects        []string `json:"subjects"`                 // e.g., ["events.graph.entity.>"]
	Storage         string   `json:"storage,omitempty"`        // "file" or "memory", default "file"
	RetentionPolicy string   `json:"retention,omitempty"`      // "limits", "interest", "work_queue", default "limits"
	RetentionDays   int      `json:"retention_days,omitempty"` // Message retention in days, default 7
	MaxSizeGB       int      `json:"max_size_gb,omitempty"`    // Max stream size in GB, default 10
	Replicas        int      `json:"replicas,omitempty"`       // Number of replicas, default 1

	// Consumer configuration (for inputs)
	ConsumerName  string `json:"consumer_name,omitempty"`  // Durable consumer name
	DeliverPolicy string `json:"deliver_policy,omitempty"` // "all", "last", "new", default "new"
	AckPolicy     string `json:"ack_policy,omitempty"`     // "explicit", "none", "all", default "explicit"
	MaxDeliver    int    `json:"max_deliver,omitempty"`    // Max redelivery attempts, default 3
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
	// lever. Empty/0 falls through to the NATS server default (1000 for
	// explicit-ack consumers); -1 is unlimited. gh#480: there was previously no
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

// Type returns the port type identifier
func (j JetStreamPort) Type() string {
	return "jetstream"
}

// ConsumerConfig holds extracted JetStream consumer configuration.
//
// Duration fields (AckWait, HeartbeatInterval) are zero-valued when the
// port-level string was empty or unparseable; consumers should test
// against zero and apply their per-component default in that case.
// Parse errors are logged once at extraction time so a malformed config
// surfaces loudly without blocking startup.
type ConsumerConfig struct {
	DeliverPolicy     string
	AckPolicy         string
	MaxDeliver        int
	AckWait           time.Duration
	HeartbeatInterval time.Duration
	MaxAckPending     int // 0 = server default (1000); -1 = unlimited (gh#480)
}

// GetConsumerConfig extracts JetStream consumer configuration from a port.
// Returns safe defaults if port doesn't have JetStream config:
// - DeliverPolicy: "new" (safe default - don't replay historical messages)
// - AckPolicy: "explicit"
// - MaxDeliver: 3
// - AckWait, HeartbeatInterval: zero (caller applies per-component default)
func GetConsumerConfig(port Port) ConsumerConfig {
	cfg := ConsumerConfig{
		DeliverPolicy: "new", // Safe default
		AckPolicy:     "explicit",
		MaxDeliver:    3,
	}

	if jsPort, ok := port.Config.(JetStreamPort); ok {
		applyJetStreamConsumerConfig(&cfg, jsPort)
	}
	return cfg
}

// GetConsumerConfigFromDefinition extracts JetStream consumer configuration from a port definition.
// This is a convenience wrapper for use with PortDefinition instead of Port.
func GetConsumerConfigFromDefinition(portDef PortDefinition) ConsumerConfig {
	cfg := ConsumerConfig{
		DeliverPolicy: "new", // Safe default
		AckPolicy:     "explicit",
		MaxDeliver:    3,
	}

	if jsPort, ok := portDef.Config.(JetStreamPort); ok {
		applyJetStreamConsumerConfig(&cfg, jsPort)
	}
	return cfg
}

// applyJetStreamConsumerConfig copies non-zero JetStreamPort consumer fields
// into the supplied ConsumerConfig. Centralised so both extractors stay in
// lockstep — adding a field only needs editing one place. Duration fields
// log a warning and stay zero (caller-default) on parse error.
func applyJetStreamConsumerConfig(cfg *ConsumerConfig, jsPort JetStreamPort) {
	if jsPort.DeliverPolicy != "" {
		cfg.DeliverPolicy = jsPort.DeliverPolicy
	}
	if jsPort.AckPolicy != "" {
		cfg.AckPolicy = jsPort.AckPolicy
	}
	if jsPort.MaxDeliver > 0 {
		cfg.MaxDeliver = jsPort.MaxDeliver
	}
	// MaxAckPending: a non-zero port value (including -1 = unlimited) overrides
	// the server default. 0 stays 0 (caller leaves the server default).
	if jsPort.MaxAckPending != 0 {
		cfg.MaxAckPending = jsPort.MaxAckPending
	}
	if jsPort.AckWait != "" {
		if d, err := time.ParseDuration(jsPort.AckWait); err == nil {
			cfg.AckWait = d
		} else {
			slog.Warn("Invalid JetStreamPort.AckWait; falling through to component default",
				"value", jsPort.AckWait,
				"error", err)
		}
	}
	if jsPort.HeartbeatInterval != "" {
		if d, err := time.ParseDuration(jsPort.HeartbeatInterval); err == nil {
			cfg.HeartbeatInterval = d
		} else {
			slog.Warn("Invalid JetStreamPort.HeartbeatInterval; falling through to component default",
				"value", jsPort.HeartbeatInterval,
				"error", err)
		}
	}
}
