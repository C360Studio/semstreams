package agentictools

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const completedOutcomeVersion = "v1"

// completedOutcome is the immutable COMPLETED record. There is deliberately
// no claimed/in-progress state: a process crash before this record exists is
// an ambiguous external-effect window governed by the executor's
// operation-specific idempotency contract.
type completedOutcome struct {
	Version     string             `json:"version"`
	ExecutionID string             `json:"execution_id"`
	RequestID   string             `json:"request_id"`
	CallID      string             `json:"call_id"`
	CallOrdinal uint32             `json:"call_ordinal"`
	Fingerprint string             `json:"fingerprint"`
	Result      agentic.ToolResult `json:"result"`
}

type completedOutcomeStore interface {
	Get(context.Context, string) ([]byte, error)
	Create(context.Context, string, []byte) error
}

type jetStreamCompletedOutcomeStore struct{ bucket jetstream.KeyValue }

func (s jetStreamCompletedOutcomeStore) Get(ctx context.Context, key string) ([]byte, error) {
	entry, err := s.bucket.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return entry.Value(), nil
}

func (s jetStreamCompletedOutcomeStore) Create(ctx context.Context, key string, value []byte) error {
	_, err := s.bucket.Create(ctx, key, value)
	return err
}

type irrecoverableOutcomeError struct{ err error }
type outcomeCollisionError struct{ err error }
type ambiguousOutcomeCreateError struct{ err error }

func (e *irrecoverableOutcomeError) Error() string   { return e.err.Error() }
func (e *irrecoverableOutcomeError) Unwrap() error   { return e.err }
func (e *outcomeCollisionError) Error() string       { return e.err.Error() }
func (e *outcomeCollisionError) Unwrap() error       { return e.err }
func (e *ambiguousOutcomeCreateError) Error() string { return e.err.Error() }
func (e *ambiguousOutcomeCreateError) Unwrap() error { return e.err }

func isAmbiguousOutcomeCreateError(err error) bool {
	var target *ambiguousOutcomeCreateError
	return errors.As(err, &target)
}

func isIrrecoverableOutcomeError(err error) bool {
	var target *irrecoverableOutcomeError
	var collision *outcomeCollisionError
	return errors.As(err, &target) || errors.As(err, &collision)
}

func outcomeIdentityDigest(executionID string) string {
	sum := sha256.Sum256([]byte(executionID))
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:]))
}

func toolCallOutcomeKey(executionID string) string {
	return completedOutcomeVersion + "." + outcomeIdentityDigest(executionID)
}

func toolResultMessageID(executionID string) string {
	return "tool-result/" + completedOutcomeVersion + "/" + outcomeIdentityDigest(executionID)
}

func toolApprovalRequiredMessageID(executionID string) string {
	return toolResultMessageID(executionID) + "/approval-required"
}

func toolCallFingerprintV1(call agentic.ToolCall) (string, error) {
	// A purpose-built ordered representation prevents additions to ToolCall's
	// marshaler from silently changing this version. encoding/json sorts map
	// keys recursively, normalizing map insertion order.
	canonical := struct {
		ID          string         `json:"id"`
		Name        string         `json:"name"`
		Arguments   map[string]any `json:"arguments"`
		Metadata    map[string]any `json:"metadata"`
		LoopID      string         `json:"loop_id"`
		TraceID     string         `json:"trace_id"`
		RequestID   string         `json:"request_id"`
		ExecutionID string         `json:"execution_id"`
		CallOrdinal uint32         `json:"call_ordinal"`
		ApprovedBy  string         `json:"approved_by"`
	}{call.ID, call.Name, call.Arguments, call.Metadata, call.LoopID, call.TraceID, call.RequestID, call.ExecutionID, call.CallOrdinal, call.ApprovedBy}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("canonicalize tool call v1: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func newCompletedOutcome(call agentic.ToolCall, result agentic.ToolResult) (completedOutcome, error) {
	if err := validateToolExecutionCorrelation(call); err != nil {
		return completedOutcome{}, err
	}
	fingerprint, err := toolCallFingerprintV1(call)
	if err != nil {
		return completedOutcome{}, err
	}
	if result.CallID != call.ID {
		return completedOutcome{}, fmt.Errorf("tool result call ID %q does not match request %q", result.CallID, call.ID)
	}
	if result.RequestID != call.RequestID || result.ExecutionID != call.ExecutionID || result.CallOrdinal != call.CallOrdinal {
		return completedOutcome{}, fmt.Errorf("tool result execution correlation does not match request")
	}
	return completedOutcome{
		Version: completedOutcomeVersion, ExecutionID: call.ExecutionID, RequestID: call.RequestID,
		CallID: call.ID, CallOrdinal: call.CallOrdinal, Fingerprint: fingerprint, Result: result,
	}, nil
}

func validateToolExecutionCorrelation(call agentic.ToolCall) error {
	if call.RequestID == "" {
		return fmt.Errorf("tool call request_id required")
	}
	if call.ExecutionID == "" {
		return fmt.Errorf("tool call execution_id required")
	}
	if call.CallOrdinal == 0 {
		return fmt.Errorf("tool call call_ordinal must be positive")
	}
	return nil
}

func correlateToolResult(call agentic.ToolCall, result agentic.ToolResult) agentic.ToolResult {
	result.CallID = call.ID
	result.RequestID = call.RequestID
	result.ExecutionID = call.ExecutionID
	result.CallOrdinal = call.CallOrdinal
	result.LoopID = call.LoopID
	result.TraceID = call.TraceID
	return result
}

func marshalCompletedOutcome(outcome completedOutcome) ([]byte, error) {
	return json.Marshal(outcome)
}

func decodeCompletedOutcome(data []byte, call agentic.ToolCall) (completedOutcome, error) {
	var outcome completedOutcome
	if err := json.Unmarshal(data, &outcome); err != nil {
		return completedOutcome{}, &irrecoverableOutcomeError{fmt.Errorf("decode completed outcome: %w", err)}
	}
	wantFingerprint, err := toolCallFingerprintV1(call)
	if err != nil {
		return completedOutcome{}, err
	}
	if outcome.Version != completedOutcomeVersion {
		return completedOutcome{}, &irrecoverableOutcomeError{fmt.Errorf("completed outcome version %q is not %q", outcome.Version, completedOutcomeVersion)}
	}
	if outcome.ExecutionID != call.ExecutionID {
		return completedOutcome{}, &outcomeCollisionError{fmt.Errorf("completed outcome execution ID does not match request")}
	}
	if outcome.RequestID != call.RequestID {
		return completedOutcome{}, &outcomeCollisionError{fmt.Errorf("completed outcome request ID does not match request")}
	}
	if outcome.CallID != call.ID {
		return completedOutcome{}, &outcomeCollisionError{fmt.Errorf("completed outcome call ID does not match request")}
	}
	if outcome.CallOrdinal != call.CallOrdinal {
		return completedOutcome{}, &outcomeCollisionError{fmt.Errorf("completed outcome call ordinal does not match request")}
	}
	if outcome.Fingerprint != wantFingerprint {
		return completedOutcome{}, &outcomeCollisionError{fmt.Errorf("completed outcome fingerprint does not match request")}
	}
	if outcome.Result.CallID != call.ID || outcome.Result.RequestID != call.RequestID ||
		outcome.Result.ExecutionID != call.ExecutionID || outcome.Result.CallOrdinal != call.CallOrdinal {
		return completedOutcome{}, &irrecoverableOutcomeError{fmt.Errorf("completed outcome result correlation does not match request")}
	}
	return outcome, nil
}

func compactTooLargeResult(call agentic.ToolCall) agentic.ToolResult {
	return correlateToolResult(call, agentic.ToolResult{Error: "too_large", ErrorKind: agentic.ToolErrorInternal})
}

func compactPanicResult(call agentic.ToolCall) agentic.ToolResult {
	return correlateToolResult(call, agentic.ToolResult{Error: "tool executor panicked", ErrorKind: agentic.ToolErrorInternal})
}

func marshalToolResult(result agentic.ToolResult) ([]byte, error) {
	resultMsg := message.NewBaseMessage(result.Schema(), &result, "agentic-tools")
	return json.Marshal(resultMsg)
}

func isObservedOversize(err error) bool {
	if errors.Is(err, nats.ErrMaxPayload) || errors.Is(err, jetstream.ErrMaxBytesExceeded) {
		return true
	}
	var apiErr *jetstream.APIError
	return errors.As(err, &apiErr) && apiErr != nil && apiErr.ErrorCode == jetstream.ErrorCode(10054)
}
