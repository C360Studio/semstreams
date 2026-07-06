package component

import "testing"

// TestGetConsumerConfig_MaxAckPending closes the gh#480 plumbing gap: a
// max_ack_pending set on a JetStreamPort MUST reach the extracted ConsumerConfig
// (which graph-ingest then maps into StreamConsumerConfig). Before this change
// there was no field at all, so the value was silently unreachable.
func TestGetConsumerConfig_MaxAckPending(t *testing.T) {
	tests := []struct {
		name string
		set  int
		want int
	}{
		{"explicit cap", 5000, 5000},
		{"unset stays 0 (server default)", 0, 0},
		{"unlimited passes through", -1, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port := Port{Config: JetStreamPort{
				Subjects:      []string{"entity.>"},
				MaxAckPending: tt.set,
			}}
			if got := GetConsumerConfig(port).MaxAckPending; got != tt.want {
				t.Errorf("GetConsumerConfig MaxAckPending = %d, want %d", got, tt.want)
			}

			def := PortDefinition{Config: JetStreamPort{
				Subjects:      []string{"entity.>"},
				MaxAckPending: tt.set,
			}}
			if got := GetConsumerConfigFromDefinition(def).MaxAckPending; got != tt.want {
				t.Errorf("GetConsumerConfigFromDefinition MaxAckPending = %d, want %d", got, tt.want)
			}
		})
	}
}
