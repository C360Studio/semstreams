package natsclient

import "testing"

// TestBuildConsumerConfig_MaxAckPending covers the final hop that a prior review
// found silently dropped -1 (unlimited): buildConsumerConfig must pass a non-zero
// MaxAckPending (including -1) through to jetstream.ConsumerConfig, while 0 stays
// unset so NATS applies its server default (gh#480).
func TestBuildConsumerConfig_MaxAckPending(t *testing.T) {
	c := &Client{}
	tests := []struct {
		name string
		set  int
		want int
	}{
		{"positive cap passes", 5000, 5000},
		{"unlimited (-1) passes, not dropped to default", -1, -1},
		{"unset stays 0 (server default)", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.buildConsumerConfig(StreamConsumerConfig{MaxAckPending: tt.set}).MaxAckPending
			if got != tt.want {
				t.Errorf("buildConsumerConfig MaxAckPending = %d, want %d", got, tt.want)
			}
		})
	}
}
