package agenticdispatch

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockIntentClassifier provides deterministic classification for testing.
type mockIntentClassifier struct {
	result *ClassifiedIntent
	err    error
}

func (m *mockIntentClassifier) Classify(_ context.Context, msg agentic.UserMessage, _ []*LoopInfo) (*ClassifiedIntent, error) {
	if m.err != nil {
		return nil, m.err
	}
	result := *m.result
	result.Content = msg.Content
	return &result, nil
}

func TestIntentType_Constants(t *testing.T) {
	// Verify intent types are distinct
	types := []IntentType{IntentNewTask, IntentContinue, IntentSignal, IntentQuestion, IntentMeta}
	seen := make(map[IntentType]bool)
	for _, it := range types {
		assert.False(t, seen[it], "duplicate intent type: %s", it)
		seen[it] = true
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "clean json",
			input: `{"type": "new_task", "confidence": 0.9}`,
			want:  `{"type": "new_task", "confidence": 0.9}`,
		},
		{
			name:  "json in markdown code block",
			input: "```json\n{\"type\": \"signal\", \"signal_type\": \"approve\"}\n```",
			want:  `{"type": "signal", "signal_type": "approve"}`,
		},
		{
			name:  "json with surrounding text",
			input: `Here is the classification: {"type": "continue", "loop_id": "loop_abc"} based on my analysis.`,
			want:  `{"type": "continue", "loop_id": "loop_abc"}`,
		},
		{
			name:  "no json",
			input: "This is just text with no JSON",
			want:  "",
		},
		{
			name:  "nested json",
			input: `{"type": "meta", "details": {"reason": "help"}}`,
			want:  `{"type": "meta", "details": {"reason": "help"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMockIntentClassifier_NewTask(t *testing.T) {
	classifier := &mockIntentClassifier{
		result: &ClassifiedIntent{
			Type:       IntentNewTask,
			Confidence: 0.9,
		},
	}

	msg := agentic.UserMessage{
		MessageID:   "msg-1",
		UserID:      "user-1",
		Content:     "Build a REST API for user authentication",
		ChannelType: "cli",
	}

	intent, err := classifier.Classify(context.Background(), msg, nil)
	require.NoError(t, err)
	assert.Equal(t, IntentNewTask, intent.Type)
	assert.Equal(t, 0.9, intent.Confidence)
	assert.Equal(t, msg.Content, intent.Content)
}

func TestMockIntentClassifier_ContinueWithLoop(t *testing.T) {
	classifier := &mockIntentClassifier{
		result: &ClassifiedIntent{
			Type:       IntentContinue,
			LoopID:     "loop_abc12345",
			Confidence: 0.85,
		},
	}

	activeLoops := []*LoopInfo{
		{
			LoopID:    "loop_abc12345",
			UserID:    "user-1",
			State:     "executing",
			CreatedAt: time.Now(),
		},
	}

	msg := agentic.UserMessage{
		MessageID:   "msg-2",
		UserID:      "user-1",
		Content:     "actually make it use JWT tokens instead",
		ChannelType: "web",
	}

	intent, err := classifier.Classify(context.Background(), msg, activeLoops)
	require.NoError(t, err)
	assert.Equal(t, IntentContinue, intent.Type)
	assert.Equal(t, "loop_abc12345", intent.LoopID)
}

func TestMockIntentClassifier_Signal(t *testing.T) {
	classifier := &mockIntentClassifier{
		result: &ClassifiedIntent{
			Type:       IntentSignal,
			LoopID:     "loop_xyz",
			SignalType: agentic.SignalApprove,
			Confidence: 0.95,
		},
	}

	msg := agentic.UserMessage{
		MessageID:   "msg-3",
		UserID:      "user-1",
		Content:     "looks good, approve it",
		ChannelType: "web",
	}

	intent, err := classifier.Classify(context.Background(), msg, nil)
	require.NoError(t, err)
	assert.Equal(t, IntentSignal, intent.Type)
	assert.Equal(t, agentic.SignalApprove, intent.SignalType)
	assert.Equal(t, "loop_xyz", intent.LoopID)
}

func TestMockIntentClassifier_FallbackOnError(t *testing.T) {
	classifier := &mockIntentClassifier{
		err: assert.AnError,
	}

	msg := agentic.UserMessage{
		MessageID: "msg-4",
		UserID:    "user-1",
		Content:   "some message",
	}

	_, err := classifier.Classify(context.Background(), msg, nil)
	assert.Error(t, err)
}
