package persona

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/processor/agentic-loop/prompt"
)

// TestPersona_ToFragment covers the field-by-field mapping from the
// stored Persona shape to prompt.Fragment. Category is the interesting
// bit — it's stored as int and re-typed into prompt.Category so the
// registry ordering still works downstream.
func TestPersona_ToFragment(t *testing.T) {
	p := &Persona{
		ID:          "role-researcher",
		Category:    100,
		Priority:    5,
		Content:     "You are a research agent.",
		Roles:       []string{"researcher", "analyst"},
		Description: "Operator-facing notes; must not appear in fragment.",
	}

	f := p.ToFragment()

	assert.Equal(t, "role-researcher", f.ID)
	assert.Equal(t, prompt.CategoryRole, f.Category)
	assert.Equal(t, 5, f.Priority)
	assert.Equal(t, "You are a research agent.", f.Content)
	assert.Equal(t, []string{"researcher", "analyst"}, f.Roles)
	assert.Nil(t, f.ContentFunc, "runtime-only hooks stay nil; personas carry static content")
	assert.Nil(t, f.Condition, "runtime-only hooks stay nil; personas carry static content")
}

// TestPersona_ToFragment_CategoryValues spot-checks that every declared
// prompt.Category constant round-trips through the int field. If a new
// category is added to prompt/types.go this test is a cheap guard.
func TestPersona_ToFragment_CategoryValues(t *testing.T) {
	cases := []struct {
		stored int
		want   prompt.Category
	}{
		{0, prompt.CategorySystem},
		{100, prompt.CategoryRole},
		{200, prompt.CategoryTools},
		{300, prompt.CategoryDomain},
		{400, prompt.CategoryConstraints},
		{500, prompt.CategoryContext},
	}
	for _, tc := range cases {
		f := (&Persona{ID: "x", Content: "y", Category: tc.stored}).ToFragment()
		assert.Equal(t, tc.want, f.Category, "category int %d should map to named constant", tc.stored)
	}
}

// TestManager_Fragments_Nil is the safety-net case documented on
// Fragments: callers wiring personas conditionally shouldn't have to
// guard against a nil manager. Returning (nil, nil) keeps the caller's
// UpsertAll into a prompt.Registry a clean no-op.
func TestManager_Fragments_Nil(t *testing.T) {
	var m *Manager

	fragments, err := m.Fragments(context.Background())
	require.NoError(t, err)
	assert.Nil(t, fragments)
}
