package agentic

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCanonicalTrajectoryEvidencePreservesFullBodyAndIsDigestAddressed(t *testing.T) {
	body := struct {
		Messages []ChatMessage `json:"messages"`
		Result   string        `json:"result"`
	}{
		Messages: []ChatMessage{{Role: "user", Content: strings.Repeat("full-message-", 2048)}},
		Result:   strings.Repeat("full-result-", 4096),
	}

	encoded, digest, key, err := CanonicalTrajectoryEvidence(TrajectoryKindModelRequested, body)
	if err != nil {
		t.Fatalf("CanonicalTrajectoryEvidence() error = %v", err)
	}
	if !bytes.Contains(encoded, []byte(body.Messages[0].Content)) || !bytes.Contains(encoded, []byte(body.Result)) {
		t.Fatal("canonical evidence did not preserve the full body")
	}
	if want := TrajectoryEvidenceKeyPrefix + digest; key != want {
		t.Fatalf("key = %q, want %q", key, want)
	}

	var decoded TrajectoryEvidenceV1
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal evidence: %v", err)
	}
	if decoded.SchemaVersion != TrajectorySchemaV1 || decoded.Kind != TrajectoryKindModelRequested {
		t.Fatalf("unexpected envelope: %#v", decoded)
	}
}
