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

func TestTrajectoryFactV1StoredEvidenceMetadataIsCoherent(t *testing.T) {
	digest := strings.Repeat("c", 64)
	valid := TrajectoryFactV1{
		SchemaVersion:   TrajectorySchemaV1,
		LoopDigest:      TrajectoryLoopDigest("loop-evidence-validation"),
		AttemptID:       "attempt1",
		AttemptOrdinal:  1,
		Kind:            TrajectoryKindToolCompleted,
		CausalPhase:     TrajectoryPhaseToolResult,
		ObservedAt:      time.Date(2026, 8, 7, 1, 2, 3, 0, time.UTC),
		EvidenceDigest:  digest,
		EvidenceSize:    128,
		EvidenceCapture: TrajectoryEvidenceStored,
		Evidence: &message.StorageReference{
			StorageInstance: "objectstore",
			Key:             TrajectoryEvidenceKeyPrefix + digest,
			ContentType:     TrajectoryEvidenceContentType,
			Size:            128,
		},
	}
	if _, err := valid.CanonicalBytes(); err != nil {
		t.Fatalf("valid stored evidence rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*TrajectoryFactV1)
	}{
		{name: "uppercase digest", mutate: func(fact *TrajectoryFactV1) {
			fact.EvidenceDigest = strings.ToUpper(fact.EvidenceDigest)
			fact.Evidence.Key = TrajectoryEvidenceKeyPrefix + fact.EvidenceDigest
		}},
		{name: "wrong key", mutate: func(fact *TrajectoryFactV1) {
			fact.Evidence.Key = TrajectoryEvidenceKeyPrefix + strings.Repeat("0", 64)
		}},
		{name: "wrong content type", mutate: func(fact *TrajectoryFactV1) {
			fact.Evidence.ContentType = "application/json"
		}},
		{name: "reference size mismatch", mutate: func(fact *TrajectoryFactV1) {
			fact.Evidence.Size++
		}},
		{name: "empty storage instance", mutate: func(fact *TrajectoryFactV1) {
			fact.Evidence.StorageInstance = " "
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fact := valid
			reference := *valid.Evidence
			fact.Evidence = &reference
			test.mutate(&fact)
			if _, err := fact.CanonicalBytes(); err == nil {
				t.Fatal("CanonicalBytes() accepted incoherent stored evidence")
			}
		})
	}
}
