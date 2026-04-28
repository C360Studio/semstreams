package service

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/types"
)

// natsPublisher defines the interface for publishing to NATS JetStream.
// All observability streams (logs, health, metrics, flows) should use
// PublishToStream for consistent async pub/sub behavior with persistence.
type natsPublisher interface {
	PublishToStream(ctx context.Context, subject string, data []byte) error
}

// Dependencies provides the standard dependencies that all services receive.
// This replaces the old Dependencies struct and provides consistent injection.
// Services should use HTTP or NATS RPC for inter-service communication.
type Dependencies struct {
	NATSClient        *natsclient.Client
	MetricsRegistry   *metric.MetricsRegistry
	Logger            *slog.Logger
	Platform          types.PlatformMeta           // Platform identity
	Manager           *config.Manager              // Centralized configuration management
	ComponentRegistry *component.Registry          // Component registry for ComponentManager
	ToolRegistry      component.ToolRegistryReader // Shared tool executor registry plumbed to component deps
	PayloadRegistry   *payloadregistry.Registry    // Shared payload registry plumbed to component deps
	ServiceManager    *Manager                     // Service manager for accessing other services
}

// Constructor defines the standard constructor signature for all services.
// Every service must have a constructor that follows this pattern.
// The constructor receives raw JSON config and must handle its own parsing.
type Constructor func(rawConfig json.RawMessage, deps *Dependencies) (Service, error)
