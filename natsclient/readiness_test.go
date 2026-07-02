package natsclient

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/nats-io/nats.go"
)

func TestIsNoResponders(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"direct", nats.ErrNoResponders, true},
		{"wrapped", fmt.Errorf("readiness budget exhausted: %w", nats.ErrNoResponders), true},
		{"timeout is not no-responders", context.DeadlineExceeded, false},
		{"other", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsNoResponders(tc.err); got != tc.want {
				t.Fatalf("IsNoResponders(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
