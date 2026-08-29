// Package processbarrier provides the agentic E2E process-replacement barrier.
//
// This is an operation-specific, test-only NATS protocol. It is not a
// framework payload or recovery mechanism: the evidence stream records actual
// executor entries for assertions, while the Core NATS release subject controls
// only the currently observed invocation.
package processbarrier

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

const (
	// ToolName is intentionally unavailable from production composition roots.
	ToolName = "e2e_process_barrier"

	// EvidenceStream retains ordered executor-entry occurrences across an app
	// replacement. Production code never reads it.
	EvidenceStream = "E2E_PROCESS_BARRIER"

	// EvidenceSubjectPrefix identifies the retained entry protocol.
	EvidenceSubjectPrefix = "e2e.process_barrier.entered."

	// ReleaseSubjectPrefix identifies the ephemeral Core NATS release protocol.
	ReleaseSubjectPrefix = "e2e.process_barrier.release."
)

var attemptSequence atomic.Uint64

// Attempt is one actual executor invocation. CallID is carried in the body as
// the exact correlation authority; subjects use a safe digest token only.
type Attempt struct {
	CallID          string    `json:"call_id"`
	AttemptID       string    `json:"attempt_id"`
	ProcessInstance string    `json:"process_instance"`
	ProcessID       int       `json:"process_id"`
	EnteredAt       time.Time `json:"entered_at"`
}

// Validate rejects malformed evidence and a record for a different call.
func (a Attempt) Validate(callID string) error {
	switch {
	case callID == "":
		return fmt.Errorf("expected call ID is empty")
	case a.CallID != callID:
		return fmt.Errorf("attempt call ID %q does not match %q", a.CallID, callID)
	case a.AttemptID == "":
		return fmt.Errorf("attempt ID is empty")
	case a.ProcessInstance == "":
		return fmt.Errorf("process instance is empty")
	case a.ProcessID <= 0:
		return fmt.Errorf("process ID must be positive")
	case a.EnteredAt.IsZero():
		return fmt.Errorf("entered time is zero")
	default:
		return nil
	}
}

// EvidenceSubject returns the retained evidence subject for callID.
func EvidenceSubject(callID string) string {
	return EvidenceSubjectPrefix + subjectToken(callID)
}

// ReleaseSubject returns the ephemeral release subject for callID.
func ReleaseSubject(callID string) string {
	return ReleaseSubjectPrefix + subjectToken(callID)
}

func subjectToken(callID string) string {
	sum := sha256.Sum256([]byte(callID))
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:]))
}

// Register installs the barrier in the E2E tool registry.
func Register(registry *agentictools.ExecutorRegistry, client *natsclient.Client) error {
	if registry == nil {
		return fmt.Errorf("register process barrier: nil tool registry")
	}
	if client == nil {
		return fmt.Errorf("register process barrier: nil NATS client")
	}
	instance, err := newProcessInstance()
	if err != nil {
		return fmt.Errorf("register process barrier: %w", err)
	}
	if err := registry.RegisterTool(ToolName, &executor{client: client, processInstance: instance}); err != nil {
		return fmt.Errorf("register process barrier: %w", err)
	}
	return nil
}

type executor struct {
	client          *natsclient.Client
	processInstance string
}

func newProcessInstance() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("generate process instance: %w", err)
	}
	return hex.EncodeToString(nonce[:]), nil
}

func (e *executor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name:        ToolName,
		Description: "E2E-only process replacement barrier; unavailable in production binaries.",
		Effect:      agentic.ToolEffectExternal,
		Parameters:  map[string]any{"type": "object"},
	}}
}

func (e *executor) Execute(ctx context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	connection := e.client.GetConnection()
	if connection == nil {
		return agentic.ToolResult{CallID: call.ID}, fmt.Errorf("process barrier NATS connection is nil")
	}
	release, err := connection.SubscribeSync(ReleaseSubject(call.ID))
	if err != nil {
		return agentic.ToolResult{CallID: call.ID}, fmt.Errorf("subscribe process barrier release: %w", err)
	}
	defer release.Unsubscribe() //nolint:errcheck // process exit and caller context also end this test-only subscription
	if err := connection.FlushWithContext(ctx); err != nil {
		return agentic.ToolResult{CallID: call.ID}, fmt.Errorf("flush process barrier release subscription: %w", err)
	}

	pid := os.Getpid()
	attempt := Attempt{
		CallID:          call.ID,
		AttemptID:       fmt.Sprintf("%s/%s/%d", call.ID, e.processInstance, attemptSequence.Add(1)),
		ProcessInstance: e.processInstance,
		ProcessID:       pid,
		EnteredAt:       time.Now().UTC(),
	}
	wire, err := json.Marshal(attempt)
	if err != nil {
		return agentic.ToolResult{CallID: call.ID}, fmt.Errorf("marshal process barrier attempt: %w", err)
	}
	if err := e.client.PublishToStreamWithMsgID(ctx, EvidenceSubject(call.ID), wire, attempt.AttemptID); err != nil {
		return agentic.ToolResult{CallID: call.ID}, fmt.Errorf("publish process barrier attempt: %w", err)
	}
	if _, err := release.NextMsgWithContext(ctx); err != nil {
		return agentic.ToolResult{CallID: call.ID}, fmt.Errorf("wait for process barrier release: %w", err)
	}
	return agentic.ToolResult{
		CallID: call.ID,
		Name:   ToolName,
		Content: fmt.Sprintf(
			"released process barrier attempt %s", attempt.AttemptID,
		),
		Metadata: map[string]any{"attempt_id": attempt.AttemptID},
	}, nil
}
