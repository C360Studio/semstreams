//go:build e2e_slow_consumer

package e2eslowconsumer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/c360studio/semstreams/natsclient"
)

const probeTimeout = 15 * time.Second

type callbackObservation struct {
	connection   *nats.Conn
	subscription *nats.Subscription
	err          error
}

// Run produces one deterministic slow-consumer diagnostic through the callback
// already installed on the connected production client.
func Run(parent context.Context, client *natsclient.Client) error {
	ctx, cancel := context.WithTimeout(parent, probeTimeout)
	defer cancel()

	if client == nil {
		return errors.New("slow-consumer probe requires the connected production client")
	}
	connection := client.GetConnection()
	if connection == nil || !connection.IsConnected() {
		return errors.New("slow-consumer probe requires an active NATS connection")
	}
	installedHandler := connection.ErrorHandler()
	if installedHandler == nil {
		return errors.New("slow-consumer probe requires the installed production error handler")
	}

	callbackEntered := make(chan callbackObservation, 1)
	releaseCallback := make(chan struct{})
	callbackHandled := make(chan struct{})
	var matchingCallback atomic.Bool
	connection.SetErrorHandler(gatedErrorHandler(
		ctx, installedHandler, callbackEntered, releaseCallback, callbackHandled, &matchingCallback,
	))
	defer connection.SetErrorHandler(installedHandler)

	handlerEntered := make(chan struct{})
	releaseHandler := make(chan struct{})
	var handlerEnteredOnce sync.Once
	var releaseHandlerOnce sync.Once
	release := func() { releaseHandlerOnce.Do(func() { close(releaseHandler) }) }
	defer release()

	subscription, err := connection.QueueSubscribe(Subject, Queue, func(_ *nats.Msg) {
		handlerEnteredOnce.Do(func() { close(handlerEntered) })
		<-releaseHandler
	})
	if err != nil {
		return fmt.Errorf("subscribe fixture: %w", err)
	}
	defer func() { _ = subscription.Unsubscribe() }()
	if err := subscription.SetPendingLimits(1, -1); err != nil {
		return fmt.Errorf("set fixture pending limit: %w", err)
	}
	if err := connection.FlushWithContext(ctx); err != nil {
		return fmt.Errorf("flush fixture subscription: %w", err)
	}

	if err := connection.Publish(Subject, []byte("block")); err != nil {
		return fmt.Errorf("publish fixture blocker: %w", err)
	}
	if err := connection.FlushWithContext(ctx); err != nil {
		return fmt.Errorf("flush fixture blocker: %w", err)
	}
	if err := waitSignal(ctx, handlerEntered, "fixture handler did not block"); err != nil {
		return err
	}

	for index := range ExpectedDropped {
		if err := connection.Publish(Subject, []byte(fmt.Sprintf("overflow-%d", index))); err != nil {
			return fmt.Errorf("publish fixture overflow %d: %w", index, err)
		}
	}
	if err := connection.FlushWithContext(ctx); err != nil {
		return fmt.Errorf("flush fixture overflow: %w", err)
	}

	var observation callbackObservation
	select {
	case observation = <-callbackEntered:
	case <-ctx.Done():
		return fmt.Errorf("slow-consumer callback did not arrive: %w", ctx.Err())
	}
	if observation.connection != connection || observation.subscription != subscription ||
		!errors.Is(observation.err, nats.ErrSlowConsumer) {
		return errors.New("slow-consumer callback did not carry the fixture subscription")
	}
	if err := waitExactDropped(ctx, subscription, ExpectedDropped); err != nil {
		return err
	}

	close(releaseCallback)
	if err := waitSignal(ctx, callbackHandled, "production error handler did not return"); err != nil {
		return err
	}
	return nil
}

func gatedErrorHandler(
	ctx context.Context,
	installedHandler nats.ErrHandler,
	callbackEntered chan<- callbackObservation,
	releaseCallback <-chan struct{},
	callbackHandled chan<- struct{},
	matchingCallback *atomic.Bool,
) nats.ErrHandler {
	return func(conn *nats.Conn, sub *nats.Subscription, err error) {
		if sub == nil || sub.Subject != Subject || sub.Queue != Queue ||
			!errors.Is(err, nats.ErrSlowConsumer) || !matchingCallback.CompareAndSwap(false, true) {
			installedHandler(conn, sub, err)
			return
		}
		callbackEntered <- callbackObservation{connection: conn, subscription: sub, err: err}
		select {
		case <-releaseCallback:
			installedHandler(conn, sub, err)
			callbackHandled <- struct{}{}
		case <-ctx.Done():
		}
	}
}

func waitExactDropped(ctx context.Context, subscription *nats.Subscription, want int) error {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		dropped, err := subscription.Dropped()
		if err != nil {
			return fmt.Errorf("read fixture dropped count: %w", err)
		}
		if dropped == want {
			return nil
		}
		if dropped > want {
			return fmt.Errorf("fixture dropped %d messages, want exactly %d", dropped, want)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("fixture dropped %d messages, want %d: %w", dropped, want, ctx.Err())
		}
	}
}

func waitSignal(ctx context.Context, signal <-chan struct{}, description string) error {
	select {
	case <-signal:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", description, ctx.Err())
	}
}
