package agenticdispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

// IntentType represents the classified intent of a user message.
type IntentType string

const (
	// IntentNewTask starts a new agentic loop.
	IntentNewTask IntentType = "new_task"
	// IntentContinue continues an existing loop with additional input.
	IntentContinue IntentType = "continue"
	// IntentSignal sends a control signal (approve, reject, etc.).
	IntentSignal IntentType = "signal"
	// IntentQuestion asks about loop status or system state.
	IntentQuestion IntentType = "question"
	// IntentMeta is a meta-query about the system itself.
	IntentMeta IntentType = "meta"
)

// ClassifiedIntent is the result of intent classification.
type ClassifiedIntent struct {
	Type       IntentType `json:"type"`
	LoopID     string     `json:"loop_id,omitempty"`     // Relevant loop, if identified
	SignalType string     `json:"signal_type,omitempty"` // For signal intents
	Confidence float64    `json:"confidence,omitempty"`  // 0.0-1.0
	Content    string     `json:"content"`               // Original or rewritten content
}

// IntentClassifier classifies user messages into structured intents.
type IntentClassifier interface {
	Classify(ctx context.Context, msg agentic.UserMessage, activeLoops []*LoopInfo) (*ClassifiedIntent, error)
}

// LLMIntentClassifier uses an LLM to classify ambiguous user messages.
type LLMIntentClassifier struct {
	modelRegistry model.RegistryReader
	logger        *slog.Logger
	modelName     string // Model endpoint or capability name
}

// NewLLMIntentClassifier creates a classifier that uses the model registry
// to resolve an endpoint and make classification calls.
func NewLLMIntentClassifier(registry model.RegistryReader, modelName string, logger *slog.Logger) *LLMIntentClassifier {
	if modelName == "" {
		modelName = "default"
	}
	return &LLMIntentClassifier{
		modelRegistry: registry,
		logger:        logger,
		modelName:     modelName,
	}
}

// Classify sends the user message to an LLM for intent classification.
func (c *LLMIntentClassifier) Classify(ctx context.Context, msg agentic.UserMessage, activeLoops []*LoopInfo) (*ClassifiedIntent, error) {
	// Build context about active loops
	loopContext := "No active loops."
	if len(activeLoops) > 0 {
		var loopDescs []string
		for _, l := range activeLoops {
			loopDescs = append(loopDescs, fmt.Sprintf("- %s: state=%s, iterations=%d/%d", l.LoopID, l.State, l.Iterations, l.MaxIterations))
		}
		loopContext = fmt.Sprintf("Active loops:\n%s", strings.Join(loopDescs, "\n"))
	}

	systemPrompt := fmt.Sprintf(`You are a message intent classifier for an agentic system. Classify the user's message into exactly one intent type. Respond with ONLY a JSON object.

Intent types:
- "new_task": The user wants to start a new task or give a new instruction
- "continue": The user is providing additional input to an existing active loop (follow-up, clarification, refinement)
- "signal": The user wants to control a loop (approve, reject, pause, resume, cancel)
- "question": The user is asking about status, progress, or system state
- "meta": The user is asking about the system itself (capabilities, help, configuration)

Current state:
%s

Respond with JSON: {"type": "<intent_type>", "loop_id": "<if applicable>", "signal_type": "<if signal: approve|reject|pause|resume|cancel>", "confidence": <0.0-1.0>}`, loopContext)

	// Resolve model endpoint
	ep := c.resolveEndpoint()
	if ep == nil {
		c.logger.Warn("No model endpoint available for intent classification, falling back to new_task")
		return &ClassifiedIntent{Type: IntentNewTask, Content: msg.Content, Confidence: 0.5}, nil
	}

	client, err := agenticmodel.NewClient(ep)
	if err != nil {
		c.logger.Error("Failed to create model client for classification", slog.Any("error", err))
		return &ClassifiedIntent{Type: IntentNewTask, Content: msg.Content, Confidence: 0.5}, nil
	}
	client.SetAdapter(agenticmodel.AdapterFor(ep.Provider))
	client.SetLogger(c.logger)

	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	resp, err := client.ChatCompletion(reqCtx, agentic.AgentRequest{
		RequestID: fmt.Sprintf("classify_%d", time.Now().UnixNano()),
		Role:      "classifier",
		Model:     c.modelName,
		Messages: []agentic.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: msg.Content},
		},
		MaxTokens:   128,
		Temperature: 0.1,
	})
	if err != nil {
		c.logger.Error("Intent classification LLM call failed", slog.Any("error", err))
		return &ClassifiedIntent{Type: IntentNewTask, Content: msg.Content, Confidence: 0.5}, nil
	}

	// Parse the JSON response
	var intent ClassifiedIntent
	if err := json.Unmarshal([]byte(resp.Message.Content), &intent); err != nil {
		// Try to extract JSON from a wrapped response
		extracted := extractJSON(resp.Message.Content)
		if extracted != "" {
			if err := json.Unmarshal([]byte(extracted), &intent); err != nil {
				c.logger.Warn("Failed to parse classification response", slog.String("content", resp.Message.Content))
				return &ClassifiedIntent{Type: IntentNewTask, Content: msg.Content, Confidence: 0.5}, nil
			}
		} else {
			c.logger.Warn("Failed to parse classification response", slog.String("content", resp.Message.Content))
			return &ClassifiedIntent{Type: IntentNewTask, Content: msg.Content, Confidence: 0.5}, nil
		}
	}

	intent.Content = msg.Content
	c.logger.Debug("Classified intent",
		slog.String("type", string(intent.Type)),
		slog.Float64("confidence", intent.Confidence),
		slog.String("loop_id", intent.LoopID))

	return &intent, nil
}

// resolveEndpoint finds a suitable model endpoint for classification.
func (c *LLMIntentClassifier) resolveEndpoint() *model.EndpointConfig {
	// Try the configured model name
	if ep := c.modelRegistry.GetEndpoint(c.modelName); ep != nil {
		return ep
	}

	// Try capability-based resolution
	chain := c.modelRegistry.GetFallbackChain(c.modelName)
	for _, name := range chain {
		if ep := c.modelRegistry.GetEndpoint(name); ep != nil {
			return ep
		}
	}

	// Fall back to default
	defaultName := c.modelRegistry.GetDefault()
	if defaultName != "" {
		return c.modelRegistry.GetEndpoint(defaultName)
	}

	return nil
}

// extractJSON finds the first JSON object in a string (handles markdown code blocks).
func extractJSON(s string) string {
	// Find first { and last }
	start := -1
	end := -1
	depth := 0
	for i, ch := range s {
		if ch == '{' {
			if start == -1 {
				start = i
			}
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
		}
	}
	if start >= 0 && end > start {
		return s[start:end]
	}
	return ""
}
