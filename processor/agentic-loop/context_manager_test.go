package agenticloop_test

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

func TestContextManager_Utilization(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		addMessages map[agenticloop.RegionType][]agentic.ChatMessage
		wantUtilMin float64
		wantUtilMax float64
	}{
		{
			name:        "empty context",
			model:       "gpt-4o",
			addMessages: map[agenticloop.RegionType][]agentic.ChatMessage{},
			wantUtilMin: 0.0,
			wantUtilMax: 0.0,
		},
		{
			name:  "system prompt only",
			model: "gpt-4o",
			addMessages: map[agenticloop.RegionType][]agentic.ChatMessage{
				agenticloop.RegionSystemPrompt: {
					{Role: "system", Content: "You are a helpful assistant."},
				},
			},
			wantUtilMin: 0.0,
			wantUtilMax: 0.1,
		},
		{
			name:  "mixed regions",
			model: "gpt-4o",
			addMessages: map[agenticloop.RegionType][]agentic.ChatMessage{
				agenticloop.RegionSystemPrompt: {
					{Role: "system", Content: "You are a helpful assistant."},
				},
				agenticloop.RegionRecentHistory: {
					{Role: "user", Content: "Hello"},
					{Role: "assistant", Content: "Hi there!"},
				},
				agenticloop.RegionToolResults: {
					{Role: "tool", Content: "Tool result data", ToolCallID: "call-1"},
				},
			},
			wantUtilMin: 0.0,
			wantUtilMax: 0.1,
		},
		{
			name:  "claude model with higher limit",
			model: "claude-sonnet",
			addMessages: map[agenticloop.RegionType][]agentic.ChatMessage{
				agenticloop.RegionSystemPrompt: {
					{Role: "system", Content: "You are a helpful assistant."},
				},
			},
			wantUtilMin: 0.0,
			wantUtilMax: 0.05,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := agenticloop.DefaultContextConfig()
			cm := agenticloop.NewContextManager("loop-1", tt.model, config)

			// Add messages to regions
			for region, messages := range tt.addMessages {
				for _, msg := range messages {
					if err := cm.AddMessage(region, msg); err != nil {
						t.Fatalf("AddMessage() error = %v", err)
					}
				}
			}

			utilization := cm.Utilization()

			if utilization < tt.wantUtilMin || utilization > tt.wantUtilMax {
				t.Errorf("Utilization() = %f, want between %f and %f", utilization, tt.wantUtilMin, tt.wantUtilMax)
			}
		})
	}
}

func TestContextManager_ShouldCompact(t *testing.T) {
	tests := []struct {
		name              string
		threshold         float64
		fillToUtilPct     float64
		wantShouldCompact bool
	}{
		{
			name:              "below threshold",
			threshold:         0.60,
			fillToUtilPct:     0.50,
			wantShouldCompact: false,
		},
		{
			name:              "at threshold",
			threshold:         0.60,
			fillToUtilPct:     0.60,
			wantShouldCompact: true,
		},
		{
			name:              "above threshold",
			threshold:         0.60,
			fillToUtilPct:     0.70,
			wantShouldCompact: true,
		},
		{
			name:              "far below threshold",
			threshold:         0.60,
			fillToUtilPct:     0.20,
			wantShouldCompact: false,
		},
		{
			name:              "far above threshold",
			threshold:         0.60,
			fillToUtilPct:     0.90,
			wantShouldCompact: true,
		},
		{
			name:              "high threshold below",
			threshold:         0.80,
			fillToUtilPct:     0.70,
			wantShouldCompact: false,
		},
		{
			name:              "low threshold above",
			threshold:         0.40,
			fillToUtilPct:     0.50,
			wantShouldCompact: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := agenticloop.DefaultContextConfig()
			config.CompactThreshold = tt.threshold
			cm := agenticloop.NewContextManager("loop-1", "gpt-4o", config)

			// Fill context to target utilization using DefaultContextLimit as the model limit
			targetTokens := int(float64(agenticloop.DefaultContextLimit) * tt.fillToUtilPct)
			fillContextToTokens(t, cm, targetTokens)

			shouldCompact := cm.ShouldCompact()

			if shouldCompact != tt.wantShouldCompact {
				t.Errorf("ShouldCompact() = %v, want %v (utilization: %f, threshold: %f)",
					shouldCompact, tt.wantShouldCompact, cm.Utilization(), tt.threshold)
			}
		})
	}
}

func TestContextManager_GetContext_RegionOrdering(t *testing.T) {
	config := agenticloop.DefaultContextConfig()
	cm := agenticloop.NewContextManager("loop-1", "gpt-4o", config)

	// Add messages to different regions in random order
	_ = cm.AddMessage(agenticloop.RegionToolResults, agentic.ChatMessage{
		Role: "tool", Content: "Tool result", ToolCallID: "call-1",
	})
	_ = cm.AddMessage(agenticloop.RegionRecentHistory, agentic.ChatMessage{
		Role: "user", Content: "Recent user message",
	})
	_ = cm.AddMessage(agenticloop.RegionSystemPrompt, agentic.ChatMessage{
		Role: "system", Content: "System prompt",
	})
	_ = cm.AddMessage(agenticloop.RegionCompactedHistory, agentic.ChatMessage{
		Role: "user", Content: "Compacted summary",
	})
	_ = cm.AddMessage(agenticloop.RegionHydratedContext, agentic.ChatMessage{
		Role: "user", Content: "Hydrated context",
	})

	// Get context
	messages := cm.GetContext()

	// Verify message count
	if len(messages) != 5 {
		t.Fatalf("GetContext() returned %d messages, want 5", len(messages))
	}

	// Verify ordering: System -> Compacted -> Hydrated -> Recent -> Tool
	expectedContent := []string{
		"System prompt",
		"Compacted summary",
		"Hydrated context",
		"Recent user message",
		"Tool result",
	}

	for i, msg := range messages {
		if msg.Content != expectedContent[i] {
			t.Errorf("Message[%d].Content = %q, want %q", i, msg.Content, expectedContent[i])
		}
	}
}

func TestContextManager_AddMessage(t *testing.T) {
	tests := []struct {
		name    string
		region  agenticloop.RegionType
		message agentic.ChatMessage
		wantErr bool
		errMsg  string
	}{
		{
			name:   "add to system prompt",
			region: agenticloop.RegionSystemPrompt,
			message: agentic.ChatMessage{
				Role: "system", Content: "System message",
			},
			wantErr: false,
		},
		{
			name:   "add to recent history",
			region: agenticloop.RegionRecentHistory,
			message: agentic.ChatMessage{
				Role: "user", Content: "User message",
			},
			wantErr: false,
		},
		{
			name:   "add to tool results",
			region: agenticloop.RegionToolResults,
			message: agentic.ChatMessage{
				Role: "tool", Content: "Tool output", ToolCallID: "call-1",
			},
			wantErr: false,
		},
		{
			name:   "add to compacted history",
			region: agenticloop.RegionCompactedHistory,
			message: agentic.ChatMessage{
				Role: "user", Content: "Summary",
			},
			wantErr: false,
		},
		{
			name:   "add to hydrated context",
			region: agenticloop.RegionHydratedContext,
			message: agentic.ChatMessage{
				Role: "user", Content: "Hydrated data",
			},
			wantErr: false,
		},
		{
			name:   "invalid region type",
			region: agenticloop.RegionType("unknown"),
			message: agentic.ChatMessage{
				Role: "user", Content: "Message",
			},
			wantErr: true,
			errMsg:  "region",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := agenticloop.DefaultContextConfig()
			cm := agenticloop.NewContextManager("loop-1", "gpt-4o", config)

			err := cm.AddMessage(tt.region, tt.message)

			if (err != nil) != tt.wantErr {
				t.Errorf("AddMessage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !containsIgnoreCase(err.Error(), tt.errMsg) {
					t.Errorf("AddMessage() error = %v, expected to contain %q", err, tt.errMsg)
				}
			}

			// Verify message was added if no error expected
			if !tt.wantErr {
				messages := cm.GetContext()
				found := false
				for _, msg := range messages {
					if msg.Content == tt.message.Content {
						found = true
						break
					}
				}
				if !found {
					t.Error("AddMessage() message not found in context")
				}
			}
		})
	}
}

func TestContextManager_GetRegionTokens(t *testing.T) {
	config := agenticloop.DefaultContextConfig()
	cm := agenticloop.NewContextManager("loop-1", "gpt-4o", config)

	// Add messages to specific regions
	_ = cm.AddMessage(agenticloop.RegionSystemPrompt, agentic.ChatMessage{
		Role: "system", Content: "System prompt with some content",
	})
	_ = cm.AddMessage(agenticloop.RegionRecentHistory, agentic.ChatMessage{
		Role: "user", Content: "User message",
	})

	// Get region token counts
	systemTokens := cm.GetRegionTokens(agenticloop.RegionSystemPrompt)
	recentTokens := cm.GetRegionTokens(agenticloop.RegionRecentHistory)
	toolTokens := cm.GetRegionTokens(agenticloop.RegionToolResults)

	// System prompt should have tokens
	if systemTokens <= 0 {
		t.Errorf("GetRegionTokens(SystemPrompt) = %d, want > 0", systemTokens)
	}

	// Recent history should have tokens
	if recentTokens <= 0 {
		t.Errorf("GetRegionTokens(RecentHistory) = %d, want > 0", recentTokens)
	}

	// Tool results should be empty
	if toolTokens != 0 {
		t.Errorf("GetRegionTokens(ToolResults) = %d, want 0", toolTokens)
	}

	// System prompt should have more tokens than recent history (longer content)
	if systemTokens <= recentTokens {
		t.Errorf("SystemPrompt tokens (%d) should be > RecentHistory tokens (%d)", systemTokens, recentTokens)
	}
}

func TestContextManager_ModelLimits(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		registry     *model.Registry
		wantLimitMin int
		wantLimitMax int
	}{
		{
			name:  "gpt-4o via registry",
			model: "gpt-4o",
			registry: &model.Registry{
				Endpoints: map[string]*model.EndpointConfig{
					"gpt-4o": {Model: "gpt-4o", MaxTokens: 128000},
				},
				Defaults: model.DefaultsConfig{Model: "gpt-4o"},
			},
			wantLimitMin: 128000,
			wantLimitMax: 128000,
		},
		{
			name:  "gpt-4o-mini via registry",
			model: "gpt-4o-mini",
			registry: &model.Registry{
				Endpoints: map[string]*model.EndpointConfig{
					"gpt-4o-mini": {Model: "gpt-4o-mini", MaxTokens: 128000},
				},
				Defaults: model.DefaultsConfig{Model: "gpt-4o-mini"},
			},
			wantLimitMin: 128000,
			wantLimitMax: 128000,
		},
		{
			name:  "claude-sonnet via registry",
			model: "claude-sonnet",
			registry: &model.Registry{
				Endpoints: map[string]*model.EndpointConfig{
					"claude-sonnet": {Model: "claude-sonnet", MaxTokens: 200000},
				},
				Defaults: model.DefaultsConfig{Model: "claude-sonnet"},
			},
			wantLimitMin: 200000,
			wantLimitMax: 200000,
		},
		{
			name:  "claude-opus via registry",
			model: "claude-opus",
			registry: &model.Registry{
				Endpoints: map[string]*model.EndpointConfig{
					"claude-opus": {Model: "claude-opus", MaxTokens: 200000},
				},
				Defaults: model.DefaultsConfig{Model: "claude-opus"},
			},
			wantLimitMin: 200000,
			wantLimitMax: 200000,
		},
		{
			name:         "unknown model uses default",
			model:        "unknown-model",
			registry:     nil, // No registry — falls back to DefaultContextLimit
			wantLimitMin: agenticloop.DefaultContextLimit,
			wantLimitMax: agenticloop.DefaultContextLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := agenticloop.DefaultContextConfig()

			var opts []agenticloop.ContextManagerOption
			if tt.registry != nil {
				opts = append(opts, agenticloop.WithModelRegistry(tt.registry))
			}
			cm := agenticloop.NewContextManager("loop-1", tt.model, config, opts...)

			// Fill context almost to the expected limit
			targetTokens := int(float64(tt.wantLimitMin) * 0.95)
			fillContextToTokens(t, cm, targetTokens)

			// Should be near but not over limit
			utilization := cm.Utilization()
			if utilization < 0.90 || utilization > 1.0 {
				t.Errorf("Utilization() = %f, want between 0.90 and 1.0", utilization)
			}
		})
	}
}

func TestContextManager_EmptyRegions(t *testing.T) {
	config := agenticloop.DefaultContextConfig()
	cm := agenticloop.NewContextManager("loop-1", "gpt-4o", config)

	// Don't add any messages

	// Get context should return empty
	messages := cm.GetContext()
	if len(messages) != 0 {
		t.Errorf("GetContext() for empty context = %d messages, want 0", len(messages))
	}

	// Utilization should be 0
	if cm.Utilization() != 0.0 {
		t.Errorf("Utilization() for empty context = %f, want 0.0", cm.Utilization())
	}

	// Should not need compaction
	if cm.ShouldCompact() {
		t.Error("ShouldCompact() for empty context = true, want false")
	}
}

func TestContextManager_MultipleMessagesPerRegion(t *testing.T) {
	config := agenticloop.DefaultContextConfig()
	cm := agenticloop.NewContextManager("loop-1", "gpt-4o", config)

	// Add multiple messages to recent history
	messages := []agentic.ChatMessage{
		{Role: "user", Content: "Message 1"},
		{Role: "assistant", Content: "Response 1"},
		{Role: "user", Content: "Message 2"},
		{Role: "assistant", Content: "Response 2"},
		{Role: "user", Content: "Message 3"},
	}

	for _, msg := range messages {
		if err := cm.AddMessage(agenticloop.RegionRecentHistory, msg); err != nil {
			t.Fatalf("AddMessage() error = %v", err)
		}
	}

	// Get context
	context := cm.GetContext()

	// Should have all 5 messages
	if len(context) != 5 {
		t.Errorf("GetContext() returned %d messages, want 5", len(context))
	}

	// Verify ordering preserved
	for i, msg := range context {
		if msg.Content != messages[i].Content {
			t.Errorf("Message[%d].Content = %q, want %q", i, msg.Content, messages[i].Content)
		}
	}
}

func TestContextManager_HeadroomReducesEffectiveCapacity(t *testing.T) {
	config := agenticloop.DefaultContextConfig()

	// Create two context managers: one with headroom, one without
	configNoHeadroom := config
	configNoHeadroom.HeadroomRatio = 0
	configNoHeadroom.HeadroomTokens = 0

	cmWithHeadroom := agenticloop.NewContextManager("loop-h", "gpt-4o", config)
	cmNoHeadroom := agenticloop.NewContextManager("loop-nh", "gpt-4o", configNoHeadroom)

	// Fill both to the same number of tokens
	targetTokens := int(float64(agenticloop.DefaultContextLimit) * 0.55)
	fillContextToTokens(t, cmWithHeadroom, targetTokens)
	fillContextToTokens(t, cmNoHeadroom, targetTokens)

	// Headroom context should report higher utilization (smaller effective limit)
	utilH := cmWithHeadroom.Utilization()
	utilN := cmNoHeadroom.Utilization()

	if utilH <= utilN {
		t.Errorf("Headroom utilization (%f) should be > no-headroom utilization (%f)", utilH, utilN)
	}
}

func TestContextManager_HeadroomTriggersCompactionEarlier(t *testing.T) {
	// With default headroom (5% ratio = 6400 on 128K), compaction at 60% threshold
	// should trigger earlier than without headroom
	config := agenticloop.DefaultContextConfig()
	config.CompactThreshold = 0.60

	configNoHeadroom := config
	configNoHeadroom.HeadroomRatio = 0
	configNoHeadroom.HeadroomTokens = 0

	// Fill to 58% of raw model limit — below threshold without headroom,
	// but above threshold with headroom (58% of 128000 / (128000-6400) ≈ 61%)
	fillPct := 0.58
	targetTokens := int(float64(agenticloop.DefaultContextLimit) * fillPct)

	cmWithHeadroom := agenticloop.NewContextManager("loop-h", "gpt-4o", config)
	fillContextToTokens(t, cmWithHeadroom, targetTokens)

	cmNoHeadroom := agenticloop.NewContextManager("loop-nh", "gpt-4o", configNoHeadroom)
	fillContextToTokens(t, cmNoHeadroom, targetTokens)

	if !cmWithHeadroom.ShouldCompact() {
		t.Error("Expected ShouldCompact()=true with headroom at 58% fill")
	}
	if cmNoHeadroom.ShouldCompact() {
		t.Error("Expected ShouldCompact()=false without headroom at 58% fill")
	}
}

func TestContextManager_HeadroomExceedsModelLimit(_ *testing.T) {
	config := agenticloop.DefaultContextConfig()
	config.HeadroomRatio = 0
	config.HeadroomTokens = agenticloop.DefaultContextLimit + 1000 // floor > model limit

	cm := agenticloop.NewContextManager("loop-edge", "gpt-4o", config)

	// Should not panic — falls back to raw model limit
	_ = cm.Utilization()
	_ = cm.ShouldCompact()
}

func TestContextManager_TotalTokens(t *testing.T) {
	config := agenticloop.DefaultContextConfig()
	cm := agenticloop.NewContextManager("loop-1", "gpt-4o", config)

	if cm.TotalTokens() != 0 {
		t.Errorf("TotalTokens() = %d for empty context, want 0", cm.TotalTokens())
	}

	_ = cm.AddMessage(agenticloop.RegionSystemPrompt, agentic.ChatMessage{
		Role: "system", Content: "Hello world",
	})

	if cm.TotalTokens() <= 0 {
		t.Error("TotalTokens() should be > 0 after adding a message")
	}
}

func TestContextManager_ModelLimit(t *testing.T) {
	config := agenticloop.DefaultContextConfig()
	cm := agenticloop.NewContextManager("loop-1", "gpt-4o", config)

	if cm.ModelLimit() != agenticloop.DefaultContextLimit {
		t.Errorf("ModelLimit() = %d, want %d", cm.ModelLimit(), agenticloop.DefaultContextLimit)
	}
}

// Helper function to fill context to approximately target token count
func fillContextToTokens(t *testing.T, cm *agenticloop.ContextManager, targetTokens int) {
	t.Helper()

	// Estimate ~4 chars per token on average
	charsPerToken := 4
	targetChars := targetTokens * charsPerToken

	// Build a large content string
	content := make([]byte, targetChars)
	for i := range content {
		content[i] = 'a' + byte(i%26)
	}

	// Add as single message to recent history
	msg := agentic.ChatMessage{
		Role:    "user",
		Content: string(content),
	}

	if err := cm.AddMessage(agenticloop.RegionRecentHistory, msg); err != nil {
		t.Fatalf("fillContextToTokens() error = %v", err)
	}
}
