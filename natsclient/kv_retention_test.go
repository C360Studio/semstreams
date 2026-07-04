package natsclient

import (
	"errors"
	"testing"
	"time"
)

// TestCheckNoLifecycleRetention covers the pure D1 guardrail (ADR-068): any TTL
// (MaxAge) or binding MaxBytes on a live graph bucket is forbidden; the NATS
// default MaxBytes of -1 (unlimited) and a zero TTL are the clean case.
func TestCheckNoLifecycleRetention(t *testing.T) {
	tests := []struct {
		name     string
		maxAge   time.Duration
		maxBytes int64
		wantErr  bool
	}{
		{"clean (zero TTL, unlimited bytes)", 0, -1, false},
		{"clean (zero TTL, zero bytes)", 0, 0, false},
		{"ttl set is a violation", 24 * time.Hour, -1, true},
		{"maxbytes cap is a violation", 0, 1 << 20, true},
		{"both set", time.Hour, 100, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckNoLifecycleRetention("ENTITY_STATES", tt.maxAge, tt.maxBytes)
			switch {
			case tt.wantErr && err == nil:
				t.Fatal("want ErrGraphBucketRetention, got nil")
			case tt.wantErr && !errors.Is(err, ErrGraphBucketRetention):
				t.Errorf("want ErrGraphBucketRetention, got %v", err)
			case !tt.wantErr && err != nil:
				t.Errorf("want nil, got %v", err)
			}
		})
	}
}
