package githubprworkflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/cache"
)

const (
	// WorkflowSlug identifies this workflow for correlation in TaskMessage/LoopCompletedEvent.
	WorkflowSlug = "github-issue-to-pr"

	componentName    = "pr-workflow-spawner"
	componentVersion = "0.1.0"
)

// workflowState tracks per-entity accumulated counters derived from entity triples
// in ENTITY_STATES. Cached locally via TTL cache for hot-path reads; the graph
// (via writeTriple) is the durable source of truth.
type workflowState struct {
	TotalTokens int
	Rejections  int
}

// consumerInfo tracks a JetStream consumer for clean shutdown.
type consumerInfo struct {
	streamName   string
	consumerName string
}

// PRWorkflowComponent spawns agent tasks for the GitHub issue-to-PR pipeline.
// It handles the Go-intensive parts of the workflow: prompt building, result parsing,
// and triple writing. The simpler reactive patterns (phase transitions, budget checks)
// are handled by JSON rules in configs/rules/github-pr-workflow/.
type PRWorkflowComponent struct {
	config     Config
	decoder    *message.Decoder
	natsClient *natsclient.Client
	logger     *slog.Logger
	platform   component.PlatformMeta

	inputPorts  []component.Port
	outputPorts []component.Port

	shutdown    chan struct{}
	done        chan struct{}
	running     bool
	mu          sync.RWMutex
	lifecycleMu sync.Mutex

	// consumerInfos tracks JetStream consumers for clean shutdown.
	consumerInfos []consumerInfo

	// stateCache caches workflow state derived from ENTITY_STATES triples.
	// Hot-path reads avoid repeated KV lookups; writeTriple is the durable store.
	stateCache cache.Cache[*workflowState]

	messagesProcessed atomic.Int64
	errors            atomic.Int64
	lastActivity      atomic.Value // stores time.Time
}

// NewComponent creates a new PRWorkflowComponent from config.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var cfg Config
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal pr-workflow config: %w", err)
	}
	cfg = cfg.withDefaults()

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("component", componentName)

	c := &PRWorkflowComponent{
		config:     cfg,
		decoder:    message.NewDecoder(deps.PayloadRegistry),
		natsClient: deps.NATSClient,
		logger:     logger,
		platform:   deps.Platform,
		shutdown:   make(chan struct{}),
		done:       make(chan struct{}),
	}

	// Build ports from config
	for _, p := range cfg.Ports.Inputs {
		c.inputPorts = append(c.inputPorts, component.BuildPortFromDefinition(
			component.PortDefinition{Name: p.Name, Subject: p.Subject, Type: p.Type, StreamName: p.Stream},
			component.DirectionInput,
		))
	}
	for _, p := range cfg.Ports.Outputs {
		c.outputPorts = append(c.outputPorts, component.BuildPortFromDefinition(
			component.PortDefinition{Name: p.Name, Subject: p.Subject, Type: p.Type, StreamName: p.Stream},
			component.DirectionOutput,
		))
	}

	return c, nil
}

// Initialize prepares the component.
func (c *PRWorkflowComponent) Initialize() error {
	return nil
}

// Start begins consuming events and agent completions.
func (c *PRWorkflowComponent) Start(ctx context.Context) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	c.mu.RLock()
	if c.running {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	// Re-create channels to support restart after Stop.
	c.mu.Lock()
	c.shutdown = make(chan struct{})
	c.done = make(chan struct{})
	c.mu.Unlock()

	c.logger.Info("Starting PR workflow spawner")

	// Ensure the GITHUB stream exists before setting up consumers. The AGENT
	// stream is owned by agentic-loop and must already exist.
	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("get JetStream context: %w", err)
	}
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "GITHUB",
		Subjects: []string{"github.event.>"},
		Storage:  jetstream.FileStorage,
		MaxAge:   72 * time.Hour,
	}); err != nil {
		return fmt.Errorf("ensure GITHUB stream: %w", err)
	}

	// Create TTL cache for workflow state (derived from entity triples, not a separate bucket).
	stateCache, err := cache.NewTTL[*workflowState](ctx, 15*time.Minute, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("create state cache: %w", err)
	}
	c.stateCache = stateCache

	// Set up a JetStream consumer for each inbound port.
	for _, port := range c.inputPorts {
		subject := portSubject(port)
		if subject == "" {
			continue
		}

		streamName := portStream(port)
		if streamName == "" {
			c.logger.Debug("Skipping input port with no stream", "subject", subject)
			continue
		}

		var handler func(context.Context, jetstream.Msg)
		switch {
		case strings.HasPrefix(subject, "github.event.issue"):
			handler = c.wrapIssueHandler()
		case strings.HasPrefix(subject, "agent.complete"):
			handler = c.wrapLoopCompletedHandler()
		default:
			c.logger.Debug("Skipping unrecognized input port", "subject", subject)
			continue
		}

		consumerName := fmt.Sprintf("github-pr-%s", port.Name)
		cfg := natsclient.StreamConsumerConfig{
			StreamName:    streamName,
			FilterSubject: subject,
			ConsumerName:  consumerName,
			AckWait:       30 * time.Second,
			MaxDeliver:    3,
			MaxAckPending: 1,
		}

		if err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, handler); err != nil {
			return fmt.Errorf("setup consumer for %s: %w", subject, err)
		}

		c.consumerInfos = append(c.consumerInfos, consumerInfo{
			streamName:   streamName,
			consumerName: consumerName,
		})
		c.logger.Debug("Subscribed (JetStream)",
			"subject", subject,
			"stream", streamName,
			"consumer", consumerName,
		)
	}

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	return nil
}

// Stop performs graceful shutdown.
func (c *PRWorkflowComponent) Stop(_ time.Duration) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	c.mu.RLock()
	if !c.running {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	c.logger.Info("Stopping PR workflow spawner")

	// Signal shutdown before stopping consumers so in-flight handlers can detect it.
	c.mu.Lock()
	close(c.shutdown)
	c.mu.Unlock()

	for _, info := range c.consumerInfos {
		c.natsClient.StopConsumer(info.streamName, info.consumerName)
		c.logger.Debug("Stopped consumer", "stream", info.streamName, "consumer", info.consumerName)
	}
	c.consumerInfos = nil

	c.mu.Lock()
	c.running = false
	close(c.done)
	c.mu.Unlock()

	return nil
}

// wrapIssueHandler returns a JetStream handler that delegates to handleIssueEvent.
func (c *PRWorkflowComponent) wrapIssueHandler() func(context.Context, jetstream.Msg) {
	return func(ctx context.Context, msg jetstream.Msg) {
		c.handleIssueEvent(ctx, msg.Data())
		if err := msg.Ack(); err != nil {
			c.logger.Error("Failed to ack issue event message", "error", err)
		}
	}
}

// wrapLoopCompletedHandler returns a JetStream handler that delegates to handleLoopCompleted.
func (c *PRWorkflowComponent) wrapLoopCompletedHandler() func(context.Context, jetstream.Msg) {
	return func(ctx context.Context, msg jetstream.Msg) {
		c.handleLoopCompleted(ctx, msg.Data())
		if err := msg.Ack(); err != nil {
			c.logger.Error("Failed to ack loop completed message", "error", err)
		}
	}
}

// handleIssueEvent processes a GitHub issue webhook event.
func (c *PRWorkflowComponent) handleIssueEvent(ctx context.Context, data []byte) {
	var event GitHubIssueWebhookEvent
	if err := json.Unmarshal(data, &event); err != nil {
		c.logger.Debug("Failed to unmarshal issue event", "error", err)
		c.errors.Add(1)
		return
	}

	if event.Action != "opened" {
		return
	}

	// Build workflow entity ID
	entityID := WorkflowEntityID(event.Repo.Owner.Login, event.Repo.Name, event.Issue.Number)

	// Write initial entity triples. Errors are best-effort: the triples that
	// succeed are still useful and the graph is eventually consistent.
	for pred, obj := range map[string]any{
		"workflow.phase":             "qualifying",
		"workflow.status":            "active",
		"github.issue.number":        fmt.Sprintf("%d", event.Issue.Number),
		"github.issue.title":         event.Issue.Title,
		"workflow.tokens.total":      "0",
		"workflow.review.rejections": "0",
	} {
		if err := c.writeTriple(ctx, entityID, pred, obj); err != nil {
			c.errors.Add(1)
			c.logger.Warn("Failed to write triple", "predicate", pred, "error", err)
		}
	}

	// Spawn qualifier agent. Encode the entity ID in the task ID using "::" as a
	// separator (entity IDs use dots, so "::" is unambiguous).
	prompt := BuildQualifierPrompt(event.Repo.Owner.Login, event.Repo.Name, event.Issue.Number, event.Issue.Title, event.Issue.Body)
	taskID := fmt.Sprintf("qualifier::%s", entityID)

	task := &agentic.TaskMessage{
		TaskID:       taskID,
		Role:         agentic.RoleQualifier,
		Model:        c.config.Model,
		Prompt:       prompt,
		WorkflowSlug: WorkflowSlug,
		WorkflowStep: "qualify",
		Metadata:     map[string]any{"entity_id": entityID},
	}

	c.publishTask(ctx, "agent.task.qualifier", task)
	c.messagesProcessed.Add(1)
	c.lastActivity.Store(time.Now())
}

// handleLoopCompleted processes an agentic loop completion event.
func (c *PRWorkflowComponent) handleLoopCompleted(ctx context.Context, data []byte) {
	// Parse the BaseMessage envelope
	base, err := c.decoder.Decode(data)
	if err != nil {
		c.logger.Debug("Failed to unmarshal loop completed envelope", "error", err)
		c.errors.Add(1)
		return
	}

	event, ok := base.Payload().(*agentic.LoopCompletedEvent)
	if !ok {
		// Not a LoopCompletedEvent, ignore
		return
	}

	if event.WorkflowSlug != WorkflowSlug {
		return
	}

	switch event.WorkflowStep {
	case "qualify":
		c.handleQualifierComplete(ctx, event)
	case "develop":
		c.handleDeveloperComplete(ctx, event)
	case "review":
		c.handleReviewerComplete(ctx, event)
	default:
		c.logger.Debug("Unknown workflow step", "step", event.WorkflowStep)
		return
	}

	c.messagesProcessed.Add(1)
	c.lastActivity.Store(time.Now())
}

// handleQualifierComplete processes the qualifier agent's result.
func (c *PRWorkflowComponent) handleQualifierComplete(ctx context.Context, event *agentic.LoopCompletedEvent) {
	verdict := ParseQualifierResult(event.Result)

	entityID := extractEntityIDFromTaskID(event.TaskID, "qualifier::")
	if entityID == "" {
		c.logger.Debug("Cannot extract entity ID from qualifier task", "task_id", event.TaskID)
		return
	}

	for pred, obj := range map[string]string{
		"workflow.qualifier.verdict": verdict.Verdict,
		"workflow.phase":             verdict.Verdict, // phase mirrors verdict
	} {
		if err := c.writeTriple(ctx, entityID, pred, obj); err != nil {
			c.errors.Add(1)
			c.logger.Warn("Failed to write triple", "predicate", pred, "error", err)
		}
	}
	if err := c.accumulateTokens(ctx, entityID, event.TokensIn+event.TokensOut); err != nil {
		c.errors.Add(1)
		c.logger.Warn("Failed to accumulate tokens", "entity_id", entityID, "error", err)
	}
}

// handleDeveloperComplete processes the developer agent's result.
func (c *PRWorkflowComponent) handleDeveloperComplete(ctx context.Context, event *agentic.LoopCompletedEvent) {
	output := ParseDeveloperResult(event.Result)

	entityID := extractEntityIDFromTaskID(event.TaskID, "developer::")
	if entityID == "" {
		c.logger.Debug("Cannot extract entity ID from developer task", "task_id", event.TaskID)
		return
	}

	if output.PRNumber > 0 {
		if err := c.writeTriple(ctx, entityID, "workflow.pr.number", fmt.Sprintf("%d", output.PRNumber)); err != nil {
			c.errors.Add(1)
			c.logger.Warn("Failed to write triple", "predicate", "workflow.pr.number", "error", err)
		}
	}
	if err := c.accumulateTokens(ctx, entityID, event.TokensIn+event.TokensOut); err != nil {
		c.errors.Add(1)
		c.logger.Warn("Failed to accumulate tokens", "entity_id", entityID, "error", err)
	}
	if err := c.writeTriple(ctx, entityID, "workflow.phase", PhaseDevComplete); err != nil {
		c.errors.Add(1)
		c.logger.Warn("Failed to write triple", "predicate", "workflow.phase", "error", err)
	}
}

// handleReviewerComplete processes the reviewer agent's result.
func (c *PRWorkflowComponent) handleReviewerComplete(ctx context.Context, event *agentic.LoopCompletedEvent) {
	review := ParseReviewerResult(event.Result)

	entityID := extractEntityIDFromTaskID(event.TaskID, "reviewer::")
	if entityID == "" {
		c.logger.Debug("Cannot extract entity ID from reviewer task", "task_id", event.TaskID)
		return
	}

	if err := c.writeTriple(ctx, entityID, "workflow.review.feedback", review.Feedback); err != nil {
		c.errors.Add(1)
		c.logger.Warn("Failed to write triple", "predicate", "workflow.review.feedback", "error", err)
	}
	if err := c.accumulateTokens(ctx, entityID, event.TokensIn+event.TokensOut); err != nil {
		c.errors.Add(1)
		c.logger.Warn("Failed to accumulate tokens", "entity_id", entityID, "error", err)
	}

	var phase string
	switch review.Verdict {
	case "approved":
		phase = PhaseApproved
	default:
		phase = PhaseChangesRequested
		if err := c.incrementRejections(ctx, entityID); err != nil {
			c.errors.Add(1)
			c.logger.Warn("Failed to increment rejections", "entity_id", entityID, "error", err)
		}
	}
	if err := c.writeTriple(ctx, entityID, "workflow.phase", phase); err != nil {
		c.errors.Add(1)
		c.logger.Warn("Failed to write triple", "predicate", "workflow.phase", "error", err)
	}
}

// writeTriple sends an AddTripleRequest to graph-ingest via NATS request/reply
// and confirms success. It returns an error if the mutation fails.
func (c *PRWorkflowComponent) writeTriple(ctx context.Context, entityID, predicate string, object any) error {
	if c.natsClient == nil {
		return fmt.Errorf("NATS client not available")
	}

	triple := message.Triple{
		Subject:    entityID,
		Predicate:  predicate,
		Object:     object,
		Source:     componentName,
		Timestamp:  time.Now(),
		Confidence: 1.0,
	}

	req := gtypes.AddTripleRequest{Triple: triple}
	reqData, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal AddTripleRequest: %w", err)
	}

	respData, err := c.natsClient.Request(ctx, "graph.mutation.triple.add", reqData, 5*time.Second)
	if err != nil {
		return fmt.Errorf("NATS request failed: %w", err)
	}

	var resp gtypes.AddTripleResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return fmt.Errorf("unmarshal AddTripleResponse: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("graph mutation failed: %s", resp.Error)
	}

	return nil
}

// publishTask publishes a TaskMessage to a JetStream subject.
func (c *PRWorkflowComponent) publishTask(ctx context.Context, subject string, task *agentic.TaskMessage) {
	baseMsg := message.NewBaseMessage(task.Schema(), task, componentName)
	data, err := json.Marshal(baseMsg)
	if err != nil {
		c.logger.Debug("Failed to marshal task message", "error", err)
		c.errors.Add(1)
		return
	}

	if c.natsClient != nil {
		if err := c.natsClient.PublishToStream(ctx, subject, data); err != nil {
			c.logger.Debug("Failed to publish task", "subject", subject, "error", err)
			c.errors.Add(1)
		}
	}
}

// getWorkflowState returns cached workflow state, falling back to ENTITY_STATES
// triples on cache miss. The graph (via writeTriple) is the durable source of truth;
// the cache avoids repeated KV reads on the hot path.
func (c *PRWorkflowComponent) getWorkflowState(ctx context.Context, entityID string) (*workflowState, error) {
	// Check cache first
	if cached, ok := c.stateCache.Get(entityID); ok {
		return cached, nil
	}

	// Cache miss: derive state from entity triples in ENTITY_STATES
	state := &workflowState{}

	js, err := c.natsClient.JetStream()
	if err != nil {
		return state, nil // Return zero state if JetStream unavailable
	}

	bucket, err := js.KeyValue(ctx, gtypes.BucketEntityStates)
	if err != nil {
		return state, nil // Bucket may not exist yet
	}

	entry, err := bucket.Get(ctx, entityID)
	if err != nil {
		// Entity not yet in graph — return zero state
		return state, nil
	}

	var entityState gtypes.EntityState
	if err := json.Unmarshal(entry.Value(), &entityState); err != nil {
		return state, nil
	}

	// Extract counters from triples
	for _, t := range entityState.Triples {
		switch t.Predicate {
		case "workflow.tokens.total":
			if v, ok := t.Object.(string); ok {
				fmt.Sscanf(v, "%d", &state.TotalTokens)
			}
		case "workflow.review.rejections":
			if v, ok := t.Object.(string); ok {
				fmt.Sscanf(v, "%d", &state.Rejections)
			}
		}
	}

	// Populate cache
	_, _ = c.stateCache.Set(entityID, state)
	return state, nil
}

// accumulateTokens adds the step's token count to the running total for the
// entity, updates the cache, and persists the new total as a triple.
func (c *PRWorkflowComponent) accumulateTokens(ctx context.Context, entityID string, tokens int) error {
	state, err := c.getWorkflowState(ctx, entityID)
	if err != nil {
		return err
	}
	state.TotalTokens += tokens
	_, _ = c.stateCache.Set(entityID, state)
	return c.writeTriple(ctx, entityID, "workflow.tokens.total", fmt.Sprintf("%d", state.TotalTokens))
}

// incrementRejections increments the rejection counter for the entity,
// updates the cache, and persists the new count as a triple.
func (c *PRWorkflowComponent) incrementRejections(ctx context.Context, entityID string) error {
	state, err := c.getWorkflowState(ctx, entityID)
	if err != nil {
		return err
	}
	state.Rejections++
	_, _ = c.stateCache.Set(entityID, state)
	return c.writeTriple(ctx, entityID, "workflow.review.rejections", fmt.Sprintf("%d", state.Rejections))
}

// WorkflowEntityID builds the 6-part entity ID for a workflow execution.
func WorkflowEntityID(org, repo string, issueNumber int) string {
	return fmt.Sprintf("%s.github.repo.%s.workflow.%d", org, repo, issueNumber)
}

// extractEntityIDFromTaskID extracts the entity ID from a task ID.
// Task IDs are formatted as "<prefix>::<entityID>" where "::" is the separator
// (entity IDs use dots, so "::" is unambiguous).
func extractEntityIDFromTaskID(taskID, prefix string) string {
	if !strings.HasPrefix(taskID, prefix) {
		return ""
	}
	rest := taskID[len(prefix):]
	if rest == "" {
		return ""
	}
	return rest
}

// --- Prompt Builders (exported for testing) ---

// BuildQualifierPrompt builds the prompt for the qualifier agent.
func BuildQualifierPrompt(org, repo string, issueNumber int, title, body string) string {
	return fmt.Sprintf(
		"You are an expert software engineer triaging GitHub issues.\n\n"+
			"Repository: %s/%s\n"+
			"Issue #%d: %s\n\n"+
			"Body:\n%s\n\n"+
			"Evaluate this issue and respond with a JSON object containing:\n"+
			"  - verdict: one of [qualified, rejected, not_a_bug, wont_fix, needs_info]\n"+
			"  - confidence: float between 0.0 and 1.0\n"+
			"  - severity: one of [critical, high, medium, low]\n"+
			"  - reasoning: brief explanation of your verdict\n\n"+
			"Only respond with the JSON object, no other text.",
		org, repo, issueNumber, title, body,
	)
}

// BuildDeveloperPrompt builds the prompt for the developer agent.
// If reviewFeedback is non-empty, it is included as previous review feedback.
func BuildDeveloperPrompt(org, repo string, issueNumber int, issueTitle, issueBody, qualifierVerdict string, qualifierConfidence float64, severity, reviewFeedback string) string {
	var sb strings.Builder
	sb.WriteString("You are an expert software engineer.\n\n")
	sb.WriteString(fmt.Sprintf("Repository: %s/%s\n", org, repo))
	sb.WriteString(fmt.Sprintf("Issue #%d: %s\n\n", issueNumber, issueTitle))
	sb.WriteString(fmt.Sprintf("Issue body:\n%s\n\n", issueBody))
	sb.WriteString(fmt.Sprintf("Qualifier verdict: %s (confidence %.2f, severity %s)\n\n",
		qualifierVerdict, qualifierConfidence, severity))

	if reviewFeedback != "" {
		sb.WriteString("Previous review feedback that must be addressed:\n")
		sb.WriteString(reviewFeedback)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Implement a fix for this issue. Create a feature branch, commit your changes, " +
		"and open a pull request. Respond with a JSON object containing:\n" +
		"  - branch_name: the branch you created\n" +
		"  - pr_number: the pull request number\n" +
		"  - pr_url: the URL of the pull request\n" +
		"  - files_changed: list of file paths modified\n\n" +
		"Only respond with the JSON object, no other text.")

	return sb.String()
}

// BuildReviewerPrompt builds the prompt for the reviewer agent.
func BuildReviewerPrompt(org, repo string, issueNumber int, issueTitle string, prNumber int, prURL string, filesChanged []string) string {
	return fmt.Sprintf(
		"You are a senior software engineer performing an adversarial code review.\n\n"+
			"Repository: %s/%s\n"+
			"Issue #%d: %s\n"+
			"Pull Request #%d: %s\n"+
			"Files changed: %s\n\n"+
			"Review the pull request thoroughly. Respond with a JSON object containing:\n"+
			"  - verdict: one of [approved, request_changes]\n"+
			"  - feedback: detailed review comments (required if verdict is request_changes)\n\n"+
			"Only respond with the JSON object, no other text.",
		org, repo,
		issueNumber, issueTitle,
		prNumber, prURL,
		strings.Join(filesChanged, ", "),
	)
}

// --- Result Parsers (exported for testing) ---

// QualifierVerdict represents the parsed qualifier agent result.
type QualifierVerdict struct {
	Verdict    string  `json:"verdict"`
	Confidence float64 `json:"confidence"`
	Severity   string  `json:"severity"`
}

// ParseQualifierResult parses the qualifier agent's JSON result.
// Returns a verdict with "needs_info" on parse failure.
func ParseQualifierResult(result string) QualifierVerdict {
	var v QualifierVerdict
	if err := json.Unmarshal([]byte(result), &v); err != nil {
		return QualifierVerdict{Verdict: PhaseNeedsInfo}
	}
	return v
}

// DeveloperOutput represents the parsed developer agent result.
type DeveloperOutput struct {
	BranchName   string   `json:"branch_name"`
	PRNumber     int      `json:"pr_number"`
	PRUrl        string   `json:"pr_url"`
	FilesChanged []string `json:"files_changed"`
}

// ParseDeveloperResult parses the developer agent's JSON result.
func ParseDeveloperResult(result string) DeveloperOutput {
	var o DeveloperOutput
	json.Unmarshal([]byte(result), &o) //nolint:errcheck // best-effort parse
	return o
}

// ReviewerOutput represents the parsed reviewer agent result.
type ReviewerOutput struct {
	Verdict  string `json:"verdict"`
	Feedback string `json:"feedback"`
}

// ParseReviewerResult parses the reviewer agent's JSON result.
func ParseReviewerResult(result string) ReviewerOutput {
	var o ReviewerOutput
	json.Unmarshal([]byte(result), &o) //nolint:errcheck // best-effort parse
	return o
}

// --- Discoverable interface ---

// Meta returns component metadata.
func (c *PRWorkflowComponent) Meta() component.Metadata {
	return component.Metadata{
		Name:        componentName,
		Type:        "processor",
		Description: "Spawns agent tasks for the GitHub issue-to-PR pipeline",
		Version:     componentVersion,
	}
}

// InputPorts returns the component's input port definitions.
func (c *PRWorkflowComponent) InputPorts() []component.Port {
	return c.inputPorts
}

// OutputPorts returns the component's output port definitions.
func (c *PRWorkflowComponent) OutputPorts() []component.Port {
	return c.outputPorts
}

// ConfigSchema returns the JSON schema for this component's configuration.
// The canonical schema definition lives in register.go as prWorkflowSchema.
func (c *PRWorkflowComponent) ConfigSchema() component.ConfigSchema {
	return prWorkflowSchema
}

// Health returns the current health status of the component.
func (c *PRWorkflowComponent) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.running {
		return component.HealthStatus{Status: "healthy"}
	}
	return component.HealthStatus{Status: "stopped"}
}

// DataFlow returns current flow metrics for the component.
func (c *PRWorkflowComponent) DataFlow() component.FlowMetrics {
	var last time.Time
	if v := c.lastActivity.Load(); v != nil {
		last = v.(time.Time)
	}
	return component.FlowMetrics{
		MessagesPerSecond: float64(c.messagesProcessed.Load()),
		ErrorRate:         float64(c.errors.Load()),
		LastActivity:      last,
	}
}

// portSubject extracts the subject string from a port's config.
func portSubject(port component.Port) string {
	if port.Config == nil {
		return ""
	}
	switch cfg := port.Config.(type) {
	case component.NATSPort:
		return cfg.Subject
	case component.JetStreamPort:
		if len(cfg.Subjects) > 0 {
			return cfg.Subjects[0]
		}
	}
	return ""
}

// portStream extracts the stream name from a JetStream port's config.
// Returns empty string for plain NATS ports.
func portStream(port component.Port) string {
	if port.Config == nil {
		return ""
	}
	if cfg, ok := port.Config.(component.JetStreamPort); ok {
		return cfg.StreamName
	}
	return ""
}
