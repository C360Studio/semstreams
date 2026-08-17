package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
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

func TestComponentBorrowRunsWithoutManagerLockAndClosesAtStopAdmission(t *testing.T) {
	manager := &ComponentManager{
		components: map[string]*component.ManagedComponent{
			"worker": {Component: baseDiscoverable{name: "worker"}, State: component.StateInitialized},
		},
	}
	if err := manager.withComponents(func(components map[string]*component.ManagedComponent) error {
		manager.mu.Lock()
		manager.mu.Unlock()
		if components["worker"] == nil {
			t.Fatal("borrow omitted admitted component")
		}
		return nil
	}); err != nil {
		t.Fatalf("withComponents() error = %v", err)
	}

	manager.stopMu.Lock()
	manager.stopping = true
	manager.stopMu.Unlock()
	if err := manager.withComponents(func(map[string]*component.ManagedComponent) error { return nil }); err == nil {
		t.Fatal("withComponents admitted a callback after terminal Stop admission")
	}
}

func TestComponentConfigPUTIsRetired(t *testing.T) {
	manager := &ComponentManager{}
	request := httptest.NewRequest(http.MethodPut, "/components/config/worker", strings.NewReader(`{"enabled":true}`))
	response := httptest.NewRecorder()

	manager.handleComponentConfig(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT component config status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
