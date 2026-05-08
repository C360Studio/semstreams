package githubwebhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

// Compile-time interface assertions.
var (
	_ component.LifecycleComponent = (*Input)(nil)
	_ component.Discoverable       = (*Input)(nil)
)

// Input implements the GitHub webhook HTTP receiver component.
type Input struct {
	name       string
	config     Config
	natsClient *natsclient.Client
	logger     *slog.Logger

	// webhookSecret is loaded from the environment variable named by
	// Config.WebhookSecretEnv during Initialize(). Empty means no HMAC
	// validation is performed.
	webhookSecret string

	// outputSubjects maps port names to their resolved NATS subjects.
	outputSubjects map[string]string

	// httpServer is the webhook listener; non-nil only after Start().
	httpServer *http.Server

	// Lifecycle state.
	started     atomic.Bool
	startTime   time.Time
	lifecycleMu sync.Mutex
	cancel      context.CancelFunc

	// Counters (accessed atomically).
	messagesReceived  int64
	messagesPublished int64
	errorCount        atomic.Int64
}

// NewInput creates a validated Input from the supplied configuration and NATS client.
func NewInput(name string, natsClient *natsclient.Client, cfg Config, logger *slog.Logger) (*Input, error) {
	if natsClient == nil {
		return nil, errs.WrapInvalid(
			fmt.Errorf("NATS client is required"),
			"github_webhook", "NewInput", "dependency validation",
		)
	}
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 8090
	}
	if cfg.Path == "" {
		cfg.Path = "/github/webhook"
	}
	if len(cfg.EventFilter) == 0 {
		cfg.EventFilter = defaultEventFilter
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Resolve output subjects from port config. The publish call sites
	// (dispatch func) index this map by the canonical port names below;
	// an operator-renamed port (e.g. Name:"issues" instead of
	// "github.event.issue") would silently land in the map under the
	// rename while publish lookups by canonical key returned the default
	// subject — events publish to the default while operator's intended
	// subscribers see nothing. Validate up front and fail loud on
	// unknown names. Audit finding 2026-05-08.
	subjects := map[string]string{
		"github.event.issue":   "github.event.issue",
		"github.event.pr":      "github.event.pr",
		"github.event.review":  "github.event.review",
		"github.event.comment": "github.event.comment",
	}
	if cfg.Ports != nil {
		for _, p := range cfg.Ports.Outputs {
			if _, canonical := subjects[p.Name]; !canonical {
				return nil, errs.WrapInvalid(
					fmt.Errorf("unknown output port name %q (must be one of: github.event.issue, github.event.pr, github.event.review, github.event.comment)", p.Name),
					"github_webhook", "NewInput", "port name validation",
				)
			}
			if p.Subject != "" {
				subjects[p.Name] = p.Subject
			}
		}
	}

	return &Input{
		name:           name,
		config:         cfg,
		natsClient:     natsClient,
		logger:         logger.With("component", name),
		outputSubjects: subjects,
	}, nil
}

// log returns the logger, falling back to slog.Default() when the component
// was constructed without a logger (e.g. in tests).
func (i *Input) log() *slog.Logger {
	if i.logger != nil {
		return i.logger
	}
	return slog.Default()
}

// ---- component.Discoverable -------------------------------------------------

// Meta returns component metadata.
func (i *Input) Meta() component.Metadata {
	return component.Metadata{
		Name:        i.name,
		Type:        "input",
		Description: "GitHub webhook receiver for issue and PR events",
		Version:     "1.0.0",
	}
}

// InputPorts returns the input ports.
// The HTTP listener is the entry point, represented as a NetworkPort.
func (i *Input) InputPorts() []component.Port {
	return []component.Port{
		{
			Name:      "http",
			Direction: component.DirectionInput,
			Required:  true,
			Description: fmt.Sprintf("HTTP webhook listener on :%d%s",
				i.config.HTTPPort, i.config.Path),
			Config: component.NetworkPort{
				Protocol: "tcp",
				Host:     "0.0.0.0",
				Port:     i.config.HTTPPort,
			},
		},
	}
}

// OutputPorts returns the four JetStream output ports, one per event category.
func (i *Input) OutputPorts() []component.Port {
	return []component.Port{
		{
			Name:        "github.event.issue",
			Direction:   component.DirectionOutput,
			Required:    false,
			Description: "GitHub issue webhook events",
			Config: component.JetStreamPort{
				StreamName: "GITHUB",
				Subjects:   []string{i.outputSubjects["github.event.issue"]},
			},
		},
		{
			Name:        "github.event.pr",
			Direction:   component.DirectionOutput,
			Required:    false,
			Description: "GitHub pull-request webhook events",
			Config: component.JetStreamPort{
				StreamName: "GITHUB",
				Subjects:   []string{i.outputSubjects["github.event.pr"]},
			},
		},
		{
			Name:        "github.event.review",
			Direction:   component.DirectionOutput,
			Required:    false,
			Description: "GitHub pull-request review webhook events",
			Config: component.JetStreamPort{
				StreamName: "GITHUB",
				Subjects:   []string{i.outputSubjects["github.event.review"]},
			},
		},
		{
			Name:        "github.event.comment",
			Direction:   component.DirectionOutput,
			Required:    false,
			Description: "GitHub issue comment webhook events",
			Config: component.JetStreamPort{
				StreamName: "GITHUB",
				Subjects:   []string{i.outputSubjects["github.event.comment"]},
			},
		},
	}
}

// ConfigSchema returns the configuration schema.
func (i *Input) ConfigSchema() component.ConfigSchema {
	return githubWebhookSchema
}

// Health returns the current health status.
func (i *Input) Health() component.HealthStatus {
	started := i.started.Load()
	uptime := time.Duration(0)
	if started && !i.startTime.IsZero() {
		uptime = time.Since(i.startTime)
	}
	return component.HealthStatus{
		Healthy:    started,
		LastCheck:  time.Now(),
		ErrorCount: int(i.errorCount.Load()),
		Uptime:     uptime,
	}
}

// DataFlow returns current throughput metrics.
func (i *Input) DataFlow() component.FlowMetrics {
	msgs := atomic.LoadInt64(&i.messagesReceived)
	var rate float64
	if !i.startTime.IsZero() {
		secs := time.Since(i.startTime).Seconds()
		if secs > 0 {
			rate = float64(msgs) / secs
		}
	}
	return component.FlowMetrics{
		MessagesPerSecond: rate,
	}
}

// ---- component.LifecycleComponent -------------------------------------------

// Initialize validates the configuration and pre-loads the webhook HMAC secret
// from the environment so that secret retrieval failures surface early.
func (i *Input) Initialize() error {
	if err := component.ValidatePortNumber(i.config.HTTPPort); err != nil {
		return errs.Wrap(err, "github_webhook", "Initialize", "validate http_port")
	}
	if i.config.Path == "" {
		return errs.WrapInvalid(
			fmt.Errorf("path must not be empty"),
			"github_webhook", "Initialize", "validate path",
		)
	}

	// Load HMAC secret if an env-var name was configured.
	if i.config.WebhookSecretEnv != "" {
		secret := os.Getenv(i.config.WebhookSecretEnv)
		if secret == "" {
			i.log().Warn("webhook secret env var is set but empty; signature validation disabled",
				"env_var", i.config.WebhookSecretEnv)
		}
		i.webhookSecret = secret
	}

	return nil
}

// Start launches the HTTP server and begins accepting webhook deliveries.
func (i *Input) Start(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "github_webhook", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "github_webhook", "Start", "context already cancelled")
	}

	i.lifecycleMu.Lock()
	defer i.lifecycleMu.Unlock()

	if i.started.Load() {
		return errs.WrapFatal(
			fmt.Errorf("component already started"),
			"github_webhook", "Start", "check started state",
		)
	}

	compCtx, cancel := context.WithCancel(ctx)
	i.cancel = cancel

	mux := http.NewServeMux()
	mux.HandleFunc(i.config.Path, func(w http.ResponseWriter, r *http.Request) {
		i.handleWebhook(compCtx, w, r)
	})

	i.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", i.config.HTTPPort),
		Handler: mux,
	}

	go func() {
		if err := i.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			i.errorCount.Add(1)
			i.log().Error("webhook HTTP server error", "error", err)
		}
	}()

	i.startTime = time.Now()
	i.started.Store(true)
	i.log().Info("github webhook input started",
		"port", i.config.HTTPPort, "path", i.config.Path)
	return nil
}

// Stop performs a graceful HTTP server shutdown within the supplied timeout.
func (i *Input) Stop(timeout time.Duration) error {
	i.lifecycleMu.Lock()
	defer i.lifecycleMu.Unlock()

	if !i.started.Load() {
		return nil
	}

	if i.cancel != nil {
		i.cancel()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), timeout)
	defer shutdownCancel()

	var shutdownErr error
	if i.httpServer != nil {
		shutdownErr = i.httpServer.Shutdown(shutdownCtx)
	}

	i.started.Store(false)
	i.log().Info("github webhook input stopped")

	if shutdownErr != nil {
		return errs.WrapTransient(shutdownErr, "github_webhook", "Stop", "http server shutdown")
	}
	return nil
}

// ---- HTTP handler -----------------------------------------------------------

// handleWebhook validates and dispatches a single webhook delivery.
func (i *Input) handleWebhook(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body first; it is needed for both HMAC validation and parsing.
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10 MiB guard
	if err != nil {
		i.errorCount.Add(1)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Validate HMAC signature when a secret is configured.
	if i.webhookSecret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if !validateSignature(sig, body, i.webhookSecret) {
			i.errorCount.Add(1)
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}
	}

	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		http.Error(w, "missing X-GitHub-Event header", http.StatusBadRequest)
		return
	}

	// Check event type filter.
	if !i.acceptsEventType(eventType) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("event type filtered"))
		return
	}

	atomic.AddInt64(&i.messagesReceived, 1)

	// Parse the raw GitHub payload into a minimal intermediate map so we can
	// extract common fields before dispatching to the typed parsers.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		i.errorCount.Add(1)
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Extract the shared header fields.
	header, err := extractEventHeader(eventType, raw)
	if err != nil {
		i.errorCount.Add(1)
		http.Error(w, "failed to parse event header", http.StatusBadRequest)
		return
	}

	// Apply repo allowlist.
	if !i.repoAllowed(header.Repository.FullName) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("repository filtered"))
		return
	}

	// Dispatch by event type.
	if err := i.dispatch(ctx, header, eventType, raw); err != nil {
		i.errorCount.Add(1)
		i.log().Error("failed to dispatch webhook event",
			"event_type", eventType, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	atomic.AddInt64(&i.messagesPublished, 1)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("accepted"))
}

// dispatch serialises the typed event and publishes it to the correct subject.
func (i *Input) dispatch(ctx context.Context, header WebhookEvent, eventType string, raw map[string]json.RawMessage) error {
	switch eventType {
	case "issues":
		evt, err := parseIssueEvent(header, raw)
		if err != nil {
			return fmt.Errorf("parse issue event: %w", err)
		}
		return i.publish(ctx, i.outputSubjects["github.event.issue"], evt)

	case "pull_request":
		evt, err := parsePREvent(header, raw)
		if err != nil {
			return fmt.Errorf("parse pull_request event: %w", err)
		}
		return i.publish(ctx, i.outputSubjects["github.event.pr"], evt)

	case "pull_request_review":
		evt, err := parseReviewEvent(header, raw)
		if err != nil {
			return fmt.Errorf("parse pull_request_review event: %w", err)
		}
		return i.publish(ctx, i.outputSubjects["github.event.review"], evt)

	case "issue_comment":
		evt, err := parseCommentEvent(header, raw)
		if err != nil {
			return fmt.Errorf("parse issue_comment event: %w", err)
		}
		return i.publish(ctx, i.outputSubjects["github.event.comment"], evt)

	default:
		// Already filtered above; this branch should not be reached.
		return nil
	}
}

// publish serialises v and publishes it to NATS JetStream.
func (i *Input) publish(ctx context.Context, subject string, v any) error {
	if i.natsClient == nil {
		return fmt.Errorf("NATS client not initialised")
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	if err := i.natsClient.PublishToStream(ctx, subject, data); err != nil {
		return fmt.Errorf("publish to %s: %w", subject, err)
	}
	return nil
}

// ---- Filtering helpers ------------------------------------------------------

// acceptsEventType returns true when eventType is present in the configured
// event filter (case-sensitive, matching GitHub header values).
func (i *Input) acceptsEventType(eventType string) bool {
	for _, f := range i.config.EventFilter {
		if f == eventType {
			return true
		}
	}
	return false
}

// repoAllowed returns true when either no allowlist is configured, or the
// supplied fullName (owner/repo) is explicitly listed.
func (i *Input) repoAllowed(fullName string) bool {
	if len(i.config.RepoAllowlist) == 0 {
		return true
	}
	for _, allowed := range i.config.RepoAllowlist {
		if allowed == fullName {
			return true
		}
	}
	return false
}

// ---- HMAC validation --------------------------------------------------------

// validateSignature verifies the X-Hub-Signature-256 header produced by GitHub.
// The header format is "sha256=<hex>". Returns false for any malformed or
// mismatching signature.
func validateSignature(header string, body []byte, secret string) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	return hmac.Equal(got, expected)
}

// ---- Raw JSON parsing helpers -----------------------------------------------

// rawString safely extracts a string value from a raw JSON map.
// Returns "" when the key is absent or the value is not a JSON string.
func rawString(raw map[string]json.RawMessage, key string) string {
	v, ok := raw[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return ""
	}
	return s
}

// rawInt safely extracts an integer value from a raw JSON map.
func rawInt(raw map[string]json.RawMessage, key string) int {
	v, ok := raw[key]
	if !ok {
		return 0
	}
	var n int
	if err := json.Unmarshal(v, &n); err != nil {
		return 0
	}
	return n
}

// rawLoginFromObject extracts the "login" field from a nested GitHub user object.
// GitHub represents users as {"login":"...", ...}.
func rawLoginFromObject(raw map[string]json.RawMessage, key string) string {
	v, ok := raw[key]
	if !ok {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(v, &obj); err != nil {
		return ""
	}
	return rawString(obj, "login")
}

// rawLabelsFromArray extracts label names from a GitHub labels array.
// Each element is an object with a "name" field: [{"name":"bug",...}, ...].
func rawLabelsFromArray(raw map[string]json.RawMessage, key string) []string {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	var labelObjs []map[string]json.RawMessage
	if err := json.Unmarshal(v, &labelObjs); err != nil {
		return nil
	}
	names := make([]string, 0, len(labelObjs))
	for _, obj := range labelObjs {
		if name := rawString(obj, "name"); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// rawRefName extracts the branch name from a GitHub ref object.
// GitHub represents refs as {"ref":"main","sha":"...","repo":{...}}.
func rawRefName(raw map[string]json.RawMessage, key string) string {
	v, ok := raw[key]
	if !ok {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(v, &obj); err != nil {
		return ""
	}
	return rawString(obj, "ref")
}

// extractEventHeader builds the shared WebhookEvent from a raw payload map.
func extractEventHeader(eventType string, raw map[string]json.RawMessage) (WebhookEvent, error) {
	action := rawString(raw, "action")
	sender := rawLoginFromObject(raw, "sender")

	var repo Repository
	if v, ok := raw["repository"]; ok {
		var repoRaw map[string]json.RawMessage
		if err := json.Unmarshal(v, &repoRaw); err != nil {
			return WebhookEvent{}, fmt.Errorf("parse repository: %w", err)
		}
		fullName := rawString(repoRaw, "full_name")
		parts := strings.SplitN(fullName, "/", 2)
		if len(parts) == 2 {
			repo.Owner = parts[0]
			repo.Name = parts[1]
		}
		repo.FullName = fullName
	}

	return WebhookEvent{
		EventType:  eventType,
		Action:     action,
		Repository: repo,
		Sender:     sender,
		ReceivedAt: time.Now().UTC(),
	}, nil
}

// parseIssueEvent builds an IssueEvent from the raw payload.
func parseIssueEvent(header WebhookEvent, raw map[string]json.RawMessage) (*IssueEvent, error) {
	var issueRaw map[string]json.RawMessage
	if v, ok := raw["issue"]; ok {
		if err := json.Unmarshal(v, &issueRaw); err != nil {
			return nil, fmt.Errorf("parse issue object: %w", err)
		}
	}

	return &IssueEvent{
		WebhookEvent: header,
		Issue: IssuePayload{
			Number:  rawInt(issueRaw, "number"),
			Title:   rawString(issueRaw, "title"),
			Body:    rawString(issueRaw, "body"),
			State:   rawString(issueRaw, "state"),
			Labels:  rawLabelsFromArray(issueRaw, "labels"),
			Author:  rawLoginFromObject(issueRaw, "user"),
			HTMLURL: rawString(issueRaw, "html_url"),
		},
	}, nil
}

// parsePREvent builds a PREvent from the raw payload.
func parsePREvent(header WebhookEvent, raw map[string]json.RawMessage) (*PREvent, error) {
	prPayload, err := extractPRPayload(raw)
	if err != nil {
		return nil, err
	}
	return &PREvent{
		WebhookEvent: header,
		PullRequest:  prPayload,
	}, nil
}

// parseReviewEvent builds a ReviewEvent from the raw payload.
func parseReviewEvent(header WebhookEvent, raw map[string]json.RawMessage) (*ReviewEvent, error) {
	prPayload, err := extractPRPayload(raw)
	if err != nil {
		return nil, err
	}

	var reviewRaw map[string]json.RawMessage
	if v, ok := raw["review"]; ok {
		if err := json.Unmarshal(v, &reviewRaw); err != nil {
			return nil, fmt.Errorf("parse review object: %w", err)
		}
	}

	return &ReviewEvent{
		WebhookEvent: header,
		PullRequest:  prPayload,
		Review: ReviewPayload{
			State: rawString(reviewRaw, "state"),
			Body:  rawString(reviewRaw, "body"),
		},
	}, nil
}

// parseCommentEvent builds a CommentEvent from the raw payload.
func parseCommentEvent(header WebhookEvent, raw map[string]json.RawMessage) (*CommentEvent, error) {
	var commentRaw map[string]json.RawMessage
	if v, ok := raw["comment"]; ok {
		if err := json.Unmarshal(v, &commentRaw); err != nil {
			return nil, fmt.Errorf("parse comment object: %w", err)
		}
	}

	return &CommentEvent{
		WebhookEvent: header,
		Comment: CommentPayload{
			Body:   rawString(commentRaw, "body"),
			Author: rawLoginFromObject(commentRaw, "user"),
		},
	}, nil
}

// extractPRPayload is shared by parsePREvent and parseReviewEvent.
func extractPRPayload(raw map[string]json.RawMessage) (PRPayload, error) {
	var prRaw map[string]json.RawMessage
	if v, ok := raw["pull_request"]; ok {
		if err := json.Unmarshal(v, &prRaw); err != nil {
			return PRPayload{}, fmt.Errorf("parse pull_request object: %w", err)
		}
	}

	return PRPayload{
		Number:  rawInt(prRaw, "number"),
		Title:   rawString(prRaw, "title"),
		Body:    rawString(prRaw, "body"),
		State:   rawString(prRaw, "state"),
		Head:    rawRefName(prRaw, "head"),
		Base:    rawRefName(prRaw, "base"),
		HTMLURL: rawString(prRaw, "html_url"),
	}, nil
}
