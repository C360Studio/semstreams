package agenticdispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/auth"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/service"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
)

func init() {
	service.RegisterOpenAPISpec("agentic-dispatch", agenticDispatchOpenAPISpec())
}

// Compile-time check that Component implements the HTTP handler interface
var _ interface {
	RegisterHTTPHandlers(prefix string, mux *http.ServeMux)
} = (*Component)(nil)

// HTTPMessageRequest represents a message request via HTTP.
// This is the request format for the POST /message endpoint.
type HTTPMessageRequest struct {
	Content     string            `json:"content"`
	UserID      string            `json:"user_id,omitempty"`
	ChannelType string            `json:"channel_type,omitempty"`
	ChannelID   string            `json:"channel_id,omitempty"`
	ReplyTo     string            `json:"reply_to,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// HTTPMessageResponse represents the response from the HTTP message endpoint.
type HTTPMessageResponse struct {
	ResponseID string `json:"response_id"`
	Type       string `json:"type"`
	Content    string `json:"content"`
	InReplyTo  string `json:"in_reply_to,omitempty"`
	Error      string `json:"error,omitempty"`
	Timestamp  string `json:"timestamp"`
}

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	// requestIDKey is the context key for request ID.
	requestIDKey contextKey = "request_id"
)

// extractRequestID extracts or generates a request ID from the HTTP request.
func extractRequestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	return uuid.New().String()[:8]
}

// withRequestID adds a request ID to the context and response headers.
func (c *Component) withRequestID(w http.ResponseWriter, r *http.Request) (context.Context, string) {
	requestID := extractRequestID(r)
	w.Header().Set("X-Request-ID", requestID)
	return context.WithValue(r.Context(), requestIDKey, requestID), requestID
}

// RegisterHTTPHandlers registers HTTP endpoints for agentic-dispatch.
// This enables synchronous message processing via HTTP for web clients and E2E tests.
func (c *Component) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	// POST /message - synchronous message processing
	mux.HandleFunc("POST "+prefix+"message", c.handleHTTPMessage)

	// GET /commands - list available commands
	mux.HandleFunc("GET "+prefix+"commands", c.handleListCommands)

	// GET /health - component health check
	mux.HandleFunc("GET "+prefix+"health", c.handleHTTPHealth)

	// Loop management endpoints
	mux.HandleFunc("GET "+prefix+"loops", c.handleListLoops)
	mux.HandleFunc("GET "+prefix+"loops/{id}", c.handleGetLoop)
	mux.HandleFunc("POST "+prefix+"loops/{id}/signal", c.handleLoopSignal)
	mux.HandleFunc("POST "+prefix+"loops/{id}/approval", c.handleLoopApproval)

	// Real-time activity stream (SSE)
	mux.HandleFunc("GET "+prefix+"activity", c.handleActivityStream)

	// Debug endpoint for internal state
	mux.HandleFunc("GET "+prefix+"debug/state", c.handleDebugState)

	c.logger.Info("agentic-dispatch HTTP handlers registered", slog.String("prefix", prefix))
}

// handleHTTPMessage processes a user message synchronously via HTTP.
// Unlike the NATS path, this returns the response directly instead of publishing to a stream.
func (c *Component) handleHTTPMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()

	// Parse request body
	var req HTTPMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Validate required fields
	if req.Content == "" {
		c.writeJSONError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Resolve identity through auth.IdentityFromRequest so a future
	// HTTP middleware contract (ADR-030) can populate authenticated
	// identity via ctx and we'll pick it up here without rewriting
	// the handler. Today, no middleware sets ctx, so this resolves
	// to req.UserID || "http-user" — behavior-equivalent to the
	// prior inline default. auth.WithIdentity propagates the
	// resolved identity to ctx so natsclient auto-injects
	// X-Caller-* headers on downstream NATS publishes.
	id := auth.IdentityFromRequest(r, req.UserID)
	req.UserID = id.ID
	ctx = auth.WithIdentity(ctx, id)
	if req.ChannelType == "" {
		req.ChannelType = "http"
	}
	if req.ChannelID == "" {
		req.ChannelID = fmt.Sprintf("http-%d", time.Now().UnixNano())
	}

	// Build UserMessage
	msg := agentic.UserMessage{
		MessageID:   uuid.New().String(),
		ChannelType: req.ChannelType,
		ChannelID:   req.ChannelID,
		UserID:      req.UserID,
		Content:     req.Content,
		ReplyTo:     req.ReplyTo,
		Metadata:    req.Metadata,
		Timestamp:   time.Now(),
	}

	// Record message received metric
	c.metrics.recordMessageReceived(msg.ChannelType)

	c.logger.Debug("HTTP message received",
		slog.String("message_id", msg.MessageID),
		slog.String("user_id", msg.UserID),
		slog.String("channel", msg.ChannelType),
		slog.String("content_preview", truncate(msg.Content, 50)))

	// Process the message and get response synchronously
	resp := c.processMessageSync(ctx, msg)

	// Record routing duration
	duration := time.Since(startTime).Seconds()
	c.metrics.recordRoutingDuration(duration)

	// Convert to HTTP response format
	httpResp := HTTPMessageResponse{
		ResponseID: resp.ResponseID,
		Type:       resp.Type,
		Content:    resp.Content,
		InReplyTo:  resp.InReplyTo,
		Timestamp:  resp.Timestamp.Format(time.RFC3339),
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(httpResp); err != nil {
		c.logger.Error("Failed to encode HTTP response", slog.String("error", err.Error()))
	}
}

// processMessageSync processes a message and returns the response synchronously.
// This is used by the HTTP handler to avoid the pub/sub response path.
func (c *Component) processMessageSync(ctx context.Context, msg agentic.UserMessage) agentic.UserResponse {
	// Check if it's a command
	if strings.HasPrefix(msg.Content, "/") {
		return c.processCommandSync(ctx, msg)
	}

	// It's a task submission
	return c.processTaskSubmissionSync(ctx, msg)
}

// processCommandSync processes a command and returns the response synchronously.
func (c *Component) processCommandSync(ctx context.Context, msg agentic.UserMessage) agentic.UserResponse {
	name, cmd, args, found := c.registry.Match(msg.Content)
	if !found {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Unknown command. Type /help for available commands.",
			Timestamp:   time.Now(),
		}
	}

	// Check permission
	if cmd.Config.Permission != "" && !c.hasPermission(msg.UserID, cmd.Config.Permission) {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Permission denied: requires '%s'", cmd.Config.Permission),
			Timestamp:   time.Now(),
		}
	}

	// Resolve loop ID
	loopID := ""
	if len(args) > 0 && args[0] != "" {
		loopID = args[0]
	} else if c.config.AutoContinue {
		loopID = c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID)
	}

	// Check if loop is required
	if cmd.Config.RequireLoop && loopID == "" {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "No active loop. Specify a loop_id or start a task first.",
			Timestamp:   time.Now(),
		}
	}

	// Execute handler
	resp, err := cmd.Handler(ctx, msg, args, loopID)
	if err != nil {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     fmt.Sprintf("Command failed: %s", err.Error()),
			Timestamp:   time.Now(),
		}
	}

	// Record command executed
	c.metrics.recordCommandExecuted(name)

	c.logger.Debug("HTTP command executed",
		slog.String("command", name),
		slog.String("user_id", msg.UserID))

	// Also publish to stream for async consumers (optional - allows CLI, other services to see responses)
	c.sendResponse(ctx, resp)

	return resp
}

// processTaskSubmissionSync processes a task submission and returns acknowledgment.
// The actual task execution happens asynchronously via NATS.
func (c *Component) processTaskSubmissionSync(ctx context.Context, msg agentic.UserMessage) agentic.UserResponse {
	// Check submit permission
	if !c.hasPermission(msg.UserID, "submit_task") {
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Permission denied: cannot submit tasks",
			Timestamp:   time.Now(),
		}
	}

	// Determine loop ID (continue existing or create new)
	loopID := ""
	if msg.ReplyTo != "" {
		loopID = msg.ReplyTo
	} else if c.config.AutoContinue {
		loopID = c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID)
	}

	// Create new loop if needed
	if loopID == "" {
		loopID = "loop_" + uuid.New().String()[:8]
	}

	taskID := uuid.New().String()

	// Create task message
	task := agentic.TaskMessage{
		LoopID:           loopID,
		TaskID:           taskID,
		Role:             c.config.DefaultRole,
		Model:            c.resolveModel(),
		Prompt:           msg.Content,
		ContextRequestID: msg.ContextRequestID,
	}

	// Track the loop
	c.loopTracker.Track(&LoopInfo{
		LoopID:           loopID,
		TaskID:           taskID,
		UserID:           msg.UserID,
		ChannelType:      msg.ChannelType,
		ChannelID:        msg.ChannelID,
		State:            "pending",
		MaxIterations:    20,
		ContextRequestID: msg.ContextRequestID,
		CreatedAt:        time.Now(),
	})

	// Record loop started
	c.metrics.recordLoopStarted()

	// Wrap task in BaseMessage envelope (required by agentic-loop)
	baseMsg := message.NewBaseMessage(task.Schema(), &task, "agentic-dispatch-http")
	taskData, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Error("Failed to marshal task", slog.String("error", err.Error()))
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Failed to create task. Please try again.",
			Timestamp:   time.Now(),
		}
	}

	subject := component.ResolveSubject(c.outputPortDefs(), "agent.task", taskID)
	if err := c.natsClient.PublishToStream(ctx, subject, taskData); err != nil {
		c.logger.Error("Failed to publish task", slog.String("error", err.Error()))
		return agentic.UserResponse{
			ResponseID:  uuid.New().String(),
			ChannelType: msg.ChannelType,
			ChannelID:   msg.ChannelID,
			UserID:      msg.UserID,
			Type:        agentic.ResponseTypeError,
			Content:     "Failed to submit task. Please try again.",
			Timestamp:   time.Now(),
		}
	}

	// Record task submitted
	c.metrics.recordTaskSubmitted()

	c.logger.Debug("HTTP task submitted",
		slog.String("loop_id", loopID),
		slog.String("task_id", taskID),
		slog.String("user_id", msg.UserID))

	// Create acknowledgment response
	resp := agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		InReplyTo:   loopID,
		Type:        agentic.ResponseTypeStatus,
		Content:     fmt.Sprintf("Task submitted. Loop: %s", loopID),
		Timestamp:   time.Now(),
	}

	// Also publish acknowledgment to stream
	c.sendResponse(ctx, resp)

	return resp
}

// handleListCommands returns the list of available commands.
func (c *Component) handleListCommands(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	commands := c.registry.All()

	type commandInfo struct {
		Name    string `json:"name"`
		Help    string `json:"help"`
		Pattern string `json:"pattern"`
	}

	result := make([]commandInfo, 0, len(commands))
	for name, cfg := range commands {
		result = append(result, commandInfo{
			Name:    name,
			Help:    cfg.Help,
			Pattern: cfg.Pattern,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		c.logger.ErrorContext(ctx, "Failed to encode commands list", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleHTTPHealth returns the component health status.
func (c *Component) handleHTTPHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	health := c.Health()

	w.Header().Set("Content-Type", "application/json")
	if !health.Healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if err := json.NewEncoder(w).Encode(health); err != nil {
		c.logger.ErrorContext(ctx, "Failed to encode health status", slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// writeJSONError writes a JSON error response.
func (c *Component) writeJSONError(w http.ResponseWriter, status int, message string) {
	// Log error responses for debugging
	c.logger.Warn("HTTP error response",
		slog.Int("status", status),
		slog.String("message", message))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := HTTPMessageResponse{
		ResponseID: uuid.New().String(),
		Type:       "error",
		Content:    message,
		Timestamp:  time.Now().Format(time.RFC3339),
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.Error("failed to encode error response", slog.String("error", err.Error()))
	}
}

// truncate truncates a string to the given length.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// SignalRequest represents a control signal request for a loop.
type SignalRequest struct {
	Type   string `json:"type"`   // pause, resume, cancel
	Reason string `json:"reason"` // optional reason
}

// SignalResponse represents the response to a signal request.
type SignalResponse struct {
	LoopID    string `json:"loop_id"`
	Signal    string `json:"signal"`
	Accepted  bool   `json:"accepted"`
	Message   string `json:"message,omitempty"`
	Timestamp string `json:"timestamp"`
}

// ApprovalRequest is the body of POST /loops/{id}/approval. Drives
// the beta.19 approval flow from an HTTP caller — the framework's
// approval-response handler subscribes on
// agent.approval_response.<loop_id>; this endpoint marshals the
// payload and publishes there.
//
// UserID is the body-level "claimed" identity, optional. Final
// identity resolves through IdentityFromRequest (ctx > body >
// "http-user" default) so middleware can authenticate it without
// handler edits.
type ApprovalRequest struct {
	Decision          string         `json:"decision"`                     // approve | reject | modify
	ModifiedArguments map[string]any `json:"modified_arguments,omitempty"` // only meaningful for modify
	Reason            string         `json:"reason,omitempty"`             // optional, free text
	UserID            string         `json:"user_id,omitempty"`            // optional; resolves via IdentityFromRequest
}

// ApprovalAcceptResponse is the dispatch HTTP response for a
// successful approval submission. Named distinctly from the
// agentic.ApprovalResponse wire payload to avoid type confusion —
// this struct is the dispatch's HTTP-success envelope, not the
// NATS wire format the framework's loop consumes.
type ApprovalAcceptResponse struct {
	LoopID    string `json:"loop_id"`
	Decision  string `json:"decision"`
	Accepted  bool   `json:"accepted"`
	Message   string `json:"message,omitempty"`
	Timestamp string `json:"timestamp"`
}

// ActivityEvent represents a real-time activity event sent via SSE.
type ActivityEvent struct {
	Type      string          `json:"type"` // loop_created, loop_updated, loop_deleted
	LoopID    string          `json:"loop_id"`
	Timestamp time.Time       `json:"timestamp"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// handleListLoops returns all tracked loops with optional filtering.
func (c *Component) handleListLoops(w http.ResponseWriter, r *http.Request) {
	ctx, requestID := c.withRequestID(w, r)
	startTime := time.Now()

	// Get optional query filters
	userID := r.URL.Query().Get("user_id")
	state := r.URL.Query().Get("state")

	c.logger.DebugContext(ctx, "listing loops",
		slog.String("request_id", requestID),
		slog.String("user_id", userID),
		slog.String("state", state))

	var loops []*LoopInfo
	if userID != "" {
		loops = c.loopTracker.GetUserLoops(userID)
	} else {
		loops = c.loopTracker.GetAllLoops()
	}

	// Apply state filter if specified
	if state != "" {
		filtered := make([]*LoopInfo, 0, len(loops))
		for _, loop := range loops {
			if loop.State == state {
				filtered = append(filtered, loop)
			}
		}
		loops = filtered
	}

	c.metrics.recordHTTPRequest("/loops", "GET", "200")
	c.metrics.recordHTTPDuration("/loops", "GET", time.Since(startTime).Seconds())

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(loops); err != nil {
		c.logger.ErrorContext(ctx, "failed to encode loops list",
			slog.String("request_id", requestID),
			slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleGetLoop returns a single loop by ID.
func (c *Component) handleGetLoop(w http.ResponseWriter, r *http.Request) {
	ctx, requestID := c.withRequestID(w, r)
	startTime := time.Now()

	loopID := r.PathValue("id")
	if loopID == "" {
		c.metrics.recordHTTPRequest("/loops/{id}", "GET", "400")
		c.writeJSONError(w, http.StatusBadRequest, "loop ID is required")
		return
	}

	c.logger.DebugContext(ctx, "getting loop",
		slog.String("request_id", requestID),
		slog.String("loop_id", loopID))

	loop := c.loopTracker.Get(loopID)
	if loop == nil {
		c.metrics.recordHTTPRequest("/loops/{id}", "GET", "404")
		c.metrics.recordHTTPDuration("/loops/{id}", "GET", time.Since(startTime).Seconds())
		c.writeJSONError(w, http.StatusNotFound, "loop not found")
		return
	}

	c.metrics.recordHTTPRequest("/loops/{id}", "GET", "200")
	c.metrics.recordHTTPDuration("/loops/{id}", "GET", time.Since(startTime).Seconds())

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(loop); err != nil {
		c.logger.ErrorContext(ctx, "failed to encode loop",
			slog.String("request_id", requestID),
			slog.String("loop_id", loopID),
			slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleLoopSignal sends a control signal to a loop.
func (c *Component) handleLoopSignal(w http.ResponseWriter, r *http.Request) {
	ctx, requestID := c.withRequestID(w, r)
	startTime := time.Now()

	loopID := r.PathValue("id")
	if loopID == "" {
		c.metrics.recordHTTPRequest("/loops/{id}/signal", "POST", "400")
		c.writeJSONError(w, http.StatusBadRequest, "loop ID is required")
		return
	}

	// Check if loop exists
	loop := c.loopTracker.Get(loopID)
	if loop == nil {
		c.metrics.recordHTTPRequest("/loops/{id}/signal", "POST", "404")
		c.metrics.recordHTTPDuration("/loops/{id}/signal", "POST", time.Since(startTime).Seconds())
		c.writeJSONError(w, http.StatusNotFound, "loop not found")
		return
	}

	// Parse signal request
	var req SignalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.metrics.recordHTTPRequest("/loops/{id}/signal", "POST", "400")
		c.writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Validate signal type
	switch req.Type {
	case "pause", "resume", "cancel":
		// Valid signal types
	default:
		c.metrics.recordHTTPRequest("/loops/{id}/signal", "POST", "400")
		c.writeJSONError(w, http.StatusBadRequest, "invalid signal type: must be pause, resume, or cancel")
		return
	}

	c.logger.DebugContext(ctx, "sending signal to loop",
		slog.String("request_id", requestID),
		slog.String("loop_id", loopID),
		slog.String("signal", req.Type),
		slog.String("reason", req.Reason),
		slog.String("user_id", loop.UserID))

	// Send signal via NATS
	if err := c.loopTracker.SendSignal(ctx, c.natsClient, loopID, req.Type, req.Reason); err != nil {
		c.logger.ErrorContext(ctx, "failed to send signal",
			slog.String("request_id", requestID),
			slog.String("loop_id", loopID),
			slog.String("signal", req.Type),
			slog.String("error", err.Error()))
		c.metrics.recordHTTPRequest("/loops/{id}/signal", "POST", "500")
		c.metrics.recordLoopSignal(req.Type, false)
		c.writeJSONError(w, http.StatusInternalServerError, "failed to send signal: "+err.Error())
		return
	}

	c.metrics.recordHTTPRequest("/loops/{id}/signal", "POST", "200")
	c.metrics.recordHTTPDuration("/loops/{id}/signal", "POST", time.Since(startTime).Seconds())
	c.metrics.recordLoopSignal(req.Type, true)

	c.logger.DebugContext(ctx, "signal sent to loop",
		slog.String("request_id", requestID),
		slog.String("loop_id", loopID),
		slog.String("signal", req.Type))

	// Return success response
	resp := SignalResponse{
		LoopID:    loopID,
		Signal:    req.Type,
		Accepted:  true,
		Message:   fmt.Sprintf("Signal '%s' sent to loop %s", req.Type, loopID),
		Timestamp: time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.ErrorContext(ctx, "failed to encode signal response",
			slog.String("request_id", requestID),
			slog.String("error", err.Error()))
	}
}

// handleLoopApproval drives the beta.19 approval flow over HTTP.
// Mirrors handleLoopSignal's shape: path-param extraction, loop
// existence check, JSON body decode, validation, NATS publish,
// JSON success response. Identity resolves via auth.IdentityFromRequest
// (ctx > body > "http-user" default) so middleware can authenticate
// without handler edits.
//
// The framework's agentic-loop subscribes on
// agent.approval_response.<loop_id>; this handler publishes the
// agentic.ApprovalResponse wire payload there. Concurrent races
// against the same call_id are arbitrated by the loop's atomic
// LoopManager.ResolveApprovalIfPending (beta.19 M1 fix), so dispatch
// just publishes — no locking needed here.
func (c *Component) handleLoopApproval(w http.ResponseWriter, r *http.Request) {
	ctx, requestID := c.withRequestID(w, r)
	startTime := time.Now()

	loopID := r.PathValue("id")
	if loopID == "" {
		c.metrics.recordHTTPRequest("/loops/{id}/approval", "POST", "400")
		c.writeJSONError(w, http.StatusBadRequest, "loop ID is required")
		return
	}

	// Loop must exist in dispatch's tracker. A 404 here means we
	// either never saw the loop (e.g., process restart lost the
	// in-memory tracker before this request) or the loop has been
	// removed.
	if c.loopTracker.Get(loopID) == nil {
		c.metrics.recordHTTPRequest("/loops/{id}/approval", "POST", "404")
		c.metrics.recordHTTPDuration("/loops/{id}/approval", "POST", time.Since(startTime).Seconds())
		c.writeJSONError(w, http.StatusNotFound, "loop not found")
		return
	}

	// Decode body.
	var req ApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.metrics.recordHTTPRequest("/loops/{id}/approval", "POST", "400")
		c.writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Validate decision.
	switch req.Decision {
	case agentic.ApprovalDecisionApprove,
		agentic.ApprovalDecisionReject,
		agentic.ApprovalDecisionModify:
		// Valid.
	default:
		c.metrics.recordHTTPRequest("/loops/{id}/approval", "POST", "400")
		c.writeJSONError(w, http.StatusBadRequest, fmt.Sprintf(
			"invalid decision %q: must be %s, %s, or %s",
			req.Decision,
			agentic.ApprovalDecisionApprove,
			agentic.ApprovalDecisionReject,
			agentic.ApprovalDecisionModify,
		))
		return
	}

	// Atomic CallID snapshot. The previous Get→deref pattern read
	// loop.PendingApproval outside the tracker's lock and races
	// against concurrent SetPendingApproval / UpdateCompletion /
	// ClearPendingApproval mutations. Returns ("", false) when the
	// loop is no longer awaiting approval — the cache divergence
	// case (process restart, race lost, already resolved). 409
	// Conflict is the right REST signal for "resource exists but is
	// in the wrong state for this operation."
	callID, awaiting := c.loopTracker.GetPendingApprovalCallID(loopID)
	if !awaiting {
		c.metrics.recordHTTPRequest("/loops/{id}/approval", "POST", "409")
		c.writeJSONError(w, http.StatusConflict, "loop not awaiting approval")
		return
	}

	approverID := auth.IdentityFromRequest(r, req.UserID)
	// Propagate to ctx so natsclient auto-injects X-Caller-* headers.
	ctx = auth.WithIdentity(ctx, approverID)

	c.logger.DebugContext(ctx, "submitting approval response for loop",
		slog.String("request_id", requestID),
		slog.String("loop_id", loopID),
		slog.String("call_id", callID),
		slog.String("decision", req.Decision),
		slog.String("approved_by", approverID.ID))

	// Build + publish the framework's ApprovalResponse payload.
	subject, err := c.publishApprovalResponse(ctx, loopID, callID, &req, approverID.ID)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to publish approval response",
			slog.String("request_id", requestID),
			slog.String("loop_id", loopID),
			slog.String("error", err.Error()))
		c.metrics.recordHTTPRequest("/loops/{id}/approval", "POST", "500")
		c.metrics.recordLoopApproval(req.Decision, false)
		c.writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Clear the local cache after a successful publish so a fast-
	// follow duplicate HTTP request doesn't re-publish for the same
	// CallID. The framework's ResolveApprovalIfPending arbitrates
	// duplicates atomically anyway, but clearing here saves a NATS
	// round-trip + metric noise.
	c.loopTracker.ClearPendingApproval(loopID)

	c.metrics.recordHTTPRequest("/loops/{id}/approval", "POST", "200")
	c.metrics.recordHTTPDuration("/loops/{id}/approval", "POST", time.Since(startTime).Seconds())
	c.metrics.recordLoopApproval(req.Decision, true)

	c.logger.DebugContext(ctx, "approval response published",
		slog.String("request_id", requestID),
		slog.String("loop_id", loopID),
		slog.String("decision", req.Decision),
		slog.String("subject", subject))

	resp := ApprovalAcceptResponse{
		LoopID:    loopID,
		Decision:  req.Decision,
		Accepted:  true,
		Message:   fmt.Sprintf("Approval '%s' submitted for loop %s", req.Decision, loopID),
		Timestamp: time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		c.logger.ErrorContext(ctx, "failed to encode approval response",
			slog.String("request_id", requestID),
			slog.String("error", err.Error()))
	}
}

// publishApprovalResponse builds the agentic.ApprovalResponse wire
// envelope and publishes it on agent.approval_response.<loop_id>.
// Returns the resolved subject (for logging) and any error from
// marshal or publish. Extracted from handleLoopApproval to keep the
// HTTP handler under revive's function-length budget.
//
// Defensive nil-check on the NATS client mirrors LoopTracker.SendSignal —
// production wiring always has a client, but unit tests construct
// Components with natsClient nil and we surface a clean error rather
// than letting the underlying client.PublishToStream NPE.
func (c *Component) publishApprovalResponse(ctx context.Context, loopID, callID string, req *ApprovalRequest, approver string) (string, error) {
	if c.natsClient == nil {
		return "", ErrNATSClientNil
	}
	response := &agentic.ApprovalResponse{
		LoopID:            loopID,
		CallID:            callID,
		Decision:          req.Decision,
		ModifiedArguments: req.ModifiedArguments,
		Reason:            req.Reason,
		ApprovedBy:        approver,
		DecidedAt:         time.Now().UTC(),
	}
	envelope := message.NewBaseMessage(response.Schema(), response, "agentic-dispatch")
	data, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("marshal approval response: %w", err)
	}

	subject := component.ResolveSubject(c.config.Ports.Outputs, "agent.approval_response", loopID)
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return subject, fmt.Errorf("publish approval response on %s: %w", subject, err)
	}
	return subject, nil
}

// handleActivityStream streams real-time activity events via SSE.
func (c *Component) handleActivityStream(w http.ResponseWriter, r *http.Request) {
	ctx, requestID := c.withRequestID(w, r)
	clientID := r.Header.Get("X-Client-ID")
	if clientID == "" {
		clientID = requestID
	}

	// Setup SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		c.metrics.recordSSEError("streaming_not_supported")
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Get KV bucket for AGENT_LOOPS
	kv, err := c.natsClient.GetKeyValueBucket(ctx, "AGENT_LOOPS")
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to access KV bucket for activity stream",
			slog.String("request_id", requestID),
			slog.String("client_id", clientID),
			slog.String("error", err.Error()))
		c.metrics.recordSSEError("kv_bucket_access")
		c.sendActivityError(w, flusher, "Failed to access AGENT_LOOPS bucket", err)
		return
	}

	// Create watcher for all keys
	watcher, err := kv.WatchAll(ctx)
	if err != nil {
		c.logger.ErrorContext(ctx, "failed to create KV watcher for activity stream",
			slog.String("request_id", requestID),
			slog.String("client_id", clientID),
			slog.String("error", err.Error()))
		c.metrics.recordSSEError("watcher_create")
		c.sendActivityError(w, flusher, "Failed to create watcher", err)
		return
	}
	defer func() {
		if stopErr := watcher.Stop(); stopErr != nil {
			c.logger.WarnContext(ctx, "failed to stop activity watcher",
				slog.String("client_id", clientID),
				slog.String("error", stopErr.Error()))
		}
	}()

	// Track SSE connection
	c.metrics.recordSSEConnect()
	defer c.metrics.recordSSEDisconnect()

	c.logger.InfoContext(ctx, "activity SSE client connected",
		slog.String("request_id", requestID),
		slog.String("client_id", clientID),
		slog.String("remote_addr", r.RemoteAddr))

	// Send initial connected event
	c.sendActivityEvent(w, flusher, "connected", map[string]string{
		"message":   "Watching for activity events",
		"client_id": clientID,
	})
	c.metrics.recordSSEEvent("connected")

	// Send retry directive
	fmt.Fprintf(w, "retry: 5000\n\n")
	flusher.Flush()

	// Heartbeat ticker for connection health
	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	// Stream events
	for {
		select {
		case <-ctx.Done():
			c.logger.InfoContext(ctx, "activity SSE client disconnected",
				slog.String("client_id", clientID),
				slog.String("reason", ctx.Err().Error()))
			return

		case <-heartbeatTicker.C:
			// Send heartbeat comment to keep connection alive and detect stale connections
			fmt.Fprintf(w, ":heartbeat %d\n\n", time.Now().Unix())
			flusher.Flush()
			c.metrics.recordSSEEvent("heartbeat")

		case entry, ok := <-watcher.Updates():
			if !ok {
				c.logger.WarnContext(ctx, "KV watcher closed unexpectedly",
					slog.String("client_id", clientID))
				c.metrics.recordSSEError("watcher_closed")
				c.sendActivityError(w, flusher, "Watcher closed unexpectedly", nil)
				return
			}

			if entry == nil {
				// Initial sync complete
				c.sendActivityEvent(w, flusher, "sync_complete", map[string]string{
					"message": "Initial sync complete",
				})
				c.metrics.recordSSEEvent("sync_complete")
				continue
			}

			// Determine event type from KV operation
			eventType := c.mapKVOperation(entry.Operation(), entry.Revision())

			// Build activity event
			event := ActivityEvent{
				Type:      eventType,
				LoopID:    entry.Key(),
				Timestamp: entry.Created(),
			}

			// Include value for non-delete operations
			if entry.Operation() != jetstream.KeyValueDelete {
				if json.Valid(entry.Value()) {
					event.Data = entry.Value()
				} else {
					event.Data, _ = json.Marshal(string(entry.Value()))
				}
			}

			// Send SSE event
			data, err := json.Marshal(event)
			if err != nil {
				c.logger.ErrorContext(ctx, "failed to marshal activity event",
					slog.String("client_id", clientID),
					slog.String("loop_id", entry.Key()),
					slog.String("error", err.Error()))
				c.metrics.recordSSEError("marshal_event")
				continue
			}

			fmt.Fprintf(w, "event: activity\ndata: %s\n\n", data)
			flusher.Flush()
			c.metrics.recordSSEEvent(eventType)

			c.logger.DebugContext(ctx, "sent activity event",
				slog.String("client_id", clientID),
				slog.String("loop_id", entry.Key()),
				slog.String("event_type", eventType))
		}
	}
}

// mapKVOperation maps a KV operation to an activity event type.
func (c *Component) mapKVOperation(op jetstream.KeyValueOp, revision uint64) string {
	switch op {
	case jetstream.KeyValuePut:
		if revision == 1 {
			return "loop_created"
		}
		return "loop_updated"
	case jetstream.KeyValueDelete:
		return "loop_deleted"
	default:
		return "unknown"
	}
}

// sendActivityEvent sends an SSE event for activity stream.
func (c *Component) sendActivityEvent(w http.ResponseWriter, flusher http.Flusher, event string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		c.logger.Error("Failed to marshal activity event", slog.String("event", event), slog.String("error", err.Error()))
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
	flusher.Flush()
}

// sendActivityError sends an error event via SSE.
func (c *Component) sendActivityError(w http.ResponseWriter, flusher http.Flusher, message string, err error) {
	errorData := map[string]string{"error": message}
	if err != nil {
		errorData["details"] = err.Error()
	}
	c.sendActivityEvent(w, flusher, "error", errorData)
}

// DebugState represents the internal state of the component for debugging.
type DebugState struct {
	Started      bool        `json:"started"`
	StartTime    time.Time   `json:"start_time,omitempty"`
	Uptime       string      `json:"uptime,omitempty"`
	LoopCount    int         `json:"loop_count"`
	CommandCount int         `json:"command_count"`
	Loops        []*LoopInfo `json:"loops"`
	Commands     []string    `json:"commands"`
	Config       DebugConfig `json:"config"`
}

// DebugConfig contains non-sensitive configuration for debugging.
type DebugConfig struct {
	DefaultRole  string `json:"default_role"`
	DefaultModel string `json:"default_model"` // Resolved from model registry
	AutoContinue bool   `json:"auto_continue"`
	StreamName   string `json:"stream_name"`
}

// handleDebugState returns internal component state for debugging.
func (c *Component) handleDebugState(w http.ResponseWriter, r *http.Request) {
	ctx, requestID := c.withRequestID(w, r)

	c.logger.DebugContext(ctx, "debug state requested",
		slog.String("request_id", requestID),
		slog.String("remote_addr", r.RemoteAddr))

	c.mu.RLock()
	started := c.started
	startTime := c.startTime
	c.mu.RUnlock()

	var uptime string
	if started {
		uptime = time.Since(startTime).Round(time.Second).String()
	}

	// Get command names
	commands := c.registry.All()
	commandNames := make([]string, 0, len(commands))
	for name := range commands {
		commandNames = append(commandNames, name)
	}

	state := DebugState{
		Started:      started,
		StartTime:    startTime,
		Uptime:       uptime,
		LoopCount:    c.loopTracker.Count(),
		CommandCount: c.registry.Count(),
		Loops:        c.loopTracker.GetAllLoops(),
		Commands:     commandNames,
		Config: DebugConfig{
			DefaultRole:  c.config.DefaultRole,
			DefaultModel: c.resolveModel(),
			AutoContinue: c.config.AutoContinue,
			StreamName:   c.config.StreamName,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(state); err != nil {
		c.logger.ErrorContext(ctx, "failed to encode debug state",
			slog.String("request_id", requestID),
			slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// agenticDispatchOpenAPISpec returns the OpenAPI specification for agentic-dispatch endpoints.
func agenticDispatchOpenAPISpec() *service.OpenAPISpec {
	return &service.OpenAPISpec{
		Tags: []service.TagSpec{
			{
				Name:        "AgenticDispatch",
				Description: "User message processing and command dispatch",
			},
		},
		Paths: map[string]service.PathSpec{
			"/message": {
				POST: &service.OperationSpec{
					Summary:     "Process a user message",
					Description: "Processes a user message synchronously. Commands (starting with /) are executed immediately. Regular messages are submitted as tasks.",
					Tags:        []string{"AgenticDispatch"},
					RequestBody: &service.RequestBodySpec{
						Description: "User message to process",
						Required:    true,
						SchemaRef:   "#/components/schemas/HTTPMessageRequest",
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Message processed successfully",
							ContentType: "application/json",
						},
						"400": {
							Description: "Invalid request",
						},
					},
				},
			},
			"/commands": {
				GET: &service.OperationSpec{
					Summary:     "List available commands",
					Description: "Returns the list of all registered commands with their descriptions and usage",
					Tags:        []string{"AgenticDispatch"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "List of available commands",
							ContentType: "application/json",
						},
					},
				},
			},
			"/health": {
				GET: &service.OperationSpec{
					Summary:     "Component health check",
					Description: "Returns the health status of the agentic-dispatch component",
					Tags:        []string{"AgenticDispatch"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Component is healthy",
							ContentType: "application/json",
						},
						"503": {
							Description: "Component is unhealthy",
						},
					},
				},
			},
			"/loops": {
				GET: &service.OperationSpec{
					Summary:     "List all tracked loops",
					Description: "Returns all active and recent loops. Supports optional filtering by user_id and state query parameters.",
					Tags:        []string{"AgenticDispatch"},
					Parameters: []service.ParameterSpec{
						{Name: "user_id", In: "query", Description: "Filter by user ID"},
						{Name: "state", In: "query", Description: "Filter by loop state (pending, executing, paused, complete, failed, cancelled)"},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "List of loops",
							ContentType: "application/json",
						},
					},
				},
			},
			"/loops/{id}": {
				GET: &service.OperationSpec{
					Summary:     "Get single loop by ID",
					Description: "Returns detailed information about a specific loop including state, iterations, and metadata.",
					Tags:        []string{"AgenticDispatch"},
					Parameters: []service.ParameterSpec{
						{Name: "id", In: "path", Description: "Loop ID", Required: true},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Loop details",
							ContentType: "application/json",
						},
						"404": {
							Description: "Loop not found",
						},
					},
				},
			},
			"/loops/{id}/signal": {
				POST: &service.OperationSpec{
					Summary:     "Send control signal to loop",
					Description: "Sends a control signal (pause, resume, cancel) to an active loop.",
					Tags:        []string{"AgenticDispatch"},
					RequestBody: &service.RequestBodySpec{
						Description: "Control signal to send",
						Required:    true,
						SchemaRef:   "#/components/schemas/SignalRequest",
					},
					Parameters: []service.ParameterSpec{
						{Name: "id", In: "path", Description: "Loop ID", Required: true},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Signal accepted",
							ContentType: "application/json",
						},
						"400": {
							Description: "Invalid signal type",
						},
						"404": {
							Description: "Loop not found",
						},
					},
				},
			},
			"/loops/{id}/approval": {
				POST: &service.OperationSpec{
					Summary:     "Submit human approval response for a gated tool call",
					Description: "Drives the beta.19 approval flow over HTTP. The loop must be awaiting approval (see config.approval_required). Decision is one of approve, reject, modify; modified_arguments substitutes for the original tool call arguments when decision=modify. Identity comes from X-User-Id-aware middleware via ctx (preferred) or the body user_id field (fallback), defaulting to http-user.",
					Tags:        []string{"AgenticDispatch"},
					RequestBody: &service.RequestBodySpec{
						Description: "Approval decision and optional modifications",
						Required:    true,
						SchemaRef:   "#/components/schemas/ApprovalRequest",
					},
					Parameters: []service.ParameterSpec{
						{Name: "id", In: "path", Description: "Loop ID", Required: true},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Approval submitted",
							ContentType: "application/json",
						},
						"400": {
							Description: "Invalid request body or decision value",
						},
						"404": {
							Description: "Loop not found",
						},
						"409": {
							Description: "Loop exists but is not awaiting approval",
						},
						"500": {
							Description: "Failed to publish approval (NATS error)",
						},
					},
				},
			},
			"/activity": {
				GET: &service.OperationSpec{
					Summary:     "Real-time activity events (SSE)",
					Description: "Server-Sent Events stream of loop activity. Events include loop_created, loop_updated, loop_deleted. Connect with EventSource or curl -N.",
					Tags:        []string{"AgenticDispatch"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "SSE event stream",
							ContentType: "text/event-stream",
						},
					},
				},
			},
			"/debug/state": {
				GET: &service.OperationSpec{
					Summary:     "Internal component state for debugging",
					Description: "Returns internal state including active loops, registered commands, configuration, and uptime. Useful for debugging and monitoring.",
					Tags:        []string{"AgenticDispatch"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "Debug state",
							ContentType: "application/json",
						},
					},
				},
			},
		},
		ResponseTypes: []reflect.Type{
			reflect.TypeOf(LoopInfo{}),
			reflect.TypeOf(HTTPMessageResponse{}),
			reflect.TypeOf(SignalResponse{}),
			reflect.TypeOf(ActivityEvent{}),
			reflect.TypeOf(ApprovalAcceptResponse{}),
		},
		RequestBodyTypes: []reflect.Type{
			reflect.TypeOf(HTTPMessageRequest{}),
			reflect.TypeOf(SignalRequest{}),
			reflect.TypeOf(ApprovalRequest{}),
		},
	}
}
