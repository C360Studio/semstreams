package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/types"
)

func TestConfigureFromServicesBindsExactManagerWithoutMutatingCallerDependencies(t *testing.T) {
	registry := NewServiceRegistry()
	manager := NewServiceManager(registry)
	differentManager := NewServiceManager(NewServiceRegistry())
	platform := types.PlatformMeta{Org: "owner-test", Platform: "composition"}
	callerDeps := &Dependencies{Platform: platform, ServiceManager: differentManager}

	var constructorDeps *Dependencies
	for _, name := range []string{"component-manager", "probe"} {
		serviceName := name
		if err := registry.Register(serviceName, func(_ json.RawMessage, deps *Dependencies) (Service, error) {
			if serviceName == "probe" {
				constructorDeps = deps
			}
			return &MockService{name: serviceName}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	err := manager.ConfigureFromServices(types.ServiceConfigs{
		"metrics": {Enabled: false, Config: json.RawMessage(`{}`)},
		"probe":   {Enabled: true, Config: json.RawMessage(`{}`)},
	}, callerDeps)
	if err != nil {
		t.Fatal(err)
	}
	if constructorDeps == nil || constructorDeps.ServiceManager != manager {
		t.Fatalf("constructor ServiceManager = %p, want exact receiver %p", constructorDeps.ServiceManager, manager)
	}
	if constructorDeps == callerDeps {
		t.Fatal("constructor received caller dependency record instead of a shallow copy")
	}
	if constructorDeps.Platform != platform {
		t.Fatalf("constructor Platform = %#v, want preserved identity %#v", constructorDeps.Platform, platform)
	}
	if manager.dependencies == callerDeps || manager.dependencies.ServiceManager != manager {
		t.Fatalf("retained dependencies = %#v, want a manager-bound shallow copy", manager.dependencies)
	}
	if callerDeps.ServiceManager != differentManager || callerDeps.Platform != platform {
		t.Fatalf("caller dependencies mutated: %#v", callerDeps)
	}
}

func TestCreateServiceConstructorCanReadOwningManager(t *testing.T) {
	registry := NewServiceRegistry()
	manager := NewServiceManager(registry)
	existing := &MockService{name: "existing"}
	if err := manager.RegisterInstance("existing", existing); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("reader", func(_ json.RawMessage, deps *Dependencies) (Service, error) {
		if deps.ServiceManager != manager {
			return nil, errors.New("constructor did not receive owning manager")
		}
		got, ok := deps.ServiceManager.GetService("existing")
		if !ok || got != existing {
			return nil, errors.New("constructor could not read existing service")
		}
		return &MockService{name: "reader"}, nil
	}); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := manager.CreateService("reader", json.RawMessage(`{}`), &Dependencies{})
		done <- err
	}()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatalf("constructor did not complete while reading Manager: %v", ctx.Err())
	}
}

func TestCreateServiceRevalidatesConcurrentDuplicateBeforeCommit(t *testing.T) {
	registry := NewServiceRegistry()
	manager := NewServiceManager(registry)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })

	if err := registry.Register("duplicate", func(_ json.RawMessage, deps *Dependencies) (Service, error) {
		if deps.ServiceManager != manager {
			return nil, errors.New("constructor did not receive owning manager")
		}
		entered <- struct{}{}
		<-release
		return &MockService{name: "duplicate"}, nil
	}); err != nil {
		t.Fatal(err)
	}

	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := manager.CreateService("duplicate", json.RawMessage(`{}`), &Dependencies{})
			results <- err
		}()
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for range 2 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatalf("constructors did not execute concurrently outside Manager lock: %v", ctx.Err())
		}
	}
	releaseOnce.Do(func() { close(release) })

	successes := 0
	duplicates := 0
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				successes++
				continue
			}
			var duplicate *DuplicateServiceError
			if errors.As(err, &duplicate) {
				duplicates++
				continue
			}
			t.Fatalf("CreateService error = %T %v", err, err)
		case <-ctx.Done():
			t.Fatalf("CreateService calls did not complete: %v", ctx.Err())
		}
	}
	if successes != 1 || duplicates != 1 {
		t.Fatalf("results: successes=%d duplicates=%d, want 1 and 1", successes, duplicates)
	}
	if got := manager.GetAllServices(); len(got) != 1 || got["duplicate"] == nil {
		t.Fatalf("committed services = %#v, want exactly duplicate", got)
	}
}

func TestCreateServiceRejectsCommitWhenCompositionSealsDuringConstructor(t *testing.T) {
	registry := NewServiceRegistry()
	manager := NewServiceManager(registry)
	if err := manager.RegisterInstance("component-manager", &MockService{name: "component-manager"}); err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	if err := registry.Register("late", func(_ json.RawMessage, deps *Dependencies) (Service, error) {
		if deps.ServiceManager != manager {
			return nil, errors.New("constructor did not receive owning manager")
		}
		close(entered)
		<-release
		return &MockService{name: "late"}, nil
	}); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := manager.CreateService("late", json.RawMessage(`{}`), &Dependencies{})
		result <- err
	}()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatalf("constructor did not reach synchronized admission point: %v", ctx.Err())
	}
	if _, err := manager.sealComposition(); err != nil {
		t.Fatalf("seal composition while constructor blocked: %v", err)
	}
	releaseOnce.Do(func() { close(release) })

	select {
	case err := <-result:
		var sealed *CompositionSealedError
		if !errors.As(err, &sealed) {
			t.Fatalf("CreateService error = %T %v, want CompositionSealedError", err, err)
		}
	case <-ctx.Done():
		t.Fatalf("CreateService did not return after seal: %v", ctx.Err())
	}
	if _, exists := manager.GetService("late"); exists {
		t.Fatal("service committed after composition sealed")
	}
}

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
