package agenticmodel_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/model"
	agenticmodel "github.com/c360studio/semstreams/processor/agentic-model"
)

// captureRequestServer returns an httptest server that captures the
// inbound request body as a generic map and replies with a minimal
// successful chat completion. Use this when the test only needs to
// inspect the wire shape of the request.
func captureRequestServer(t *testing.T) (*httptest.Server, func() map[string]any) {
	t.Helper()
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "rf-test", "object": "chat.completion",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 1},
		})
	}))
	return server, func() map[string]any { return captured }
}

func TestBuildChatRequest_ResponseFormatNil_OmitsField(t *testing.T) {
	server, getCaptured := captureRequestServer(t)
	defer server.Close()

	client, err := agenticmodel.NewClient(&model.EndpointConfig{URL: server.URL, Model: "test"})
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	_, err = client.ChatCompletion(context.Background(), agentic.AgentRequest{
		RequestID: "rf-nil",
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "hi"}},
		Model:     "test",
		// ResponseFormat deliberately nil.
	})
	if err != nil {
		t.Fatalf("ChatCompletion() failed: %v", err)
	}

	captured := getCaptured()
	if _, exists := captured["response_format"]; exists {
		t.Errorf("response_format should be absent when nil; got: %v", captured["response_format"])
	}
}

func TestBuildChatRequest_ResponseFormatJSONSchema_WireShape(t *testing.T) {
	server, getCaptured := captureRequestServer(t)
	defer server.Close()

	client, err := agenticmodel.NewClient(&model.EndpointConfig{URL: server.URL, Model: "test"})
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []any{"fan_out", "synthesize"},
			},
			"args": map[string]any{"type": "object"},
		},
		"required":             []any{"action", "args"},
		"additionalProperties": false,
	}

	_, err = client.ChatCompletion(context.Background(), agentic.AgentRequest{
		RequestID:      "rf-schema",
		Messages:       []agentic.ChatMessage{{Role: "user", Content: "hi"}},
		Model:          "test",
		ResponseFormat: agentic.NewJSONSchemaFormat("decide_action", schema),
	})
	if err != nil {
		t.Fatalf("ChatCompletion() failed: %v", err)
	}

	rfRaw, ok := getCaptured()["response_format"]
	if !ok {
		t.Fatal("response_format missing from captured request")
	}
	rf, ok := rfRaw.(map[string]any)
	if !ok {
		t.Fatalf("response_format not a JSON object; got %T", rfRaw)
	}

	if got := rf["type"]; got != "json_schema" {
		t.Errorf("response_format.type = %v, want \"json_schema\"", got)
	}

	jsRaw, ok := rf["json_schema"]
	if !ok {
		t.Fatal("response_format.json_schema missing")
	}
	js, ok := jsRaw.(map[string]any)
	if !ok {
		t.Fatalf("response_format.json_schema not a JSON object; got %T", jsRaw)
	}
	if got := js["name"]; got != "decide_action" {
		t.Errorf("response_format.json_schema.name = %v, want \"decide_action\"", got)
	}
	if got := js["strict"]; got != true {
		t.Errorf("response_format.json_schema.strict = %v, want true", got)
	}
	schemaWire, ok := js["schema"].(map[string]any)
	if !ok {
		t.Fatalf("response_format.json_schema.schema not a JSON object; got %T", js["schema"])
	}
	if got := schemaWire["type"]; got != "object" {
		t.Errorf("schema.type = %v, want \"object\"", got)
	}
	if got := schemaWire["additionalProperties"]; got != false {
		t.Errorf("schema.additionalProperties = %v, want false", got)
	}
	if _, ok := schemaWire["properties"]; !ok {
		t.Error("schema.properties missing on the wire")
	}
}

func TestBuildChatRequest_ResponseFormatJSONObject_WireShape(t *testing.T) {
	server, getCaptured := captureRequestServer(t)
	defer server.Close()

	client, err := agenticmodel.NewClient(&model.EndpointConfig{URL: server.URL, Model: "test"})
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	_, err = client.ChatCompletion(context.Background(), agentic.AgentRequest{
		RequestID:      "rf-object",
		Messages:       []agentic.ChatMessage{{Role: "user", Content: "hi"}},
		Model:          "test",
		ResponseFormat: agentic.NewJSONObjectFormat(),
	})
	if err != nil {
		t.Fatalf("ChatCompletion() failed: %v", err)
	}

	rfRaw, ok := getCaptured()["response_format"]
	if !ok {
		t.Fatal("response_format missing from captured request")
	}
	rf, ok := rfRaw.(map[string]any)
	if !ok {
		t.Fatalf("response_format not a JSON object; got %T", rfRaw)
	}
	if got := rf["type"]; got != "json_object" {
		t.Errorf("response_format.type = %v, want \"json_object\"", got)
	}
	if _, exists := rf["json_schema"]; exists {
		t.Errorf("response_format.json_schema should be absent for json_object mode; got: %v", rf["json_schema"])
	}
}

func TestBuildChatRequest_ResponseFormatSchemaMarshalFailure_DropsSchema(t *testing.T) {
	server, getCaptured := captureRequestServer(t)
	defer server.Close()

	client, err := agenticmodel.NewClient(&model.EndpointConfig{URL: server.URL, Model: "test"})
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	// chan int cannot be JSON-marshaled. The plumbing layer drops the
	// schema (sets it to null on the wire) and is expected to emit a Warn
	// log; the request still proceeds so the operator sees the upstream
	// 400 alongside the local Warn for fast root-cause identification.
	_, err = client.ChatCompletion(context.Background(), agentic.AgentRequest{
		RequestID: "rf-bad-schema",
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "hi"}},
		Model:     "test",
		ResponseFormat: &agentic.ResponseFormat{
			Type:   agentic.ResponseFormatJSONSchema,
			Name:   "broken_schema",
			Schema: map[string]any{"unmarshalable": make(chan int)},
			Strict: true,
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion() failed: %v", err)
	}

	rf, ok := getCaptured()["response_format"].(map[string]any)
	if !ok {
		t.Fatal("response_format missing or wrong shape")
	}
	js, ok := rf["json_schema"].(map[string]any)
	if !ok {
		t.Fatal("json_schema missing or wrong shape")
	}
	if got, exists := js["schema"]; !exists || got != nil {
		t.Errorf("schema = %v (exists=%v), want explicit null on wire after marshal failure", got, exists)
	}
	// Name + strict should still come through — only the schema body is dropped.
	if got := js["name"]; got != "broken_schema" {
		t.Errorf("name = %v, want %q", got, "broken_schema")
	}
}

func TestBuildChatRequest_ResponseFormatStrictFalse_PreservedOnWire(t *testing.T) {
	server, getCaptured := captureRequestServer(t)
	defer server.Close()

	client, err := agenticmodel.NewClient(&model.EndpointConfig{URL: server.URL, Model: "test"})
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	_, err = client.ChatCompletion(context.Background(), agentic.AgentRequest{
		RequestID: "rf-strict-false",
		Messages:  []agentic.ChatMessage{{Role: "user", Content: "hi"}},
		Model:     "test",
		ResponseFormat: &agentic.ResponseFormat{
			Type:   agentic.ResponseFormatJSONSchema,
			Name:   "permissive_compat",
			Schema: map[string]any{"type": "object"},
			Strict: false,
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion() failed: %v", err)
	}

	rf := getCaptured()["response_format"].(map[string]any)
	js := rf["json_schema"].(map[string]any)
	if got := js["strict"]; got != false {
		t.Errorf("response_format.json_schema.strict = %v, want false (explicit opt-out preserved)", got)
	}
}
