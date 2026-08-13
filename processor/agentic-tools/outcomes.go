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
// an ambiguous external-effect window and the executor must use ToolCall.ID as
// its downstream idempotency key.
type completedOutcome struct {
	Version     string             `json:"version"`
	CallID      string             `json:"call_id"`
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

func (e *irrecoverableOutcomeError) Error() string { return e.err.Error() }
func (e *irrecoverableOutcomeError) Unwrap() error { return e.err }
func (e *outcomeCollisionError) Error() string     { return e.err.Error() }
func (e *outcomeCollisionError) Unwrap() error     { return e.err }

func isIrrecoverableOutcomeError(err error) bool {
	var target *irrecoverableOutcomeError
	var collision *outcomeCollisionError
	return errors.As(err, &target) || errors.As(err, &collision)
}

func outcomeIdentityDigest(callID string) string {
	sum := sha256.Sum256([]byte(callID))
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:]))
}

func toolCallOutcomeKey(callID string) string {
	return completedOutcomeVersion + "." + outcomeIdentityDigest(callID)
}

func toolResultMessageID(callID string) string {
	return "tool-result/" + completedOutcomeVersion + "/" + outcomeIdentityDigest(callID)
}

func toolApprovalRequiredMessageID(callID string) string {
	return toolResultMessageID(callID) + "/approval-required"
}

func toolCallFingerprintV1(call agentic.ToolCall) (string, error) {
	// A purpose-built ordered representation prevents additions to ToolCall's
	// marshaler from silently changing this version. encoding/json sorts map
	// keys recursively, normalizing map insertion order.
	canonical := struct {
		ID         string         `json:"id"`
		Name       string         `json:"name"`
		Arguments  map[string]any `json:"arguments"`
		Metadata   map[string]any `json:"metadata"`
		LoopID     string         `json:"loop_id"`
		TraceID    string         `json:"trace_id"`
		ApprovedBy string         `json:"approved_by"`
	}{call.ID, call.Name, call.Arguments, call.Metadata, call.LoopID, call.TraceID, call.ApprovedBy}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("canonicalize tool call v1: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func newCompletedOutcome(call agentic.ToolCall, result agentic.ToolResult) (completedOutcome, error) {
	fingerprint, err := toolCallFingerprintV1(call)
	if err != nil {
		return completedOutcome{}, err
	}
	if result.CallID != call.ID {
		return completedOutcome{}, fmt.Errorf("tool result call ID %q does not match request %q", result.CallID, call.ID)
	}
	return completedOutcome{
		Version: completedOutcomeVersion, CallID: call.ID, Fingerprint: fingerprint, Result: result,
	}, nil
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
	if outcome.CallID != call.ID {
		return completedOutcome{}, &outcomeCollisionError{fmt.Errorf("completed outcome call ID does not match request")}
	}
	if outcome.Fingerprint != wantFingerprint {
		return completedOutcome{}, &outcomeCollisionError{fmt.Errorf("completed outcome fingerprint does not match request")}
	}
	if outcome.Result.CallID != call.ID {
		return completedOutcome{}, &irrecoverableOutcomeError{fmt.Errorf("completed outcome result correlation does not match request")}
	}
	return outcome, nil
}

func compactTooLargeResult(call agentic.ToolCall) agentic.ToolResult {
	return agentic.ToolResult{
		CallID:    call.ID,
		Error:     "too_large",
		ErrorKind: agentic.ToolErrorInternal,
		LoopID:    call.LoopID,
		TraceID:   call.TraceID,
	}
}

func compactPanicResult(call agentic.ToolCall) agentic.ToolResult {
	return agentic.ToolResult{
		CallID:    call.ID,
		Error:     "tool executor panicked",
		ErrorKind: agentic.ToolErrorInternal,
		LoopID:    call.LoopID,
		TraceID:   call.TraceID,
	}
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
