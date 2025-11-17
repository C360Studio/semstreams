package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"
)

// CLIConfig holds command-line configuration
type CLIConfig struct {
	ConfigPath      string
	LogLevel        string
	LogFormat       string
	Debug           bool
	DebugPort       int
	ShutdownTimeout time.Duration
	HealthPort      int
	ShowVersion     bool
	ShowHelp        bool
	Validate        bool
}

func parseFlags() *CLIConfig {
	cfg := &CLIConfig{}

	// Define flags with environment variable fallback
	flag.StringVar(&cfg.ConfigPath, "config",
		getEnv("SEMSTREAMS_CONFIG", "configs/example.json"),
		"Path to configuration file (env: SEMSTREAMS_CONFIG)")

	flag.StringVar(&cfg.ConfigPath, "c",
		getEnv("SEMSTREAMS_CONFIG", "configs/example.json"),
		"Path to configuration file (env: SEMSTREAMS_CONFIG)")

	flag.StringVar(&cfg.LogLevel, "log-level",
		getEnv("SEMSTREAMS_LOG_LEVEL", "info"),
		"Log level: debug, info, warn, error (env: SEMSTREAMS_LOG_LEVEL)")

	flag.StringVar(&cfg.LogFormat, "log-format",
		getEnv("SEMSTREAMS_LOG_FORMAT", "json"),
		"Log format: json, text (env: SEMSTREAMS_LOG_FORMAT)")

	flag.BoolVar(&cfg.Debug, "debug",
		getEnvBool("SEMSTREAMS_DEBUG", false),
		"Enable debug mode (env: SEMSTREAMS_DEBUG)")

	flag.IntVar(&cfg.DebugPort, "debug-port",
		getEnvInt("SEMSTREAMS_DEBUG_PORT", 8083),
		"Debug server port, 0 to disable (env: SEMSTREAMS_DEBUG_PORT)")

	flag.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout",
		getEnvDuration("SEMSTREAMS_SHUTDOWN_TIMEOUT", 30*time.Second),
		"Graceful shutdown timeout (env: SEMSTREAMS_SHUTDOWN_TIMEOUT)")

	flag.IntVar(&cfg.HealthPort, "health-port",
		getEnvInt("SEMSTREAMS_HEALTH_PORT", 8080),
		"Health check port, 0 to disable (env: SEMSTREAMS_HEALTH_PORT)")

	flag.BoolVar(&cfg.ShowVersion, "version", false, "Show version information")
	flag.BoolVar(&cfg.ShowVersion, "v", false, "Show version information")
	flag.BoolVar(&cfg.ShowHelp, "help", false, "Show help information")
	flag.BoolVar(&cfg.ShowHelp, "h", false, "Show help information")
	flag.BoolVar(&cfg.Validate, "validate", false, "Validate configuration and exit")

	// Custom usage
	flag.Usage = func() {
		printDetailedHelp()
	}

	flag.Parse()

	// Override log level if debug is set
	if cfg.Debug {
		cfg.LogLevel = "debug"
	}

	return cfg
}

func validateFlags(cfg *CLIConfig) error {
	// Skip validation for special flags
	if cfg.ShowVersion || cfg.ShowHelp {
		return nil
	}

	// Validate config file exists
	if _, err := os.Stat(cfg.ConfigPath); err != nil {
		return fmt.Errorf("config file not found: %s", cfg.ConfigPath)
	}

	// Validate log level
	validLevels := []string{"debug", "info", "warn", "error"}
	if !contains(validLevels, cfg.LogLevel) {
		return fmt.Errorf("invalid log level: %s", cfg.LogLevel)
	}

	// Validate log format
	validFormats := []string{"json", "text"}
	if !contains(validFormats, cfg.LogFormat) {
		return fmt.Errorf("invalid log format: %s", cfg.LogFormat)
	}

	// Validate health port
	if cfg.HealthPort < 0 || cfg.HealthPort > 65535 {
		return fmt.Errorf("invalid health port: %d", cfg.HealthPort)
	}

	// Validate debug port
	if cfg.DebugPort < 0 || cfg.DebugPort > 65535 {
		return fmt.Errorf("invalid debug port: %d", cfg.DebugPort)
	}

	return nil
}

func printDetailedHelp() {
	_, _ = fmt.Fprintf(os.Stderr, `%s - Semantic Stream Processing

Usage: %s [options]

Options:
`, appName, os.Args[0])
	flag.PrintDefaults()
	_, _ = fmt.Fprintf(os.Stderr, `
Examples:
  # Run with custom config
  %s --config=/path/to/config.json

  # Run with debug logging
  %s --log-level=debug --log-format=text

  # Run with environment variables
  export SEMSTREAMS_CONFIG=/etc/semstreams/config.json
  export SEMSTREAMS_LOG_LEVEL=debug
  %s

  # Validate configuration only
  %s --validate

Version: %s
Build: %s
`, os.Args[0], os.Args[0], os.Args[0], os.Args[0], Version, BuildTime)
}

// Environment variable helper functions
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

// Utility function to check if slice contains string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
