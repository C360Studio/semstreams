package agenticloop

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/agentic"
)

const toolExecutionIdentityVersion = "v1"

func stampToolExecutionCorrelation(requestID string, calls []agentic.ToolCall) error {
	if requestID == "" {
		return fmt.Errorf("tool execution request_id required")
	}
	for i := range calls {
		ordinal := uint32(i + 1)
		if calls[i].ID == "" {
			return fmt.Errorf("tool execution provider call_id required at ordinal %d", ordinal)
		}
		calls[i].RequestID = requestID
		calls[i].CallOrdinal = ordinal
		calls[i].ExecutionID = deriveToolExecutionID(requestID, calls[i].ID, ordinal)
	}
	return nil
}

func deriveToolExecutionID(requestID, callID string, ordinal uint32) string {
	hash := sha256.New()
	writeIdentityPart(hash, requestID)
	writeIdentityPart(hash, callID)
	var ordinalBytes [4]byte
	binary.BigEndian.PutUint32(ordinalBytes[:], ordinal)
	_, _ = hash.Write(ordinalBytes[:])
	digest := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(hash.Sum(nil)))
	return "tool-exec-" + toolExecutionIdentityVersion + "-" + digest
}

type identityHashWriter interface {
	Write([]byte) (int, error)
}

func writeIdentityPart(dst identityHashWriter, value string) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = dst.Write(size[:])
	_, _ = dst.Write([]byte(value))
}
