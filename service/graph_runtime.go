package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/projection"
)

// WireGraphRuntime applies the existing storage-retention guard and constructs
// the built-in graph mutation client. It creates no ownership substrate or
// background coordination service.
func WireGraphRuntime(
	ctx context.Context,
	natsClient *natsclient.Client,
	logger *slog.Logger,
	contracts ...projection.Contract,
) (*projection.MutationClient, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := graph.EnsureCatalogRetentionClean(ctx, natsClient, logger); err != nil {
		return nil, fmt.Errorf("ensure framework graph catalog retention-clean: %w", err)
	}
	client, err := projection.NewMutationClient(projection.MutationClientConfig{
		NATS: natsClient, Contracts: contracts,
	})
	if err != nil {
		return nil, fmt.Errorf("build graph mutation client: %w", err)
	}
	return client, nil
}
