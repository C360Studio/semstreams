// Package bootstrapobservability contains the plain Phase-A construction
// helpers shared by the production and E2E composition roots.
package bootstrapobservability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/frameworkcapabilities/graphresearch"
	rulepackcap "github.com/c360studio/semstreams/frameworkcapabilities/rulepacks"
	"github.com/c360studio/semstreams/internal/logforwarderpolicy"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/logging"
	"github.com/c360studio/semstreams/types"
)

// PhaseALogging is the non-forwarding logger graph created before NATS. Every
// logger derives from the same caller-supplied local handler and base attrs.
type PhaseALogging struct {
	Process       *slog.Logger
	Client        *slog.Logger
	ConfigManager *slog.Logger

	baseGraph slog.Handler
	baseAttrs []slog.Attr
}

// NewProductionPhaseA creates the production metrics registry and local plus
// WARN/ERROR-counter logger graph as one ordered Phase-A operation.
func NewProductionPhaseA(
	output io.Writer,
	level, format string,
	baseAttrs []slog.Attr,
) (*metric.MetricsRegistry, *PhaseALogging, error) {
	metrics := metric.NewMetricsRegistry()
	local, err := NewLocalHandler(output, level, format)
	if err != nil {
		return nil, nil, err
	}
	phase, err := NewPhaseALogging(
		local,
		logging.NewCounterHandler(metrics.CoreMetrics().LogEntriesTotal),
		baseAttrs,
	)
	if err != nil {
		return nil, nil, err
	}
	return metrics, phase, nil
}

// NewE2EPhaseA creates the E2E metrics registry and explicit stdout-only
// logger graph as one ordered Phase-A operation.
func NewE2EPhaseA(
	output io.Writer,
	level, format string,
	baseAttrs []slog.Attr,
) (*metric.MetricsRegistry, *PhaseALogging, error) {
	metrics := metric.NewMetricsRegistry()
	local, err := NewLocalHandler(output, level, format)
	if err != nil {
		return nil, nil, err
	}
	phase, err := NewPhaseALogging(local, nil, baseAttrs)
	if err != nil {
		return nil, nil, err
	}
	return metrics, phase, nil
}

// NewLocalHandler creates the one explicitly configured local handler shared
// by all Phase-A and steady-state logger graphs.
func NewLocalHandler(output io.Writer, level, format string) (slog.Handler, error) {
	if output == nil {
		return nil, fmt.Errorf("local log output cannot be nil")
	}
	parsedLevel, err := parseLevel(level)
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{
		Level:     parsedLevel,
		AddSource: strings.EqualFold(level, "debug"),
	}
	switch strings.ToLower(format) {
	case "json":
		return slog.NewJSONHandler(output, opts), nil
	case "text":
		return slog.NewTextHandler(output, opts), nil
	default:
		return nil, fmt.Errorf("invalid log format %q (must be json or text)", format)
	}
}

// NewPhaseALogging creates process, client, and config-manager loggers from
// one explicit local handler. counter is optional only because E2E explicitly
// requires stdout-only behavior.
func NewPhaseALogging(local, counter slog.Handler, baseAttrs []slog.Attr) (*PhaseALogging, error) {
	if local == nil {
		return nil, fmt.Errorf("local log handler cannot be nil")
	}
	localGraph := local
	if counter != nil {
		localGraph = logging.NewMultiHandler(local, counter)
	}
	attrs := append([]slog.Attr(nil), baseAttrs...)
	baseGraph := handlerWithAttrs(localGraph, attrs)
	process := slog.New(baseGraph)
	return &PhaseALogging{
		Process:       process,
		Client:        process.With("component", "natsclient"),
		ConfigManager: process.With("component", "config-manager"),
		baseGraph:     baseGraph,
		baseAttrs:     attrs,
	}, nil
}

// Steady derives the steady-state process logger from the same local graph and
// base attributes. A nil destination is the explicit stdout-only/disabled path.
func (l *PhaseALogging) Steady(destination slog.Handler) *slog.Logger {
	if destination == nil {
		return l.Process
	}
	return slog.New(logging.NewMultiHandler(l.baseGraph, handlerWithAttrs(destination, l.baseAttrs)))
}

// NewClient constructs a client with its explicit non-forwarding logger and
// metrics registry. Nil dependencies are rejected instead of defaulted.
func NewClient(urls string, logger *slog.Logger, metrics *metric.MetricsRegistry) (*natsclient.Client, error) {
	if logger == nil {
		return nil, fmt.Errorf("client logger cannot be nil")
	}
	if metrics == nil {
		return nil, logBootFailure(logger, "client-create", fmt.Errorf("client metrics registry cannot be nil"))
	}
	client, err := natsclient.NewClient(urls, natsclient.WithLogger(logger), natsclient.WithMetrics(metrics))
	if err != nil {
		return nil, logBootFailure(logger, "client-create", fmt.Errorf("create NATS client: %w", err))
	}
	return client, nil
}

// ConnectClient connects and waits for readiness. It is the sole owner of the
// configured local failure record for both primary binary connection paths.
func ConnectClient(ctx context.Context, client *natsclient.Client, logger *slog.Logger) error {
	if logger == nil {
		return fmt.Errorf("client logger cannot be nil")
	}
	if client == nil {
		return logBootFailure(logger, "client-connect", fmt.Errorf("NATS client cannot be nil"))
	}
	if err := client.Connect(ctx); err != nil {
		return logBootFailure(logger, "client-connect", fmt.Errorf("connect to NATS: %w", err))
	}
	connectionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.WaitForConnection(connectionCtx); err != nil {
		return logBootFailure(logger, "client-readiness", fmt.Errorf("NATS connection timeout: %w", err))
	}
	return nil
}

// StartConfigManager completes config arbitration and returns the effective
// desired state selected by the manager. The caller-supplied logger is retained
// by the manager for its lifetime.
func StartConfigManager(
	ctx context.Context,
	initial *config.Config,
	client *natsclient.Client,
	logger *slog.Logger,
) (*config.Manager, *config.Config, error) {
	if logger == nil {
		return nil, nil, fmt.Errorf("config-manager logger cannot be nil")
	}
	manager, err := config.NewConfigManager(ctx, initial, client, logger)
	if err != nil {
		return nil, nil, logBootFailure(
			logger, "config-manager-create", fmt.Errorf("create config manager: %w", err),
		)
	}
	if err := manager.Start(ctx); err != nil {
		return nil, nil, logBootFailure(
			logger, "config-manager-start", fmt.Errorf("start config manager: %w", err),
		)
	}
	return manager, manager.GetConfig().Get(), nil
}

// StartValidatedConfigManager arbitrates desired state and validates the
// selected effective configuration before returning it to either root.
func StartValidatedConfigManager(
	ctx context.Context,
	initial *config.Config,
	client *natsclient.Client,
	logger *slog.Logger,
) (*config.Manager, *config.Config, error) {
	manager, effective, err := StartConfigManager(ctx, initial, client, logger)
	if err != nil {
		return nil, nil, err
	}
	if err := ValidateEffectiveConfig(effective, logger); err != nil {
		_ = manager.Stop(5 * time.Second)
		return nil, nil, err
	}
	return manager, effective, nil
}

// ValidateEffectiveConfig applies every composition validation gate to the
// post-arbitration config before resources or forwarding are composed from it.
func ValidateEffectiveConfig(cfg *config.Config, logger *slog.Logger) error {
	if logger == nil {
		return fmt.Errorf("effective-config logger cannot be nil")
	}
	if cfg == nil {
		return logBootFailure(logger, "effective-config-validation", fmt.Errorf("effective configuration cannot be nil"))
	}
	if err := cfg.Validate(); err != nil {
		return logBootFailure(logger, "effective-config-validation", fmt.Errorf("invalid effective configuration: %w", err))
	}
	if err := rulepackcap.ValidateConfig(cfg); err != nil {
		return logBootFailure(
			logger, "effective-rule-pack-validation", fmt.Errorf("invalid effective rule-pack composition: %w", err),
		)
	}
	if err := graphresearch.ValidateConfig(cfg); err != nil {
		return logBootFailure(
			logger, "effective-capability-validation", fmt.Errorf("invalid effective capability composition: %w", err),
		)
	}
	return nil
}

// EnsureEffectiveStreams verifies account limits and provisions all streams
// from post-arbitration desired state before forwarding can be installed.
func EnsureEffectiveStreams(
	ctx context.Context,
	cfg *config.Config,
	client *natsclient.Client,
	logger *slog.Logger,
) error {
	if logger == nil {
		return fmt.Errorf("stream bootstrap logger cannot be nil")
	}
	manager := config.NewStreamsManager(client, logger)
	if err := manager.VerifyJetStreamLimits(ctx, cfg); err != nil {
		return logBootFailure(logger, "jetstream-limit-verification", fmt.Errorf("verify jetstream limits: %w", err))
	}
	if err := manager.EnsureStreams(ctx, cfg); err != nil {
		return logBootFailure(logger, "stream-provisioning", fmt.Errorf("ensure streams: %w", err))
	}
	return nil
}

// NewForwardingHandler resolves effective outer activation before decoding
// inner policy. Disabled and absent entries therefore cannot fail decoding.
func NewForwardingHandler(
	services types.ServiceConfigs,
	publisher logging.NATSPublisher,
	logger *slog.Logger,
) (slog.Handler, error) {
	outer, exists := services["log-forwarder"]
	if !exists || !outer.Enabled {
		return nil, nil
	}
	if logger == nil {
		return nil, fmt.Errorf("log-forwarder bootstrap logger cannot be nil")
	}
	if publisher == nil {
		return nil, logBootFailure(logger, "log-forwarder-composition", fmt.Errorf("enabled log-forwarder publisher cannot be nil"))
	}
	policy, err := logforwarderpolicy.Resolve(outer.Config)
	if err != nil {
		return nil, logBootFailure(
			logger, "log-forwarder-composition", fmt.Errorf("resolve enabled log-forwarder policy: %w", err),
		)
	}
	return logging.NewNATSLogHandler(publisher, logging.NATSLogHandlerConfig{
		MinLevel:       policy.MinLevel,
		ExcludeSources: policy.ExcludeSources,
	}), nil
}

func logBootFailure(logger *slog.Logger, stage string, err error) error {
	logger.Error("Boot phase failed", "boot_stage", stage, "error", err)
	return err
}

func handlerWithAttrs(handler slog.Handler, attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return handler
	}
	return handler.WithAttrs(attrs)
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q (must be debug, info, warn or error)", level)
	}
}
