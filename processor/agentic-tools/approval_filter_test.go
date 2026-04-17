package agentictools

import (
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovalFilter_ApprovesNormalTools(t *testing.T) {
	filter := NewApprovalFilter([]string{"create_rule", "delete_rule"})

	calls := []agentic.ToolCall{
		{ID: "call-1", Name: "bash"},
		{ID: "call-2", Name: "web_search"},
	}

	result, err := filter.FilterToolCalls("loop-1", calls)
	require.NoError(t, err)

	assert.Len(t, result.Approved, 2)
	assert.Empty(t, result.Rejected)
}

func TestApprovalFilter_RejectsApprovalRequired(t *testing.T) {
	filter := NewApprovalFilter([]string{"create_rule"})

	calls := []agentic.ToolCall{
		{ID: "call-1", Name: "create_rule"},
	}

	result, err := filter.FilterToolCalls("loop-1", calls)
	require.NoError(t, err)

	assert.Empty(t, result.Approved)
	assert.Len(t, result.Rejected, 1)
	assert.Contains(t, result.Rejected[0].Reason, ApprovalRequiredPrefix)
	assert.Contains(t, result.Rejected[0].Reason, "create_rule")
}

func TestApprovalFilter_MixedBatch(t *testing.T) {
	filter := NewApprovalFilter([]string{"delete_rule"})

	calls := []agentic.ToolCall{
		{ID: "call-1", Name: "bash"},
		{ID: "call-2", Name: "delete_rule"},
	}

	result, err := filter.FilterToolCalls("loop-1", calls)
	require.NoError(t, err)

	assert.Len(t, result.Approved, 1)
	assert.Equal(t, "bash", result.Approved[0].Name)
	assert.Len(t, result.Rejected, 1)
	assert.Equal(t, "delete_rule", result.Rejected[0].Call.Name)
}

func TestApprovalFilter_EmptyList(t *testing.T) {
	filter := NewApprovalFilter(nil)

	calls := []agentic.ToolCall{
		{ID: "call-1", Name: "create_rule"},
		{ID: "call-2", Name: "bash"},
	}

	result, err := filter.FilterToolCalls("loop-1", calls)
	require.NoError(t, err)

	assert.Len(t, result.Approved, 2)
	assert.Empty(t, result.Rejected)
}

func TestIsApprovalRequired(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		want   bool
	}{
		{
			name:   "approval prefix present",
			reason: ApprovalRequiredPrefix + "Tool 'bash' requires human approval before execution",
			want:   true,
		},
		{
			name:   "normal error reason",
			reason: "tool execution failed: permission denied",
			want:   false,
		},
		{
			name:   "empty string",
			reason: "",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsApprovalRequired(tt.reason)
			assert.Equal(t, tt.want, got)
		})
	}
}
