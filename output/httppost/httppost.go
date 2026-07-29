// Package httppost provides HTTP POST output component for sending messages to HTTP endpoints
package httppost

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/acme"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/security"
	"github.com/c360studio/semstreams/pkg/tlsutil"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
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
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "url is required")
	}

	// Validate URL format
	if _, err := url.Parse(c.URL); err != nil {
		return errs.WrapInvalid(err, "Config", "Validate", "invalid URL format")
	}

	if c.Timeout < 0 || c.Timeout > 300 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
			"timeout must be between 0 and 300 seconds")
	}

	if c.RetryCount < 0 || c.RetryCount > 10 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
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
	config      Config // Store full config for port type checking
	natsClient  *natsclient.Client
	logger      *slog.Logger
	security    security.Config
	httpClient  *http.Client

	// Lifecycle management
	shutdown      chan struct{}
	done          chan struct{}
	running       bool
	startTime     time.Time
	mu            sync.RWMutex
	lifecycleMu   sync.Mutex
	wg            *sync.WaitGroup
	subscriptions []*natsclient.Subscription
	tlsCleanup    func() // TLS cleanup function (ACME renewal loop)

	// Metrics
	messagesSent    int64
	messagesRetried int64
	errors          int64
	lastActivity    time.Time

	// Lifecycle reporting
	lifecycleReporter component.LifecycleReporter
}

// NewOutput creates a new HTTP POST output from configuration
func NewOutput(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := component.SafeUnmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "Output", "NewOutput", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}

	// Extract subjects from port configuration
	var inputSubjects []string
	for _, input := range config.Ports.Inputs {
		if input.Type == "nats" || input.Type == "jetstream" {
			inputSubjects = append(inputSubjects, input.Subject)
		}
	}

	if len(inputSubjects) == 0 {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "Output", "NewOutput", "no input subjects configured")
	}

	// Validate URL
	if config.URL == "" {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "Output", "NewOutput", "URL is required")
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
				return nil, errs.WrapFatal(err, "httppost-output", "NewOutput",
					"load TLS config with ACME")
			}
		} else {
			// Use manual TLS configuration
			tlsConfig, err = tlsutil.LoadClientTLSConfigWithMTLS(
				deps.Security.TLS.Client,
				deps.Security.TLS.Client.MTLS,
			)
			if err != nil {
				return nil, errs.WrapFatal(err, "httppost-output", "NewOutput",
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
		config:      config, // Store full config for port type checking
		natsClient:  deps.NATSClient,
		logger:      deps.GetLogger(),
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
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Output", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Output", "Start", "context already cancelled")
	}

	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()

	if h.running {
		return errs.WrapFatal(errs.ErrAlreadyStarted, "Output", "Start", "check running state")
	}

	if h.natsClient == nil {
		return errs.WrapFatal(errs.ErrMissingConfig, "Output", "Start", "NATS client required")
	}

	// Recreate shutdown/done channels for restart support
	h.shutdown = make(chan struct{})
	h.done = make(chan struct{})
	h.wg = &sync.WaitGroup{}

	// Subscribe to input ports based on port type
	if err := h.setupSubscriptions(ctx); err != nil {
		return err
	}

	// Initialize lifecycle reporter for observability
	h.lifecycleReporter = component.NewCatalogLifecycleReporter(ctx, h.natsClient, h.name, h.logger)

	h.mu.Lock()
	h.running = true
	h.startTime = time.Now()
	h.mu.Unlock()

	// Report idle state after startup
	if h.lifecycleReporter != nil {
		if err := h.lifecycleReporter.ReportStage(ctx, "idle"); err != nil {
			h.logger.Debug("failed to report lifecycle stage", slog.String("stage", "idle"), slog.Any("error", err))
		}
	}

	return nil
}

// setupSubscriptions creates subscriptions for input ports based on port type
func (h *Output) setupSubscriptions(ctx context.Context) error {
	for _, port := range h.config.Ports.Inputs {
		if port.Subject == "" {
			continue
		}

		switch port.Type {
		case "jetstream":
			if err := h.setupJetStreamConsumer(ctx, port); err != nil {
				return errs.WrapTransient(err, "Output", "Start",
					fmt.Sprintf("JetStream consumer for %s", port.Subject))
			}

		case "nats":
			sub, err := h.natsClient.Subscribe(ctx, port.Subject, func(ctx context.Context, msg *nats.Msg) {
				h.handleMessage(ctx, msg.Data)
			})
			if err != nil {
				h.logger.Error("Failed to subscribe to NATS subject",
					"component", h.name,
					"subject", port.Subject,
					"error", err)
				return errs.WrapTransient(err, "Output", "Start",
					fmt.Sprintf("subscribe to %s", port.Subject))
			}
			h.subscriptions = append(h.subscriptions, sub)
			h.logger.Debug("Subscribed to NATS subject successfully",
				"component", h.name,
				"subject", port.Subject)

		default:
			h.logger.Warn("Unknown port type, skipping", "port", port.Name, "type", port.Type)
		}
	}
	return nil
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (h *Output) setupJetStreamConsumer(ctx context.Context, port component.PortDefinition) error {
	streamName := port.StreamName
	if streamName == "" {
		streamName = h.deriveStreamName(port.Subject)
	}
	if streamName == "" {
		return errs.WrapInvalid(
			errs.ErrInvalidConfig,
			"Output",
			"setupJetStreamConsumer",
			fmt.Sprintf("could not derive stream name for subject %s", port.Subject),
		)
	}

	if err := h.waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "Output", "setupJetStreamConsumer",
			fmt.Sprintf("stream %s not available", streamName))
	}

	sanitizedSubject := strings.ReplaceAll(port.Subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	consumerName := fmt.Sprintf("httppost-output-%s", sanitizedSubject)

	h.logger.Debug("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", port.Subject)

	// Get consumer config from port definition (allows user configuration)
	consumerCfg := component.GetConsumerConfigFromDefinition(port)

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: port.Subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		MaxDeliver:    consumerCfg.MaxDeliver,
		AutoCreate:    false,
	}

	err := h.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		h.handleMessage(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			h.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Output", "setupJetStreamConsumer",
			fmt.Sprintf("consumer setup for stream %s", streamName))
	}

	h.logger.Debug("HTTP POST output subscribed (JetStream)", "subject", port.Subject, "stream", streamName)
	return nil
}

// waitForStream waits for a JetStream stream to be available
func (h *Output) waitForStream(ctx context.Context, streamName string) error {
	js, err := h.natsClient.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "Output", "waitForStream", "get JetStream context")
	}

	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := js.Stream(ctx, streamName)
		if err == nil {
			return nil
		}
		if i < maxRetries-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
				retryInterval = min(retryInterval*2, maxInterval)
			}
		}
	}
	return errs.WrapTransient(
		errs.ErrConnectionTimeout,
		"Output",
		"waitForStream",
		fmt.Sprintf("stream %s not available after %d retries", streamName, maxRetries),
	)
}

// deriveStreamName extracts stream name from subject convention
func (h *Output) deriveStreamName(subject string) string {
	subject = strings.TrimPrefix(subject, "*.")
	subject = strings.TrimSuffix(subject, ".>")
	subject = strings.TrimSuffix(subject, ".*")

	parts := strings.Split(subject, ".")
	if len(parts) == 0 || parts[0] == "" || parts[0] == "*" || parts[0] == ">" {
		return ""
	}
	return strings.ToUpper(parts[0])
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

	// Unsubscribe from all NATS subjects
	for _, sub := range h.subscriptions {
		if err := sub.Unsubscribe(); err != nil {
			h.logger.Warn("Failed to unsubscribe", "error", err)
		}
	}
	h.subscriptions = nil

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
		return errs.WrapTransient(
			errs.ErrConnectionTimeout,
			"Output",
			"Stop",
			fmt.Sprintf("shutdown timeout after %v", timeout),
		)
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

// reportPosting reports the posting stage (throttled to avoid KV spam)
func (h *Output) reportPosting(ctx context.Context) {
	if h.lifecycleReporter != nil {
		if err := h.lifecycleReporter.ReportStage(ctx, "posting"); err != nil {
			h.logger.Debug("failed to report lifecycle stage", slog.String("stage", "posting"), slog.Any("error", err))
		}
	}
}

// handleMessage processes incoming messages
func (h *Output) handleMessage(ctx context.Context, msgData []byte) {
	// Report posting stage for lifecycle observability
	h.reportPosting(ctx)

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
		// HTTP errors are transient and should be retried
		return errs.WrapTransient(
			fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status),
			"Output",
			"sendHTTPPost",
			"HTTP request",
		)
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
