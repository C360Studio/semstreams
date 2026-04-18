package prompt

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegistry_Upsert_ReplaceById covers the override case: a DefaultFragment
// with ID "role-researcher" must be replaced (not duplicated) when an
// incoming fragment carries the same ID. This is the semantic ADR-029
// step 3b relies on for KV-backed personas.
func TestRegistry_Upsert_ReplaceById(t *testing.T) {
	reg := NewRegistry()
	reg.Add(Fragment{ID: "role-researcher", Category: CategoryRole, Content: "STATIC original"})
	reg.Add(Fragment{ID: "system-identity", Category: CategorySystem, Content: "SYSTEM"})

	reg.Upsert(Fragment{ID: "role-researcher", Category: CategoryRole, Content: "OVERRIDDEN", Roles: []string{"researcher"}})

	matched := reg.GetForContext(&AssemblyContext{Role: "researcher"})
	require.Len(t, matched, 2)

	var researcher Fragment
	for _, f := range matched {
		if f.ID == "role-researcher" {
			researcher = f
		}
	}
	assert.Equal(t, "OVERRIDDEN", researcher.Content, "upsert must replace content in place")
	assert.Equal(t, []string{"researcher"}, researcher.Roles, "upsert must overwrite every field, not merge")
}

// TestRegistry_Upsert_AppendWhenNew ensures Upsert does not silently drop
// fragments whose ID is not yet registered — that would break any caller
// that uses Upsert exclusively instead of Add.
func TestRegistry_Upsert_AppendWhenNew(t *testing.T) {
	reg := NewRegistry()
	reg.Upsert(Fragment{ID: "new-fragment", Category: CategorySystem, Content: "FRESH"})

	matched := reg.GetForContext(&AssemblyContext{})
	require.Len(t, matched, 1)
	assert.Equal(t, "FRESH", matched[0].Content)
}

// TestRegistry_UpsertAll verifies the batch variant produces the same
// end-state as a sequence of Upserts: new IDs appended, existing IDs
// replaced in place.
func TestRegistry_UpsertAll(t *testing.T) {
	reg := NewRegistry()
	reg.AddAll([]Fragment{
		{ID: "a", Category: CategorySystem, Content: "A v1"},
		{ID: "b", Category: CategorySystem, Content: "B v1"},
	})

	reg.UpsertAll([]Fragment{
		{ID: "b", Category: CategorySystem, Content: "B v2"},
		{ID: "c", Category: CategorySystem, Content: "C v1"},
	})

	matched := reg.GetForContext(&AssemblyContext{})
	require.Len(t, matched, 3)

	byID := map[string]string{}
	for _, f := range matched {
		byID[f.ID] = f.Content
	}
	assert.Equal(t, "A v1", byID["a"])
	assert.Equal(t, "B v2", byID["b"], "existing id should be replaced")
	assert.Equal(t, "C v1", byID["c"], "new id should be appended")
}

// TestRegistry_Upsert_OverridesDefaults is the shape ADR-029 step 3b
// ships: start with DefaultFragments, upsert a stored persona with the
// same ID, and Assemble should emit the overridden content for the
// matching role.
func TestRegistry_Upsert_OverridesDefaults(t *testing.T) {
	reg := NewRegistry()
	reg.AddAll(DefaultFragments())

	reg.Upsert(Fragment{
		ID:       "role-researcher",
		Category: CategoryRole,
		Content:  "CUSTOM researcher persona from KV",
		Roles:    []string{"researcher"},
	})

	result := Assemble(reg, &AssemblyContext{Role: "researcher"})
	assert.Contains(t, result.SystemMessage, "CUSTOM researcher persona from KV")
	assert.NotContains(t, result.SystemMessage, "Research methodology:", "default researcher fragment must be replaced, not appended alongside")
	assert.Contains(t, result.FragmentsUsed, "role-researcher", "override retains the original ID for observability")
}

// TestRegistry_Upsert_ThreadSafe confirms concurrent Upsert + GetForContext
// calls don't race. Runs under -race in CI; the assertion is simply "no
// panic or deadlock." Matches the style of the existing Add thread-safety
// test.
func TestRegistry_Upsert_ThreadSafe(_ *testing.T) {
	reg := NewRegistry()
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 100 {
			reg.Upsert(Fragment{
				ID:       strings.Repeat("a", (i%5)+1),
				Category: CategorySystem,
				Content:  "content",
			})
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			_ = reg.GetForContext(&AssemblyContext{})
		}
	}()

	wg.Wait()
}
