package agentic

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
)

func TestTrajectoryFactV1CanonicalBytesAreBoundedAndBodyFree(t *testing.T) {
	fact := TrajectoryFactV1{
		SchemaVersion:     TrajectorySchemaV1,
		LoopDigest:        TrajectoryLoopDigest("org.platform.agent.loop"),
		AttemptID:         strings.Repeat("a", 32),
		AttemptOrdinal:    42,
		Kind:              TrajectoryKindToolCompleted,
		SourceKind:        TrajectorySourceToolCall,
		SourceCorrelation: strings.Repeat("b", 64),
		CausalIteration:   12,
		CausalPhase:       TrajectoryPhaseToolResult,
		CausalOrdinal:     99,
		ObservedAt:        time.Date(2026, 8, 7, 1, 2, 3, 4, time.UTC),
		ElapsedMS:         1234,
		Status:            TrajectoryStatusFailed,
		TokensIn:          1,
		TokensOut:         2,
		MessageCount:      3,
		ToolCount:         4,
		URLCount:          5,
		ModelPreview:      strings.Repeat("model", 2000),
		ProviderPreview:   strings.Repeat("provider", 2000),
		ToolPreview:       strings.Repeat("tool", 2000),
		CapabilityPreview: strings.Repeat("capability", 2000),
		ErrorCategory:     TrajectoryErrorUnknown,
		EvidenceDigest:    strings.Repeat("c", 64),
		EvidenceSize:      1024,
		Evidence: &message.StorageReference{
			StorageInstance: "objectstore",
			Key:             TrajectoryEvidenceKeyPrefix + strings.Repeat("c", 64),
			ContentType:     TrajectoryEvidenceContentType,
			Size:            1024,
		},
		EvidenceCapture: TrajectoryEvidenceStored,
	}

	got, err := fact.CanonicalBytes()
	if err != nil {
		t.Fatalf("CanonicalBytes() error = %v", err)
	}
	if len(got) >= TrajectoryFactMaxBytes {
		t.Fatalf("encoded fact size = %d, want < %d", len(got), TrajectoryFactMaxBytes)
	}
	for _, forbidden := range []string{"messages", "arguments", "result", "prompt", "response", "metadata"} {
		if strings.Contains(string(got), `"`+forbidden+`"`) {
			t.Fatalf("fact contains forbidden body field %q: %s", forbidden, got)
		}
	}
	var roundTrip TrajectoryFactV1
	if err := json.Unmarshal(got, &roundTrip); err != nil {
		t.Fatalf("unmarshal canonical fact: %v", err)
	}
	if roundTrip.ModelPreview == fact.ModelPreview {
		t.Fatal("oversized preview was not bounded")
	}
}

func TestTrajectoryFactKeyHashesExternalLoopIdentity(t *testing.T) {
	loopID := "customer.raw.loop.id.with.secrets"
	attemptID := strings.Repeat("d", 32)
	key, err := TrajectoryFactKey(loopID, attemptID)
	if err != nil {
		t.Fatalf("TrajectoryFactKey() error = %v", err)
	}
	if strings.Contains(key, loopID) {
		t.Fatalf("key exposes raw loop ID: %q", key)
	}
	if !strings.HasSuffix(key, "."+attemptID) {
		t.Fatalf("key = %q, want attempt ID suffix", key)
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z') && !(r >= '2' && r <= '7') && !(r >= '0' && r <= '9') && r != '.' {
			t.Fatalf("key contains non-NATS-safe rune %q", r)
		}
	}
}
