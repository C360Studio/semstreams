package message

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntityClass_String(t *testing.T) {
	tests := []struct {
		class    EntityClass
		expected string
	}{
		{ClassObject, "Object"},
		{ClassEvent, "Event"},
		{ClassAgent, "Agent"},
		{ClassPlace, "Place"},
		{ClassProcess, "Process"},
		{ClassThing, "Thing"},
	}

	for _, test := range tests {
		t.Run(test.expected, func(t *testing.T) {
			assert.Equal(t, test.expected, test.class.String())
		})
	}
}

func TestEntityClass_IsValid(t *testing.T) {
	validClasses := []EntityClass{
		ClassObject, ClassEvent, ClassAgent, ClassPlace, ClassProcess, ClassThing,
	}

	for _, class := range validClasses {
		t.Run("Valid_"+class.String(), func(t *testing.T) {
			assert.True(t, class.IsValid())
		})
	}

	invalidClasses := []EntityClass{
		"", "Invalid", "object", "EVENT", "unknown",
	}

	for _, class := range invalidClasses {
		t.Run("Invalid_"+string(class), func(t *testing.T) {
			assert.False(t, class.IsValid())
		})
	}
}

func TestEntityClass_JSONMarshal(t *testing.T) {
	class := ClassObject
	data, err := json.Marshal(class)

	require.NoError(t, err)
	assert.Equal(t, `"Object"`, string(data))
}

func TestEntityClass_JSONUnmarshal(t *testing.T) {
	data := `"Event"`
	var class EntityClass

	err := json.Unmarshal([]byte(data), &class)

	require.NoError(t, err)
	assert.Equal(t, ClassEvent, class)
}

func TestEntityRole_String(t *testing.T) {
	tests := []struct {
		role     EntityRole
		expected string
	}{
		{RolePrimary, "primary"},
		{RoleObserved, "observed"},
		{RoleComponent, "component"},
		{RoleSource, "source"},
		{RoleTarget, "target"},
		{RoleContext, "context"},
		{RoleRelated, "related"},
	}

	for _, test := range tests {
		t.Run(test.expected, func(t *testing.T) {
			assert.Equal(t, test.expected, test.role.String())
		})
	}
}

func TestEntityRole_IsValid(t *testing.T) {
	validRoles := []EntityRole{
		RolePrimary, RoleObserved, RoleComponent, RoleSource, RoleTarget, RoleContext, RoleRelated,
	}

	for _, role := range validRoles {
		t.Run("Valid_"+role.String(), func(t *testing.T) {
			assert.True(t, role.IsValid())
		})
	}

	invalidRoles := []EntityRole{
		"", "invalid", "PRIMARY", "Observer", "unknown",
	}

	for _, role := range invalidRoles {
		t.Run("Invalid_"+string(role), func(t *testing.T) {
			assert.False(t, role.IsValid())
		})
	}
}

func TestEntityRole_JSONMarshal(t *testing.T) {
	role := RolePrimary
	data, err := json.Marshal(role)

	require.NoError(t, err)
	assert.Equal(t, `"primary"`, string(data))
}

func TestEntityRole_JSONUnmarshal(t *testing.T) {
	data := `"observed"`
	var role EntityRole

	err := json.Unmarshal([]byte(data), &role)

	require.NoError(t, err)
	assert.Equal(t, RoleObserved, role)
}

func TestEntityClass_Constants(t *testing.T) {
	// Verify the constants have the expected string values
	assert.Equal(t, "Object", string(ClassObject))
	assert.Equal(t, "Event", string(ClassEvent))
	assert.Equal(t, "Agent", string(ClassAgent))
	assert.Equal(t, "Place", string(ClassPlace))
	assert.Equal(t, "Process", string(ClassProcess))
	assert.Equal(t, "Thing", string(ClassThing))
}

func TestEntityRole_Constants(t *testing.T) {
	// Verify the constants have the expected string values
	assert.Equal(t, "primary", string(RolePrimary))
	assert.Equal(t, "observed", string(RoleObserved))
	assert.Equal(t, "component", string(RoleComponent))
	assert.Equal(t, "source", string(RoleSource))
	assert.Equal(t, "target", string(RoleTarget))
	assert.Equal(t, "context", string(RoleContext))
	assert.Equal(t, "related", string(RoleRelated))
}
