package agentic_test

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/stretchr/testify/require"
)

func priorMessagesWire(t testing.TB, history string, user bool) []byte {
	t.Helper()
	var payload message.Payload = &agentic.TaskMessage{
		LoopID: "8d4f916a-85c2-4c35-b57a-5f97bb5b0864", TaskID: "task-history",
		Role: "general", Model: "model", Prompt: "current question",
	}
	if user {
		payload = &agentic.UserMessage{
			MessageID: "source-history", ChannelType: "http", ChannelID: "channel", UserID: "user",
			Content: "current question",
		}
	}
	data, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "history-test"))
	require.NoError(t, err)
	var envelope map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &envelope))
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(envelope["payload"], &fields))
	if history != "" {
		fields["prior_messages"] = json.RawMessage(history)
	}
	envelope["payload"], err = json.Marshal(fields)
	require.NoError(t, err)
	data, err = json.Marshal(envelope)
	require.NoError(t, err)
	return data
}

// spec: agentic-loop / A task carries its prior conversational input
func TestPriorMessagesProductionGrammar(t *testing.T) {
	for _, tc := range []struct {
		name, history string
		valid         bool
	}{
		{"missing", "", true}, {"null", "null", true}, {"empty", "[]", true},
		{"ordered repeated roles", `[{"role":"assistant","content":" displayed answer "},{"role":"user","content":"same"},{"role":"user","content":"same"}]`, true},
		{"whitespace is nonempty text", `[{"role":"user","content":" "}]`, true},
		{"system", `[{"role":"system","content":"text"}]`, false},
		{"developer", `[{"role":"developer","content":"text"}]`, false},
		{"tool", `[{"role":"tool","content":"text"}]`, false},
		{"unknown role", `[{"role":"USER","content":"text"}]`, false},
		{"empty content", `[{"role":"user","content":""}]`, false},
		{"name", `[{"role":"user","content":"text","name":"caller"}]`, false},
		{"reasoning", `[{"role":"assistant","content":"text","reasoning_content":"private"}]`, false},
		{"reasoning alias", `[{"role":"assistant","content":"text","reasoning":"private"}]`, false},
		{"tool calls", `[{"role":"assistant","content":"text","tool_calls":[{"id":"call","name":"tool"}]}]`, false},
		{"tool call ID", `[{"role":"assistant","content":"text","tool_call_id":"call"}]`, false},
		{"tool error", `[{"role":"assistant","content":"text","is_error":true}]`, false},
		{"reasoning records", `[{"role":"assistant","content":"text","reasoning_records":[{}]}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decoder := payloadbuiltins.NewTestDecoder(t)
			decoded, err := decoder.Decode(priorMessagesWire(t, tc.history, false))
			if err == nil {
				err = decoded.Validate()
			}
			if !tc.valid {
				require.ErrorContains(t, err, "prior_messages")
				// Routable user input must survive decoding to receive a negative response.
				_, err = decoder.Decode(priorMessagesWire(t, tc.history, true))
				require.NoError(t, err)
				return
			}
			require.NoError(t, err)
			wire, err := json.Marshal(decoded)
			require.NoError(t, err)
			var envelope map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(wire, &envelope))
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(envelope["payload"], &fields))
			if tc.history == "" || tc.history == "null" || tc.history == "[]" {
				require.NotContains(t, fields, "prior_messages")
			} else {
				require.JSONEq(t, tc.history, string(fields["prior_messages"]))
			}
		})
	}
}

// spec: agentic-loop / A task carries its prior conversational input
func FuzzPriorMessagesProductionGrammar(f *testing.F) {
	for _, role := range []string{"user", "assistant", "system", "developer", "tool", "", "USER"} {
		f.Add(role, "displayed text", uint8(0))
	}
	f.Add("user", "", uint8(0))
	for flag := uint8(1); flag <= 7; flag++ {
		f.Add("assistant", "displayed answer", flag)
	}
	f.Fuzz(func(t *testing.T, role, content string, extra uint8) {
		entry := map[string]any{"role": role, "content": content}
		field := []string{"", "name", "reasoning_content", "reasoning", "tool_calls", "tool_call_id", "is_error", "reasoning_records"}[extra%8]
		switch field {
		case "":
		case "tool_calls", "reasoning_records":
			entry[field] = []map[string]any{{}}
		case "is_error":
			entry[field] = true
		default:
			entry[field] = "execution-only"
		}
		history, err := json.Marshal([]any{entry})
		require.NoError(t, err)
		decoder := payloadbuiltins.NewTestDecoder(t)
		decoded, err := decoder.Decode(priorMessagesWire(t, string(history), false))
		if err == nil {
			err = decoded.Validate()
		}
		valid := (role == "user" || role == "assistant") && content != "" && field == ""
		if !valid {
			require.ErrorContains(t, err, "prior_messages")
			return
		}
		require.NoError(t, err)
		wire, err := json.Marshal(decoded)
		require.NoError(t, err)
		_, err = decoder.Decode(wire)
		require.NoError(t, err)
	})
}
