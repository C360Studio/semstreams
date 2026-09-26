package boot

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"
)

// CLI holds the command-line configuration both framework binaries parse with
// one parser and one set of defaults (design D15).
type CLI struct {
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

// ParseFlags parses args (the process arguments without the program name) into
// a CLI, with environment-variable fallbacks. A malformed flag exits the
// process with status 2, as the standard flag package does. Composition verbs
// (catalog, validate, graph) are positional and stop flag parsing, so a verb's
// own arguments are left for the verb.
func ParseFlags(args []string, build BuildInfo) CLI {
	var cfg CLI
	fs := newFlagSet(&cfg, build)
	_ = fs.Parse(args) // ExitOnError: Parse never returns a non-nil error.

	// Override log level if debug is set
	if cfg.Debug {
		cfg.LogLevel = "debug"
	}
	return cfg
}

// newFlagSet declares every flag against cfg. It is also how help prints the
// defaults, so the help text can never drift from the parser.
func newFlagSet(cfg *CLI, build BuildInfo) *flag.FlagSet {
	fs := flag.NewFlagSet(appName, flag.ExitOnError)

	fs.StringVar(&cfg.ConfigPath, "config",
		getEnv("SEMSTREAMS_CONFIG", "configs/example.json"),
		"Path to configuration file (env: SEMSTREAMS_CONFIG)")

	fs.StringVar(&cfg.ConfigPath, "c",
		getEnv("SEMSTREAMS_CONFIG", "configs/example.json"),
		"Path to configuration file (env: SEMSTREAMS_CONFIG)")

	fs.StringVar(&cfg.LogLevel, "log-level",
		getEnv("SEMSTREAMS_LOG_LEVEL", "info"),
		"Log level: debug, info, warn, error (env: SEMSTREAMS_LOG_LEVEL)")

	fs.StringVar(&cfg.LogFormat, "log-format",
		getEnv("SEMSTREAMS_LOG_FORMAT", "json"),
		"Log format: json, text (env: SEMSTREAMS_LOG_FORMAT)")

	fs.BoolVar(&cfg.Debug, "debug",
		getEnvBool("SEMSTREAMS_DEBUG", false),
		"Enable debug mode (env: SEMSTREAMS_DEBUG)")

	fs.IntVar(&cfg.DebugPort, "debug-port",
		getEnvInt("SEMSTREAMS_DEBUG_PORT", 8083),
		"Debug server port, 0 to disable (env: SEMSTREAMS_DEBUG_PORT)")

	fs.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout",
		getEnvDuration("SEMSTREAMS_SHUTDOWN_TIMEOUT", 30*time.Second),
		"Graceful shutdown timeout (env: SEMSTREAMS_SHUTDOWN_TIMEOUT)")

	// Default is 0 (disabled). When set, binds a dedicated /health +
	// /healthz listener on this port independent of the service-manager
	// UI's HTTP port — convenience for Docker / Kubernetes probes that
	// want a stable, lightweight health surface. The service-manager's
	// main HTTP server still serves /health on services.service-manager.
	// config.http_port; this flag is additive.
	fs.IntVar(&cfg.HealthPort, "health-port",
		getEnvInt("SEMSTREAMS_HEALTH_PORT", 0),
		"Dedicated /health + /healthz listener port, 0 to disable (env: SEMSTREAMS_HEALTH_PORT). Independent of services.service-manager.config.http_port.")

	fs.BoolVar(&cfg.ShowVersion, "version", false, "Show version information")
	fs.BoolVar(&cfg.ShowVersion, "v", false, "Show version information")
	fs.BoolVar(&cfg.ShowHelp, "help", false, "Show help information")
	fs.BoolVar(&cfg.ShowHelp, "h", false, "Show help information")
	fs.BoolVar(&cfg.Validate, "validate", false, "Validate configuration and exit")

	fs.Usage = func() { printDetailedHelp(build) }
	return fs
}

func validateFlags(cfg Options) error {
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

func printDetailedHelp(build BuildInfo) {
	_, _ = fmt.Fprintf(os.Stderr, `%s - Semantic Stream Processing

Usage: %s [options]
       %s catalog
       %s validate <config-path>
       %s graph <config-path> [--mermaid]

Options:
`, appName, os.Args[0], os.Args[0], os.Args[0], os.Args[0])
	fs := newFlagSet(&CLI{}, build)
	fs.SetOutput(os.Stderr)
	fs.PrintDefaults()
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

  # Validate the composition offline (alias of: %s validate <config-path>)
  %s --validate --config /path/to/config.json

Version: %s
Build: %s
`, os.Args[0], os.Args[0], os.Args[0], os.Args[0], os.Args[0], build.Version, build.BuildTime)
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
