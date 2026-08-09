package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/types"
)

type compositionHTTPService struct {
	MockService
}

func (s *compositionHTTPService) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	mux.HandleFunc(prefix+"/probe", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *compositionHTTPService) OpenAPISpec() *OpenAPISpec {
	return &OpenAPISpec{Paths: map[string]PathSpec{"/probe": {}}}
}

func TestResolveServiceConfigsIsPureAndCanonical(t *testing.T) {
	original := types.ServiceConfigs{
		"optional": {
			Enabled: true,
			Config:  json.RawMessage(`{ "b": 2, "a": { "y": 2, "x": 1 } }`),
		},
		"component-manager": {
			Enabled: false,
			Config:  json.RawMessage(`{}`),
		},
	}
	wantOriginal := cloneServiceConfigs(t, original)

	resolved, err := ResolveServiceConfigs(original)
	if err != nil {
		t.Fatalf("ResolveServiceConfigs: %v", err)
	}
	if !reflect.DeepEqual(original, wantOriginal) {
		t.Fatalf("resolver mutated input: got %#v want %#v", original, wantOriginal)
	}
	if got := string(resolved["optional"].Config); got != `{"a":{"x":1,"y":2},"b":2}` {
		t.Fatalf("canonical config = %s", got)
	}
	if resolved["component-manager"].Enabled {
		t.Fatal("explicit component-manager false was not preserved")
	}
	if got, ok := resolved["service-manager"]; !ok || !got.Enabled {
		t.Fatalf("service-manager was not materialized enabled: %#v", got)
	}
	if got, ok := resolved["metrics"]; !ok || !got.Enabled {
		t.Fatalf("optional metrics default was not retained: %#v", got)
	}
	if _, ok := resolved["message-logger"]; ok {
		t.Fatal("message-logger must not be injected")
	}

	resolved["optional"] = types.ServiceConfig{Enabled: false}
	if !original["optional"].Enabled {
		t.Fatal("resolved map aliases input map")
	}
}

func TestConfigureRejectsExplicitlyDisabledMandatoryServices(t *testing.T) {
	for _, name := range []string{"component-manager", "service-manager"} {
		t.Run(name, func(t *testing.T) {
			manager := NewServiceManager(NewServiceRegistry())
			err := manager.ConfigureFromServices(types.ServiceConfigs{
				name: {Enabled: false, Config: json.RawMessage(`{}`)},
			}, &Dependencies{})
			var disabled *MandatoryServiceDisabledError
			if !errors.As(err, &disabled) || disabled.Name != name {
				t.Fatalf("mandatory disable error = %T %v", err, err)
			}
			if len(manager.services) != 0 {
				t.Fatalf("mandatory disable constructed services: %v", manager.services)
			}
		})
	}
}

func TestOmittedOrDisabledMessageLoggerCreatesNoRuntimeSurface(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		services types.ServiceConfigs
	}{
		{name: "omitted", services: types.ServiceConfigs{}},
		{name: "disabled", services: types.ServiceConfigs{
			"message-logger": {Enabled: false, Config: json.RawMessage(`{}`)},
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			registry := NewServiceRegistry()
			for _, name := range []string{"component-manager", "metrics"} {
				serviceName := name
				if err := registry.Register(serviceName, func(json.RawMessage, *Dependencies) (Service, error) {
					return &MockService{name: serviceName}, nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			constructorCalls := 0
			if err := registry.Register("message-logger", func(json.RawMessage, *Dependencies) (Service, error) {
				constructorCalls++
				return &MessageLogger{entries: make([]MessageLogEntry, 1)}, nil
			}); err != nil {
				t.Fatal(err)
			}
			manager := NewServiceManager(registry)
			if err := manager.ConfigureFromServices(testCase.services, &Dependencies{}); err != nil {
				t.Fatal(err)
			}
			if constructorCalls != 0 {
				t.Fatalf("message-logger constructor called %d times", constructorCalls)
			}
			if _, exists := manager.GetService("message-logger"); exists {
				t.Fatal("message-logger instance exists")
			}
			if _, err := manager.sealComposition(); err != nil {
				t.Fatal(err)
			}
			manager.httpMux = http.NewServeMux()
			if err := manager.registerServiceHandlers(); err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			manager.httpMux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/messagelogger/subjects", nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("message-logger route status = %d, want 404", response.Code)
			}
			for path := range manager.generateOpenAPIDocument().Paths {
				if len(path) >= len("/messagelogger/") && path[:len("/messagelogger/")] == "/messagelogger/" {
					t.Fatalf("message-logger OpenAPI path exists: %s", path)
				}
			}
		})
	}
}

func TestPendingServiceChangesClassifications(t *testing.T) {
	boot := types.ServiceConfigs{
		"add-disabled":      {Enabled: false, Config: json.RawMessage(`{"a":1}`)},
		"enable":            {Enabled: false, Config: json.RawMessage(`{"a":1}`)},
		"disable":           {Enabled: true, Config: json.RawMessage(`{"a":1}`)},
		"remove":            {Enabled: true, Config: json.RawMessage(`{"a":1}`)},
		"reconfigure":       {Enabled: true, Config: json.RawMessage(`{"a":1}`)},
		"disabled-churn":    {Enabled: false, Config: json.RawMessage(`{"a":1}`)},
		"activation-wins":   {Enabled: false, Config: json.RawMessage(`{"a":1}`)},
		"canonical-no-diff": {Enabled: true, Config: json.RawMessage(`{"a":1,"b":2}`)},
	}
	desired := types.ServiceConfigs{
		"add":               {Enabled: true, Config: json.RawMessage(`{"a":1}`)},
		"enable":            {Enabled: true, Config: json.RawMessage(`{"a":1}`)},
		"disable":           {Enabled: false, Config: json.RawMessage(`{"a":2}`)},
		"reconfigure":       {Enabled: true, Config: json.RawMessage(`{"a":2}`)},
		"disabled-churn":    {Enabled: false, Config: json.RawMessage(`{"a":2}`)},
		"activation-wins":   {Enabled: true, Config: json.RawMessage(`{"a":2}`)},
		"canonical-no-diff": {Enabled: true, Config: json.RawMessage(`{ "b": 2, "a": 1 }`)},
	}

	resolvedBoot, err := resolveServiceConfigs(boot, false)
	if err != nil {
		t.Fatal(err)
	}
	resolvedDesired, err := resolveServiceConfigs(desired, false)
	if err != nil {
		t.Fatal(err)
	}
	got := pendingServiceChanges(resolvedBoot, resolvedDesired)
	want := []PendingServiceChange{
		{Name: "activation-wins", Change: "enable"},
		{Name: "add", Change: "add"},
		{Name: "disable", Change: "disable"},
		{Name: "enable", Change: "enable"},
		{Name: "reconfigure", Change: "reconfigure"},
		{Name: "remove", Change: "remove"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pending changes = %#v, want %#v", got, want)
	}
}

func TestCompositionSealRejectsDuplicatesAndPostSealWrites(t *testing.T) {
	registry := NewServiceRegistry()
	if err := registry.Register("later", func(json.RawMessage, *Dependencies) (Service, error) {
		return &MockService{name: "later"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	manager := NewServiceManager(registry)
	componentManager := &MockService{name: "component-manager"}
	if err := manager.RegisterInstance("component-manager", componentManager); err != nil {
		t.Fatal(err)
	}
	if err := manager.RegisterInstance("component-manager", &MockService{name: "replacement"}); err == nil {
		t.Fatal("duplicate registration succeeded")
	} else {
		var duplicate *DuplicateServiceError
		if !errors.As(err, &duplicate) {
			t.Fatalf("duplicate error = %T %v", err, err)
		}
	}

	if _, err := manager.sealComposition(); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CreateService("later", json.RawMessage(`{}`), &Dependencies{}); err == nil {
		t.Fatal("post-seal CreateService succeeded")
	} else {
		var sealed *CompositionSealedError
		if !errors.As(err, &sealed) {
			t.Fatalf("create error = %T %v", err, err)
		}
	}
	if err := manager.RegisterInstance("fixed", &MockService{name: "fixed"}); err == nil {
		t.Fatal("post-seal RegisterInstance succeeded")
	}
	if got, ok := manager.GetService("component-manager"); !ok || got != componentManager {
		t.Fatal("sealed identity changed")
	}
}

func TestConfigureFromServicesRejectsPostSealWithoutRewritingBootTruth(t *testing.T) {
	registry := NewServiceRegistry()
	for _, name := range []string{"component-manager", "metrics", "optional"} {
		serviceName := name
		if err := registry.Register(serviceName, func(json.RawMessage, *Dependencies) (Service, error) {
			return &MockService{name: serviceName}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	manager := NewServiceManager(registry)
	initialDeps := &Dependencies{}
	initial := types.ServiceConfigs{
		"service-manager": {Enabled: true, Config: json.RawMessage(`{"http_port":18080}`)},
		"optional":        {Enabled: true, Config: json.RawMessage(`{"value":1}`)},
	}
	if err := manager.ConfigureFromServices(initial, initialDeps); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.sealComposition(); err != nil {
		t.Fatal(err)
	}
	wantBoot := cloneServiceConfigs(t, manager.bootServiceConfigs)
	wantConfig := manager.config
	wantDeps := manager.dependencies

	desired := types.ServiceConfigs{
		"service-manager": {Enabled: true, Config: json.RawMessage(`{"http_port":19090}`)},
		"optional":        {Enabled: true, Config: json.RawMessage(`{"value":2}`)},
	}
	resolvedDesired, err := ResolveServiceConfigs(desired)
	if err != nil {
		t.Fatal(err)
	}
	wantPending := pendingServiceChanges(wantBoot, resolvedDesired)

	err = manager.ConfigureFromServices(desired, &Dependencies{})
	var sealed *CompositionSealedError
	if !errors.As(err, &sealed) {
		t.Fatalf("post-seal configure error = %T %v", err, err)
	}
	if manager.config != wantConfig || manager.dependencies != wantDeps {
		t.Fatal("post-seal configure mutated manager config or dependencies")
	}
	if !reflect.DeepEqual(manager.bootServiceConfigs, wantBoot) {
		t.Fatalf("post-seal configure rewrote boot baseline: got %#v want %#v", manager.bootServiceConfigs, wantBoot)
	}
	if got := pendingServiceChanges(manager.bootServiceConfigs, resolvedDesired); !reflect.DeepEqual(got, wantPending) {
		t.Fatalf("post-seal configure changed pending truth: got %#v want %#v", got, wantPending)
	}
}

func TestConfigureFromServicesRejectsFixedConfiguredIdentityCollision(t *testing.T) {
	registry := NewServiceRegistry()
	constructorCalls := 0
	if err := registry.Register("milestone", func(json.RawMessage, *Dependencies) (Service, error) {
		constructorCalls++
		return &MockService{name: "configured-milestone"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	manager := NewServiceManager(registry)
	fixed := &MockService{name: "fixed-milestone"}
	if err := manager.RegisterInstance("milestone", fixed); err != nil {
		t.Fatal(err)
	}

	err := manager.ConfigureFromServices(types.ServiceConfigs{
		"milestone": {Enabled: true, Config: json.RawMessage(`{"stream":"configured"}`)},
	}, &Dependencies{})
	var duplicate *DuplicateServiceError
	if !errors.As(err, &duplicate) || duplicate.Name != "milestone" {
		t.Fatalf("configured/fixed collision error = %T %v", err, err)
	}
	if constructorCalls != 0 {
		t.Fatalf("configured milestone constructor called %d times", constructorCalls)
	}
	if got, ok := manager.GetService("milestone"); !ok || got != fixed {
		t.Fatal("configured collision replaced fixed milestone")
	}
	if len(manager.bootServiceConfigs) != 0 {
		t.Fatalf("configured collision retained false boot baseline: %#v", manager.bootServiceConfigs)
	}
}

func TestCompositionSealUsesFullSetAndHTTPSubset(t *testing.T) {
	manager := NewServiceManager(NewServiceRegistry())
	plain := &MockService{name: "plain"}
	httpService := &compositionHTTPService{MockService: MockService{name: "http-capable"}}
	for name, instance := range map[string]Service{
		"plain":             plain,
		"http-capable":      httpService,
		"component-manager": &MockService{name: "component-manager"},
	} {
		if err := manager.RegisterInstance(name, instance); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := manager.sealComposition(); err != nil {
		t.Fatal(err)
	}
	wantIdentities := []string{"component-manager", "http-capable", "plain"}
	if !reflect.DeepEqual(manager.sealedServices, wantIdentities) {
		t.Fatalf("sealed identities = %v, want %v", manager.sealedServices, wantIdentities)
	}

	manager.httpMux = http.NewServeMux()
	if err := manager.registerServiceHandlers(); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	manager.httpMux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/httpcapable/probe", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("HTTP-capable route status = %d", response.Code)
	}
	response = httptest.NewRecorder()
	manager.httpMux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/plain/probe", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("plain service unexpectedly contributed route: %d", response.Code)
	}

	doc := manager.generateOpenAPIDocument()
	if _, ok := doc.Paths["/httpcapable/probe"]; !ok {
		t.Fatal("HTTP-capable service did not contribute OpenAPI")
	}
	if _, ok := doc.Paths["/plain/probe"]; ok {
		t.Fatal("plain service unexpectedly contributed OpenAPI")
	}
}

func TestServiceListUsesSortedSealedRowsAndRestartFields(t *testing.T) {
	manager := NewServiceManager(NewServiceRegistry())
	manager.services = map[string]Service{
		"zeta":  &MockService{name: "zeta"},
		"alpha": &MockService{name: "alpha"},
	}
	manager.sealed = true
	manager.sealedServices = []string{"alpha", "zeta"}
	manager.bootServiceConfigs = types.ServiceConfigs{
		"alpha": {Enabled: true, Config: json.RawMessage(`{}`)},
		"zeta":  {Enabled: true, Config: json.RawMessage(`{}`)},
	}

	response := httptest.NewRecorder()
	manager.handleServiceList(response, httptest.NewRequest(http.MethodGet, "/services", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /services status = %d", response.Code)
	}
	var body struct {
		Services []struct {
			Name string `json:"name"`
		} `json:"services"`
		RestartRequired bool                   `json:"restart_required"`
		Pending         []PendingServiceChange `json:"pending_service_changes"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Services) != 2 {
		t.Fatalf("service row count = %d", len(body.Services))
	}
	gotNames := []string{body.Services[0].Name, body.Services[1].Name}
	if want := []string{"alpha", "zeta"}; !reflect.DeepEqual(gotNames, want) {
		t.Fatalf("service rows = %v, want %v", gotNames, want)
	}
	if body.RestartRequired || len(body.Pending) != 0 {
		t.Fatalf("unchanged boot state reported restart: required=%v pending=%v", body.RestartRequired, body.Pending)
	}
}

func cloneServiceConfigs(t *testing.T, configs types.ServiceConfigs) types.ServiceConfigs {
	t.Helper()
	clone := make(types.ServiceConfigs, len(configs))
	for name, serviceConfig := range configs {
		serviceConfig.Config = bytes.Clone(serviceConfig.Config)
		clone[name] = serviceConfig
	}
	return clone
}
