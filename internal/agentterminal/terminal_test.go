package agentterminal

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/stretchr/testify/require"
)

func terminalDecoder(t *testing.T) *message.Decoder {
	t.Helper()
	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
	return message.NewDecoder(reg)
}

func terminalEnvelope(t *testing.T, payload message.Payload) []byte {
	t.Helper()
	msg := message.NewBaseMessage(payload.Schema(), payload, "agentic-loop", message.WithTime(time.Unix(1_700_000_000, 0)))
	data, err := json.Marshal(msg)
	require.NoError(t, err)
	return data
}

func mutateEnvelope(t *testing.T, data []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var wire map[string]any
	require.NoError(t, json.Unmarshal(data, &wire))
	mutate(wire)
	mutated, err := json.Marshal(wire)
	require.NoError(t, err)
	return mutated
}

func TestDecodeProductionTerminalMatrix(t *testing.T) {
	at := time.Unix(1_700_000_100, 0).UTC()
	tests := []struct {
		name    string
		payload message.Payload
		class   Class
		state   string
	}{
		{"success", &agentic.LoopCompletedEvent{LoopID: "loop-1", TaskID: "task-1", Outcome: agentic.OutcomeSuccess, Result: "done", CompletedAt: at}, ClassSucceeded, agentic.LoopStateComplete.String()},
		{"failure", &agentic.LoopFailedEvent{LoopID: "loop-1", TaskID: "task-1", Outcome: agentic.OutcomeFailed, Error: "boom", FailedAt: at}, ClassFailed, agentic.LoopStateFailed.String()},
		{"cancellation", &agentic.LoopCancelledEvent{LoopID: "loop-1", TaskID: "task-1", Outcome: agentic.OutcomeCancelled, CancelledBy: "user", CancelledAt: at}, ClassCancelled, agentic.LoopStateCancelled.String()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := Decode(terminalDecoder(t), terminalEnvelope(t, tt.payload))
			require.NoError(t, err)
			require.Equal(t, tt.class, event.Class)
			require.Equal(t, tt.state, event.State)
			require.Equal(t, at, event.TerminalAt)
			require.NotEmpty(t, event.SourceMessageID)
		})
	}
}

func TestDecodeRejectsInvalidEnvelopeAndPayload(t *testing.T) {
	at := time.Unix(1_700_000_100, 0).UTC()
	valid := terminalEnvelope(t, &agentic.LoopCompletedEvent{LoopID: "loop-1", TaskID: "task-1", Outcome: agentic.OutcomeSuccess, CompletedAt: at})
	tests := []struct {
		name   string
		data   []byte
		reason Reason
	}{
		{"empty source id", mutateEnvelope(t, valid, func(w map[string]any) { w["id"] = "" }), ReasonIdentity},
		{"invalid type", mutateEnvelope(t, valid, func(w map[string]any) {
			w["type"] = map[string]any{"domain": "agentic", "category": "", "version": "v1"}
		}), ReasonEnvelope},
		{"invalid metadata", mutateEnvelope(t, valid, func(w map[string]any) { w["meta"] = map[string]any{} }), ReasonEnvelope},
		{"nil payload", mutateEnvelope(t, valid, func(w map[string]any) { w["payload"] = nil }), ReasonPayload},
		{"empty loop", mutateEnvelope(t, valid, func(w map[string]any) { w["payload"].(map[string]any)["loop_id"] = "" }), ReasonPayload},
		{"empty task", mutateEnvelope(t, valid, func(w map[string]any) { w["payload"].(map[string]any)["task_id"] = "" }), ReasonPayload},
		{"zero timestamp", mutateEnvelope(t, valid, func(w map[string]any) { w["payload"].(map[string]any)["completed_at"] = "0001-01-01T00:00:00Z" }), ReasonTimestamp},
		{"flat envelope", []byte(`{"domain":"agentic","category":"loop_completed","version":"v1","payload":{"loop_id":"loop-1","task_id":"task-1","outcome":"success","completed_at":"2023-11-14T22:15:00Z"}}`), ReasonEnvelope},
		{"unregistered", mutateEnvelope(t, valid, func(w map[string]any) { w["type"].(map[string]any)["category"] = "not_registered" }), ReasonEnvelope},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(terminalDecoder(t), tt.data)
			require.Error(t, err)
			require.Equal(t, tt.reason, ErrorReason(err))
		})
	}
}

func TestDecodeRejectsEveryZeroTerminalTimestamp(t *testing.T) {
	tests := []message.Payload{
		&agentic.LoopCompletedEvent{LoopID: "loop-1", TaskID: "task-1", Outcome: agentic.OutcomeSuccess},
		&agentic.LoopFailedEvent{LoopID: "loop-1", TaskID: "task-1", Outcome: agentic.OutcomeFailed},
		&agentic.LoopCancelledEvent{LoopID: "loop-1", TaskID: "task-1", Outcome: agentic.OutcomeCancelled},
	}
	for _, payload := range tests {
		_, err := Decode(terminalDecoder(t), terminalEnvelope(t, payload))
		require.Error(t, err)
		require.Equal(t, ReasonTimestamp, ErrorReason(err))
	}
}

func TestDecodeRejectsNilDecoderAndRegisteredNonterminal(t *testing.T) {
	_, err := Decode(nil, []byte(`{}`))
	require.Error(t, err)
	require.Equal(t, ReasonEnvelope, ErrorReason(err))

	created := &agentic.LoopCreatedEvent{LoopID: "loop-1", TaskID: "task-1", CreatedAt: time.Now()}
	_, err = Decode(terminalDecoder(t), terminalEnvelope(t, created))
	require.Error(t, err)
	require.Equal(t, ReasonPayload, ErrorReason(err))
}

func TestDecodeRejectsEveryCategoryOutcomeCollision(t *testing.T) {
	at := time.Unix(1_700_000_100, 0).UTC()
	for _, outcome := range []string{agentic.OutcomeFailed, agentic.OutcomeCancelled, agentic.OutcomeTruncated, "complete", ""} {
		data := terminalEnvelope(t, &agentic.LoopCompletedEvent{LoopID: "loop-1", TaskID: "task-1", Outcome: agentic.OutcomeSuccess, CompletedAt: at})
		data = mutateEnvelope(t, data, func(w map[string]any) { w["payload"].(map[string]any)["outcome"] = outcome })
		_, err := Decode(terminalDecoder(t), data)
		require.Error(t, err, outcome)
		require.Equal(t, ReasonCollision, ErrorReason(err), outcome)
	}
	for _, outcome := range []string{agentic.OutcomeSuccess, agentic.OutcomeCancelled, agentic.OutcomeTruncated, "complete", ""} {
		data := terminalEnvelope(t, &agentic.LoopFailedEvent{LoopID: "loop-1", TaskID: "task-1", Outcome: agentic.OutcomeFailed, FailedAt: at})
		data = mutateEnvelope(t, data, func(w map[string]any) { w["payload"].(map[string]any)["outcome"] = outcome })
		_, err := Decode(terminalDecoder(t), data)
		require.Error(t, err, outcome)
		require.Equal(t, ReasonCollision, ErrorReason(err), outcome)
	}
	for _, outcome := range []string{agentic.OutcomeSuccess, agentic.OutcomeFailed, agentic.OutcomeTruncated, "complete", ""} {
		data := terminalEnvelope(t, &agentic.LoopCancelledEvent{LoopID: "loop-1", TaskID: "task-1", Outcome: agentic.OutcomeCancelled, CancelledAt: at})
		data = mutateEnvelope(t, data, func(w map[string]any) { w["payload"].(map[string]any)["outcome"] = outcome })
		_, err := Decode(terminalDecoder(t), data)
		require.Error(t, err, outcome)
		require.Equal(t, ReasonCollision, ErrorReason(err), outcome)
	}
}
