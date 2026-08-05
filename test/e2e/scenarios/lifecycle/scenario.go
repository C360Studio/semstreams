// Package lifecycle provides the ADR-047 end-to-end scenario that
// drives the running lifecycle-gateway + rule-engine + Manager
// substrate via the production HTTP wire (NOT internal helpers —
// the unit tests already cover helper-direct paths; this scenario
// exists to lock the wire-level contract).
//
// The mission workflow + UDP-to-Graphable processor lives in
// cmd/e2e-semstreams/mission (e2e-only fixture per ADR-047's
// "lifecycle workflows are app-owned, not framework-shipped"). The
// scenario drives:
//
//  1. GET  /lifecycle-gateway/workflows                     → mission appears with counts_by_phase.planning=1
//  2. GET  /lifecycle-gateway/workflows/mission             → seeded instance listed
//  3. GET  /lifecycle-gateway/workflows/mission/{id}        → state matches
//  4. POST /lifecycle-gateway/workflows/mission/{id}/state  → owner_org_id patched
//  5. UDP  mission.command=launch                           → rule fires lifecycle_transition; phase moves planning → flying
//  6. POST /lifecycle-gateway/workflows/mission/{id}/transition → operator drives flying → completed
//  7. GET  /lifecycle-gateway/workflows/mission/{id}/history → 2 transition events (rule + operator)
//  8. WS   /lifecycle-gateway/workflows/mission?stream=true  → live update arrives on concurrent operator patch
//
// Discipline:
//   - Drives the running container (not internal helpers) per
//     [[feedback_integration_tests_must_drive_production_wire]].
//   - Uses target: e2e in compose so cmd/e2e-semstreams runs (the
//     framework binary doesn't register the mission workflow) per
//     [[feedback_e2e_required_for_breaking_changes]].
//   - lifecycle-gateway logs ONE expected Warn (S7 default-permissive
//     origin policy when AllowedOrigins empty); we set
//     allowed_origins explicitly in lifecycle-flow.json to suppress
//     it per [[feedback_warning_not_fail_masks_integration_drift]].
package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/gorilla/websocket"
)

// MissionEntityID is the identifier seeded by the e2e binary via
// `--lifecycle-seed`. Matches docker/compose/lifecycle.yml and
// cmd/e2e-semstreams's --lifecycle-seed flag.
const MissionEntityID = "c360.test.lifecycle.gcs.mission.m001"

// Config holds runtime settings for the lifecycle scenario.
type Config struct {
	// BaseURL is the lifecycle-flow http endpoint. Default points
	// at docker/compose/lifecycle.yml's host-side mapping (38080 →
	// container 8080). Override via NewScenario for custom ports.
	BaseURL string

	// UDPEndpoint is the mission-command UDP target. Default points
	// at docker/compose/lifecycle.yml's host-side mapping (34550 →
	// container 14550).
	UDPEndpoint string

	// WSScheme is "ws" (default) or "wss". The lifecycle compose
	// doesn't terminate TLS so ws is the right choice; tests against
	// production deployments would override.
	WSScheme string

	// RuleTransitionTimeout caps the wait for the rule-driven
	// planning → flying transition after the UDP command is sent.
	// Generous default (5s) because the UDP → graph-ingest →
	// entity-watcher → rule → Manager.Transition path traverses
	// multiple async hops; tighten only if observed steady-state
	// latency demands it.
	RuleTransitionTimeout time.Duration

	// WatchEventTimeout caps how long the WebSocket stream stage
	// waits for the live patch to surface.
	WatchEventTimeout time.Duration
}

// DefaultConfig returns settings matching docker/compose/lifecycle.yml.
func DefaultConfig() *Config {
	return &Config{
		BaseURL:               "http://localhost:38080",
		UDPEndpoint:           "127.0.0.1:34550",
		WSScheme:              "ws",
		RuleTransitionTimeout: 5 * time.Second,
		WatchEventTimeout:     3 * time.Second,
	}
}

// Scenario implements scenarios.Scenario for the lifecycle tier.
type Scenario struct {
	name        string
	description string
	config      *Config
	httpClient  *http.Client
}

// NewScenario constructs the scenario with the supplied client +
// config. The observability client is unused (lifecycle scenarios
// drive the gateway directly), accepted only for symmetry with the
// existing scenario constructor signatures.
func NewScenario(_ *client.ObservabilityClient, cfg *Config) *Scenario {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	return &Scenario{
		name:        "lifecycle",
		description: "ADR-047 lifecycle-gateway + rule-engine + Manager round-trip via HTTP wire",
		config:      cfg,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

// Name returns the scenario name.
func (s *Scenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *Scenario) Description() string { return s.description }

// Setup is a no-op — the e2e binary seeds the mission via its
// --lifecycle-seed flag at container start.
func (s *Scenario) Setup(_ context.Context) error { return nil }

// Teardown is a no-op — docker compose handles container/volume teardown.
func (s *Scenario) Teardown(_ context.Context) error { return nil }

// Execute runs the staged scenario.
func (s *Scenario) Execute(ctx context.Context) (*scenarios.Result, error) {
	result := &scenarios.Result{
		ScenarioName: s.name,
		StartTime:    time.Now(),
		Metrics:      make(map[string]any),
		Details:      make(map[string]any),
	}
	stages := []struct {
		name string
		fn   func(context.Context, *scenarios.Result) error
	}{
		{"list-workflows", s.stageListWorkflows},
		{"list-instances", s.stageListInstances},
		{"get-instance", s.stageGetInstance},
		{"operator-state-patch", s.stageOperatorPatch},
		{"rule-driven-transition", s.stageRuleTransition},
		{"operator-transition", s.stageOperatorTransition},
		{"history", s.stageHistory},
		{"websocket-live-update", s.stageWebSocket},
	}
	for _, stage := range stages {
		start := time.Now()
		if err := stage.fn(ctx, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			return result, nil
		}
		result.Metrics[fmt.Sprintf("%s_duration_ms", stage.name)] = time.Since(start).Milliseconds()
	}
	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	return result, nil
}

// --- Stages ---

// stageListWorkflows verifies GET /workflows returns the mission
// workflow with counts_by_phase.planning >= 1 (the seeded instance).
func (s *Scenario) stageListWorkflows(ctx context.Context, result *scenarios.Result) error {
	var entries []workflowEntry
	if err := s.getJSON(ctx, "/lifecycle-gateway/workflows", &entries); err != nil {
		return err
	}
	for _, e := range entries {
		if e.Workflow == "mission" {
			if c := e.CountsByPhase["planning"]; c < 1 {
				return fmt.Errorf("expected counts_by_phase.planning >= 1 for mission, got %d (full entry: %+v)", c, e)
			}
			result.Details["workflow_entry"] = e
			return nil
		}
	}
	return fmt.Errorf("mission workflow not in /workflows response (got %d entries)", len(entries))
}

// stageListInstances verifies GET /workflows/mission returns the
// seeded instance.
func (s *Scenario) stageListInstances(ctx context.Context, result *scenarios.Result) error {
	var instances []map[string]any
	if err := s.getJSON(ctx, "/lifecycle-gateway/workflows/mission", &instances); err != nil {
		return err
	}
	for _, inst := range instances {
		if inst["entity_id"] == MissionEntityID {
			result.Details["seeded_instance"] = inst
			return nil
		}
	}
	return fmt.Errorf("seeded mission %q not in instance list (got %d)", MissionEntityID, len(instances))
}

// stageGetInstance verifies GET /workflows/mission/{id} returns the
// seeded mission state.
func (s *Scenario) stageGetInstance(ctx context.Context, _ *scenarios.Result) error {
	state, err := s.getMissionState(ctx)
	if err != nil {
		return err
	}
	if state.Phase != "planning" {
		return fmt.Errorf("expected initial phase=planning, got %q", state.Phase)
	}
	if state.EntityID != MissionEntityID {
		return fmt.Errorf("expected entity_id=%q, got %q", MissionEntityID, state.EntityID)
	}
	return nil
}

// stageOperatorPatch sends a POST .../state with an owner_org_id
// patch and verifies it lands.
func (s *Scenario) stageOperatorPatch(ctx context.Context, _ *scenarios.Result) error {
	body, _ := json.Marshal(map[string]any{"owner_org_id": "test-org-patch"})
	if err := s.postJSON(ctx, "/lifecycle-gateway/workflows/mission/"+MissionEntityID+"/state", body); err != nil {
		return err
	}
	state, err := s.getMissionState(ctx)
	if err != nil {
		return err
	}
	if state.OwnerOrgID != "test-org-patch" {
		return fmt.Errorf("operator patch did not land — owner_org_id=%q (want test-org-patch)", state.OwnerOrgID)
	}
	return nil
}

// stageRuleTransition sends a UDP mission-command and polls the
// mission's phase until it reaches `flying` or the timeout expires.
// Proves the rule engine wired through Manager.Transition end-to-end.
func (s *Scenario) stageRuleTransition(ctx context.Context, result *scenarios.Result) error {
	cmd := map[string]any{
		"org_id":     "c360",
		"platform":   "test",
		"mission_id": "m001",
		"command":    "launch",
	}
	payload, _ := json.Marshal(cmd)
	if err := s.sendUDP(payload); err != nil {
		return fmt.Errorf("send mission-command UDP: %w", err)
	}
	deadline := time.Now().Add(s.config.RuleTransitionTimeout)
	for time.Now().Before(deadline) {
		state, err := s.getMissionState(ctx)
		if err == nil && state.Phase == "flying" {
			result.Details["rule_transition_latency_ms"] = time.Since(deadline.Add(-s.config.RuleTransitionTimeout)).Milliseconds()
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	state, _ := s.getMissionState(ctx)
	return fmt.Errorf("rule did not transition mission to flying within %s — final phase=%q", s.config.RuleTransitionTimeout, state.Phase)
}

// stageOperatorTransition explicitly transitions flying → completed
// via the operator endpoint, proving the human-driven path.
func (s *Scenario) stageOperatorTransition(ctx context.Context, _ *scenarios.Result) error {
	body, _ := json.Marshal(map[string]any{"phase": "completed", "note": "scenario operator transition"})
	if err := s.postJSON(ctx, "/lifecycle-gateway/workflows/mission/"+MissionEntityID+"/transition", body); err != nil {
		return err
	}
	state, err := s.getMissionState(ctx)
	if err != nil {
		return err
	}
	if state.Phase != "completed" {
		return fmt.Errorf("operator transition did not land — phase=%q (want completed)", state.Phase)
	}
	return nil
}

// stageHistory verifies that the current participant entity carries the full
// recent phase sequence and correct provenance: framework seed, rule-driven
// transition, and operator-driven transition. This is the restart-stable,
// bounded operator window; it does not depend on KV revision retention.
func (s *Scenario) stageHistory(ctx context.Context, result *scenarios.Result) error {
	var events []historyEvent
	if err := s.getJSON(ctx, "/lifecycle-gateway/workflows/mission/"+MissionEntityID+"/history", &events); err != nil {
		return err
	}
	result.Details["history_event_count"] = len(events)
	result.Details["history_events"] = events

	// Expected sequence: (anything → planning) [seed], planning →
	// flying [rule], flying → completed [operator]. The seed event's
	// From field is "" because the entity didn't exist before Create.
	want := []struct{ from, to, source string }{
		{"", "planning", "framework"},
		{"planning", "flying", "rule"},
		{"flying", "completed", "operator"},
	}
	if len(events) < len(want) {
		return fmt.Errorf("expected at least %d history events (create + rule + operator transitions), got %d: %+v",
			len(want), len(events), events)
	}
	for i, w := range want {
		if events[i].From != w.from || events[i].To != w.to || events[i].Triggered != w.source {
			return fmt.Errorf("history event %d: got from=%q to=%q source=%q, want from=%q to=%q source=%q (full sequence: %+v)",
				i, events[i].From, events[i].To, events[i].Triggered, w.from, w.to, w.source, events)
		}
	}
	return nil
}

// stageWebSocket opens the watch stream, fires a concurrent operator
// patch (Note field), and asserts a live update arrives on the stream
// within WatchEventTimeout. Mission is already in `completed`
// (terminal), so we patch the operator_writable Note field rather
// than attempting a transition.
func (s *Scenario) stageWebSocket(ctx context.Context, result *scenarios.Result) error {
	wsURL := &url.URL{
		Scheme:   s.config.WSScheme,
		Host:     stripScheme(s.config.BaseURL),
		Path:     "/lifecycle-gateway/workflows/mission",
		RawQuery: "stream=true",
	}
	header := http.Header{}
	header.Set("Origin", s.config.BaseURL)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL.String(), header)
	if err != nil {
		return fmt.Errorf("ws dial %s: %w", wsURL, err)
	}
	defer func() { _ = conn.Close() }()

	// Wait for the bootstrap snapshot (the gateway delivers existing
	// instances before live updates per Manager.Watch's contract).
	if err := conn.SetReadDeadline(time.Now().Add(s.config.WatchEventTimeout)); err != nil {
		return fmt.Errorf("ws set read deadline: %w", err)
	}
	var bootstrap map[string]any
	if err := conn.ReadJSON(&bootstrap); err != nil {
		return fmt.Errorf("ws read bootstrap: %w", err)
	}
	result.Details["ws_bootstrap"] = bootstrap

	// Fire the patch concurrently after a small delay so the
	// stream is firmly in live-update mode.
	patchErr := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		body, _ := json.Marshal(map[string]any{"note": "scenario ws patch"})
		patchErr <- s.postJSON(ctx, "/lifecycle-gateway/workflows/mission/"+MissionEntityID+"/state", body)
	}()

	// Wait for the live update that reflects the patch.
	deadline := time.Now().Add(s.config.WatchEventTimeout)
	for time.Now().Before(deadline) {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return fmt.Errorf("ws extend deadline: %w", err)
		}
		var live map[string]any
		if err := conn.ReadJSON(&live); err != nil {
			return fmt.Errorf("ws read live update: %w", err)
		}
		if note, _ := live["note"].(string); note == "scenario ws patch" {
			result.Details["ws_live_update"] = live
			if err := <-patchErr; err != nil {
				return fmt.Errorf("ws patch concurrent: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("ws did not deliver live update with note=\"scenario ws patch\" within %s", s.config.WatchEventTimeout)
}

// --- helpers ---

type workflowEntry struct {
	Workflow      string         `json:"workflow"`
	KVBucket      string         `json:"kv_bucket"`
	CountsByPhase map[string]int `json:"counts_by_phase"`
}

type missionState struct {
	EntityID   string `json:"entity_id"`
	Phase      string `json:"phase"`
	OwnerOrgID string `json:"owner_org_id"`
	Note       string `json:"note"`
}

type historyEvent struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	At        time.Time `json:"at"`
	Triggered string    `json:"triggered"`
	Note      string    `json:"note,omitempty"`
}

func (s *Scenario) getJSON(ctx context.Context, path string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.config.BaseURL+path, nil)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: status=%d body=%s", path, resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *Scenario) postJSON(ctx context.Context, path string, body []byte) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.config.BaseURL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: status=%d body=%s", path, resp.StatusCode, string(respBody))
	}
	return nil
}

func (s *Scenario) getMissionState(ctx context.Context) (*missionState, error) {
	var state missionState
	if err := s.getJSON(ctx, "/lifecycle-gateway/workflows/mission/"+MissionEntityID, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *Scenario) sendUDP(payload []byte) error {
	addr, err := net.ResolveUDPAddr("udp", s.config.UDPEndpoint)
	if err != nil {
		return fmt.Errorf("resolve UDP %s: %w", s.config.UDPEndpoint, err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("dial UDP %s: %w", s.config.UDPEndpoint, err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write(payload); err != nil {
		return fmt.Errorf("write UDP: %w", err)
	}
	return nil
}

func stripScheme(rawURL string) string {
	if i := strings.Index(rawURL, "://"); i > 0 {
		return rawURL[i+3:]
	}
	return rawURL
}
