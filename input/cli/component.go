// Package cli provides a CLI input component for interactive user sessions.
// It reads from stdin, publishes user messages to NATS, and handles Ctrl+C signals.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	errs "github.com/c360studio/semstreams/pkg/errs"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

// Component implements the CLI input processor
type Component struct {
	config     Config
	deps       component.Dependencies
	decoder    *message.Decoder
	natsClient *natsclient.Client
	logger     *slog.Logger
	metrics    *cliMetrics

	// Lifecycle state
	mu        sync.RWMutex
	started   bool
	startTime time.Time
	cancel    context.CancelFunc

	// Active loop tracking
	activeLoopID string

	// I/O
	reader io.Reader
	writer io.Writer

	// Ports
	inputPorts  []component.Port
	outputPorts []component.Port
}

// NewComponent creates a new CLI input component
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Apply defaults
	defaults := DefaultConfig()
	if config.UserID == "" {
		config.UserID = defaults.UserID
	}
	if config.SessionID == "" {
		config.SessionID = defaults.SessionID
	}
	if config.Prompt == "" {
		config.Prompt = defaults.Prompt
	}
	if config.StreamName == "" {
		config.StreamName = defaults.StreamName
	}
	if config.Ports == nil {
		config.Ports = defaults.Ports
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	inputPorts := buildDefaultInputPorts()
	outputPorts := buildDefaultOutputPorts()

	if config.Ports != nil {
		if len(config.Ports.Inputs) > 0 {
			inputPorts = component.MergePortConfigs(inputPorts, config.Ports.Inputs, component.DirectionInput)
		}
		if len(config.Ports.Outputs) > 0 {
			outputPorts = component.MergePortConfigs(outputPorts, config.Ports.Outputs, component.DirectionOutput)
		}
	}

	comp := &Component{
		config:      config,
		deps:        deps,
		decoder:     message.NewDecoder(deps.PayloadRegistry),
		natsClient:  deps.NATSClient,
		logger:      deps.GetLogger(),
		metrics:     getMetrics(deps.MetricsRegistry),
		reader:      os.Stdin,
		writer:      os.Stdout,
		inputPorts:  inputPorts,
		outputPorts: outputPorts,
	}

	return comp, nil
}

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "cli-input",
		Type:        "input",
		Description: "CLI input for interactive user sessions with Ctrl+C signal support",
		Version:     "1.0.0",
	}
}

// InputPorts returns input port definitions
func (c *Component) InputPorts() []component.Port {
	return c.inputPorts
}

// OutputPorts returns output port definitions
func (c *Component) OutputPorts() []component.Port {
	return c.outputPorts
}

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return buildConfigSchema()
}

// Health returns current health status
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	healthy := c.started
	uptime := time.Duration(0)
	if c.started {
		uptime = time.Since(c.startTime)
	}

	status := "stopped"
	if healthy {
		status = "running"
	}

	return component.HealthStatus{
		Healthy:   healthy,
		LastCheck: time.Now(),
		Uptime:    uptime,
		Status:    status,
	}
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	return component.FlowMetrics{
		MessagesPerSecond: 0,
		BytesPerSecond:    0,
		ErrorRate:         0,
		LastActivity:      time.Now(),
	}
}

// Initialize prepares the component
func (c *Component) Initialize() error {
	return nil
}

// Start begins the CLI input loop
func (c *Component) Start(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}

	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return fmt.Errorf("cli-input already started")
	}
	c.started = true
	c.startTime = time.Now()
	c.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	c.logger.Info("Starting CLI input component",
		slog.String("user_id", c.config.UserID),
		slog.String("session_id", c.config.SessionID))

	// Setup response subscription
	if err := c.setupSubscriptions(ctx); err != nil {
		c.mu.Lock()
		c.started = false
		c.mu.Unlock()
		return fmt.Errorf("failed to setup subscriptions: %w", err)
	}

	// Setup signal handler for Ctrl+C
	go c.handleSignals(ctx)

	// Start input loop
	go c.inputLoop(ctx)

	return nil
}

// Stop halts the CLI input with graceful shutdown
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	// Create timeout context for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Signal cancellation to input loop and signal handler
	if c.cancel != nil {
		c.cancel()
	}

	// Wait for graceful shutdown or timeout
	// The input loop will exit when context is cancelled
	select {
	case <-ctx.Done():
		c.logger.Warn("CLI input stop timed out", slog.Duration("timeout", timeout))
	default:
		// Immediate shutdown for CLI (no long-running operations to wait for)
	}

	c.mu.Lock()
	c.started = false
	c.mu.Unlock()

	c.logger.Info("CLI input component stopped")
	return nil
}

// setupSubscriptions sets up JetStream subscriptions for responses
func (c *Component) setupSubscriptions(ctx context.Context) error {
	// Subscribe to responses for this CLI session via JetStream
	subject := fmt.Sprintf("user.response.cli.%s", c.config.SessionID)

	// Create JetStream consumer for USER stream
	consumerName := fmt.Sprintf("cli-response-%s", c.config.SessionID)
	cfg := natsclient.StreamConsumerConfig{
		StreamName:    c.config.StreamName, // USER stream
		ConsumerName:  consumerName,
		FilterSubject: subject,
		DeliverPolicy: "new",
		AckPolicy:     "explicit",
		MaxDeliver:    3,
		AutoCreate:    false,
	}

	err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		c.handleResponse(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			c.logger.Error("Failed to ack response message", slog.String("error", ackErr.Error()))
		}
	})
	if err != nil {
		return fmt.Errorf("failed to setup JetStream consumer for %s: %w", subject, err)
	}

	c.logger.Debug("Subscribed to responses via JetStream",
		slog.String("subject", subject),
		slog.String("stream", c.config.StreamName))
	return nil
}

// handleSignals handles OS signals (Ctrl+C)
func (c *Component) handleSignals(ctx context.Context) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)

	for {
		select {
		case <-ctx.Done():
			signal.Stop(sigCh)
			return
		case <-sigCh:
			c.handleCtrlC(ctx)
		}
	}
}

// handleCtrlC sends a cancel signal for the active loop
func (c *Component) handleCtrlC(ctx context.Context) {
	c.mu.RLock()
	loopID := c.activeLoopID
	c.mu.RUnlock()

	if loopID == "" {
		fmt.Fprintln(c.writer, "\nNo active loop to cancel. Press Ctrl+C again to exit.")
		return
	}

	c.logger.Info("Ctrl+C detected, sending cancel signal",
		slog.String("loop_id", loopID))

	sig := &agentic.UserSignal{
		SignalID:    uuid.New().String(),
		Type:        agentic.SignalCancel,
		LoopID:      loopID,
		UserID:      c.config.UserID,
		ChannelType: "cli",
		ChannelID:   c.config.SessionID,
		Timestamp:   time.Now(),
	}

	// Wrap in BaseMessage for consistency
	baseMsg := message.NewBaseMessage(sig.Schema(), sig, "cli-input")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal cancel signal", slog.String("error", err.Error()))
		return
	}

	subject := fmt.Sprintf("user.signal.%s", loopID)
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Error("Failed to publish cancel signal", slog.String("error", err.Error()))
		return
	}

	// Record signal sent
	c.metrics.recordSignalSent("cancel")

	fmt.Fprintf(c.writer, "\nCancel signal sent for loop %s\n", loopID)
}

// inputLoop reads from stdin and publishes user messages
func (c *Component) inputLoop(ctx context.Context) {
	scanner := bufio.NewScanner(c.reader)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Print prompt
		fmt.Fprint(c.writer, c.config.Prompt)

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				c.logger.Error("Error reading input", slog.String("error", err.Error()))
			}
			return // EOF or error
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Handle local commands
		if c.handleLocalCommand(line) {
			continue
		}

		// Publish user message
		c.publishMessage(ctx, line)
	}
}

// handleLocalCommand handles local CLI commands (not sent to router)
func (c *Component) handleLocalCommand(line string) bool {
	switch line {
	case "/quit", "/exit":
		fmt.Fprintln(c.writer, "Goodbye!")
		if c.cancel != nil {
			c.cancel()
		}
		return true
	case "/clear":
		// Clear active loop
		c.mu.Lock()
		c.activeLoopID = ""
		c.mu.Unlock()
		fmt.Fprintln(c.writer, "Active loop cleared.")
		return true
	}
	return false
}

// publishMessage publishes a user message to NATS wrapped in BaseMessage
func (c *Component) publishMessage(ctx context.Context, content string) {
	msg := &agentic.UserMessage{
		MessageID:   uuid.New().String(),
		ChannelType: "cli",
		ChannelID:   c.config.SessionID,
		UserID:      c.config.UserID,
		Content:     content,
		Timestamp:   time.Now(),
	}

	// Include reply-to if we have an active loop
	c.mu.RLock()
	if c.activeLoopID != "" {
		msg.ReplyTo = c.activeLoopID
	}
	c.mu.RUnlock()

	// Wrap in BaseMessage for agentic-dispatch compatibility
	baseMsg := message.NewBaseMessage(msg.Schema(), msg, "cli-input")
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal message", slog.String("error", err.Error()))
		return
	}

	subject := fmt.Sprintf("user.message.cli.%s", c.config.SessionID)
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		c.logger.Error("Failed to publish message", slog.String("error", err.Error()))
		fmt.Fprintln(c.writer, "Error: Failed to send message")
		return
	}

	// Record message published
	c.metrics.recordMessagePublished()

	c.logger.Debug("Published user message",
		slog.String("message_id", msg.MessageID),
		slog.String("subject", subject))
}

// handleResponse processes responses from the router (wrapped in BaseMessage)
func (c *Component) handleResponse(ctx context.Context, data []byte) {
	// Responses are wrapped in BaseMessage by agentic-dispatch
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.ErrorContext(ctx, "Failed to unmarshal BaseMessage", slog.String("error", err.Error()))
		return
	}

	resp, ok := baseMsg.Payload().(*agentic.UserResponse)
	if !ok {
		c.logger.ErrorContext(ctx, "Unexpected payload type",
			slog.String("type", fmt.Sprintf("%T", baseMsg.Payload())))
		return
	}

	// Record response received
	c.metrics.recordResponseReceived(resp.Type)

	c.logger.DebugContext(ctx, "Received response",
		slog.String("response_id", resp.ResponseID),
		slog.String("type", resp.Type),
		slog.String("in_reply_to", resp.InReplyTo))

	// Track active loop from responses
	if resp.InReplyTo != "" {
		c.mu.Lock()
		c.activeLoopID = resp.InReplyTo
		c.mu.Unlock()
	}

	// Display response based on type
	c.displayResponse(*resp)
}

// displayResponse formats and displays a response to the user
func (c *Component) displayResponse(resp agentic.UserResponse) {
	switch resp.Type {
	case agentic.ResponseTypeError:
		fmt.Fprintf(c.writer, "\n[ERROR] %s\n", resp.Content)
	case agentic.ResponseTypeStatus:
		fmt.Fprintf(c.writer, "\n[STATUS] %s\n", resp.Content)
	case agentic.ResponseTypeResult:
		fmt.Fprintf(c.writer, "\n[RESULT]\n%s\n", resp.Content)
	case agentic.ResponseTypePrompt:
		fmt.Fprintf(c.writer, "\n[PROMPT] %s\n", resp.Content)
		// Display actions if present
		for _, action := range resp.Actions {
			fmt.Fprintf(c.writer, "  [%s] %s\n", action.ID, action.Label)
		}
	case agentic.ResponseTypeStream:
		// Streaming content - print without newline prefix for continuity
		fmt.Fprint(c.writer, resp.Content)
	default:
		fmt.Fprintf(c.writer, "\n%s\n", resp.Content)
	}

	// Print prompt after response
	fmt.Fprint(c.writer, c.config.Prompt)
}

// SetReader sets the input reader (for testing)
func (c *Component) SetReader(r io.Reader) {
	c.reader = r
}

// SetWriter sets the output writer (for testing)
func (c *Component) SetWriter(w io.Writer) {
	c.writer = w
}

// SetActiveLoop sets the active loop ID (for testing)
func (c *Component) SetActiveLoop(loopID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activeLoopID = loopID
}

// GetActiveLoop returns the active loop ID
func (c *Component) GetActiveLoop() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.activeLoopID
}
