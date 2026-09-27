package boot

import (
	"context"
	"errors"
	"reflect"
	"testing"

	rulepkg "github.com/c360studio/semstreams/processor/rule"
)

// orderRecorder records lifecycle calls in the order they happen. Every call
// runs on the test goroutine, so no lock is needed.
type orderRecorder struct {
	calls    []string
	startErr error
}

type recordingServices struct{ *orderRecorder }

func (r recordingServices) StartAll(context.Context) error {
	r.calls = append(r.calls, "services.start")
	return r.startErr
}
func (r recordingServices) StartHealthListener(context.Context, int) error { return nil }
func (r recordingServices) StopAll(context.Context) error {
	r.calls = append(r.calls, "services.stop")
	return nil
}

type recordingRules struct{ *orderRecorder }

func (r recordingRules) Start(context.Context, []rulepkg.HotReloadTarget) error {
	r.calls = append(r.calls, "rules.start")
	return nil
}
func (r recordingRules) Stop() error {
	r.calls = append(r.calls, "rules.stop")
	return nil
}

// TestRuleHotReloadRuntimeOrdersAroundServices pins design D3 of
// config-bucket-authority-namespace: rule hot reload seeds and reconciles
// after every service has started, and stops before any service stops.
func TestRuleHotReloadRuntimeOrdersAroundServices(t *testing.T) {
	recorder := &orderRecorder{}
	runtime := &ruleHotReloadRuntime{
		runtimeManager: recordingServices{recorder},
		rules:          recordingRules{recorder},
	}
	if err := runtime.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if err := runtime.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	want := []string{"services.start", "rules.start", "rules.stop", "services.stop"}
	if !reflect.DeepEqual(recorder.calls, want) {
		t.Fatalf("lifecycle order = %v, want %v", recorder.calls, want)
	}
}

// TestRuleHotReloadRuntimeDoesNotStartAfterAFailedServiceStart proves a
// failed StartAll never reaches the rule manager's Start.
func TestRuleHotReloadRuntimeDoesNotStartAfterAFailedServiceStart(t *testing.T) {
	recorder := &orderRecorder{startErr: errors.New("boom")}
	runtime := &ruleHotReloadRuntime{
		runtimeManager: recordingServices{recorder},
		rules:          recordingRules{recorder},
	}
	if err := runtime.StartAll(context.Background()); err == nil {
		t.Fatal("StartAll must return the services' start error")
	}
	if want := []string{"services.start"}; !reflect.DeepEqual(recorder.calls, want) {
		t.Fatalf("lifecycle order = %v, want %v", recorder.calls, want)
	}
}
