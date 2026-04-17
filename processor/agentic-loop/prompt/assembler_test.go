package prompt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssemble_DefaultFragments(t *testing.T) {
	reg := NewRegistry()
	reg.AddAll(DefaultFragments())

	result := Assemble(reg, &AssemblyContext{
		Role: "general",
	})

	assert.Contains(t, result.SystemMessage, "AI agent")
	assert.Contains(t, result.SystemMessage, "general-purpose agent")
	assert.Contains(t, result.FragmentsUsed, "system-identity")
	assert.Contains(t, result.FragmentsUsed, "role-general")
	// Should NOT contain other roles
	assert.NotContains(t, result.SystemMessage, "architect agent")
	assert.NotContains(t, result.SystemMessage, "explore agent")
}

func TestAssemble_RoleFiltering(t *testing.T) {
	reg := NewRegistry()
	reg.AddAll(DefaultFragments())

	tests := []struct {
		role     string
		contains string
		excludes string
	}{
		{"architect", "architect agent", "editor agent"},
		{"editor", "editor agent", "architect agent"},
		{"reviewer", "reviewer agent", "editor agent"},
		{"explorer", "explore agent", "general-purpose"},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			result := Assemble(reg, &AssemblyContext{Role: tt.role})
			assert.Contains(t, result.SystemMessage, tt.contains)
			assert.NotContains(t, result.SystemMessage, tt.excludes)
		})
	}
}

func TestAssemble_CategoryOrdering(t *testing.T) {
	reg := NewRegistry()
	reg.Add(Fragment{ID: "c-constraints", Category: CategoryConstraints, Content: "CONSTRAINTS"})
	reg.Add(Fragment{ID: "a-system", Category: CategorySystem, Content: "SYSTEM"})
	reg.Add(Fragment{ID: "b-role", Category: CategoryRole, Content: "ROLE"})

	result := Assemble(reg, &AssemblyContext{})

	systemIdx := strings.Index(result.SystemMessage, "SYSTEM")
	roleIdx := strings.Index(result.SystemMessage, "ROLE")
	constraintIdx := strings.Index(result.SystemMessage, "CONSTRAINTS")

	require.Greater(t, roleIdx, systemIdx, "role should come after system")
	require.Greater(t, constraintIdx, roleIdx, "constraints should come after role")
}

func TestAssemble_IterationBudget(t *testing.T) {
	reg := NewRegistry()
	reg.AddAll(DefaultFragments())

	// Early iterations — no budget message
	result := Assemble(reg, &AssemblyContext{
		Role:          "general",
		Iteration:     2,
		MaxIterations: 20,
	})
	assert.NotContains(t, result.SystemMessage, "URGENT")
	assert.NotContains(t, result.SystemMessage, "wrapping up")

	// Past 50% — nudge
	result = Assemble(reg, &AssemblyContext{
		Role:          "general",
		Iteration:     12,
		MaxIterations: 20,
	})
	assert.Contains(t, result.SystemMessage, "wrapping up")

	// Past 75% — urgent
	result = Assemble(reg, &AssemblyContext{
		Role:          "general",
		Iteration:     18,
		MaxIterations: 20,
	})
	assert.Contains(t, result.SystemMessage, "URGENT")
}

func TestAssemble_ChildAgentConstraint(t *testing.T) {
	reg := NewRegistry()
	reg.AddAll(DefaultFragments())

	// No parent — no child constraint
	result := Assemble(reg, &AssemblyContext{Role: "general"})
	assert.NotContains(t, result.SystemMessage, "child agent")

	// With parent — includes child constraint
	result = Assemble(reg, &AssemblyContext{
		Role:         "general",
		ParentLoopID: "loop_parent_123",
		Depth:        1,
		MaxDepth:     3,
	})
	assert.Contains(t, result.SystemMessage, "child agent")
	assert.Contains(t, result.SystemMessage, "loop_parent_123")
}

func TestAssemble_ConditionGating(t *testing.T) {
	reg := NewRegistry()
	reg.Add(Fragment{
		ID:       "conditional",
		Category: CategoryDomain,
		Content:  "GATED CONTENT",
		Condition: func(ctx *AssemblyContext) bool {
			return ctx.WorkflowSlug == "special"
		},
	})

	// Not matching
	result := Assemble(reg, &AssemblyContext{WorkflowSlug: "other"})
	assert.NotContains(t, result.SystemMessage, "GATED CONTENT")

	// Matching
	result = Assemble(reg, &AssemblyContext{WorkflowSlug: "special"})
	assert.Contains(t, result.SystemMessage, "GATED CONTENT")
}

func TestAssemble_EmptyContentSkipped(t *testing.T) {
	reg := NewRegistry()
	reg.Add(Fragment{ID: "empty", Category: CategorySystem, Content: ""})
	reg.Add(Fragment{ID: "nonempty", Category: CategorySystem, Content: "HELLO"})

	result := Assemble(reg, &AssemblyContext{})
	assert.Equal(t, "HELLO", result.SystemMessage)
	assert.NotContains(t, result.FragmentsUsed, "empty")
}

func TestAssemble_ContentFuncOverridesContent(t *testing.T) {
	reg := NewRegistry()
	reg.Add(Fragment{
		ID:       "dynamic",
		Category: CategorySystem,
		Content:  "STATIC",
		ContentFunc: func(ctx *AssemblyContext) string {
			return "DYNAMIC for " + ctx.Role
		},
	})

	result := Assemble(reg, &AssemblyContext{Role: "editor"})
	assert.Contains(t, result.SystemMessage, "DYNAMIC for editor")
	assert.NotContains(t, result.SystemMessage, "STATIC")
}

func TestRegistry_ThreadSafe(_ *testing.T) {
	reg := NewRegistry()
	done := make(chan struct{})

	// Concurrent writes
	go func() {
		for i := range 100 {
			reg.Add(Fragment{ID: strings.Repeat("a", i+1), Category: CategorySystem, Content: "content"})
		}
		close(done)
	}()

	// Concurrent reads
	for range 100 {
		_ = reg.GetForContext(&AssemblyContext{})
	}

	<-done
}
