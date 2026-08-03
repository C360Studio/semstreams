package natsclient

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/nats-io/nats.go"
)

func TestIsBenignDrainError(t *testing.T) {
	t.Parallel()

	arbitraryErr := errors.New("drain failed")
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "connection already closed", err: nats.ErrConnectionClosed, want: true},
		{name: "wrapped connection already closed", err: fmt.Errorf("drain: %w", nats.ErrConnectionClosed), want: true},
		{name: "arbitrary drain failure", err: arbitraryErr, want: false},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: false},
		{name: "context cancelled", err: context.Canceled, want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := isBenignDrainError(test.err); got != test.want {
				t.Fatalf("isBenignDrainError(%v) = %t, want %t", test.err, got, test.want)
			}
		})
	}
}
