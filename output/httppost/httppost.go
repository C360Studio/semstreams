// Package httppost provides HTTP POST output component for sending messages to HTTP endpoints
package httppost

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/pkg/acme"
	"github.com/c360/semstreams/pkg/security"
	"github.com/c360/semstreams/pkg/tlsutil"
)

// Config holds configuration for HTTP POST output component
type Config struct {
	Ports       *component.PortConfig `json:"ports"        schema:"type:ports,description:Port configuration,category:basic"`
	URL         string                `json:"url"          schema:"type:string,description:HTTP endpoint URL,category:basic"`
	Headers     map[string]string     `json:"headers"      schema:"type:object,description:HTTP headers,category:advanced"`
	Timeout     int                   `json:"timeout"      schema:"type:int,description:Timeout (sec),category:advanced"`
	RetryCount  int                   `json:"retry_count"  schema:"type:int,description:Retry count,category:advanced"`
	ContentType string                `json:"content_type" schema:"type:string,description:Content-Type,category:basic"`
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	if c.URL == "" {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Config", "Validate", "url is required")
	}

	// Validate URL format
	if _, err := url.Parse(c.URL); err != nil {
		return errors.WrapInvalid(err, "Config", "Validate", "invalid URL format")
	}

	if c.Timeout < 0 || c.Timeout > 300 {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Config", "Validate",
			"timeout must be between 0 and 300 seconds")
	}

	if c.RetryCount < 0 || c.RetryCount > 10 {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Config", "Validate",
			"retry_count must be between 0 and 10")
	}

	return nil
}

// DefaultConfig returns default configuration for HTTP POST output
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name:        "nats_input",
			Type:        "nats",
			Subject:     "output.>",
			Required:    true,
			Description: "NATS subjects to send via HTTP POST",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs: inputDefs,
		},
		URL:         "http://localhost:8080/webhook",
		Headers:     make(map[string]string),
		Timeout:     30,
		RetryCount:  3,
		ContentType: "application/json",
	}
}

// httpPostSchema defines the configuration schema for HTTP POST output component
var httpPostSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Output implements HTTP POST output for NATS messages
type Output struct {
	name        string
	subjects    []string
	url         string
	headers     map[string]string
	timeout     time.Duration
	retryCount  int
	contentType string
	natsClient  *natsclient.Client
	security    security.Config
	httpClient  *http.Client

	// Lifecycle management
	shutdown    chan struct{}
	done        chan struct{}
	running     bool
	startTime   time.Time
	mu          sync.RWMutex
	lifecycleMu sync.Mutex
	wg          *sync.WaitGroup
	tlsCleanup  func() // TLS cleanup function (ACME renewal loop)

	// Metrics
	messagesSent    int64
	messagesRetried int64
	errors          int64
	lastActivity    time.Time
}

// NewOutput creates a new HTTP POST output from configuration
func NewOutput(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := component.SafeUnmarshal(rawConfig, &config); err != nil {
		return nil, errors.WrapInvalid(err, "Output", "NewOutput", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}

	// Extract subjects from port configuration
	var inputSubjects []string
	for _, input := range config.Ports.Inputs {
		if input.Type == "nats" {
			inputSubjects = append(inputSubjects, input.Subject)
		}
	}

	if len(inputSubjects) == 0 {
		return nil, errors.WrapInvalid(errors.ErrInvalidConfig, "Output", "NewOutput", "no input subjects configured")
	}

	// Validate URL
	if config.URL == "" {
		return nil, errors.WrapInvalid(errors.ErrInvalidConfig, "Output", "NewOutput", "URL is required")
	}

	timeout := time.Duration(config.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// Create HTTP client with optional TLS configuration
	httpClient := &http.Client{
		Timeout: timeout,
	}

	var tlsCleanup func()

	// Configure TLS if client TLS is configured at platform level
	if len(deps.Security.TLS.Client.CAFiles) > 0 ||
		deps.Security.TLS.Client.InsecureSkipVerify ||
		deps.Security.TLS.Client.MinVersion != "" ||
		deps.Security.TLS.Client.MTLS.Enabled ||
		(deps.Security.TLS.Client.Mode == "acme" && deps.Security.TLS.Client.ACME.Enabled) {

		var tlsConfig *tls.Config
		var err error

		// Check if ACME mode is enabled for client
		if deps.Security.TLS.Client.Mode == "acme" && deps.Security.TLS.Client.ACME.Enabled {
			// Create lifecycle context for ACME operations
			// Note: Using background context as httppost doesn't have explicit lifecycle context yet
			ctx := context.Background()

			tlsConfig, tlsCleanup, err = tlsutil.LoadClientTLSConfigWithACME(
				ctx,
				deps.Security.TLS.Client,
			)
			if err != nil {
				return nil, errors.WrapFatal(err, "httppost-output", "NewOutput",
					"load TLS config with ACME")
			}
		} else {
			// Use manual TLS configuration
			tlsConfig, err = tlsutil.LoadClientTLSConfigWithMTLS(
				deps.Security.TLS.Client,
				deps.Security.TLS.Client.MTLS,
			)
			if err != nil {
				return nil, errors.WrapFatal(err, "httppost-output", "NewOutput",
					"load TLS config with mTLS")
			}
		}

		httpClient.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
	}

	return &Output{
		name:        "httppost-output",
		subjects:    inputSubjects,
		url:         config.URL,
		headers:     config.Headers,
		timeout:     timeout,
		retryCount:  config.RetryCount,
		contentType: config.ContentType,
		natsClient:  deps.NATSClient,
		security:    deps.Security,
		httpClient:  httpClient,
		shutdown:    make(chan struct{}),
		done:        make(chan struct{}),
		wg:          &sync.WaitGroup{},
		tlsCleanup:  tlsCleanup,
	}, nil
}

// Initialize prepares the output (no-op for HTTP POST)
func (h *Output) Initialize() error {
	return nil
}

// Start begins sending messages via HTTP POST
func (h *Output) Start(ctx context.Context) error {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()

	if h.running {
		return errors.WrapFatal(errors.ErrAlreadyStarted, "Output", "Start", "check running state")
	}

	if h.natsClient == nil {
		return errors.WrapFatal(errors.ErrMissingConfig, "Output", "Start", "NATS client required")
	}

	// Subscribe to input subjects
	for _, subject := range h.subjects {
		if err := h.natsClient.Subscribe(ctx, subject, h.handleMessage); err != nil {
			return errors.WrapTransient(err, "Output", "Start", fmt.Sprintf("subscribe to %s", subject))
		}
	}

	h.mu.Lock()
	h.running = true
	h.startTime = time.Now()
	h.mu.Unlock()

	return nil
}

// Stop gracefully stops the output
func (h *Output) Stop(timeout time.Duration) error {
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()

	if !h.running {
		return nil
	}

	// Signal shutdown
	close(h.shutdown)

	// Wait for goroutines with timeout
	waitCh := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		// Clean shutdown
	case <-time.After(timeout):
		return errors.WrapTransient(fmt.Errorf("shutdown timeout after %v", timeout), "Output", "Stop", "shutdown")
	}

	// Stop ACME renewal loop if active
	if h.tlsCleanup != nil {
		h.tlsCleanup()
	}

	h.mu.Lock()
	h.running = false
	close(h.done)
	h.mu.Unlock()

	return nil
}

// handleMessage processes incoming messages
func (h *Output) handleMessage(ctx context.Context, msgData []byte) {
	h.mu.Lock()
	h.lastActivity = time.Now()
	h.mu.Unlock()

	// Send HTTP POST with retries
	for attempt := 0; attempt <= h.retryCount; attempt++ {
		// Check context cancellation before retry
		select {
		case <-ctx.Done():
			atomic.AddInt64(&h.errors, 1)
			return
		default:
		}

		if attempt > 0 {
			atomic.AddInt64(&h.messagesRetried, 1)
			// Exponential backoff with context cancellation
			timer := time.NewTimer(time.Duration(attempt*attempt) * 100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				atomic.AddInt64(&h.errors, 1)
				return
			case <-timer.C:
			}
		}

		if err := h.sendHTTPPost(ctx, msgData); err == nil {
			atomic.AddInt64(&h.messagesSent, 1)
			return
		}
	}

	// All retries failed
	atomic.AddInt64(&h.errors, 1)
}

// sendHTTPPost sends a single HTTP POST request
func (h *Output) sendHTTPPost(ctx context.Context, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, "POST", h.url, bytes.NewReader(data))
	if err != nil {
		return err
	}

	// Set content type
	req.Header.Set("Content-Type", h.contentType)

	// Set custom headers
	for key, value := range h.headers {
		req.Header.Set(key, value)
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Read and discard body to reuse connection
	_, _ = io.Copy(io.Discard, resp.Body)

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return nil
}

// Discoverable interface implementation

// Meta returns component metadata
func (h *Output) Meta() component.Metadata {
	return component.Metadata{
		Name:        h.name,
		Type:        "output",
		Description: "HTTP POST output for sending messages to HTTP endpoints",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions
func (h *Output) InputPorts() []component.Port {
	ports := make([]component.Port, len(h.subjects))
	for i, subj := range h.subjects {
		ports[i] = component.Port{
			Name:      fmt.Sprintf("input_%d", i),
			Direction: component.DirectionInput,
			Required:  true,
			Config:    component.NATSPort{Subject: subj},
		}
	}
	return ports
}

// OutputPorts returns configured output port definitions (none for HTTP POST)
func (h *Output) OutputPorts() []component.Port {
	// HTTP POST output has no NATS output ports
	return []component.Port{}
}

// ConfigSchema returns the configuration schema
func (h *Output) ConfigSchema() component.ConfigSchema {
	return httpPostSchema
}

// Health returns the current health status
func (h *Output) Health() component.HealthStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    h.running,
		LastCheck:  time.Now(),
		ErrorCount: int(atomic.LoadInt64(&h.errors)),
		Uptime:     time.Since(h.startTime),
	}
}

// DataFlow returns current data flow metrics
func (h *Output) DataFlow() component.FlowMetrics {
	h.mu.RLock()
	defer h.mu.RUnlock()

	sent := atomic.LoadInt64(&h.messagesSent)
	errorCount := atomic.LoadInt64(&h.errors)

	var errorRate float64
	total := sent + errorCount
	if total > 0 {
		errorRate = float64(errorCount) / float64(total)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0, // TODO: Calculate rate
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      h.lastActivity,
	}
}

// Register registers the HTTP POST output component with the given registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "httppost",
		Factory:     NewOutput,
		Schema:      httpPostSchema,
		Type:        "output",
		Protocol:    "httppost",
		Domain:      "network",
		Description: "HTTP POST output for sending messages to HTTP endpoints with retries",
		Version:     "0.1.0",
	})
}

// initACMEClient initializes ACME client from security.ACMEConfig
func initACMEClient(cfg security.ACMEConfig) (*acme.Client, error) {
	renewBefore, err := time.ParseDuration(cfg.RenewBefore)
	if err != nil {
		renewBefore = 8 * time.Hour // Default
	}

	return acme.NewClient(acme.Config{
		DirectoryURL:  cfg.DirectoryURL,
		Email:         cfg.Email,
		Domains:       cfg.Domains,
		ChallengeType: cfg.ChallengeType,
		RenewBefore:   renewBefore,
		StoragePath:   cfg.StoragePath,
		CABundle:      cfg.CABundle,
	})
}
