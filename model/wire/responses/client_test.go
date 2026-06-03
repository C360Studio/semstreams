package responses_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/model/wire"
	"github.com/c360studio/semstreams/model/wire/responses"
)

// TestClient_Responses_SuccessfulCall asserts the happy path:
// non-streaming Responses() POSTs the marshaled request to the
// server, decodes a documented Response envelope, and returns it.
func TestClient_Responses_SuccessfulCall(t *testing.T) {
	var capturedBody string
	var capturedAuth string
	var capturedContentType string
	var capturedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		capturedContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{
			"id":"resp_test_1",
			"object":"response",
			"created_at":1717070000,
			"status":"completed",
			"model":"gpt-5.5",
			"output":[
				{"type":"message","id":"msg_1","status":"completed","role":"assistant",
				 "content":[{"type":"output_text","text":"hello"}]}
			],
			"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}
		}`)
	}))
	defer srv.Close()

	c, err := responses.NewClient(responses.ClientConfig{
		BaseURL:    srv.URL + "/v1",
		HTTPClient: srv.Client(),
		AuthHeader: "Bearer sk-test",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	req := &responses.Request{
		Model: "gpt-5.5",
		Input: []responses.InputItem{
			responses.NewInputUserMessage("hello world"),
		},
	}
	resp, err := c.Responses(context.Background(), req)
	if err != nil {
		t.Fatalf("Responses: %v", err)
	}

	if capturedPath != "/v1/responses" {
		t.Errorf("path = %q, want /v1/responses", capturedPath)
	}
	if capturedAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", capturedAuth)
	}
	if capturedContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", capturedContentType)
	}
	if !strings.Contains(capturedBody, `"model":"gpt-5.5"`) {
		t.Errorf("request body missing model: %s", capturedBody)
	}
	if !strings.Contains(capturedBody, `"type":"input_text"`) {
		t.Errorf("request body missing input_text content part: %s", capturedBody)
	}

	if resp.ID != "resp_test_1" {
		t.Errorf("resp.ID = %q, want resp_test_1", resp.ID)
	}
	if resp.Status != "completed" {
		t.Errorf("resp.Status = %q, want completed", resp.Status)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("len(resp.Output) = %d, want 1", len(resp.Output))
	}
	if !resp.Output[0].IsMessage() {
		t.Errorf("Output[0] is not a message; type=%q", resp.Output[0].Type)
	}
	if got := resp.Output[0].OutputText(); got != "hello" {
		t.Errorf("OutputText = %q, want hello", got)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 13 {
		t.Errorf("Usage = %+v, want TotalTokens=13", resp.Usage)
	}
}

// TestClient_Responses_APIError asserts non-2xx returns *APIError
// with the status code recorded. Body shape mirrors ChatCompletion's
// standard error envelope per ADR-051 ("Same envelope shape").
func TestClient_Responses_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"type":"invalid_request","code":"missing_param","message":"input is required"}}`)
	}))
	defer srv.Close()

	c, err := responses.NewClient(responses.ClientConfig{
		BaseURL:    srv.URL + "/v1",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = c.Responses(context.Background(), &responses.Request{Model: "gpt-5.5"})
	if err == nil {
		t.Fatal("Responses: expected error, got nil")
	}
	var apiErr *responses.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Type != "invalid_request" {
		t.Errorf("Type = %q, want invalid_request", apiErr.Type)
	}
	if apiErr.Code != "missing_param" {
		t.Errorf("Code = %q, want missing_param", apiErr.Code)
	}
	// errors.As should also work against *wire.APIError since
	// responses.APIError is a type alias.
	var wireErr *wire.APIError
	if !errors.As(err, &wireErr) {
		t.Errorf("errors.As against *wire.APIError failed; type-alias not transparent")
	}
}

// TestNewClient_Validation pins the config validation surface.
func TestNewClient_Validation(t *testing.T) {
	if _, err := responses.NewClient(responses.ClientConfig{}); err == nil {
		t.Error("NewClient with empty config: expected error, got nil")
	}
	if _, err := responses.NewClient(responses.ClientConfig{BaseURL: "x"}); err == nil {
		t.Error("NewClient without HTTPClient: expected error, got nil")
	}
	if _, err := responses.NewClient(responses.ClientConfig{HTTPClient: http.DefaultClient}); err == nil {
		t.Error("NewClient without BaseURL or ResponsesURL: expected error, got nil")
	}
}

// TestClient_Responses_NilRequest pins the explicit nil-request guard.
func TestClient_Responses_NilRequest(t *testing.T) {
	c, _ := responses.NewClient(responses.ClientConfig{
		BaseURL:    "http://unused",
		HTTPClient: http.DefaultClient,
	})
	_, err := c.Responses(context.Background(), nil)
	if err == nil {
		t.Error("expected error on nil request; got nil")
	}
}

// TestClient_Responses_ResponsesURLOverride pins that
// ResponsesURL takes precedence over BaseURL when set.
func TestClient_Responses_ResponsesURLOverride(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"r","object":"response","model":"m","output":[]}`)
	}))
	defer srv.Close()

	c, err := responses.NewClient(responses.ClientConfig{
		HTTPClient:   srv.Client(),
		ResponsesURL: srv.URL + "/custom/responses",
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.Responses(context.Background(), &responses.Request{Model: "m"}); err != nil {
		t.Fatalf("Responses: %v", err)
	}
	if capturedPath != "/custom/responses" {
		t.Errorf("path = %q, want /custom/responses", capturedPath)
	}
}

// TestClient_Responses_ExtraHeaders pins per-request header
// propagation.
func TestClient_Responses_ExtraHeaders(t *testing.T) {
	var capturedXFoo string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedXFoo = r.Header.Get("X-Foo")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"r","object":"response","model":"m","output":[]}`)
	}))
	defer srv.Close()

	hdrs := http.Header{}
	hdrs.Set("X-Foo", "bar")
	c, _ := responses.NewClient(responses.ClientConfig{
		BaseURL:      srv.URL + "/v1",
		HTTPClient:   srv.Client(),
		ExtraHeaders: hdrs,
	})
	if _, err := c.Responses(context.Background(), &responses.Request{Model: "m"}); err != nil {
		t.Fatalf("Responses: %v", err)
	}
	if capturedXFoo != "bar" {
		t.Errorf("X-Foo = %q, want bar", capturedXFoo)
	}
}

// TestRequest_OmitsZeroValues pins that the zero-value request
// produces a compact JSON body — no leaking `"max_output_tokens":0`,
// no `"store":false` when unset, etc. The pointer-typed fields on
// Request are what make this work. Snapshot comparison (not
// substring containment) avoids false matches against content-part
// fields with the same names.
func TestRequest_OmitsZeroValues(t *testing.T) {
	in := &responses.Request{
		Model: "gpt-5.5",
		Input: []responses.InputItem{
			responses.NewInputUserMessage("hi"),
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`
	if string(b) != want {
		t.Errorf("zero-value request did not match snapshot\n  got:  %s\n  want: %s", string(b), want)
	}
}
