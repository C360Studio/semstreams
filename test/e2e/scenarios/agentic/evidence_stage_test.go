package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// spec: e2e-evidence / Required observations determine success
func TestAgenticToolStageRejectsMissingForeignAndFailedResult(t *testing.T) {
	loopID := pagedLoopToken
	request := agentic.TrajectoryFactV1{
		LoopDigest: agentic.TrajectoryLoopDigest(loopID), Kind: agentic.TrajectoryKindToolRequested,
		SourceKind: agentic.TrajectorySourceToolCall, SourceCorrelation: "call-controlled",
		ToolPreview: "query_entity", Status: agentic.TrajectoryStatusRequested,
	}
	completion := agentic.TrajectoryFactV1{
		LoopDigest: agentic.TrajectoryLoopDigest(loopID), Kind: agentic.TrajectoryKindToolCompleted,
		SourceKind: agentic.TrajectorySourceToolCall, SourceCorrelation: "call-controlled",
		ToolPreview: "query_entity", Status: agentic.TrajectoryStatusCompleted,
	}
	for _, tc := range []struct {
		name  string
		facts []agentic.TrajectoryFactV1
		good  bool
	}{
		{name: "matched", facts: []agentic.TrajectoryFactV1{request, completion}, good: true},
		{name: "missing result", facts: []agentic.TrajectoryFactV1{request}},
		{name: "foreign result", facts: []agentic.TrajectoryFactV1{request, func() agentic.TrajectoryFactV1 {
			foreign := completion
			foreign.SourceCorrelation = "call-other"
			return foreign
		}()}},
		{name: "failed result", facts: []agentic.TrajectoryFactV1{request, func() agentic.TrajectoryFactV1 {
			failed := completion
			failed.Status = agentic.TrajectoryStatusFailed
			return failed
		}()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newAgenticToolStageFixture(t, loopID, tc.facts)
			result := &scenarios.Result{Details: map[string]any{
				"task_id": "task-controlled", "loop_id": loopID,
				"terminal_task_id": "task-controlled", "terminal_loop_id": loopID,
			}, Metrics: map[string]any{}}
			if err := result.DeclareChecks("run-controlled", "agentic", s.CheckRequirements()); err != nil {
				t.Fatal(err)
			}
			if err := s.captureBaseline(t.Context(), result); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{agenticTerminalCheck, agenticChunksCheck} {
				if err := result.RecordCheck(scenarios.CheckObservation{
					ID: id, RunID: "run-controlled", MemberID: "agentic", Status: "passed",
				}); err != nil {
					t.Fatal(err)
				}
			}
			stageErr := s.verifyToolExecution(t.Context(), result)
			if tc.good && stageErr != nil {
				t.Fatalf("healthy stage: %v", stageErr)
			}
			if !tc.good && stageErr == nil {
				t.Fatal("invalid tool result returned nil from actual stage")
			}
			var toolObservations []scenarios.CheckObservation
			for _, observation := range result.CheckObservations {
				if observation.ID == agenticToolCheck {
					toolObservations = append(toolObservations, observation)
				}
			}
			if len(toolObservations) != 1 {
				t.Fatalf("tool observations = %#v", toolObservations)
			}
			finalErr := result.FinalizeChecks()
			if tc.good {
				if toolObservations[0].Status != "passed" || finalErr != nil || !result.Success || result.EvidenceStatus != "complete" {
					t.Fatalf("healthy tool evidence = %#v, final=%v, success=%v", toolObservations[0], finalErr, result.Success)
				}
				if toolObservations[0].Evidence["tool_call_id"] != "call-controlled" ||
					toolObservations[0].Evidence["metric_delta"] != "1" {
					t.Fatalf("tool identity/delta evidence = %#v", toolObservations[0].Evidence)
				}
			} else if toolObservations[0].Status != "failed" || finalErr == nil || result.Success ||
				!strings.Contains(finalErr.Error(), agenticToolCheck) {
				t.Fatalf("invalid tool evidence = %#v, final=%v, success=%v", toolObservations[0], finalErr, result.Success)
			}
		})
	}
}

func newAgenticToolStageFixture(t *testing.T, loopID string, facts []agentic.TrajectoryFactV1) *Scenario {
	t.Helper()
	srv, err := natsserver.NewServer(&natsserver.Options{
		Port: -1, NoLog: true, NoSigs: true, JetStream: true, StoreDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("in-process NATS not ready")
	}
	t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })
	responder, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { responder.Close() })
	page := agentic.TrajectoryPage{
		SchemaVersion: agentic.TrajectorySchemaV1, LoopID: loopID, Coverage: "observed",
		Facts: facts, ObservedTotals: agentic.TrajectoryObservedTotals{Facts: uint64(len(facts))},
	}
	payload, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	_, err = responder.Subscribe("agentic.query.trajectory", func(msg *nats.Msg) {
		var request agentic.TrajectoryQueryRequest
		if json.Unmarshal(msg.Data, &request) != nil || request.LoopID != loopID {
			_ = msg.Respond([]byte(`{"error":"foreign loop"}`))
			return
		}
		_ = msg.Respond(payload)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := responder.Flush(); err != nil {
		t.Fatal(err)
	}
	vc, err := client.NewNATSValidationClient(t.Context(), srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = vc.Close(context.Background()) })

	var metricValue atomic.Int64
	metricsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "semstreams_agentic_tools_executions_total{tool_name=\"query_entity\",status=\"success\"} %d\n", metricValue.Add(1)-1)
	}))
	t.Cleanup(metricsServer.Close)
	cfg := DefaultConfig()
	cfg.EvidenceRunID, cfg.EvidenceMemberID = "run-controlled", "agentic"
	s := NewScenario(nil, cfg)
	s.nats = vc
	s.metrics = client.NewMetricsClient(metricsServer.URL)
	return s
}

func TestAgenticMetricLabelUsesShippedEndpointModel(t *testing.T) {
	raw, err := os.ReadFile("../../../../configs/agentic.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		ModelRegistry struct {
			Endpoints map[string]struct {
				Model string `json:"model"`
			} `json:"endpoints"`
		} `json:"model_registry"`
		Components struct {
			AgenticModel struct {
				Config struct {
					Endpoints map[string]struct {
						Model string `json:"model"`
					} `json:"endpoints"`
				} `json:"config"`
			} `json:"agentic-model"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if model := config.ModelRegistry.Endpoints["mock"].Model; model != agenticMetricsModel || model == "mock" {
		t.Fatalf("registry endpoint model = %q, metric label = %q; alias mock is not the model name", model, agenticMetricsModel)
	}
	if model := config.Components.AgenticModel.Config.Endpoints["mock"].Model; model != agenticMetricsModel {
		t.Fatalf("agentic-model endpoint model = %q, metric label = %q", model, agenticMetricsModel)
	}
}
