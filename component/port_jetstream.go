package component

import "fmt"

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
