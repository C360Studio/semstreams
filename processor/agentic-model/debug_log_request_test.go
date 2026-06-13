package agenticmodel

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"testing"
)

// debugTestPayload is a stand-in for an outgoing chat request. The Secret
// field stands in for the full conversation body we must NOT dump by
// default (semspec#165 grep-poison + multi-GB volume).
type debugTestPayload struct {
	Model  string `json:"model"`
	Secret string `json:"secret"`
}

func newBufferDebugClient(buf *bytes.Buffer) *Client {
	return &Client{
		logger: slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
}

func TestDebugLogRequest_DefaultOmitsBody(t *testing.T) {
	const secret = "FULL-CONVERSATION-BODY-DO-NOT-DUMP"
	payload := debugTestPayload{Model: "test-model", Secret: secret}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	sum := sha256.Sum256(body)
	wantSha8 := hex.EncodeToString(sum[:])[:8]

	var buf bytes.Buffer
	c := newBufferDebugClient(&buf)
	c.debugLogRequest("req-1", "test-model", 7, &payload)

	out := buf.String()
	if out == "" {
		t.Fatal("expected a debug line, got none")
	}
	if strings.Contains(out, secret) {
		t.Errorf("default debug line leaked the full payload body:\n%s", out)
	}
	for _, want := range []string{
		"payload_bytes=" + strconv.Itoa(len(body)),
		"payload_sha8=" + wantSha8,
		"message_count=7",
		"request_id=req-1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("debug line missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "payload=") {
		t.Errorf("default debug line should not carry a payload= attr:\n%s", out)
	}
}

func TestDebugLogRequest_FullPayloadGated(t *testing.T) {
	const secret = "FULL-CONVERSATION-BODY-WHEN-GATED"
	payload := debugTestPayload{Model: "test-model", Secret: secret}
	body, err := json.Marshal(&payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	t.Setenv(fullPayloadLogEnvVar, "1")

	var buf bytes.Buffer
	c := newBufferDebugClient(&buf)
	c.debugLogRequest("req-2", "test-model", 3, &payload)

	out := buf.String()
	if !strings.Contains(out, secret) {
		t.Errorf("gated debug line should carry the full body but did not:\n%s", out)
	}
	if !strings.Contains(out, "payload_sha8=") {
		t.Errorf("gated debug line should still carry the size/hash summary:\n%s", out)
	}
	// The summary's byte count must describe the same body that got dumped.
	if !strings.Contains(out, "payload_bytes="+strconv.Itoa(len(body))) {
		t.Errorf("gated debug line payload_bytes should equal len(body)=%d:\n%s", len(body), out)
	}
}

func TestDebugLogRequest_GateMustBeExactlyOne(t *testing.T) {
	const secret = "BODY-SHOULD-STAY-HIDDEN"
	payload := debugTestPayload{Model: "test-model", Secret: secret}

	// Any value other than "1" leaves the body redacted — a truthy-looking
	// "true"/"yes" must not accidentally enable the firehose.
	t.Setenv(fullPayloadLogEnvVar, "true")

	var buf bytes.Buffer
	c := newBufferDebugClient(&buf)
	c.debugLogRequest("req-3", "test-model", 1, &payload)

	if strings.Contains(buf.String(), secret) {
		t.Errorf("env=%q should not enable full payload logging:\n%s", "true", buf.String())
	}
}

func TestDebugLogRequest_SkipsWhenDebugDisabled(t *testing.T) {
	payload := debugTestPayload{Model: "test-model", Secret: "x"}

	var buf bytes.Buffer
	c := &Client{
		logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})),
	}
	c.debugLogRequest("req-4", "test-model", 1, &payload)

	if buf.Len() != 0 {
		t.Errorf("expected no output when debug disabled, got:\n%s", buf.String())
	}
}
