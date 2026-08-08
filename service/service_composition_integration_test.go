//go:build integration

package service

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/types"
)

func TestServiceCompositionDesiredStateProductionSeam(t *testing.T) {
	testClient := natsclient.NewTestClient(t, natsclient.WithKV())
	defer testClient.Terminate()

	bootDesired := types.ServiceConfigs{
		"metrics":           {Enabled: false, Config: json.RawMessage(`{}`)},
		"http-capable":      {Enabled: true, Config: json.RawMessage(`{"value":1}`)},
		"component-manager": {Enabled: true, Config: json.RawMessage(`{}`)},
	}
	configManager, err := config.NewConfigManager(&config.Config{
		Version:  "1.0.0",
		Platform: config.PlatformConfig{Org: "c360", ID: "service-composition-test", Type: "test"},
		Services: cloneServiceConfigs(t, bootDesired),
	}, testClient.Client, slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	registry := NewServiceRegistry()
	if err := registry.Register("component-manager", func(json.RawMessage, *Dependencies) (Service, error) {
		return &MockService{name: "component-manager"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("http-capable", func(json.RawMessage, *Dependencies) (Service, error) {
		return &compositionHTTPService{MockService: MockService{name: "http-capable"}}, nil
	}); err != nil {
		t.Fatal(err)
	}

	manager := NewServiceManager(registry)
	if err := manager.RegisterInstance("milestone", &MockService{name: "milestone"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.ConfigureFromServices(configManager.GetConfig().Get().Services, &Dependencies{
		Manager: configManager,
		Logger:  slog.Default(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.sealComposition(); err != nil {
		t.Fatal(err)
	}

	manager.httpMux = http.NewServeMux()
	manager.registerSystemEndpoints()
	if err := manager.registerServiceHandlers(); err != nil {
		t.Fatal(err)
	}
	assertCompositionProbeRoute(t, manager.httpMux)
	if _, ok := manager.generateOpenAPIDocument().Paths["/httpcapable/probe"]; !ok {
		t.Fatal("sealed HTTP service missing from boot OpenAPI")
	}

	bootResponse := getServiceCompositionResponse(t, manager.httpMux)
	if bootResponse.RestartRequired || len(bootResponse.Pending) != 0 {
		t.Fatalf("boot desired state reported pending restart: %#v", bootResponse)
	}
	wantRows := []string{"component-manager", "http-capable", "milestone"}
	if !reflect.DeepEqual(bootResponse.Names(), wantRows) {
		t.Fatalf("boot service rows = %v, want %v", bootResponse.Names(), wantRows)
	}

	if err := configManager.GetConfig().Mutate(func(current *config.Config) error {
		current.Services["http-capable"] = types.ServiceConfig{
			Enabled: true,
			Config:  json.RawMessage(`{"value":2}`),
		}
		current.Services["added"] = types.ServiceConfig{Enabled: true, Config: json.RawMessage(`{}`)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	pendingResponse := getServiceCompositionResponse(t, manager.httpMux)
	wantPending := []PendingServiceChange{
		{Name: "added", Change: serviceChangeAdd},
		{Name: "http-capable", Change: serviceChangeReconfigure},
	}
	if !pendingResponse.RestartRequired || !reflect.DeepEqual(pendingResponse.Pending, wantPending) {
		t.Fatalf("pending desired response = %#v, want %v", pendingResponse, wantPending)
	}
	if !reflect.DeepEqual(pendingResponse.Names(), wantRows) {
		t.Fatalf("desired mutation changed sealed rows: %v", pendingResponse.Names())
	}
	assertCompositionProbeRoute(t, manager.httpMux)
	openAPI := manager.generateOpenAPIDocument()
	if _, ok := openAPI.Paths["/httpcapable/probe"]; !ok {
		t.Fatal("desired mutation removed sealed OpenAPI contributor")
	}
	if _, ok := openAPI.Paths["/added/probe"]; ok {
		t.Fatal("desired-only service entered sealed OpenAPI")
	}

	if err := configManager.GetConfig().Mutate(func(current *config.Config) error {
		current.Services = cloneServiceConfigs(t, bootDesired)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reverted := getServiceCompositionResponse(t, manager.httpMux)
	if reverted.RestartRequired || len(reverted.Pending) != 0 {
		t.Fatalf("reverted desired state still reports restart: %#v", reverted)
	}
}

type serviceCompositionResponse struct {
	Services []struct {
		Name string `json:"name"`
	} `json:"services"`
	RestartRequired bool                   `json:"restart_required"`
	Pending         []PendingServiceChange `json:"pending_service_changes"`
}

func (r serviceCompositionResponse) Names() []string {
	names := make([]string, 0, len(r.Services))
	for _, service := range r.Services {
		names = append(names, service.Name)
	}
	return names
}

func getServiceCompositionResponse(t *testing.T, handler http.Handler) serviceCompositionResponse {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/services", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /services status = %d: %s", response.Code, response.Body.String())
	}
	var body serviceCompositionResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func assertCompositionProbeRoute(t *testing.T, handler http.Handler) {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/httpcapable/probe", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("sealed probe status = %d", response.Code)
	}
}
