package wire

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"strings"
	"testing"
)

// fakeReadCloser wraps an io.Reader as an io.ReadCloser.
type fakeReadCloser struct{ io.Reader }

func (fakeReadCloser) Close() error { return nil }

func newStreamFromString(s string) *Stream {
	return newStream(fakeReadCloser{Reader: strings.NewReader(s)}, 0)
}

const basicSSE = `data: {"id":"r1","model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}

data: {"id":"r1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"lo"}}]}

data: {"id":"r1","model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

func TestStream_BasicRecvAndAccumulate(t *testing.T) {
	t.Parallel()
	s := newStreamFromString(basicSSE)
	defer s.Close()

	acc := NewAccumulator()
	for {
		chunk, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if err := acc.Add(chunk); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	msg, finish, _ := acc.Final()
	if got, ok := msg.ContentString(); !ok || got != "Hello" {
		t.Errorf("content got (%q,%v), want (Hello,true)", got, ok)
	}
	if msg.Role != "assistant" {
		t.Errorf("role drift: %q", msg.Role)
	}
	if finish != "stop" {
		t.Errorf("finish_reason: %q", finish)
	}
}

func TestStream_ToolCallAccumulation(t *testing.T) {
	t.Parallel()
	sse := `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"check_flight"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"flight"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":\"AA100\"}"}}]}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	s := newStreamFromString(sse)
	defer s.Close()
	acc := NewAccumulator()
	for {
		c, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if err := acc.Add(c); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	msg, finish, _ := acc.Final()
	if finish != "tool_calls" {
		t.Errorf("finish reason: %q", finish)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("want 1 tool call, got %d", len(msg.ToolCalls))
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "check_flight" {
		t.Errorf("tc id/name drift: %+v", tc)
	}
	if tc.Function.Arguments != `{"flight":"AA100"}` {
		t.Errorf("arguments concat drift: %q", tc.Function.Arguments)
	}
}

func TestStream_GeminiThoughtSignaturePreserved(t *testing.T) {
	t.Parallel()
	sse := `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"f","arguments":"{}"},"extra_content":{"google":{"thought_signature":"SIG-A"}}}]}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	s := newStreamFromString(sse)
	defer s.Close()
	acc := NewAccumulator()
	for {
		c, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if err := acc.Add(c); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	msg, _, _ := acc.Final()
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("want 1 tool call")
	}
	got, ok := msg.ToolCalls[0].Extras["extra_content"]
	if !ok {
		t.Fatalf("extra_content missing on accumulated tool_call: %v", msg.ToolCalls[0].Extras)
	}
	if !strings.Contains(string(got), "SIG-A") {
		t.Errorf("thought_signature lost: %s", got)
	}
}

func TestStream_LastWriterWinsOnExtras(t *testing.T) {
	t.Parallel()
	sse := `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"c1","function":{"name":"f"},"sig":"v1"}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"sig":"v2"}]}}]}

data: [DONE]

`
	s := newStreamFromString(sse)
	defer s.Close()
	acc := NewAccumulator()
	conflicts := 0
	acc.OnConflict = func(_, key string) {
		if key == "sig" {
			conflicts++
		}
	}
	for {
		c, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if err := acc.Add(c); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	msg, _, _ := acc.Final()
	if conflicts != 1 {
		t.Errorf("OnConflict count: got %d want 1", conflicts)
	}
	got := string(msg.ToolCalls[0].Extras["sig"])
	if got != `"v2"` {
		t.Errorf("last-writer-wins drift: got %s want %q", got, `"v2"`)
	}
}

// TestStream_PermutationRobust verifies §2b: arbitrary chunked-frame
// boundaries on the same logical stream produce the same accumulated
// message. We re-split the SSE bytes at random boundaries (fixed seed)
// and assert the final marshaled message is identical.
func TestStream_PermutationRobust(t *testing.T) {
	t.Parallel()
	canonical := accumulateAll(t, basicSSE)

	// Drive the stream byte-by-byte through a chunkedReader to shake
	// out any frame-boundary assumptions.
	for i, seed := range []uint64{1, 7, 42, 99, 1024} {
		_ = i
		rng := rand.New(rand.NewPCG(seed, seed*2+1))
		stream := newStream(fakeReadCloser{Reader: &chunkedReader{src: []byte(basicSSE), rng: rng}}, 0)
		acc := NewAccumulator()
		for {
			c, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("seed %d Recv: %v", seed, err)
			}
			if err := acc.Add(c); err != nil {
				t.Fatalf("seed %d Add: %v", seed, err)
			}
		}
		msg, finish, _ := acc.Final()
		gotJSON, _ := json.Marshal(msg)
		if !bytes.Equal(gotJSON, canonical.json) || finish != canonical.finish {
			t.Errorf("seed %d permutation drift\nwant: %s\n got: %s", seed, canonical.json, gotJSON)
		}
	}
}

type accumResult struct {
	json   []byte
	finish string
}

func accumulateAll(t *testing.T, sse string) accumResult {
	t.Helper()
	s := newStreamFromString(sse)
	defer s.Close()
	acc := NewAccumulator()
	for {
		c, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if err := acc.Add(c); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	msg, finish, _ := acc.Final()
	b, _ := json.Marshal(msg)
	return accumResult{json: b, finish: finish}
}

// chunkedReader returns 1..maxChunk bytes per Read, simulating
// non-deterministic network framing.
type chunkedReader struct {
	src []byte
	off int
	rng *rand.Rand
}

const maxChunk = 17

func (r *chunkedReader) Read(p []byte) (int, error) {
	if r.off >= len(r.src) {
		return 0, io.EOF
	}
	n := 1 + r.rng.IntN(maxChunk)
	n = min(n, len(p), len(r.src)-r.off)
	copy(p, r.src[r.off:r.off+n])
	r.off += n
	return n, nil
}

func TestStream_ToolCallContinuationByID_NoIndex(t *testing.T) {
	t.Parallel()
	// Provider that emits index only on the opening frame; subsequent
	// fragments key on ID. Accumulator must merge into the same slot.
	sse := `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_x","type":"function","function":{"name":"f"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_x","function":{"arguments":"{\"a\":1"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"id":"call_x","function":{"arguments":"}"}}]}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	s := newStreamFromString(sse)
	defer s.Close()
	acc := NewAccumulator()
	for {
		c, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if err := acc.Add(c); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	msg, _, _ := acc.Final()
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("want 1 tool call, got %d", len(msg.ToolCalls))
	}
	if got := msg.ToolCalls[0].Function.Arguments; got != `{"a":1}` {
		t.Errorf("arguments concat with id-fallback drift: %q", got)
	}
}

func TestStream_ToolCallContinuationNoIndexNoID(t *testing.T) {
	t.Parallel()
	// Provider that emits index AND id only on the opening frame.
	// Accumulator must merge continuation fragments into the most
	// recent in-flight call.
	sse := `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_y","type":"function","function":{"name":"f","arguments":"{"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"\"k\":"}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"function":{"arguments":"true}"}}]}}]}

data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]

`
	s := newStreamFromString(sse)
	defer s.Close()
	acc := NewAccumulator()
	for {
		c, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if err := acc.Add(c); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	msg, _, _ := acc.Final()
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("want 1 tool call, got %d", len(msg.ToolCalls))
	}
	if got := msg.ToolCalls[0].Function.Arguments; got != `{"k":true}` {
		t.Errorf("arguments concat with no-index/no-id fallback drift: %q", got)
	}
}

func TestStream_OnConflictSuppressesIdenticalRewrite(t *testing.T) {
	t.Parallel()
	// Provider that re-sends the same thought_signature on every
	// delta of the same logical tool_call. OnConflict must not fire
	// for identical-byte rewrites.
	sse := `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"c1","function":{"name":"f","arguments":"{"},"sig":"identical"}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"}"},"sig":"identical"}]}}]}

data: [DONE]

`
	s := newStreamFromString(sse)
	defer s.Close()
	conflicts := 0
	acc := NewAccumulator()
	acc.OnConflict = func(_, _ string) { conflicts++ }
	for {
		c, err := s.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if err := acc.Add(c); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	if conflicts != 0 {
		t.Errorf("OnConflict fired %d times for identical-byte overwrites; expected 0", conflicts)
	}
}

func TestStream_DoneSentinelEnds(t *testing.T) {
	t.Parallel()
	s := newStreamFromString("data: [DONE]\n\n")
	defer s.Close()
	if _, err := s.Recv(); !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF on [DONE], got %v", err)
	}
}

func TestStream_MalformedJSONReturnsError(t *testing.T) {
	t.Parallel()
	s := newStreamFromString("data: {not json}\n\n")
	defer s.Close()
	if _, err := s.Recv(); err == nil {
		t.Fatal("expected error on malformed json")
	}
}
