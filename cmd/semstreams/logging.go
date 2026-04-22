package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"strings"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/logging"
)

// parseLogLevel converts a string log level to slog.Level.
func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// setupLogger creates the logger with MultiHandler.
// The NATSLogHandler is created with a fully initialized publisher.
// The CounterHandler increments semstreams_log_entries_total on every WARN+
// record by (component, level) — it is pass-through, so the stdout and
// NATS handlers still observe every record.
// This should only be called AFTER NATS is connected and streams are created.
func setupLogger(level, format string, natsClient *natsclient.Client, cfg *config.Config, metricsRegistry *metric.MetricsRegistry) *slog.Logger {
	logLevel := parseLogLevel(level)

	opts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: level == "debug",
	}

	// Create stdout handler
	var stdoutHandler slog.Handler
	switch strings.ToLower(format) {
	case "json":
		stdoutHandler = slog.NewJSONHandler(os.Stdout, opts)
	case "text":
		stdoutHandler = slog.NewTextHandler(os.Stdout, opts)
	default:
		stdoutHandler = slog.NewJSONHandler(os.Stdout, opts)
	}

	// Get exclude_sources from config
	excludeSources := getExcludeSources(cfg)

	// Create NATS handler with publisher already set (no nil, no mutation)
	natsHandler := logging.NewNATSLogHandler(natsClient, logging.NATSLogHandlerConfig{
		MinLevel:       logLevel,
		ExcludeSources: excludeSources,
	})

	counterHandler := logging.NewCounterHandler(metricsRegistry.CoreMetrics().LogEntriesTotal)

	// Compose handlers using MultiHandler
	multiHandler := logging.NewMultiHandler(stdoutHandler, natsHandler, counterHandler)

	return slog.New(multiHandler).With(
		"service", "semstreams",
		"version", Version,
		"pid", os.Getpid(),
	)
}

// getExcludeSources extracts exclude_sources from log-forwarder config.
func getExcludeSources(cfg *config.Config) []string {
	// Default: exclude WebSocket worker logs to prevent feedback loops
	excludeSources := []string{"flow-service.websocket"}

	if cfg == nil || cfg.Services == nil {
		return excludeSources
	}

	logFwdCfg, ok := cfg.Services["log-forwarder"]
	if !ok || !logFwdCfg.Enabled {
		return excludeSources
	}

	var lfCfg struct {
		ExcludeSources []string `json:"exclude_sources"`
	}
	if err := json.Unmarshal(logFwdCfg.Config, &lfCfg); err == nil && len(lfCfg.ExcludeSources) > 0 {
		return lfCfg.ExcludeSources
	}

	return excludeSources
}
