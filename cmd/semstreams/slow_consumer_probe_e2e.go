//go:build e2e_slow_consumer

package main

import (
	"context"

	"github.com/c360studio/semstreams/internal/e2eslowconsumer"
	"github.com/c360studio/semstreams/natsclient"
)

func runSlowConsumerProbe(ctx context.Context, client *natsclient.Client) error {
	return e2eslowconsumer.Run(ctx, client)
}
