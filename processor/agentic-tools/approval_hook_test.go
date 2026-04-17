package agentictools

import (
	"encoding/json"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewComponent_ApprovalFilter_NonEmptyList verifies that NewComponent wires
// a non-nil ApprovalFilter when approval_required contains at least one tool name.
// The filter is the gating mechanism between tool dispatch and NATS execution; its
// presence (or absence) is the contract being tested here.
func TestNewComponent_ApprovalFilter_NonEmptyList(t *testing.T) {
	config := DefaultConfig()
	config.ApprovalRequired = []string{"bash", "delete_rule"}

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	disc, err := NewComponent(rawConfig, component.Dependencies{NATSClient: nil})
	require.NoError(t, err)
	require.NotNil(t, disc)

	comp, ok := disc.(*Component)
	require.True(t, ok, "NewComponent must return *Component")

	assert.NotNil(t, comp.approvalFilter,
		"approvalFilter must be non-nil when approval_required is non-empty")
}

// TestNewComponent_ApprovalFilter_EmptyList verifies that NewComponent leaves
// approvalFilter nil when approval_required is empty, meaning every tool call
// proceeds without an approval gate.
func TestNewComponent_ApprovalFilter_EmptyList(t *testing.T) {
	config := DefaultConfig()
	config.ApprovalRequired = nil

	rawConfig, err := json.Marshal(config)
	require.NoError(t, err)

	disc, err := NewComponent(rawConfig, component.Dependencies{NATSClient: nil})
	require.NoError(t, err)
	require.NotNil(t, disc)

	comp, ok := disc.(*Component)
	require.True(t, ok, "NewComponent must return *Component")

	assert.Nil(t, comp.approvalFilter,
		"approvalFilter must be nil when approval_required is empty")
}
