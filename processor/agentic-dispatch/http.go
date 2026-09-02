package agenticdispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
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

	// Resumable-reply anchors (gh#256). Distinct from ReplyTo (which routes to
	// a loop to continue): these let a reply re-enter and resume a paused run.
	// RunID is the bare run anchor the resumed loop re-attaches to; InReplyTo
	// marks the message as a reply to a specific loop's question so a rule can
	// fire on the resumed loop. Both optional; absent for ordinary submissions.
	RunID     string `json:"run_id,omitempty"`
	InReplyTo string `json:"in_reply_to,omitempty"`
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

	// Resolve identity through the IdentityFromRequest helper so a
	// future HTTP middleware contract (ADR-030) can populate
	// authenticated identity via ctx and we'll pick it up here
	// without rewriting the handler. Today, no middleware sets ctx,
	// so this resolves to req.UserID || "http-user" — behavior-
	// equivalent to the prior inline default.
	req.UserID = IdentityFromRequest(r, req.UserID)
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
		RunID:       req.RunID,
		InReplyTo:   req.InReplyTo,
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

// refusedSubmissionResponse is the HTTP submission lane's typed answer to a
// refusal. The content is the refusal's own message, which names the field the
// caller can act on; the previous "Please try again." named nothing and was
// wrong about the remedy for half the failures it covered (#1225).
//
// It never counts anything: the refusal it is handed was already metered and
// logged exactly once, where it was built.
func refusedSubmissionResponse(msg agentic.UserMessage, refusal error) agentic.UserResponse {
	return agentic.UserResponse{
		ResponseID:  uuid.New().String(),
		ChannelType: msg.ChannelType,
		ChannelID:   msg.ChannelID,
		UserID:      msg.UserID,
		Type:        agentic.ResponseTypeError,
		Content:     refusal.Error(),
		Timestamp:   time.Now(),
	}
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

	// Determine loop ID (continue existing or create new). The mint decision is
	// made HERE, before the gate: an unresolved continuation is the signal to
	// start a conversation, not a malformed token, and the gate refuses an empty
	// token as malformed.
	loopID := ""
	if msg.ReplyTo != "" {
		loopID = msg.ReplyTo
	} else if c.config.AutoContinue {
		loopID = c.loopTracker.GetActiveLoop(msg.UserID, msg.ChannelID)
	}

	if loopID == "" {
		// Create new loop. The token is framework-minted and full: a truncated
		// one carried 32 bits, and a collision merged two conversations silently
		// (ADR-105, #1192).
		loopID = uuid.New().String()
	} else if _, err := c.admitLoopRequest(ctx, loopAdmissionRequest{
		Seam:      seamHTTPSubmission,
		Field:     "reply_to",
		Operation: loopOpContinue,
		LoopID:    loopID,
		Requester: msg.UserID,
	}); err != nil {
		// The client hears about it here, synchronously, in the response it is
		// already waiting on, naming the field — rather than "Task submitted"
		// followed by an async TERM it never sees (ADR-105, #1192).
		return refusedSubmissionResponse(msg, err)
	}

	taskID := uuid.New().String()

	// Create task message (shared builder — see buildTaskMessage; gh#256).
	task := c.buildTaskMessage(ctx, msg, loopID, taskID)

	// Wrap task in BaseMessage envelope (required by agentic-loop). The marshal
	// is where TaskMessage.Validate runs, so it is the last thing that can
	// refuse this submission on its own content — including the client-authored
	// run_id / in_reply_to resume anchors, which never pass through the
	// continuation branch above. Nothing is tracked or counted until it returns
	// (#1225).
	baseMsg := message.NewBaseMessage(task.Schema(), &task, "agentic-dispatch-http")
	taskData, err := json.Marshal(baseMsg)
	if err != nil {
		return refusedSubmissionResponse(msg,
			c.refuseSubmission(seamHTTPSubmission, loopID, codeSubmissionInvalid, err))
	}

	subject, err := component.ResolveSubject(c.outputPortDefs(), "agent.task", taskID)
	if err != nil {
		return refusedSubmissionResponse(msg,
			c.refuseSubmission(seamHTTPSubmission, loopID, codeSubmissionUndeliverable, err))
	}

	// Track the loop and count it started — after the task is assembled and
	// addressable, before the publish. See the channel path for why this window
	// is the safe one (#1225).
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
	c.metrics.recordLoopStarted()

	if err := c.natsClient.PublishToStream(ctx, subject, taskData); err != nil {
		return refusedSubmissionResponse(msg,
			c.refuseSubmission(seamHTTPSubmission, loopID, codeSubmissionUndeliverable, err))
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
// Data is a *Loop projected from the AGENT_LOOPS KV entry. See the Loop schema
// for the full field set; fields absent from a given source stay empty.
// (OpenAPI 3.0 cannot express per-event SSE JSON schema — see the Loop and
// ActivityEvent component schemas for the documented shapes.)
//
// Type values:
//   - "loop_created"   — new live loop entry (non-COMPLETE_ key, revision 1)
//   - "loop_updated"   — live loop updated (non-COMPLETE_ key, revision > 1)
//   - "loop_deleted"   — KV entry deleted
//   - "loop_completed" — terminal event (COMPLETE_<id> key); LoopID is the bare
//     loop ID (prefix stripped) so it equals data.loop_id. The primary signal for
//     terminal-ness is event.type == "loop_completed". When present, data.outcome
//     carries the verdict: "success", "failed", or "cancelled". Note: data.state
//     is NOT populated on terminal events — production terminal payloads
//     (LoopCompletedEvent, LoopFailedEvent, LoopCancelledEvent) have no state field.
type ActivityEvent struct {
	Type      string    `json:"type"` // loop_created, loop_updated, loop_deleted, loop_completed
	LoopID    string    `json:"loop_id"`
	Timestamp time.Time `json:"timestamp"`
	Data      *Loop     `json:"data,omitempty"`
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

	// Project to the canonical wire type before encoding.
	wireLoops := make([]Loop, len(loops))
	for i, l := range loops {
		wireLoops[i] = loopFromInfo(l)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(wireLoops); err != nil {
		c.logger.ErrorContext(ctx, "failed to encode loops list",
			slog.String("request_id", requestID),
			slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// answerLoopRefusal writes an admission refusal as this endpoint's answer and
// records the request under the status it chose. The status mapping has one
// home (loopRefusalHTTPStatus) so three endpoints cannot disagree about what
// "not owned" answers; an error this package did not classify is a bug here,
// not a caller problem, and answers 500.
//
// It counts no refusal of its own — the gate already metered and logged it
// exactly once. The HTTP series it moves is the endpoint's existing
// request-by-status counter, which is why the new statuses need no new series.
func (c *Component) answerLoopRefusal(w http.ResponseWriter, path, method string, start time.Time, refusal error) {
	status, ok := loopRefusalHTTPStatus(refusal)
	if !ok {
		status = http.StatusInternalServerError
	}
	c.metrics.recordHTTPRequest(path, method, strconv.Itoa(status))
	c.metrics.recordHTTPDuration(path, method, time.Since(start).Seconds())
	c.writeJSONError(w, status, refusal.Error())
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

	// Form and existence, through the one gate. Ownership is deliberately NOT
	// consulted on a read — that carve-out is recorded in the capability spec's
	// ungated-seam list, not left for a reader to infer from its absence here.
	if _, err := c.admitLoopRequest(ctx, loopAdmissionRequest{
		Seam:      seamHTTPLoopRead,
		Field:     "id",
		Operation: loopOpRead,
		LoopID:    loopID,
		Requester: IdentityFromRequest(r, ""),
	}); err != nil {
		c.answerLoopRefusal(w, "/loops/{id}", "GET", startTime, err)
		return
	}

	// Existence is merged, so an admitted loop may live only in the durable
	// record. Answering 404 here would contradict the admission that just
	// succeeded, so the durable record is projected onto the same wire type.
	wireLoop, ok := c.loopWireByID(ctx, loopID)
	if !ok {
		c.metrics.recordHTTPRequest("/loops/{id}", "GET", "503")
		c.metrics.recordHTTPDuration("/loops/{id}", "GET", time.Since(startTime).Seconds())
		c.writeJSONError(w, http.StatusServiceUnavailable, "loop record is not readable right now")
		return
	}

	c.metrics.recordHTTPRequest("/loops/{id}", "GET", "200")
	c.metrics.recordHTTPDuration("/loops/{id}", "GET", time.Since(startTime).Seconds())

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(wireLoop); err != nil {
		c.logger.ErrorContext(ctx, "failed to encode loop",
			slog.String("request_id", requestID),
			slog.String("loop_id", loopID),
			slog.String("error", err.Error()))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// loopWireByID projects an ADMITTED loop onto the canonical wire type. The
// tracker is preferred because it is the live record; a loop the gate admitted
// from the durable record alone is re-read and projected from there. ok=false
// means the record vanished between admission and this read — rare, and
// answered as transient rather than as absence, because absence was already
// ruled out.
func (c *Component) loopWireByID(ctx context.Context, loopID string) (Loop, bool) {
	if tracked := c.loopTracker.Get(loopID); tracked != nil {
		return loopFromInfo(tracked), true
	}
	persisted, err := c.loadPersistedLoop(ctx, loopID)
	if err != nil || persisted == nil {
		return Loop{}, false
	}
	return loopFromEntity(persisted, c.deps.Platform.Org, c.deps.Platform.Platform), true
}

// handleLoopApproval drives the beta.19 approval flow over HTTP.
// Path-param extraction, gate admission, JSON body decode,
// validation, NATS publish, JSON success response. Identity resolves
// via IdentityFromRequest
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

	// Decode body BEFORE the gate: the approve permission is checked against
	// the resolved identity, and the body is one of the three places that
	// identity can come from (IdentityFromRequest: ctx > body > default).
	var req ApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		c.metrics.recordHTTPRequest("/loops/{id}/approval", "POST", "400")
		c.writeJSONError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	approver := IdentityFromRequest(r, req.UserID)

	// Form, existence, and the approve permission, through the one gate.
	// Ownership is deliberately NOT consulted: a second-party reviewer is the
	// entire point of an approval. The permission's default admits everyone, so
	// no default deployment changes behaviour.
	if _, err := c.admitLoopRequest(ctx, loopAdmissionRequest{
		Seam:      seamHTTPLoopApproval,
		Field:     "id",
		Operation: loopOpApprove,
		LoopID:    loopID,
		Requester: approver,
	}); err != nil {
		c.answerLoopRefusal(w, "/loops/{id}/approval", "POST", startTime, err)
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

	c.logger.DebugContext(ctx, "submitting approval response for loop",
		slog.String("request_id", requestID),
		slog.String("loop_id", loopID),
		slog.String("call_id", callID),
		slog.String("decision", req.Decision),
		slog.String("approved_by", approver))

	// Build + publish the framework's ApprovalResponse payload.
	subject, err := c.publishApprovalResponse(ctx, loopID, callID, &req, approver)
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
// Defensive nil-check on the NATS client: production wiring always has
// a client, but unit tests construct
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

	subject, err := component.ResolveSubject(c.config.Ports.Outputs, "agent.approval_response", loopID)
	if err != nil {
		return "", fmt.Errorf("resolve approval response subject: %w", err)
	}
	if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return subject, fmt.Errorf("publish approval response on %s: %w", subject, err)
	}
	return subject, nil
}

// handleActivityStream lives in http_activity.go: it streams the /activity
// SSE wire from the component's shared AGENT_LOOPS graph view (ADR-081)
// instead of a per-client kv.WatchAll.

// activityEventTypeAndID derives the wire event type and bare loop ID from a
// raw AGENT_LOOPS KV key, operation, and revision. It is the single source of
// truth for the isCompletion / bareLoopID / eventType decision that
// handleActivityStream emits onto the /activity SSE wire.
//
// For COMPLETE_<id> keys the prefix is stripped so the returned loopID matches
// data.loop_id; eventType is always "loop_completed" for those keys.
// For non-terminal keys, eventType derives from op/revision: put at revision 1
// is loop_created, later puts are loop_updated, deletes are loop_deleted.
func activityEventTypeAndID(key string, op jetstream.KeyValueOp, revision uint64) (eventType, loopID string) {
	if strings.HasPrefix(key, completeKeyPrefix) {
		return "loop_completed", strings.TrimPrefix(key, completeKeyPrefix)
	}
	switch op {
	case jetstream.KeyValuePut:
		if revision == 1 {
			return "loop_created", key
		}
		return "loop_updated", key
	case jetstream.KeyValueDelete:
		return "loop_deleted", key
	default:
		return "unknown", key
	}
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
							SchemaRef:   "#/components/schemas/Loop",
							IsArray:     true,
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
							SchemaRef:   "#/components/schemas/Loop",
						},
						"400": {
							Description: "Loop ID is missing or is not a framework-minted loop token",
						},
						"404": {
							Description: "Loop not found",
						},
						"503": {
							Description: "Loop state is not readable right now; retry",
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
							Description: "Invalid request body or decision value, or a loop ID that is not a framework-minted loop token",
						},
						"403": {
							Description: "Requester is not in the approve permission list (default admits everyone)",
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
						"503": {
							Description: "Loop state is not readable right now; retry",
						},
					},
				},
			},
			"/activity": {
				GET: &service.OperationSpec{
					Summary:     "Real-time activity events (SSE)",
					Description: "Server-Sent Events stream of loop activity. Event types: loop_created, loop_updated, loop_deleted, loop_completed. loop_completed fires when a COMPLETE_<id> KV key is written; the envelope loop_id is the bare id (prefix stripped) matching data.loop_id — use event.type==\"loop_completed\" to detect terminal entries. When type is loop_completed, data.outcome carries the verdict (\"success\", \"failed\", or \"cancelled\"); data.state is NOT populated on terminal events. Each event's data field is an ActivityEvent whose data field is a Loop (see #/components/schemas/Loop and #/components/schemas/ActivityEvent). Connect with EventSource or curl -N. Note: OpenAPI 3.0 cannot express per-event SSE JSON schema; consult the ActivityEvent and Loop component schemas.",
					Tags:        []string{"AgenticDispatch"},
					Responses: map[string]service.ResponseSpec{
						"200": {
							Description: "SSE event stream of ActivityEvent objects. Each event's data field is a Loop (see #/components/schemas/ActivityEvent and #/components/schemas/Loop).",
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
			reflect.TypeOf(Loop{}),
			reflect.TypeOf(LoopInfo{}),
			reflect.TypeOf(HTTPMessageResponse{}),
			reflect.TypeOf(ActivityEvent{}),
			reflect.TypeOf(ApprovalAcceptResponse{}),
		},
		RequestBodyTypes: []reflect.Type{
			reflect.TypeOf(HTTPMessageRequest{}),
			reflect.TypeOf(ApprovalRequest{}),
		},
	}
}
