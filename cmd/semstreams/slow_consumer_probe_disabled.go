//go:build !e2e_slow_consumer

package main

import (
	"context"

	"github.com/c360studio/semstreams/natsclient"
)

func runSlowConsumerProbe(context.Context, *natsclient.Client) error {
	return nil
}
