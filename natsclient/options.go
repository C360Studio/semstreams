package natsclient

import (
	"log"
	"time"

	"github.com/c360/semstreams/metric"
)

// Logger interface for injecting custom loggers
type Logger interface {
	Printf(format string, v ...any)
	Errorf(format string, v ...any)
	Debugf(format string, v ...any)
}

// defaultLogger implements Logger using standard log package
type defaultLogger struct{}

func (l *defaultLogger) Printf(format string, v ...any) {
	log.Printf("[NATS] "+format, v...)
}

func (l *defaultLogger) Errorf(format string, v ...any) {
	log.Printf("[NATS ERROR] "+format, v...)
}

func (l *defaultLogger) Debugf(_ string, _ ...any) {
	// Silent by default
}

// ClientOption is a functional option for configuring the Client
type ClientOption func(*Client) error

// WithMaxReconnects sets the maximum number of reconnection attempts (-1 for infinite)
func WithMaxReconnects(max int) ClientOption {
	return func(c *Client) error {
		c.maxReconnects = max
		return nil
	}
}

// WithReconnectWait sets the wait time between reconnection attempts
func WithReconnectWait(d time.Duration) ClientOption {
	return func(c *Client) error {
		c.reconnectWait = d
		return nil
	}
}

// WithPingInterval sets the ping interval for connection health checks
func WithPingInterval(d time.Duration) ClientOption {
	return func(c *Client) error {
		c.pingInterval = d
		return nil
	}
}

// WithHealthInterval sets the interval for health monitoring
func WithHealthInterval(d time.Duration) ClientOption {
	return func(c *Client) error {
		c.healthInterval = d
		return nil
	}
}

// WithLogger sets a custom logger for the client
func WithLogger(logger Logger) ClientOption {
	return func(c *Client) error {
		if logger == nil {
			logger = &defaultLogger{}
		}
		c.logger = logger
		return nil
	}
}

// WithDisconnectCallback sets a callback for disconnection events
// This is in addition to NATS's built-in disconnect handler
func WithDisconnectCallback(fn func(error)) ClientOption {
	return func(c *Client) error {
		c.onDisconnect = fn
		return nil
	}
}

// WithReconnectCallback sets a callback for reconnection events
// This is in addition to NATS's built-in reconnect handler
func WithReconnectCallback(fn func()) ClientOption {
	return func(c *Client) error {
		c.onReconnect = fn
		return nil
	}
}

// WithHealthChangeCallback sets a callback for health status changes
func WithHealthChangeCallback(fn func(healthy bool)) ClientOption {
	return func(c *Client) error {
		c.onHealthChange = fn
		return nil
	}
}

// WithConnectionLostCallback sets a callback for when connection is completely lost
func WithConnectionLostCallback(fn func(error)) ClientOption {
	return func(c *Client) error {
		c.onConnectionLost = fn
		return nil
	}
}

// WithCircuitBreakerThreshold sets the number of failures before opening circuit
func WithCircuitBreakerThreshold(threshold int32) ClientOption {
	return func(c *Client) error {
		if threshold < 1 {
			threshold = 5 // reasonable default
		}
		c.circuitThreshold = threshold
		return nil
	}
}

// WithMaxBackoff sets the maximum backoff duration for circuit breaker
func WithMaxBackoff(d time.Duration) ClientOption {
	return func(c *Client) error {
		if d < time.Second {
			d = time.Minute // reasonable default
		}
		c.maxBackoff = d
		return nil
	}
}

// WithCredentials sets username and password for authentication
func WithCredentials(username, password string) ClientOption {
	return func(c *Client) error {
		c.username = username
		c.password = password
		return nil
	}
}

// WithToken sets a token for authentication
func WithToken(token string) ClientOption {
	return func(c *Client) error {
		c.token = token
		return nil
	}
}

// WithTLS enables TLS with optional certificate paths
func WithTLS(certFile, keyFile, caFile string) ClientOption {
	return func(c *Client) error {
		c.tlsCertFile = certFile
		c.tlsKeyFile = keyFile
		c.tlsCAFile = caFile
		c.tlsEnabled = true
		return nil
	}
}

// WithName sets the client name for identification
func WithName(name string) ClientOption {
	return func(c *Client) error {
		c.clientName = name
		return nil
	}
}

// WithTimeout sets the connection timeout
func WithTimeout(d time.Duration) ClientOption {
	return func(c *Client) error {
		c.timeout = d
		return nil
	}
}

// WithDrainTimeout sets the timeout for draining on disconnect
func WithDrainTimeout(d time.Duration) ClientOption {
	return func(c *Client) error {
		c.drainTimeout = d
		return nil
	}
}

// WithCompression enables message compression
func WithCompression(enabled bool) ClientOption {
	return func(c *Client) error {
		c.compression = enabled
		return nil
	}
}

// WithMetrics enables JetStream metrics collection using the provided registry.
// Metrics will track streams and consumers created through this client.
func WithMetrics(registry *metric.MetricsRegistry) ClientOption {
	return func(c *Client) error {
		if registry == nil {
			return nil // No metrics
		}

		metrics, err := newJetStreamMetrics(registry)
		if err != nil {
			return err
		}

		c.jsMetrics = metrics
		return nil
	}
}
