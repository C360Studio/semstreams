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

	"github.com/c360studio/semstreams/model/wire/responses"
)

// TestEvent_RoundTrip pins that the Event struct decodes the
// documented Responses event shapes and re-encodes back to
// equivalent JSON. Doc-derived; live-fixture parity gate is in
// types_test.go.
func TestEvent_RoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		check   func(t *testing.T, ev *responses.Event)
	}{
		{
			name: "response.created",
			payload: `{
				"type":"response.created",
				"sequence_number":0,
				"response":{"id":"resp_1","object":"response","model":"gpt-5.5","status":"in_progress","output":[]}
			}`,
			check: func(t *testing.T, ev *responses.Event) {
				if ev.Type != responses.EventTypeResponseCreated {
					t.Errorf("Type = %q, want %q", ev.Type, responses.EventTypeResponseCreated)
				}
				if ev.Response == nil || ev.Response.ID != "resp_1" {
					t.Errorf("Response not decoded; got %+v", ev.Response)
				}
			},
		},
		{
			name: "response.output_item.added (message)",
			payload: `{
				"type":"response.output_item.added",
				"sequence_number":3,
				"output_index":0,
				"item":{"type":"message","id":"msg_1","role":"assistant","status":"in_progress"}
			}`,
			check: func(t *testing.T, ev *responses.Event) {
				if ev.OutputIndex == nil || *ev.OutputIndex != 0 {
					t.Errorf("OutputIndex = %v, want 0", ev.OutputIndex)
				}
				if ev.Item == nil || !ev.Item.IsMessage() {
					t.Errorf("Item not decoded as message; got %+v", ev.Item)
				}
			},
		},
		{
			name: "response.content_part.added",
			payload: `{
				"type":"response.content_part.added",
				"sequence_number":4,
				"item_id":"msg_1",
				"output_index":0,
				"content_index":0,
				"part":{"type":"output_text","text":""}
			}`,
			check: func(t *testing.T, ev *responses.Event) {
				cp, err := ev.ContentPart()
				if err != nil {
					t.Fatalf("ContentPart: %v", err)
				}
				if cp.Type != responses.ContentTypeOutputText {
					t.Errorf("ContentPart.Type = %q, want %q", cp.Type, responses.ContentTypeOutputText)
				}
			},
		},
		{
			name: "response.output_text.delta",
			payload: `{
				"type":"response.output_text.delta",
				"sequence_number":5,
				"item_id":"msg_1",
				"output_index":0,
				"content_index":0,
				"delta":"Hello "
			}`,
			check: func(t *testing.T, ev *responses.Event) {
				if ev.Delta != "Hello " {
					t.Errorf("Delta = %q, want %q", ev.Delta, "Hello ")
				}
			},
		},
		{
			name: "response.function_call_arguments.delta",
			payload: `{
				"type":"response.function_call_arguments.delta",
				"sequence_number":6,
				"item_id":"fc_1",
				"output_index":1,
				"delta":"{\"a\":17"
			}`,
			check: func(t *testing.T, ev *responses.Event) {
				if ev.Delta != `{"a":17` {
					t.Errorf("Delta = %q, want %q", ev.Delta, `{"a":17`)
				}
			},
		},
		{
			name: "response.reasoning_summary_text.delta",
			payload: `{
				"type":"response.reasoning_summary_text.delta",
				"sequence_number":2,
				"item_id":"rs_1",
				"output_index":0,
				"summary_index":0,
				"delta":"considering "
			}`,
			check: func(t *testing.T, ev *responses.Event) {
				if ev.Delta != "considering " {
					t.Errorf("Delta = %q, want %q", ev.Delta, "considering ")
				}
				if ev.SummaryIndex == nil || *ev.SummaryIndex != 0 {
					t.Errorf("SummaryIndex = %v, want 0", ev.SummaryIndex)
				}
			},
		},
		{
			name: "response.reasoning_summary_part.added",
			payload: `{
				"type":"response.reasoning_summary_part.added",
				"sequence_number":1,
				"item_id":"rs_1",
				"output_index":0,
				"summary_index":0,
				"part":{"type":"summary_text","text":""}
			}`,
			check: func(t *testing.T, ev *responses.Event) {
				sp, err := ev.SummaryPart()
				if err != nil {
					t.Fatalf("SummaryPart: %v", err)
				}
				if sp.Type != responses.SummaryTypeText {
					t.Errorf("SummaryPart.Type = %q, want %q", sp.Type, responses.SummaryTypeText)
				}
			},
		},
		{
			name: "response.completed",
			payload: `{
				"type":"response.completed",
				"sequence_number":99,
				"response":{"id":"resp_1","object":"response","model":"gpt-5.5","status":"completed","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello world"}]}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}
			}`,
			check: func(t *testing.T, ev *responses.Event) {
				if ev.Response == nil || ev.Response.Status != "completed" {
					t.Fatalf("Response not decoded; got %+v", ev.Response)
				}
				if len(ev.Response.Output) != 1 {
					t.Errorf("Output count = %d, want 1", len(ev.Response.Output))
				}
			},
		},
		{
			name: "error",
			payload: `{
				"type":"error",
				"sequence_number":1,
				"error":{"type":"rate_limit","code":"rate_limit_exceeded","message":"slow down"}
			}`,
			check: func(t *testing.T, ev *responses.Event) {
				ae, err := ev.APIError()
				if err != nil {
					t.Fatalf("APIError: %v", err)
				}
				if ae.Code != "rate_limit_exceeded" {
					t.Errorf("APIError.Code = %q, want rate_limit_exceeded", ae.Code)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ev responses.Event
			if err := json.Unmarshal([]byte(tc.payload), &ev); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			tc.check(t, &ev)
			// Re-encode, decode again, confirm Type stable.
			b, err := json.Marshal(ev)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var roundtrip responses.Event
			if err := json.Unmarshal(b, &roundtrip); err != nil {
				t.Fatalf("re-decode: %v", err)
			}
			if roundtrip.Type != ev.Type {
				t.Errorf("Type drift: original=%q roundtrip=%q", ev.Type, roundtrip.Type)
			}
		})
	}
}

// TestEvent_HelperWrongType pins that ContentPart/SummaryPart/
// APIError refuse to decode against the wrong event Type, returning
// a clear error rather than silently producing garbage.
func TestEvent_HelperWrongType(t *testing.T) {
	ev := &responses.Event{Type: responses.EventTypeOutputTextDelta, Part: json.RawMessage(`{}`)}
	if _, err := ev.ContentPart(); err == nil {
		t.Error("ContentPart on wrong type: expected error, got nil")
	}
	if _, err := ev.SummaryPart(); err == nil {
		t.Error("SummaryPart on wrong type: expected error, got nil")
	}
	if _, err := ev.APIError(); err == nil {
		t.Error("APIError on wrong type: expected error, got nil")
	}
}

// TestEvent_UnknownTypeForwardCompat pins that an event with an
// unknown Type decodes without error and the unknown fields
// (if any) end up zero — the caller can iterate forward without
// the stream halting. Future API extensions stay non-breaking.
func TestEvent_UnknownTypeForwardCompat(t *testing.T) {
	payload := `{"type":"response.future_thing.added","sequence_number":42,"something_new":"ignored"}`
	var ev responses.Event
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ev.Type != "response.future_thing.added" {
		t.Errorf("Type = %q, want response.future_thing.added", ev.Type)
	}
	if ev.SequenceNumber != 42 {
		t.Errorf("SequenceNumber = %d, want 42", ev.SequenceNumber)
	}
}

// TestStream_DrivesCannedSSE pins the wire-level parser: a body
// containing canned SSE frames decodes into the corresponding Events
// in order and EOFs at end of body.
func TestStream_DrivesCannedSSE(t *testing.T) {
	body := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_1","object":"response","model":"gpt-5.5","status":"in_progress","output":[]}}`,
		"",
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","sequence_number":1,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"Hi"}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","sequence_number":2,"response":{"id":"resp_1","object":"response","model":"gpt-5.5","status":"completed","output":[{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hi"}]}],"usage":{"input_tokens":3,"output_tokens":1,"total_tokens":4}}}`,
		"",
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	c, err := responses.NewClient(responses.ClientConfig{
		BaseURL:    srv.URL + "/v1",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	stream, err := c.ResponsesStream(context.Background(), &responses.Request{Model: "gpt-5.5"})
	if err != nil {
		t.Fatalf("ResponsesStream: %v", err)
	}
	defer stream.Close()

	var events []*responses.Event
	for {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		events = append(events, ev)
	}
	wantTypes := []string{
		responses.EventTypeResponseCreated,
		responses.EventTypeOutputTextDelta,
		responses.EventTypeResponseCompleted,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("event count = %d, want %d", len(events), len(wantTypes))
	}
	for i, ev := range events {
		if ev.Type != wantTypes[i] {
			t.Errorf("events[%d].Type = %q, want %q", i, ev.Type, wantTypes[i])
		}
	}
}

// TestStream_APIErrorOnNon2xx pins that ResponsesStream returns
// *APIError on non-2xx, with the stream nil, matching the
// ChatCompletionStream contract.
func TestStream_APIErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"type":"rate_limit","code":"rate_limit_exceeded","message":"slow down"}}`)
	}))
	defer srv.Close()

	c, _ := responses.NewClient(responses.ClientConfig{
		BaseURL:    srv.URL + "/v1",
		HTTPClient: srv.Client(),
	})
	stream, err := c.ResponsesStream(context.Background(), &responses.Request{Model: "gpt-5.5"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if stream != nil {
		t.Error("expected nil stream on error")
	}
	var apiErr *responses.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", apiErr.StatusCode)
	}
}

// TestStream_NilRequest pins the explicit nil-request guard.
func TestStream_NilRequest(t *testing.T) {
	c, _ := responses.NewClient(responses.ClientConfig{
		BaseURL:    "http://unused",
		HTTPClient: http.DefaultClient,
	})
	if _, err := c.ResponsesStream(context.Background(), nil); err == nil {
		t.Error("expected error on nil request, got nil")
	}
}

// TestAccumulator_BuildsFromDeltas pins the accumulator's incremental
// fallback: a stream of output_text.delta events without a terminal
// lifecycle event still yields a usable Response with accumulated
// text in the right slots.
func TestAccumulator_BuildsFromDeltas(t *testing.T) {
	acc := responses.NewAccumulator()

	idx0 := 0
	cidx0 := 0
	msgItem := responses.OutputItem{
		Type: responses.ItemTypeMessage,
		ID:   "msg_1",
		Role: responses.RoleAssistant,
	}
	emptyTextPart := json.RawMessage(`{"type":"output_text","text":""}`)

	events := []*responses.Event{
		{
			Type:        responses.EventTypeOutputItemAdded,
			OutputIndex: &idx0,
			Item:        &msgItem,
		},
		{
			Type:         responses.EventTypeContentPartAdded,
			ItemID:       "msg_1",
			OutputIndex:  &idx0,
			ContentIndex: &cidx0,
			Part:         emptyTextPart,
		},
		{
			Type:         responses.EventTypeOutputTextDelta,
			ItemID:       "msg_1",
			OutputIndex:  &idx0,
			ContentIndex: &cidx0,
			Delta:        "Hello ",
		},
		{
			Type:         responses.EventTypeOutputTextDelta,
			ItemID:       "msg_1",
			OutputIndex:  &idx0,
			ContentIndex: &cidx0,
			Delta:        "world",
		},
	}
	for _, ev := range events {
		if err := acc.Add(ev); err != nil {
			t.Fatalf("Add(%s): %v", ev.Type, err)
		}
	}
	resp := acc.Final()
	if resp == nil {
		t.Fatal("Final() returned nil")
	}
	if resp.Status != "incomplete" {
		t.Errorf("Status = %q, want incomplete (no terminal event)", resp.Status)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("Output count = %d, want 1", len(resp.Output))
	}
	got := resp.Output[0].OutputText()
	if got != "Hello world" {
		t.Errorf("OutputText = %q, want %q", got, "Hello world")
	}
}

// TestAccumulator_TerminalLifecyclePromotes pins that a
// response.completed event with a fully-populated Response is
// returned by Final regardless of prior incremental state.
func TestAccumulator_TerminalLifecyclePromotes(t *testing.T) {
	acc := responses.NewAccumulator()

	final := &responses.Response{
		ID:     "resp_x",
		Model:  "gpt-5.5",
		Status: "completed",
		Output: []responses.OutputItem{
			{
				Type: responses.ItemTypeMessage,
				ID:   "msg_x",
				Role: responses.RoleAssistant,
				Content: []responses.ContentPart{
					{Type: responses.ContentTypeOutputText, Text: "final answer"},
				},
			},
		},
	}
	if err := acc.Add(&responses.Event{
		Type:     responses.EventTypeResponseCompleted,
		Response: final,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got := acc.Final()
	if got == nil {
		t.Fatal("Final() nil")
	}
	if got.ID != "resp_x" || got.Status != "completed" {
		t.Errorf("Final response not promoted; got %+v", got)
	}
	if got.Output[0].OutputText() != "final answer" {
		t.Errorf("OutputText = %q, want final answer", got.Output[0].OutputText())
	}
}

// TestAccumulator_BuildsFunctionCall pins that streaming
// function_call_arguments deltas accumulate into the OutputItem's
// Arguments string, and an output_item.added carrier sets the
// Type/CallID/Name before deltas arrive.
func TestAccumulator_BuildsFunctionCall(t *testing.T) {
	acc := responses.NewAccumulator()

	idx1 := 1
	fcItem := responses.OutputItem{
		Type:   responses.ItemTypeFunctionCall,
		ID:     "fc_1",
		CallID: "call_abc",
		Name:   "multiply",
	}
	events := []*responses.Event{
		{
			Type:        responses.EventTypeOutputItemAdded,
			OutputIndex: &idx1,
			Item:        &fcItem,
		},
		{
			Type:        responses.EventTypeFunctionCallArgumentsDelta,
			ItemID:      "fc_1",
			OutputIndex: &idx1,
			Delta:       `{"a":17,`,
		},
		{
			Type:        responses.EventTypeFunctionCallArgumentsDelta,
			ItemID:      "fc_1",
			OutputIndex: &idx1,
			Delta:       `"b":23}`,
		},
	}
	for _, ev := range events {
		if err := acc.Add(ev); err != nil {
			t.Fatalf("Add(%s): %v", ev.Type, err)
		}
	}
	resp := acc.Final()
	if resp == nil || len(resp.Output) != 1 {
		t.Fatalf("Final() did not yield 1 output item; got %+v", resp)
	}
	item := resp.Output[0]
	if !item.IsFunctionCall() {
		t.Errorf("item.Type = %q, want function_call", item.Type)
	}
	if item.CallID != "call_abc" {
		t.Errorf("CallID = %q, want call_abc", item.CallID)
	}
	if item.Name != "multiply" {
		t.Errorf("Name = %q, want multiply", item.Name)
	}
	if item.Arguments != `{"a":17,"b":23}` {
		t.Errorf("Arguments = %q, want %q", item.Arguments, `{"a":17,"b":23}`)
	}
}

// TestAccumulator_BuildsReasoningSummary pins reasoning summary
// streaming: summary_part.added + summary_text.delta accumulate
// into the item's Summary array.
func TestAccumulator_BuildsReasoningSummary(t *testing.T) {
	acc := responses.NewAccumulator()

	idx0 := 0
	sidx0 := 0
	rsItem := responses.OutputItem{
		Type:             responses.ItemTypeReasoning,
		ID:               "rs_1",
		EncryptedContent: "opaque-blob",
	}
	emptySummaryPart := json.RawMessage(`{"type":"summary_text","text":""}`)
	events := []*responses.Event{
		{
			Type:        responses.EventTypeOutputItemAdded,
			OutputIndex: &idx0,
			Item:        &rsItem,
		},
		{
			Type:         responses.EventTypeReasoningSummaryPartAdded,
			ItemID:       "rs_1",
			OutputIndex:  &idx0,
			SummaryIndex: &sidx0,
			Part:         emptySummaryPart,
		},
		{
			Type:         responses.EventTypeReasoningSummaryTextDelta,
			ItemID:       "rs_1",
			OutputIndex:  &idx0,
			SummaryIndex: &sidx0,
			Delta:        "considering ",
		},
		{
			Type:         responses.EventTypeReasoningSummaryTextDelta,
			ItemID:       "rs_1",
			OutputIndex:  &idx0,
			SummaryIndex: &sidx0,
			Delta:        "multiplication",
		},
	}
	for _, ev := range events {
		if err := acc.Add(ev); err != nil {
			t.Fatalf("Add(%s): %v", ev.Type, err)
		}
	}
	resp := acc.Final()
	if resp == nil || len(resp.Output) != 1 {
		t.Fatalf("Final() did not yield 1 output item; got %+v", resp)
	}
	item := resp.Output[0]
	if !item.IsReasoning() {
		t.Errorf("item.Type = %q, want reasoning", item.Type)
	}
	if item.EncryptedContent != "opaque-blob" {
		t.Errorf("EncryptedContent = %q, want opaque-blob", item.EncryptedContent)
	}
	if len(item.Summary) != 1 {
		t.Fatalf("Summary count = %d, want 1", len(item.Summary))
	}
	if item.Summary[0].Text != "considering multiplication" {
		t.Errorf("Summary[0].Text = %q, want %q", item.Summary[0].Text, "considering multiplication")
	}
}

// TestAccumulator_RejectsNilAndMissingFields pins the
// programming-error guards.
func TestAccumulator_RejectsNilAndMissingFields(t *testing.T) {
	acc := responses.NewAccumulator()
	if err := acc.Add(nil); err == nil {
		t.Error("Add(nil): expected error, got nil")
	}
	// Delta event without output_index — accumulator can't slot the
	// delta, must error.
	if err := acc.Add(&responses.Event{
		Type:  responses.EventTypeOutputTextDelta,
		Delta: "X",
	}); err == nil {
		t.Error("output_text.delta without output_index: expected error, got nil")
	}
}

// TestClient_Responses_StreamingRejected stays as a regression guard
// — the non-streaming Responses() method must reject req.Stream=true
// to make the misuse loud.
func TestClient_Responses_StreamingRejected(t *testing.T) {
	c, _ := responses.NewClient(responses.ClientConfig{
		BaseURL:    "http://unused",
		HTTPClient: http.DefaultClient,
	})
	_, err := c.Responses(context.Background(), &responses.Request{
		Model:  "gpt-5.5",
		Stream: true,
	})
	if err == nil || !strings.Contains(err.Error(), "use ResponsesStream") {
		t.Errorf("expected streaming-rejected error, got %v", err)
	}
}
