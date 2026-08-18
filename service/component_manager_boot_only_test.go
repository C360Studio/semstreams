package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNewComponentManagerRejectsRetiredWatchConfig(t *testing.T) {
	_, err := NewComponentManager(json.RawMessage(`{"watch_config":true}`), nil)
	if err == nil {
		t.Fatal("NewComponentManager accepted retired watch_config")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("NewComponentManager error = %v, want unknown field", err)
	}
}

func TestComponentManagerHasNoPostBootConfigOrMutationSeam(t *testing.T) {
	managerType := reflect.TypeOf(ComponentManager{})
	for _, retiredField := range []string{
		"configManager", "configUpdates", "modelRegistryUpdates", "supervisorRequests",
	} {
		if _, ok := managerType.FieldByName(retiredField); ok {
			t.Errorf("ComponentManager retains retired post-boot field %q", retiredField)
		}
	}

	pointerType := reflect.TypeOf((*ComponentManager)(nil))
	for _, retiredMethod := range []string{
		"CreateComponent", "RemoveComponent", "CreateComponentsFromConfig", "GetManagedComponents",
	} {
		if _, ok := pointerType.MethodByName(retiredMethod); ok {
			t.Errorf("ComponentManager retains retired runtime mutation method %q", retiredMethod)
		}
	}
}
