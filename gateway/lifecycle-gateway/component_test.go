// Package lifecyclegateway — unit + integration tests.
//
// Production-wire discipline: every endpoint test drives the route
// through `Component.RegisterHTTPHandlers` on a real
// `http.ServeMux` mounted on `httptest.NewServer`. A regression
// that broke path-prefix stripping, method dispatch, or response
// envelope shape would fail these tests, not just the
// helper-direct path. Per
// [[feedback_integration_tests_must_drive_production_wire]].
package lifecyclegateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// --- Mock LifecycleManager ---

type fakeParticipant struct {
	EntityIDF      string `json:"entity_id"`
	WorkflowF      string `json:"workflow"`
	PhaseF         string `json:"phase"`
	TerminalF      bool   `json:"-"`
	OwnerOrgIDF    string `json:"owner_org_id"`
	ParentMissionF string `json:"parent_mission,omitempty"`
}

func (p *fakeParticipant) EntityID() string             { return p.EntityIDF }
func (p *fakeParticipant) Workflow() string             { return p.WorkflowF }
func (p *fakeParticipant) Phase() string                { return p.PhaseF }
func (p *fakeParticipant) IsTerminal() bool             { return p.TerminalF }
func (p *fakeParticipant) KVBucket() string             { return "MISSIONS" }
func (p *fakeParticipant) KVKey(entityID string) string { return entityID }
func (p *fakeParticipant) ParentEntityID() string       { return p.ParentMissionF }

// fakeManager implements LifecycleManager. State lives in memory;
// each method records its calls for assertion. Suitable for any
// gateway path that doesn't require KV-revision-based History or
// Watch behaviour.
type fakeManager struct {
	mu                sync.Mutex
	entities          map[string]map[string]*fakeParticipant // workflow → id → state
	defs              []lifecycle.WorkflowDef
	historyByID       map[string][]lifecycle.TransitionEvent
	writableFields    map[string]map[string]bool // workflow → field → allowed
	updateCalls       int
	transitionCalls   int
	transitionErr     error
	updateErr         error
	listErr           error            // global List error (any workflow)
	listErrByWorkflow map[string]error // per-workflow List error (overrides listErr when set)
	watchCh           chan lifecycle.Participant
	watchErr          error
}

func newFakeManager() *fakeManager {
	return &fakeManager{
		entities:       make(map[string]map[string]*fakeParticipant),
		historyByID:    make(map[string][]lifecycle.TransitionEvent),
		writableFields: make(map[string]map[string]bool),
	}
}

func (m *fakeManager) registerWorkflow(workflow string, transitions lifecycle.Transitions) {
	m.defs = append(m.defs, lifecycle.WorkflowDef{
		Workflow:               workflow,
		KVBucket:               "MISSIONS",
		Transitions:            transitions,
		OperatorWritableFields: []string{"owner_org_id"},
	})
	m.writableFields[workflow] = map[string]bool{"owner_org_id": true}
}

func (m *fakeManager) seed(workflow string, p *fakeParticipant) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entities[workflow] == nil {
		m.entities[workflow] = make(map[string]*fakeParticipant)
	}
	p.WorkflowF = workflow
	m.entities[workflow][p.EntityIDF] = p
}

func (m *fakeManager) ListWorkflows() []lifecycle.WorkflowDef {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]lifecycle.WorkflowDef, len(m.defs))
	copy(out, m.defs)
	return out
}

func (m *fakeManager) List(_ context.Context, workflow string, opts lifecycle.ListOptions) ([]lifecycle.Participant, error) {
	if perWorkflowErr, ok := m.listErrByWorkflow[workflow]; ok {
		return nil, perWorkflowErr
	}
	if m.listErr != nil {
		return nil, m.listErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	byID, ok := m.entities[workflow]
	if !ok {
		return nil, fmt.Errorf("%w: workflow %q", lifecycle.ErrWorkflowNotRegistered, workflow)
	}
	out := make([]lifecycle.Participant, 0, len(byID))
	for _, p := range byID {
		if opts.Phase != "" && p.Phase() != opts.Phase {
			continue
		}
		if opts.Active && p.IsTerminal() {
			continue
		}
		if opts.Match != nil {
			skip := false
			for k, v := range opts.Match {
				if k == "owner_org_id" && p.OwnerOrgIDF != fmt.Sprintf("%v", v) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}
		out = append(out, p)
	}
	if opts.Offset > 0 && opts.Offset < len(out) {
		out = out[opts.Offset:]
	} else if opts.Offset >= len(out) {
		out = nil
	}
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

func (m *fakeManager) Get(_ context.Context, workflow, entityID string) (lifecycle.Participant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entities[workflow]; !ok {
		return nil, fmt.Errorf("%w: workflow %q", lifecycle.ErrWorkflowNotRegistered, workflow)
	}
	p, ok := m.entities[workflow][entityID]
	if !ok {
		return nil, fmt.Errorf("%w: workflow=%q entity_id=%q", lifecycle.ErrEntityNotFound, workflow, entityID)
	}
	return p, nil
}

func (m *fakeManager) History(_ context.Context, workflow, entityID string) ([]lifecycle.TransitionEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entities[workflow]; !ok {
		return nil, fmt.Errorf("%w: workflow %q", lifecycle.ErrWorkflowNotRegistered, workflow)
	}
	if _, ok := m.entities[workflow][entityID]; !ok {
		return nil, fmt.Errorf("%w: workflow=%q entity_id=%q", lifecycle.ErrEntityNotFound, workflow, entityID)
	}
	return m.historyByID[entityID], nil
}

func (m *fakeManager) Children(_ context.Context, parentEntityID string) ([]lifecycle.Participant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []lifecycle.Participant
	for _, byID := range m.entities {
		for _, p := range byID {
			if p.ParentMissionF == parentEntityID {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

func (m *fakeManager) UpdateFromOperator(_ context.Context, workflow, entityID string, patch map[string]any) error {
	m.updateCalls++
	if m.updateErr != nil {
		return m.updateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entities[workflow]; !ok {
		return fmt.Errorf("%w: workflow %q", lifecycle.ErrWorkflowNotRegistered, workflow)
	}
	p, ok := m.entities[workflow][entityID]
	if !ok {
		return fmt.Errorf("%w: workflow=%q entity_id=%q", lifecycle.ErrEntityNotFound, workflow, entityID)
	}
	for field, val := range patch {
		allowed := m.writableFields[workflow]
		if allowed == nil || !allowed[field] {
			return fmt.Errorf("%w: workflow=%q field=%q", lifecycle.ErrFieldNotOperatorWritable, workflow, field)
		}
		if field == "owner_org_id" {
			p.OwnerOrgIDF = fmt.Sprintf("%v", val)
		}
	}
	return nil
}

func (m *fakeManager) Transition(_ context.Context, workflow, entityID, newPhase string, _ lifecycle.TransitionSource, _ string) error {
	m.transitionCalls++
	if m.transitionErr != nil {
		return m.transitionErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.entities[workflow]; !ok {
		return fmt.Errorf("%w: workflow %q", lifecycle.ErrWorkflowNotRegistered, workflow)
	}
	p, ok := m.entities[workflow][entityID]
	if !ok {
		return fmt.Errorf("%w: workflow=%q entity_id=%q", lifecycle.ErrEntityNotFound, workflow, entityID)
	}
	p.PhaseF = newPhase
	return nil
}

func (m *fakeManager) Watch(_ context.Context, _ string) (<-chan lifecycle.Participant, error) {
	if m.watchErr != nil {
		return nil, m.watchErr
	}
	if m.watchCh == nil {
		m.watchCh = make(chan lifecycle.Participant, 8)
	}
	return m.watchCh, nil
}

// --- Production-wire setup ---

// newTestGateway constructs a Component with a fakeManager, mounts
// it on a real http.ServeMux, and serves via httptest.NewServer.
// Returns the server (caller closes) + the fake manager so tests
// can drive state + assert calls.
func newTestGateway(t *testing.T, prefix string) (*httptest.Server, *fakeManager) {
	t.Helper()
	mgr := newFakeManager()
	mgr.registerWorkflow("mission", lifecycle.Transitions{
		"planning":  {"flying", "aborted"},
		"flying":    {"completed", "failed"},
		"completed": {},
		"failed":    {},
		"aborted":   {},
	})
	cfg := DefaultConfig()
	comp := &Component{
		name:    ComponentName,
		config:  cfg,
		manager: mgr,
		logger:  slog.Default(),
	}
	mux := http.NewServeMux()
	comp.RegisterHTTPHandlers(prefix, mux)
	srv := httptest.NewServer(mux)
	return srv, mgr
}

// --- Endpoint tests ---

func TestListWorkflows_Success(t *testing.T) {
	t.Parallel()
	srv, mgr := newTestGateway(t, "/gw")
	defer srv.Close()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-1", PhaseF: "planning"})
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-2", PhaseF: "flying"})

	resp, err := http.Get(srv.URL + "/gw/workflows")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d, body=%s", resp.StatusCode, mustBody(resp))
	}
	var got []map[string]any
	mustDecodeJSON(t, resp, &got)
	if len(got) != 1 {
		t.Fatalf("expected 1 workflow type, got %d", len(got))
	}
	if got[0]["workflow"] != "mission" {
		t.Errorf("workflow name: got %v", got[0]["workflow"])
	}
	counts, _ := got[0]["counts_by_phase"].(map[string]any)
	if counts["planning"] != float64(1) || counts["flying"] != float64(1) {
		t.Errorf("phase counts: got %v", counts)
	}
}

// TestListWorkflows_PartialListFailure exercises the B2 fix: when one
// workflow's List call fails (e.g. KV unreachable), the gateway must
// still render every workflow type but mark the failing one with a
// `counts_error` field — silent fall-through to empty counts was the
// original silent-fail bug. Regression-locks the loud-fail discipline
// per [[feedback_no_clean_except_handwave]].
func TestListWorkflows_PartialListFailure(t *testing.T) {
	t.Parallel()
	srv, mgr := newTestGateway(t, "/gw")
	defer srv.Close()
	// Register a second workflow so we can confirm one failure doesn't
	// blow up the whole response.
	mgr.registerWorkflow("calibration", lifecycle.Transitions{"idle": {"running"}, "running": {}})
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-1", PhaseF: "planning"})
	mgr.seed("calibration", &fakeParticipant{EntityIDF: "c-1", PhaseF: "idle"})

	mgr.listErrByWorkflow = map[string]error{
		"mission": fmt.Errorf("simulated KV unreachable for MISSIONS bucket"),
	}

	resp, err := http.Get(srv.URL + "/gw/workflows")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d (partial List failure must NOT fail the whole response)", resp.StatusCode)
	}
	var got []map[string]any
	mustDecodeJSON(t, resp, &got)
	if len(got) != 2 {
		t.Fatalf("expected 2 workflow entries, got %d", len(got))
	}
	byName := map[string]map[string]any{}
	for _, e := range got {
		byName[e["workflow"].(string)] = e
	}
	mission := byName["mission"]
	calib := byName["calibration"]
	// The failing workflow must carry counts_error AND empty counts.
	if mission["counts_error"] == nil || mission["counts_error"] == "" {
		t.Errorf("mission entry missing counts_error: %v", mission)
	}
	if counts, ok := mission["counts_by_phase"].(map[string]any); !ok || len(counts) != 0 {
		t.Errorf("mission counts_by_phase should be empty on failure, got %v", mission["counts_by_phase"])
	}
	// The healthy workflow must render normally (no counts_error,
	// counts populated).
	if _, hasErr := calib["counts_error"]; hasErr {
		t.Errorf("calibration entry should have no counts_error, got %v", calib["counts_error"])
	}
	if counts, _ := calib["counts_by_phase"].(map[string]any); counts["idle"] != float64(1) {
		t.Errorf("calibration counts_by_phase: got %v", calib["counts_by_phase"])
	}
}

func TestListInstances_FiltersByPhase(t *testing.T) {
	t.Parallel()
	srv, mgr := newTestGateway(t, "/gw")
	defer srv.Close()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-1", PhaseF: "planning"})
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-2", PhaseF: "flying"})

	resp, err := http.Get(srv.URL + "/gw/workflows/mission?phase=flying")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var got []map[string]any
	mustDecodeJSON(t, resp, &got)
	if len(got) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(got))
	}
	if got[0]["entity_id"] != "m-1" && got[0]["entity_id"] != "m-2" {
		t.Errorf("entity_id: got %v", got[0]["entity_id"])
	}
}

func TestListInstances_LimitOffset(t *testing.T) {
	t.Parallel()
	srv, mgr := newTestGateway(t, "/gw")
	defer srv.Close()
	for i := 0; i < 5; i++ {
		mgr.seed("mission", &fakeParticipant{EntityIDF: fmt.Sprintf("m-%d", i), PhaseF: "planning"})
	}
	resp, err := http.Get(srv.URL + "/gw/workflows/mission?limit=2&offset=1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var got []map[string]any
	mustDecodeJSON(t, resp, &got)
	if len(got) != 2 {
		t.Errorf("limit=2 should return 2, got %d", len(got))
	}
}

func TestListInstances_InvalidLimit(t *testing.T) {
	t.Parallel()
	srv, _ := newTestGateway(t, "/gw")
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/gw/workflows/mission?limit=abc")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestListInstances_UnknownWorkflow_404(t *testing.T) {
	t.Parallel()
	srv, _ := newTestGateway(t, "/gw")
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/gw/workflows/no-such-workflow")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 (ErrWorkflowNotRegistered → 404), got %d", resp.StatusCode)
	}
}

func TestGetInstance_Success(t *testing.T) {
	t.Parallel()
	srv, mgr := newTestGateway(t, "/gw")
	defer srv.Close()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-1", PhaseF: "flying", OwnerOrgIDF: "acme"})

	resp, err := http.Get(srv.URL + "/gw/workflows/mission/m-1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var got map[string]any
	mustDecodeJSON(t, resp, &got)
	if got["entity_id"] != "m-1" || got["phase"] != "flying" || got["owner_org_id"] != "acme" {
		t.Errorf("instance state: got %v", got)
	}
}

func TestGetInstance_NotFound_404(t *testing.T) {
	t.Parallel()
	srv, _ := newTestGateway(t, "/gw")
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/gw/workflows/mission/missing")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestStatePatch_Success(t *testing.T) {
	t.Parallel()
	srv, mgr := newTestGateway(t, "/gw")
	defer srv.Close()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-1", PhaseF: "planning"})

	body := bytes.NewBufferString(`{"owner_org_id": "acme"}`)
	resp, err := http.Post(srv.URL+"/gw/workflows/mission/m-1/state", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d, body=%s", resp.StatusCode, mustBody(resp))
	}
	if mgr.updateCalls != 1 {
		t.Errorf("UpdateFromOperator should be called once, got %d", mgr.updateCalls)
	}
	if mgr.entities["mission"]["m-1"].OwnerOrgIDF != "acme" {
		t.Errorf("patch didn't land, owner_org_id=%q", mgr.entities["mission"]["m-1"].OwnerOrgIDF)
	}
}

func TestStatePatch_FieldRejected_400(t *testing.T) {
	t.Parallel()
	srv, mgr := newTestGateway(t, "/gw")
	defer srv.Close()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-1", PhaseF: "planning"})

	// "phase" isn't in writableFields → fake returns ErrFieldNotOperatorWritable.
	body := bytes.NewBufferString(`{"phase": "completed"}`)
	resp, err := http.Post(srv.URL+"/gw/workflows/mission/m-1/state", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestStatePatch_InvalidJSON_400(t *testing.T) {
	t.Parallel()
	srv, _ := newTestGateway(t, "/gw")
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/gw/workflows/mission/m-1/state", "application/json",
		bytes.NewBufferString(`not json`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestStatePatch_MethodNotAllowed_405(t *testing.T) {
	t.Parallel()
	srv, _ := newTestGateway(t, "/gw")
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/gw/workflows/mission/m-1/state")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestOperatorTransition_Success(t *testing.T) {
	t.Parallel()
	srv, mgr := newTestGateway(t, "/gw")
	defer srv.Close()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-1", PhaseF: "planning"})

	body := bytes.NewBufferString(`{"phase": "flying", "note": "operator nudge"}`)
	resp, err := http.Post(srv.URL+"/gw/workflows/mission/m-1/transition", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d, body=%s", resp.StatusCode, mustBody(resp))
	}
	if mgr.entities["mission"]["m-1"].PhaseF != "flying" {
		t.Errorf("transition didn't land, phase=%q", mgr.entities["mission"]["m-1"].PhaseF)
	}
	if mgr.transitionCalls != 1 {
		t.Errorf("Transition should be called once, got %d", mgr.transitionCalls)
	}
}

func TestOperatorTransition_MissingPhase_400(t *testing.T) {
	t.Parallel()
	srv, _ := newTestGateway(t, "/gw")
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/gw/workflows/mission/m-1/transition",
		"application/json", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing phase, got %d", resp.StatusCode)
	}
}

func TestHistory_Success(t *testing.T) {
	t.Parallel()
	srv, mgr := newTestGateway(t, "/gw")
	defer srv.Close()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "m-1", PhaseF: "flying"})
	mgr.historyByID["m-1"] = []lifecycle.TransitionEvent{
		{From: "planning", To: "flying", At: time.Now(), Triggered: lifecycle.TransitionSourceRule, Note: ""},
	}

	resp, err := http.Get(srv.URL + "/gw/workflows/mission/m-1/history")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var got []map[string]any
	mustDecodeJSON(t, resp, &got)
	if len(got) != 1 || got[0]["from"] != "planning" {
		t.Errorf("history: got %v", got)
	}
}

func TestChildren_Success(t *testing.T) {
	t.Parallel()
	srv, mgr := newTestGateway(t, "/gw")
	defer srv.Close()
	mgr.seed("mission", &fakeParticipant{EntityIDF: "parent", PhaseF: "planning"})
	mgr.seed("mission", &fakeParticipant{EntityIDF: "child-1", PhaseF: "planning", ParentMissionF: "parent"})
	mgr.seed("mission", &fakeParticipant{EntityIDF: "child-2", PhaseF: "flying", ParentMissionF: "parent"})

	resp, err := http.Get(srv.URL + "/gw/workflows/mission/parent/children")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var got []map[string]any
	mustDecodeJSON(t, resp, &got)
	if len(got) != 2 {
		t.Errorf("expected 2 children, got %d", len(got))
	}
}

func TestUnknownSubresource_404(t *testing.T) {
	t.Parallel()
	srv, _ := newTestGateway(t, "/gw")
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/gw/workflows/mission/m-1/garbage")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// --- WebSocket ---

func TestWebSocket_StreamsUpdates(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.registerWorkflow("mission", lifecycle.Transitions{"planning": {"flying"}, "flying": {}})
	mgr.watchCh = make(chan lifecycle.Participant, 4)

	cfg := DefaultConfig()
	comp := &Component{
		name:    ComponentName,
		config:  cfg,
		manager: mgr,
		logger:  slog.Default(),
		upgrade: websocketUpgrader(),
	}
	mux := http.NewServeMux()
	comp.RegisterHTTPHandlers("/gw", mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/gw/workflows/mission?stream=true"
	conn, _, err := dialWS(wsURL)
	if err != nil {
		t.Fatalf("WS dial: %v", err)
	}
	defer conn.Close()

	// Push a Participant snapshot through the watch channel.
	p := &fakeParticipant{EntityIDF: "m-ws-1", WorkflowF: "mission", PhaseF: "flying"}
	mgr.watchCh <- p

	// Read with deadline.
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	var got map[string]any
	if err := conn.ReadJSON(&got); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if got["entity_id"] != "m-ws-1" || got["phase"] != "flying" {
		t.Errorf("WS payload: got %v", got)
	}
}

func TestWebSocket_DisabledReturns403(t *testing.T) {
	t.Parallel()
	mgr := newFakeManager()
	mgr.registerWorkflow("mission", lifecycle.Transitions{"planning": {}})

	cfg := DefaultConfig()
	cfg.EnableWebSocket = false
	comp := &Component{
		name:    ComponentName,
		config:  cfg,
		manager: mgr,
		logger:  slog.Default(),
	}
	mux := http.NewServeMux()
	comp.RegisterHTTPHandlers("/gw", mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/gw/workflows/mission?stream=true")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 when WS disabled, got %d", resp.StatusCode)
	}
}

// --- Component lifecycle ---

func TestFactory_RejectsNilManager(t *testing.T) {
	t.Parallel()
	// Loud-fail: deps.LifecycleManager nil → factory MUST reject so
	// the wiring gap surfaces at boot, not at the first endpoint hit.
	_, err := CreateLifecycleGateway(nil, component.Dependencies{})
	if err == nil {
		t.Fatalf("expected factory to reject nil LifecycleManager")
	}
	if !strings.Contains(err.Error(), "LifecycleManager") {
		t.Errorf("error should name the missing field, got %q", err.Error())
	}
}

func TestFactory_AcceptsRealManagerType(t *testing.T) {
	t.Parallel()
	// Locks the interface-vs-concrete invariant: the production wiring
	// path passes deps.LifecycleManager which is *lifecycle.Manager;
	// the factory must accept the concrete type (which implicitly
	// satisfies our local LifecycleManager interface).
	//
	// SCOPE: this test verifies type-acceptance ONLY — the zero-value
	// Manager passed here has no registered workflows + no store
	// factory wired, so any operation against it would either return
	// empty (ListWorkflows) or panic (List/Get/Update/etc). DO NOT
	// extend this test to exercise behavior; use a fakeManager via
	// newTestGateway or TestFactory_FullProductionWire for behavior
	// coverage. The factory-accepts-concrete invariant is what's
	// load-bearing here; the wired-and-functional invariant lives
	// elsewhere.
	deps := component.Dependencies{LifecycleManager: &lifecycle.Manager{}}
	comp, err := CreateLifecycleGateway(nil, deps)
	if err != nil {
		t.Fatalf("expected factory to accept *lifecycle.Manager, got %v", err)
	}
	if comp == nil {
		t.Fatalf("nil component returned")
	}
}

// TestFactory_FullProductionWire goes through the exact construction
// path production uses: CreateLifecycleGateway → Initialize → Start →
// RegisterHTTPHandlers → httptest.NewServer → HTTP request. A
// regression that broke `ApplyDefaults`, factory's CheckOrigin wiring,
// or any field initialization missed by the test's direct-struct
// constructor would surface here (B4 reviewer fix — earlier tests
// bypassed the factory and zero-init'd Upgrader).
func TestFactory_FullProductionWire(t *testing.T) {
	t.Parallel()
	// Bypass the nil-check by providing a (zero-value) Manager. The
	// concrete *lifecycle.Manager satisfies our LifecycleManager
	// interface; even though its operations would fail without
	// registrations, we only need the factory + route wiring to
	// succeed for this test.
	deps := component.Dependencies{
		LifecycleManager: &lifecycle.Manager{},
		Logger:           slog.Default(),
	}
	disc, err := CreateLifecycleGateway(nil, deps)
	if err != nil {
		t.Fatalf("CreateLifecycleGateway: %v", err)
	}
	comp, ok := disc.(*Component)
	if !ok {
		t.Fatalf("expected *Component, got %T", disc)
	}
	// Swap in the fake manager AFTER construction so the gateway has
	// real workflow registrations to read. The factory path is what
	// we want to lock; the manager is the consumed dependency.
	mgr := newFakeManager()
	mgr.registerWorkflow("mission", lifecycle.Transitions{"planning": {"flying"}, "flying": {}})
	mgr.seed("mission", &fakeParticipant{EntityIDF: "wire-1", PhaseF: "planning"})
	comp.manager = mgr

	if err := comp.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := comp.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = comp.Stop(0) }()

	mux := http.NewServeMux()
	comp.RegisterHTTPHandlers("/wire", mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/wire/workflows/mission/wire-1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d, body=%s", resp.StatusCode, mustBody(resp))
	}
	var got map[string]any
	mustDecodeJSON(t, resp, &got)
	if got["entity_id"] != "wire-1" {
		t.Errorf("entity_id: got %v", got["entity_id"])
	}
	// Confirm factory wired the Upgrader (zero-value Upgrader's
	// CheckOrigin nil-panics on WS upgrade; if the factory dropped
	// this, the WS test would crash — caught here without needing
	// to upgrade since the field is set non-nil).
	if comp.upgrade.CheckOrigin == nil {
		t.Errorf("factory did not wire Upgrader.CheckOrigin")
	}
}

// TestOpenAPISpec_DocumentsAllEndpoints locks the operator-facing
// OpenAPI surface: the 7 HTTP endpoints (plus the WebSocket-upgrade
// variant on /workflows/{type}) must appear in the spec so operator
// dashboards auto-discover them. A regression that dropped an entry
// would silently de-document an endpoint that still serves traffic.
func TestOpenAPISpec_DocumentsAllEndpoints(t *testing.T) {
	t.Parallel()
	spec := lifecycleGatewayOpenAPISpec()
	wantPaths := []string{
		"/workflows",
		"/workflows/{type}",
		"/workflows/{type}/{id}",
		"/workflows/{type}/{id}/history",
		"/workflows/{type}/{id}/children",
		"/workflows/{type}/{id}/state",
		"/workflows/{type}/{id}/transition",
	}
	for _, p := range wantPaths {
		if _, ok := spec.Paths[p]; !ok {
			t.Errorf("spec missing path %q", p)
		}
	}
	// Method coverage: state + transition must be POST-only; the rest GET-only.
	for path, ps := range spec.Paths {
		switch path {
		case "/workflows/{type}/{id}/state", "/workflows/{type}/{id}/transition":
			if ps.POST == nil {
				t.Errorf("path %q missing POST operation", path)
			}
			if ps.GET != nil {
				t.Errorf("path %q should not expose GET", path)
			}
		default:
			if ps.GET == nil {
				t.Errorf("path %q missing GET operation", path)
			}
		}
	}
}

func TestFactory_ValidatesPathPrefix(t *testing.T) {
	t.Parallel()
	// S4: "/" or "//" trim to "" → factory must reject so the
	// gateway doesn't register a bogus mux pattern.
	rawCfg := []byte(`{"path_prefix": "/"}`)
	deps := component.Dependencies{LifecycleManager: &lifecycle.Manager{}}
	_, err := CreateLifecycleGateway(rawCfg, deps)
	if err == nil {
		t.Fatalf("expected factory to reject path_prefix=\"/\"")
	}
	if !strings.Contains(err.Error(), "path_prefix") {
		t.Errorf("error should name path_prefix, got %q", err.Error())
	}
}

// --- helpers ---

func websocketUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}
}

func dialWS(url string) (*websocket.Conn, *http.Response, error) {
	return websocket.DefaultDialer.Dial(url, nil)
}

func mustBody(r *http.Response) string {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return "<read error: " + err.Error() + ">"
	}
	return string(b)
}

func mustDecodeJSON(t *testing.T, r *http.Response, into any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}
