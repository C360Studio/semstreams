package component

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStoreReadPort_ResourceID(t *testing.T) {
	port := StoreReadPort{Bucket: "MESSAGES"}
	assert.Equal(t, "store-read:MESSAGES", port.ResourceID())
}

func TestStoreReadPort_IsExclusive(t *testing.T) {
	port := StoreReadPort{Bucket: "MESSAGES"}
	assert.False(t, port.IsExclusive(), "multiple readers should be allowed")
}

func TestStoreReadPort_Kind(t *testing.T) {
	port := StoreReadPort{Bucket: "MESSAGES"}
	assert.Equal(t, PortKindStoreRead, port.Kind())
}

func TestResolvePort_StoreRead(t *testing.T) {
	def := PortDefinition{
		Name:   "content_store",
		Config: StoreReadPort{Bucket: "MESSAGES"},
	}

	port, err := def.Resolve(DirectionInput)
	assert.NoError(t, err)

	assert.Equal(t, "content_store", port.Name)
	storePort, ok := port.Config.(StoreReadPort)
	assert.True(t, ok, "config should be StoreReadPort")
	assert.Equal(t, "MESSAGES", storePort.Bucket)
}
