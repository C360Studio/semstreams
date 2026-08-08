package agentic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const (
	// TrajectoryEvidenceKeyPrefix makes logical evidence keys content-addressed.
	TrajectoryEvidenceKeyPrefix = "trajectory-evidence/v1/sha256/"
	// TrajectoryEvidenceContentType is stamped into backend-neutral references.
	TrajectoryEvidenceContentType = "application/vnd.semstreams.agentic-trajectory-evidence.v1+json"
)

// TrajectoryEvidenceV1 contains the full semantic event body for one observation.
type TrajectoryEvidenceV1 struct {
	SchemaVersion string          `json:"schema_version"`
	Kind          TrajectoryKind  `json:"kind"`
	Body          json.RawMessage `json:"body"`
}

// CanonicalTrajectoryEvidence deterministically encodes and addresses a full body.
func CanonicalTrajectoryEvidence(kind TrajectoryKind, body any) (encoded []byte, digest, key string, err error) {
	if !kind.known() {
		return nil, "", "", fmt.Errorf("unknown trajectory evidence kind %q", kind)
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, "", "", fmt.Errorf("encode trajectory evidence body: %w", err)
	}
	envelope := TrajectoryEvidenceV1{
		SchemaVersion: TrajectorySchemaV1,
		Kind:          kind,
		Body:          bodyBytes,
	}
	encoded, err = json.Marshal(envelope)
	if err != nil {
		return nil, "", "", fmt.Errorf("encode trajectory evidence: %w", err)
	}
	sum := sha256.Sum256(encoded)
	digest = hex.EncodeToString(sum[:])
	return encoded, digest, TrajectoryEvidenceKeyPrefix + digest, nil
}
