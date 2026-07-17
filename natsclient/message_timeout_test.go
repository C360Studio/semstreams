package natsclient

import (
	"context"
	"testing"
	"time"
)

func TestMessageHandlerContext_DisabledUsesLifecycleContext(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	ctx, cancel := messageHandlerContext(parent, time.Second, true)
	defer cancel()

	if _, ok := ctx.Deadline(); ok {
		t.Fatal("disabled message timeout unexpectedly installed a deadline")
	}
	parentCancel()
	select {
	case <-ctx.Done():
		if ctx.Err() != context.Canceled {
			t.Fatalf("context error = %v, want context.Canceled", ctx.Err())
		}
	case <-time.After(time.Second):
		t.Fatal("lifecycle cancellation did not reach no-timeout message context")
	}
}

func TestMessageHandlerContext_PositiveRetainsWorkDeadline(t *testing.T) {
	ctx, cancel := messageHandlerContext(context.Background(), time.Second, false)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("positive message timeout did not install a work deadline")
	}
}
