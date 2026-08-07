package http

// ADR-044 Phase 4 smoke test. Exercises the http input against
// httptest.Server fixtures, asserting that decoded JSON publishes
// arrive as well-formed BaseMessage envelopes via the stub
// publisher. The publish-side abstraction (publisher interface)
// lets these tests run without testcontainers / NATS.
//
// Coverage targets ADR-044 line 165 — "smoke test against
// httptest.Server decoding to a registered payload type" — plus
// the failure modes most likely to bite operators: 5xx retry, 4xx
// non-retry, bearer / basic auth header shape, POST body
// passthrough, jsonl line splitting.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
)

// stubPublisher captures publishes for assertion. Goroutine-safe.
type stubPublisher struct {
	mu       sync.Mutex
	messages [][]byte
	wantErr  error
}

func (s *stubPublisher) Publish(_ context.Context, _ string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wantErr != nil {
		return s.wantErr
	}
	out := make([]byte, len(data))
	copy(out, data)
	s.messages = append(s.messages, out)
	return nil
}

func (s *stubPublisher) all() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.messages))
	copy(out, s.messages)
	return out
}

func TestHTTPInputCanonicalPortsMatchRuntimeConfiguration(t *testing.T) {
	tests := []struct {
		name         string
		interval     string
		method       string
		wantInterval string
		wantMethod   string
	}{
		{name: "defaults", wantInterval: "30s", wantMethod: MethodGET},
		{name: "custom", interval: "2s", method: MethodPOST, wantInterval: "2s", wantMethod: MethodPOST},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := DefaultConfig()
			config.URL = "https://example.test/events"
			config.Interval = test.interval
			config.Method = test.method
			config.Ports = &component.PortConfig{Outputs: []component.PortDefinition{{
				Name: "events", Config: component.JetStreamPort{StreamName: "EVENTS", Subjects: []string{"events.>"}},
			}}}
			input, err := NewInput(InputDeps{Name: "http", Config: config})
			if err != nil {
				t.Fatal(err)
			}
			ports := input.InputPorts()
			if len(ports) != 2 || ports[0].Name != "http_schedule" || ports[1].Name != "http_source" {
				t.Fatalf("input ports = %#v", ports)
			}
			scheduleFacts, err := ports[0].Facts()
			if err != nil {
				t.Fatal(err)
			}
			if scheduleFacts.Kind() != component.PortKindTimer || scheduleFacts.ResourceID() != "timer:"+test.wantInterval {
				t.Fatalf("schedule facts = %#v", scheduleFacts)
			}
			sourceFacts, err := ports[1].Facts()
			if err != nil {
				t.Fatal(err)
			}
			if sourceFacts.Kind() != component.PortKindHTTPClient || sourceFacts.ResourceID() != "http-client:"+test.wantMethod+":https://example.test/events" {
				t.Fatalf("source facts = %#v", sourceFacts)
			}
			wire, err := json.Marshal(ports[1])
			if err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				Config struct {
					TriggerPort string `json:"trigger_port"`
				} `json:"config"`
			}
			if err := json.Unmarshal(wire, &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Config.TriggerPort != "http_schedule" {
				t.Fatalf("trigger_port = %q", envelope.Config.TriggerPort)
			}
			outputFacts, err := input.OutputPorts()[0].Facts()
			if err != nil {
				t.Fatal(err)
			}
			if outputFacts.Kind() != component.PortKindJetStream {
				t.Fatalf("output kind = %q", outputFacts.Kind())
			}
		})
	}
}

// newTestInput constructs an Input wired to the given server URL
// and stub publisher, skipping Start so tests can drive the
// request / publish pipeline directly via runCycle.
func newTestInput(t *testing.T, cfg Config, pub publisher) *Input {
	t.Helper()
	cfg.Ports = &component.PortConfig{
		Outputs: []component.PortDefinition{
			{Name: "out", Config: component.NATSPort{Subject: "test.http.out"}, Required: true},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config validate: %v", err)
	}
	in, err := NewInput(InputDeps{
		Name:   "http-input-test",
		Config: cfg,
	})
	if err != nil {
		t.Fatalf("NewInput: %v", err)
	}
	in.publisher = pub
	return in
}

// decodeEnvelope parses a published BaseMessage envelope and
// extracts the inner GenericJSON Data. Fails the test if the
// envelope doesn't decode cleanly — the whole point of routing
// through the payload registry is that this works. The wire shape
// matches BaseMessage's wireFormat (see message/base_message.go).
func decodeEnvelope(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var envelope struct {
		ID      string       `json:"id"`
		Type    message.Type `json:"type"`
		Payload struct {
			Data map[string]any `json:"data"`
		} `json:"payload"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("decode envelope: %v\nraw: %s", err, data)
	}
	if envelope.Type.Domain != "core" || envelope.Type.Category != "json" || envelope.Type.Version != "v1" {
		t.Errorf("envelope type: got %+v, want core.json.v1", envelope.Type)
	}
	source, _ := envelope.Meta["source"].(string)
	if source == "" {
		t.Errorf("envelope meta.source should be non-empty, got meta=%v", envelope.Meta)
	}
	return envelope.Payload.Data
}

// TestHTTPInput_GetJSON_DecodesAndPublishes is the canonical
// registry round-trip: the published bytes are decoded via the
// production message.Decoder bound to a payload-registry with all
// first-party payloads registered. This is the test that catches
// envelope drift — if a payload type ever shifts and the
// shape-cast helper silently keeps passing, this test will fail
// loud. (Required by feedback_polymorphic_config_needs_json_roundtrip_test.)
func TestHTTPInput_GetJSON_DecodesAndPublishes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sensor_id":"temp-001","value":23.5}`))
	}))
	defer srv.Close()

	pub := &stubPublisher{}
	in := newTestInput(t, Config{
		URL:     srv.URL,
		Method:  MethodGET,
		Decoder: &DecoderConfig{Mode: DecoderJSON},
	}, pub)

	in.runCycle(context.Background())

	msgs := pub.all()
	if len(msgs) != 1 {
		t.Fatalf("want 1 published message, got %d", len(msgs))
	}

	// Production-path decode: through the payload registry, the
	// same code path downstream consumers use.
	decoder := payloadbuiltins.NewTestDecoder(t)
	baseMsg, err := decoder.Decode(msgs[0])
	if err != nil {
		t.Fatalf("registry decode: %v", err)
	}
	if got := baseMsg.Type(); got.Domain != "core" || got.Category != "json" || got.Version != "v1" {
		t.Errorf("envelope type: got %+v, want core.json.v1", got)
	}
	payload, ok := baseMsg.Payload().(*message.GenericJSONPayload)
	if !ok {
		t.Fatalf("payload: got %T, want *message.GenericJSONPayload", baseMsg.Payload())
	}
	if payload.Data["sensor_id"] != "temp-001" {
		t.Errorf("sensor_id: got %v, want temp-001", payload.Data["sensor_id"])
	}
	if payload.Data["value"] != 23.5 {
		t.Errorf("value: got %v, want 23.5", payload.Data["value"])
	}
}

func TestHTTPInput_BearerAuthHeaderSent(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	in := newTestInput(t, Config{
		URL:    srv.URL,
		Method: MethodGET,
		Auth:   &AuthConfig{Type: AuthBearer, Token: "secret-token"},
	}, &stubPublisher{})

	in.runCycle(context.Background())
	if seen != "Bearer secret-token" {
		t.Errorf("Authorization header: got %q, want %q", seen, "Bearer secret-token")
	}
}

func TestHTTPInput_BasicAuthHeaderSent(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	in := newTestInput(t, Config{
		URL:    srv.URL,
		Method: MethodGET,
		Auth:   &AuthConfig{Type: AuthBasic, Username: "alice", Password: "rabbit"},
	}, &stubPublisher{})

	in.runCycle(context.Background())
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:rabbit"))
	if seen != want {
		t.Errorf("Authorization header: got %q, want %q", seen, want)
	}
}

func TestHTTPInput_RetriesOn5xx(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := hits.Add(1)
		if n < 3 {
			http.Error(w, "still warming up", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"ready":true}`))
	}))
	defer srv.Close()

	pub := &stubPublisher{}
	in := newTestInput(t, Config{
		URL: srv.URL,
		Retry: &RetryConfig{
			MaxAttempts:    5,
			InitialBackoff: "1ms",
			MaxBackoff:     "10ms",
			Multiplier:     2.0,
		},
	}, pub)

	in.runCycle(context.Background())

	if got := hits.Load(); got != 3 {
		t.Errorf("expected 3 server hits (2 retries), got %d", got)
	}
	if len(pub.all()) != 1 {
		t.Errorf("expected 1 published message after retry, got %d", len(pub.all()))
	}
}

func TestHTTPInput_NoRetryOn4xx(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "go away", http.StatusForbidden)
	}))
	defer srv.Close()

	pub := &stubPublisher{}
	in := newTestInput(t, Config{
		URL: srv.URL,
		Retry: &RetryConfig{
			MaxAttempts:    5,
			InitialBackoff: "1ms",
			MaxBackoff:     "10ms",
			Multiplier:     2.0,
		},
	}, pub)

	in.runCycle(context.Background())

	if got := hits.Load(); got != 1 {
		t.Errorf("expected 1 server hit (no retry on 4xx), got %d", got)
	}
	if len(pub.all()) != 0 {
		t.Errorf("expected 0 published messages on 4xx, got %d", len(pub.all()))
	}
}

func TestHTTPInput_JSONLDecoderEmitsPerLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w,
			"{\"id\":1}\n"+
				"\n"+ // blank line — skipped
				"{\"id\":2}\n"+
				"{\"id\":3}\n")
	}))
	defer srv.Close()

	pub := &stubPublisher{}
	in := newTestInput(t, Config{
		URL:     srv.URL,
		Decoder: &DecoderConfig{Mode: DecoderJSONL},
	}, pub)

	in.runCycle(context.Background())

	msgs := pub.all()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 published messages, got %d", len(msgs))
	}
	for i, raw := range msgs {
		data := decodeEnvelope(t, raw)
		want := float64(i + 1)
		if data["id"] != want {
			t.Errorf("message[%d] id: got %v, want %v", i, data["id"], want)
		}
	}
}

func TestHTTPInput_PostSendsBody(t *testing.T) {
	var seen []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var err error
		seen, err = readAll(r)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		_, _ = w.Write([]byte(`{"echo":"ok"}`))
	}))
	defer srv.Close()

	in := newTestInput(t, Config{
		URL:    srv.URL,
		Method: MethodPOST,
		Body:   `{"q":"select * from things"}`,
	}, &stubPublisher{})

	in.runCycle(context.Background())
	if string(seen) != `{"q":"select * from things"}` {
		t.Errorf("body: got %q, want %q", string(seen), `{"q":"select * from things"}`)
	}
}

func TestHTTPInput_MalformedJSONIsCountedButDoesNotPanic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not-actually-json`))
	}))
	defer srv.Close()

	pub := &stubPublisher{}
	in := newTestInput(t, Config{URL: srv.URL}, pub)

	in.runCycle(context.Background()) // must not panic
	if len(pub.all()) != 0 {
		t.Errorf("expected 0 publishes on malformed JSON, got %d", len(pub.all()))
	}
	if in.errCount.Load() == 0 {
		t.Errorf("expected errCount > 0 on malformed JSON")
	}
}

func TestHTTPInput_PublishErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	pub := &stubPublisher{wantErr: errors.New("downstream unavailable")}
	in := newTestInput(t, Config{URL: srv.URL}, pub)
	in.runCycle(context.Background())

	if in.errCount.Load() == 0 {
		t.Errorf("expected errCount > 0 on publish error")
	}
}

// TestHTTPInput_ContextCancellationStopsRetries asserts that
// cancelling the supplied context aborts the backoff sleep
// promptly rather than waiting for the full retry budget.
func TestHTTPInput_ContextCancellationStopsRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	pub := &stubPublisher{}
	in := newTestInput(t, Config{
		URL: srv.URL,
		Retry: &RetryConfig{
			MaxAttempts:    10,
			InitialBackoff: "500ms",
			MaxBackoff:     "5s",
			Multiplier:     2.0,
		},
	}, pub)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	in.runCycle(ctx)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("ctx cancellation should cut retry loop; took %v", elapsed)
	}
}

// readAll drains an io.ReadCloser without bringing in another
// import in the production code.
func readAll(r *http.Request) ([]byte, error) {
	body := r.Body
	if body == nil {
		return nil, nil
	}
	defer body.Close()
	const initialCap = 1 << 16
	buf := make([]byte, 0, initialCap)
	tmp := make([]byte, 4096)
	for {
		n, err := body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			if err.Error() == "EOF" {
				return buf, nil
			}
			return buf, err
		}
	}
}
