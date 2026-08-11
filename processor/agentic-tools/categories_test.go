package agentictools

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetToolCategory_KnownTools(t *testing.T) {
	tests := []struct {
		toolName string
		want     ToolCategory
	}{
		{"bash", CategoryInspect},
		{"web_search", CategoryNetwork},
		{"submit_work", CategoryCore},
		{"graph_query", CategoryKnowledge},
		{"spawn_agent", CategoryOrchestration},
		{"create_rule", CategoryMeta},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			got := GetToolCategory(tt.toolName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetToolCategory_UnknownTool(t *testing.T) {
	got := GetToolCategory("this_tool_does_not_exist")
	assert.Equal(t, CategoryCore, got, "unregistered tool should default to CategoryCore")
}

func TestSliceF2CategoryAliasesFallBackToCore(t *testing.T) {
	for _, name := range []string{"graph_search", "graph_summary"} {
		assert.Equal(t, CategoryCore, GetToolCategory(name))
	}
}

func TestRegisterToolCategory(t *testing.T) {
	const testTool = "test_tool_register_unique_abc123"

	// Confirm the tool is not already registered.
	require.Equal(t, CategoryCore, GetToolCategory(testTool), "precondition: tool should be unknown before registration")

	RegisterToolCategory(testTool, CategoryMeta)

	got := GetToolCategory(testTool)
	assert.Equal(t, CategoryMeta, got)
}

func TestReadOnlyCategories(t *testing.T) {
	ro := ReadOnlyCategories()

	// Categories expected to be present and allowed.
	assert.True(t, ro[CategoryCore], "CategoryCore should be in read-only set")
	assert.True(t, ro[CategoryKnowledge], "CategoryKnowledge should be in read-only set")
	assert.True(t, ro[CategoryNetwork], "CategoryNetwork should be in read-only set")

	// Write-capable or privileged categories must not be present.
	_, hasInspect := ro[CategoryInspect]
	assert.False(t, hasInspect, "CategoryInspect should not be in read-only set")

	_, hasOrchestration := ro[CategoryOrchestration]
	assert.False(t, hasOrchestration, "CategoryOrchestration should not be in read-only set")

	_, hasMeta := ro[CategoryMeta]
	assert.False(t, hasMeta, "CategoryMeta should not be in read-only set")
}

func TestCategoryRegistry_Concurrent(_ *testing.T) {
	const workers = 20
	const opsPerWorker = 50

	var wg sync.WaitGroup
	wg.Add(workers * 2)

	// Concurrent readers.
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				GetToolCategory("bash")
				GetToolCategory("web_search")
				GetToolCategory("submit_work")
			}
		}()
	}

	// Concurrent writers using unique names to avoid cross-test pollution.
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				toolName := "concurrent_test_tool_unique"
				_ = i // suppress unused warning; name intentionally shared to exercise write contention
				RegisterToolCategory(toolName, CategoryNetwork)
			}
		}()
	}

	// No assertions — the race detector validates correctness.
	wg.Wait()
}
