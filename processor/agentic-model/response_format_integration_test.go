//go:build live_llm

package agenticmodel_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

// Tagged separately from the package's `integration` tier (which uses NATS
// testcontainers via TestMain in model_integration_test.go) because this
// path requires only a local Ollama, no NATS. Mixing the tags would force
// every Ollama probe through the NATS init — wasteful and orthogonal.
//
// Requires a running Ollama instance at localhost:11434. The model defaults
// to qwen3:1.7b but can be overridden with OLLAMA_TEST_MODEL.
//
// Run with: go test -tags live_llm -run TestResponseFormat_Integration \
//                  ./processor/agentic-model/...
//
// Purpose: chunk-3b gate per ADR-034. If Ollama's /v1/chat/completions
// honors response_format with a JSON schema reliably for the model under
// test, chunk 3b (native /api/chat with top-level format field) is
// unnecessary. If reliability is wedge-class for any model semspec deploys,
// chunk 3b becomes a separate ADR.

func requireOllamaLive(t *testing.T) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", "localhost:11434", 2*time.Second)
	if err != nil {
		t.Skip("Ollama not available at localhost:11434, skipping")
	}
	_ = conn.Close()
}

func ollamaIntegrationModel() string {
	if m := os.Getenv("OLLAMA_TEST_MODEL"); m != "" {
		return m
	}
	return "qwen3:1.7b"
}

func newOllamaIntegrationClient(t *testing.T) *agenticmodel.Client {
	t.Helper()
	requireOllamaLive(t)
	modelName := ollamaIntegrationModel()
	client, err := agenticmodel.NewClient(&model.EndpointConfig{
		Provider: "ollama",
		URL:      "http://localhost:11434/v1",
		Model:    modelName,
	})
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	t.Logf("Using Ollama model: %s", modelName)
	return client
}

// TestResponseFormat_Integration_OllamaJSONSchema_ConformantOutput verifies
// that response_format with a JSON schema constrains real Ollama output to
// schema-conformant JSON via the /v1/chat/completions path. Pass = chunk 3b
// (native /api/chat) is not needed for the model under test. Failure here
// is the load-bearing signal that chunk 3b's design pass should start.
func TestResponseFormat_Integration_OllamaJSONSchema_ConformantOutput(t *testing.T) {
	client := newOllamaIntegrationClient(t)

	// Schema deliberately small + strict-mode-conformant: every property in
	// required, additionalProperties: false, no $ref, no anyOf at root.
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"answer": map[string]any{
				"type": "string",
				"enum": []any{"yes", "no", "maybe"},
			},
			"confidence": map[string]any{
				"type":    "number",
				"minimum": 0,
				"maximum": 1,
			},
		},
		"required":             []any{"answer", "confidence"},
		"additionalProperties": false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := client.ChatCompletion(ctx, agentic.AgentRequest{
		RequestID: "rf-integration-1",
		Model:     ollamaIntegrationModel(),
		Messages: []agentic.ChatMessage{
			{Role: "system", Content: "Answer the user's question with a JSON object matching the requested schema."},
			{Role: "user", Content: "Is the sky blue on Earth during a clear day? Reply with answer (yes/no/maybe) and confidence 0..1."},
		},
		ResponseFormat: agentic.NewJSONSchemaFormat("yes_no_maybe", schema),
		// 2048 tokens accommodates qwen3:1.7b's <think> reasoning chain
		// (variable per run); 256 was non-deterministic — the model
		// sometimes finished within budget, sometimes truncated.
		MaxTokens: 2048,
	})
	if err != nil {
		t.Fatalf("ChatCompletion() failed: %v", err)
	}
	if resp.Status != agentic.StatusComplete {
		t.Fatalf("response status = %q, want %q (response: %+v)", resp.Status, agentic.StatusComplete, resp)
	}

	t.Logf("raw content: %q", resp.Message.Content)

	var parsed struct {
		Answer     string  `json:"answer"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(resp.Message.Content), &parsed); err != nil {
		t.Fatalf("response content is not valid JSON conforming to schema: %v\ncontent: %q", err, resp.Message.Content)
	}

	switch parsed.Answer {
	case "yes", "no", "maybe":
	default:
		t.Errorf("answer = %q, want one of [yes, no, maybe] per schema enum", parsed.Answer)
	}
	if parsed.Confidence < 0 || parsed.Confidence > 1 {
		t.Errorf("confidence = %v, want in [0, 1] per schema", parsed.Confidence)
	}
}

// TestResponseFormat_Integration_OllamaJSONObject_ValidJSON confirms the
// json_object mode (bare validity, no schema) returns parseable JSON. Less
// load-bearing than the schema test but documents the second wire path.
func TestResponseFormat_Integration_OllamaJSONObject_ValidJSON(t *testing.T) {
	client := newOllamaIntegrationClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := client.ChatCompletion(ctx, agentic.AgentRequest{
		RequestID: "rf-integration-2",
		Model:     ollamaIntegrationModel(),
		Messages: []agentic.ChatMessage{
			{Role: "system", Content: "Reply with a JSON object."},
			{Role: "user", Content: "Give me a JSON object describing the color blue. Use any fields you like."},
		},
		ResponseFormat: agentic.NewJSONObjectFormat(),
		// 2048 tokens accommodates qwen3:1.7b's <think> reasoning chain
		// before the user-visible JSON answer; 256 was tight enough that
		// length_truncated fired before the JSON emerged.
		MaxTokens: 2048,
	})
	if err != nil {
		t.Fatalf("ChatCompletion() failed: %v", err)
	}
	if resp.Status != agentic.StatusComplete {
		t.Fatalf("response status = %q, want %q (content: %q)", resp.Status, agentic.StatusComplete, resp.Message.Content)
	}

	var generic map[string]any
	if err := json.Unmarshal([]byte(resp.Message.Content), &generic); err != nil {
		t.Fatalf("json_object mode returned invalid JSON: %v\ncontent: %q", err, resp.Message.Content)
	}
	if len(generic) == 0 {
		t.Errorf("json_object mode returned empty object; expected at least one field")
	}
}

// TestResponseFormat_Integration_OllamaNilResponseFormat_BaselineWorks
// confirms the no-constraint baseline still works through the OllamaAdapter
// dispatch. Guards against regressions in chunk 3a's adapter wiring.
func TestResponseFormat_Integration_OllamaNilResponseFormat_BaselineWorks(t *testing.T) {
	client := newOllamaIntegrationClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	resp, err := client.ChatCompletion(ctx, agentic.AgentRequest{
		RequestID: "rf-integration-3",
		Model:     ollamaIntegrationModel(),
		Messages: []agentic.ChatMessage{
			{Role: "user", Content: "Say hello in one word."},
		},
		// 2048 tokens accommodates qwen3:1.7b's <think> reasoning chain;
		// the actual answer is one word but the model thinks first.
		MaxTokens: 2048,
		// ResponseFormat deliberately nil — exercises the chunk 3a no-op path.
	})
	if err != nil {
		t.Fatalf("ChatCompletion() failed: %v", err)
	}
	if resp.Status != agentic.StatusComplete {
		t.Fatalf("response status = %q, want %q", resp.Status, agentic.StatusComplete)
	}
	if resp.Message.Content == "" {
		t.Errorf("baseline call returned empty content; expected a greeting")
	}
}
