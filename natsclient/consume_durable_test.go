package natsclient

import (
	"context"
	"testing"
	"time"
)

// TestValidateHeartbeatBelowAckWait enforces the ADR-070 B3 invariant: a
// heartbeat that can't fire (with margin) before AckWait expires would redeliver
// a live unit — reject it. Zero AckWait resolves to the 30s server default.
func TestValidateHeartbeatBelowAckWait(t *testing.T) {
	tests := []struct {
		name      string
		heartbeat time.Duration
		ackWait   time.Duration
		wantErr   bool
	}{
		{"heartbeat well below ackwait", 5 * time.Second, 30 * time.Second, false},
		{"heartbeat exactly half", 15 * time.Second, 30 * time.Second, false},
		{"heartbeat above half (margin violation)", 20 * time.Second, 30 * time.Second, true},
		{"heartbeat >= ackwait (the B3 bug)", 90 * time.Second, 30 * time.Second, true},
		{"zero ackwait resolves to 30s default, ok", 10 * time.Second, 0, false},
		{"zero ackwait default, heartbeat too big", 20 * time.Second, 0, true},
		{"non-positive heartbeat rejected", 0, 30 * time.Second, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHeartbeatBelowAckWait(tt.heartbeat, tt.ackWait)
			if tt.wantErr && err == nil {
				t.Fatalf("want error for heartbeat=%s ackWait=%s, got nil", tt.heartbeat, tt.ackWait)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("want nil for heartbeat=%s ackWait=%s, got %v", tt.heartbeat, tt.ackWait, err)
			}
		})
	}
}

// TestConsumeDurable_RejectsMisconfiguredHeartbeat proves the enforcement fires
// at the public entry (before any server interaction).
func TestConsumeDurable_RejectsMisconfiguredHeartbeat(t *testing.T) {
	c := &Client{}
	err := c.ConsumeDurable(nil, PortConsumerContext{Component: "test", Port: "input"}, StreamConsumerConfig{StreamName: "S", ConsumerName: "C", AckWait: 30 * time.Second},
		90*time.Second, func(_ context.Context, _ []byte) error { return nil })
	if err == nil {
		t.Fatal("ConsumeDurable must reject heartbeat >= ack_wait before touching the server")
	}
}
