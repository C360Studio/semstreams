package agentic

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// spec: agentic-dispatch / Prior messages accompany an independent chat turn
func TestUserMessage_RetiredTargetPresenceDoesNotPersistAcrossDecode(t *testing.T) {
	const valid = `"message_id":"message","channel_type":"http","channel_id":"channel","user_id":"user","content":"hello"`
	var msg UserMessage
	for _, field := range []string{`"reply_to":null`, `"REPLY_TO":false`, `"reply_\u0074o":""`, `"reply_to":[],"reply_to":null`} {
		require.NoError(t, json.Unmarshal([]byte(`{`+valid+`,`+field+`}`), &msg), "decoding must preserve the response route")
		require.ErrorContains(t, msg.Validate(), "reply_to")
		require.Equal(t, "channel", msg.ChannelID)
		encoded, err := json.Marshal(&msg)
		require.NoError(t, err)
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(encoded, &fields))
		require.NotContains(t, fields, "reply_to", "neither retired target nor presence becomes serialized state")
		require.NotContains(t, msg.RuleFields(), "reply_to")
		require.NoError(t, json.Unmarshal([]byte(`{`+valid+`}`), &msg))
		require.NoError(t, msg.Validate(), "a later omitted key must clear rejection metadata")
	}
	_, present := reflect.TypeFor[UserMessage]().FieldByName("ReplyTo")
	require.False(t, present, "the exported attachment target is retired")
}
