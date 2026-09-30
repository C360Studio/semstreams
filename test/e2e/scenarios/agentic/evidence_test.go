package agentic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

func TestAgenticRequiredCatalogAndLegacyConstructor(t *testing.T) {
	legacy := NewScenario(nil, DefaultConfig())
	if len(legacy.CheckRequirements()) != 4 {
		t.Fatalf("catalog = %#v, want three required checks and one TTFT diagnostic", legacy.CheckRequirements())
	}
	checks := legacy.CheckRequirements()
	want := []scenarios.CheckRequirement{
		{ID: "agentic.terminal.task-loop", Required: true},
		{ID: "agentic.tool.request-result", Required: true},
		{ID: "agentic.streaming.chunks", Required: true},
		{ID: "agentic.streaming.ttft", Required: false},
	}
	for index, check := range want {
		if checks[index] != check {
			t.Errorf("catalog[%d] = %#v, want %#v", index, checks[index], check)
		}
	}
	if legacy.config.EvidenceRunID != "" || legacy.config.EvidenceMemberID != "" {
		t.Fatal("legacy constructor invented evidence identity")
	}
}

func TestAgenticTerminalMatchesSubmittedTaskAndLoop(t *testing.T) {
	terminal := agentic.LoopCompletedEvent{TaskID: "task", LoopID: pagedLoopToken}
	if err := terminalMatchesTaskLoop(terminal, "task", pagedLoopToken); err != nil {
		t.Fatal(err)
	}
	if err := terminalMatchesTaskLoop(terminal, "foreign-task", pagedLoopToken); err == nil {
		t.Fatal("foreign task accepted")
	}
	if err := terminalMatchesTaskLoop(terminal, "task", "foreign-loop"); err == nil {
		t.Fatal("foreign loop accepted")
	}
}

// spec: e2e-evidence / Required observations determine success
func TestAgenticToolProofRequiresMatchedSuccessfulRequestAndResult(t *testing.T) {
	loopID := pagedLoopToken
	base := []agentic.TrajectoryFactV1{
		{LoopDigest: agentic.TrajectoryLoopDigest(loopID), Kind: agentic.TrajectoryKindToolRequested,
			SourceKind: agentic.TrajectorySourceToolCall, SourceCorrelation: "call-controlled",
			ToolPreview: "query_entity", Status: agentic.TrajectoryStatusRequested},
		{LoopDigest: agentic.TrajectoryLoopDigest(loopID), Kind: agentic.TrajectoryKindToolCompleted,
			SourceKind: agentic.TrajectorySourceToolCall, SourceCorrelation: "call-controlled",
			ToolPreview: "query_entity", Status: agentic.TrajectoryStatusCompleted},
	}
	for _, tc := range []struct {
		name   string
		edit   func([]agentic.TrajectoryFactV1) []agentic.TrajectoryFactV1
		wantOK bool
	}{
		{name: "matched", edit: func(f []agentic.TrajectoryFactV1) []agentic.TrajectoryFactV1 { return f }, wantOK: true},
		{name: "missing completion", edit: func(f []agentic.TrajectoryFactV1) []agentic.TrajectoryFactV1 { return f[:1] }},
		{name: "foreign call", edit: func(f []agentic.TrajectoryFactV1) []agentic.TrajectoryFactV1 {
			f[1].SourceCorrelation = "other"
			return f
		}},
		{name: "foreign loop", edit: func(f []agentic.TrajectoryFactV1) []agentic.TrajectoryFactV1 {
			f[1].LoopDigest = agentic.TrajectoryLoopDigest("other")
			return f
		}},
		{name: "failed completion", edit: func(f []agentic.TrajectoryFactV1) []agentic.TrajectoryFactV1 {
			f[1].Status = agentic.TrajectoryStatusFailed
			return f
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := tc.edit(append([]agentic.TrajectoryFactV1(nil), base...))
			callID, err := controlledToolResult(facts, loopID)
			if tc.wantOK {
				if err != nil || callID != "call-controlled" {
					t.Fatalf("call=%q err=%v", callID, err)
				}
			} else if err == nil {
				t.Fatalf("accepted facts %#v", facts)
			}
		})
	}
}

// spec: e2e-evidence / Required observations determine success
func TestAgenticStreamingRequiredDeltaAndDiagnosticTTFT(t *testing.T) {
	var body atomic.Value
	body.Store("semstreams_agentic_model_stream_chunks_total{model=\"mock-model\"} 10\n" +
		"semstreams_agentic_model_stream_ttft_seconds_count{model=\"mock-model\"} 2\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, body.Load().(string))
	}))
	defer server.Close()

	for _, tc := range []struct {
		name        string
		body        string
		closeServer bool
		wantOK      bool
	}{
		{name: "controlled delta", body: "semstreams_agentic_model_stream_chunks_total{model=\"mock-model\"} 13\nsemstreams_agentic_model_stream_ttft_seconds_count{model=\"mock-model\"} 3\n", wantOK: true},
		{name: "unrelated model", body: "semstreams_agentic_model_stream_chunks_total{model=\"other\"} 200\n"},
		{name: "alias is not metric model", body: "semstreams_agentic_model_stream_chunks_total{model=\"mock\"} 200\n"},
		{name: "zero controlled delta", body: "semstreams_agentic_model_stream_chunks_total{model=\"mock-model\"} 10\n"},
		{name: "absent series", body: "semstreams_agentic_model_stream_ttft_seconds_count{model=\"mock-model\"} 3\n"},
		{name: "TTFT absent remains diagnostic", body: "semstreams_agentic_model_stream_chunks_total{model=\"mock-model\"} 13\n", wantOK: true},
		{name: "transport error", closeServer: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseline := "semstreams_agentic_model_stream_chunks_total{model=\"mock-model\"} 10\nsemstreams_agentic_model_stream_ttft_seconds_count{model=\"mock-model\"} 2\n"
			body.Store(baseline)
			cfg := DefaultConfig()
			cfg.EvidenceRunID, cfg.EvidenceMemberID = "run", "agentic"
			s := NewScenario(nil, cfg)
			s.metrics = client.NewMetricsClient(server.URL)
			result := &scenarios.Result{Details: map[string]any{
				"task_id": "task", "loop_id": pagedLoopToken,
				"terminal_task_id": "task", "terminal_loop_id": pagedLoopToken,
			}, Metrics: map[string]any{}}
			if err := result.DeclareChecks("run", "agentic", s.CheckRequirements()); err != nil {
				t.Fatal(err)
			}
			if err := s.captureBaseline(context.Background(), result); err != nil {
				t.Fatal(err)
			}
			if tc.closeServer {
				body.Store("")
				server.Close()
			} else {
				body.Store(tc.body)
			}
			err := s.verifyStreamingMetrics(context.Background(), result)
			if tc.wantOK && err != nil {
				t.Fatalf("verifyStreamingMetrics: %v", err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("missing required streaming evidence returned nil")
			}
			var chunks *scenarios.CheckObservation
			for i := range result.CheckObservations {
				if result.CheckObservations[i].ID == "agentic.streaming.chunks" {
					chunks = &result.CheckObservations[i]
				}
			}
			if chunks == nil {
				t.Fatal("streaming check not recorded")
			}
			if tc.wantOK && chunks.Status != "passed" {
				t.Fatalf("chunks status = %q", chunks.Status)
			}
			if !tc.wantOK && chunks.Status != "failed" {
				t.Fatalf("chunks status = %q", chunks.Status)
			}
			if chunks.Evidence["task_id"] != "task" || chunks.Evidence["loop_id"] != pagedLoopToken {
				t.Fatalf("identity evidence = %#v", chunks.Evidence)
			}
			if tc.name == "TTFT absent remains diagnostic" {
				found := false
				for _, observation := range result.CheckObservations {
					if observation.ID == "agentic.streaming.ttft" {
						found = true
						if observation.Status != "failed" || !strings.Contains(observation.Reason, "TTFT") {
							t.Fatalf("TTFT = %#v", observation)
						}
					}
				}
				if !found {
					t.Fatal("missing TTFT diagnostic")
				}
			}
		})
	}
}

func TestAgenticBaselineTransportFailureRetainsFailedRequiredObservations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	cfg := DefaultConfig()
	cfg.EvidenceRunID, cfg.EvidenceMemberID = "run", "agentic"
	s := NewScenario(nil, cfg)
	s.metrics = client.NewMetricsClient(server.URL)
	result := &scenarios.Result{Details: map[string]any{}, Metrics: map[string]any{}}
	if err := result.DeclareChecks("run", "agentic", s.CheckRequirements()); err != nil {
		t.Fatal(err)
	}
	if err := s.captureBaseline(context.Background(), result); err == nil {
		t.Fatal("baseline transport error returned nil")
	}
	statuses := map[string]string{}
	for _, observation := range result.CheckObservations {
		statuses[observation.ID] = observation.Status
	}
	if statuses["agentic.tool.request-result"] != "failed" || statuses["agentic.streaming.chunks"] != "failed" ||
		statuses["agentic.streaming.ttft"] != "skipped" {
		t.Fatalf("baseline failure observations = %#v", statuses)
	}
}
