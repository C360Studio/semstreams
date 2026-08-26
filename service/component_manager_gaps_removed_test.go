package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
)

// externalOrphanFixture is one admitted component whose only input is a
// required JetStream port fed from outside the composition. Under ADR-100 the
// canonical judgment raises no orphan for it; the retired /gaps operation
// classified the same port as a critical orphan and set has_issues=true.
func externalOrphanFixture(t *testing.T) *ComponentManager {
	t.Helper()
	instance := &portFactsDiscoverable{
		baseDiscoverable: baseDiscoverable{name: "consumer"},
		inputs: []component.Port{{
			Name:      "user.message",
			Direction: component.DirectionInput,
			Required:  true,
			External:  true,
			Config: component.JetStreamPort{
				StreamName: "USER_MESSAGES",
				Subjects:   []string{"user.message.>"},
			},
		}},
	}
	registry := component.NewRegistry()
	admitTestRegistryComponent(t, registry, "consumer", instance)
	manager := newPortOwnershipCM(t, registry)
	if err := manager.analyzeBootComposition(); err != nil {
		t.Fatalf("analyzeBootComposition: %v", err)
	}
	return manager
}

// TestComponentGapsOperationIsAbsent proves the second composition judgment is
// gone (ADR-100 D3, one validator and one vocabulary): the /gaps route is not
// served and the ComponentManager OpenAPI document — the source the generated
// specs/openapi.v3.yaml is emitted from — does not advertise it.
func TestComponentGapsOperationIsAbsent(t *testing.T) {
	mux := http.NewServeMux()
	newPortOwnershipCM(t, nil).RegisterHTTPHandlers("/components/", mux)

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(method, "/components/gaps", nil))
		if recorder.Code != http.StatusNotFound && recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s /components/gaps = %d, want 404 or 405", method, recorder.Code)
		}
	}

	if _, advertised := componentManagerOpenAPISpec().Paths["/gaps"]; advertised {
		t.Fatal("the ComponentManager OpenAPI document still advertises /gaps")
	}
}

// TestExternalInputIsNeverACriticalOrphanOnAnyComponentOperation enumerates the
// component surface from its owning OpenAPI declaration rather than from a
// hand-kept list, so a reintroduced second judgment is caught wherever it is
// mounted. Every advertised operation is served, and none reports the
// externally fed required input as an orphan, a publisher-less port, or an
// issue count.
func TestExternalInputIsNeverACriticalOrphanOnAnyComponentOperation(t *testing.T) {
	manager := externalOrphanFixture(t)
	mux := http.NewServeMux()
	manager.RegisterHTTPHandlers("/components/", mux)

	paths := componentManagerOpenAPISpec().Paths
	if len(paths) == 0 {
		t.Fatal("the ComponentManager OpenAPI document advertises no operation")
	}
	// A judgment vocabulary the canonical library does not own. "critical" and
	// "has_issues" were the retired handler's severity words; "no_publishers"
	// and "orphaned_port" are the classifications it applied to this port.
	banned := []string{"no_publishers", "orphaned_port", "critical", "has_issues"}

	// The composition-bearing operations must answer; the per-instance read
	// models (status, config) are populated by Initialize, not by an admitted
	// registry, so they answer 404 under this fixture. Every operation's body is
	// searched either way — a 404 body may not carry the vocabulary either.
	mustAnswer := map[string]bool{"/validate": true, "/flowgraph": true, "/list": true, "/health": true}

	for path := range paths {
		concrete := strings.NewReplacer("{name}", "consumer", "{id}", "consumer").Replace(path)
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/components"+concrete, nil))
			if mustAnswer[path] && recorder.Code != http.StatusOK {
				t.Fatalf("GET /components%s = %d, want %d", concrete, recorder.Code, http.StatusOK)
			}
			if recorder.Code >= http.StatusInternalServerError {
				t.Fatalf("GET /components%s = %d: %s", concrete, recorder.Code, recorder.Body.String())
			}
			body := recorder.Body.String()
			for _, word := range banned {
				if strings.Contains(body, word) {
					t.Fatalf("GET /components%s reports %q for an external input: %s", concrete, word, body)
				}
			}
		})
	}

	// The canonical judgment agrees: no error finding, and the port carries its
	// marker in the projection.
	result := manager.bootCompositionResult()
	if result == nil || len(result.Errors) != 0 {
		t.Fatalf("boot composition result = %+v, want a result with no error finding", result)
	}
	node := result.Graph.Nodes[0]
	if len(node.Inputs) != 1 || !node.Inputs[0].External {
		t.Fatalf("projection input = %+v, want the external marker", node.Inputs)
	}
}
