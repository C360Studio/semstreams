package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/config"
)

// spec: e2e-evidence / Evidence cannot improve by omission
func TestCoreHealthRecordsComponentObservation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case config.ServicePaths.Health:
			_, _ = w.Write([]byte(`{"healthy":true,"status":"healthy"}`))
		case config.ComponentPaths.List:
			for i, name := range DefaultCoreHealthConfig().RequiredComponents {
				if i > 0 {
					_, _ = w.Write([]byte(","))
				} else {
					_, _ = w.Write([]byte("["))
				}
				_, _ = fmt.Fprintf(w, `{"name":%q,"enabled":true,"healthy":true}`, name)
			}
			_, _ = w.Write([]byte("]"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configuration := DefaultCoreHealthConfig()
	configuration.EvidenceRunID = "run-core-health"
	configuration.EvidenceMemberID = "core-health"
	scenario := NewCoreHealthScenario(client.NewObservabilityClient(server.URL), configuration)
	result, err := scenario.Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CheckObservations) != 1 || result.CheckObservations[0].ID != "core-health.components" || result.CheckObservations[0].Status != "passed" {
		t.Fatalf("healthy components require named observation; got %+v", result.CheckObservations)
	}
}

// spec: e2e-evidence / Observation identity is preserved
func TestCoreDataflowSentInputHasRunIdentity(t *testing.T) {
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	configuration := DefaultCoreDataflowConfig()
	configuration.MessageCount = 2
	configuration.MessageInterval = 0
	configuration.EvidenceRunID = "run-core-dataflow"
	configuration.EvidenceMemberID = "core-dataflow"
	scenario := NewCoreDataflowScenario(nil, nil, listener.LocalAddr().String(), configuration)
	result := &Result{Metrics: map[string]any{}, Details: map[string]any{}}
	if err := scenario.executeSendData(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < configuration.MessageCount; i++ {
		if err := listener.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		packet := make([]byte, 4096)
		n, _, err := listener.ReadFrom(packet)
		if err != nil {
			t.Fatal(err)
		}
		var input map[string]any
		if err := json.Unmarshal(packet[:n], &input); err != nil {
			t.Fatal(err)
		}
		if input["run_id"] != configuration.EvidenceRunID {
			t.Fatalf("sent sequence %d has foreign run identity: %+v", i, input)
		}
	}
}

// spec: e2e-evidence / Required observations determine success
func TestCorePassThroughRequiresDistinctSentContentAndIdentity(t *testing.T) {
	sent := map[int]coreSentInput{0: {Value: 40, Timestamp: 100}, 1: {Value: 60, Timestamp: 101}}
	valid := func(sequence, value int, timestamp int64) string {
		return fmt.Sprintf(`{"payload":{"data":{"run_id":"run-current","sequence":%d,"value":%d,"timestamp":%d,"type":"test"}}}`, sequence, value, timestamp)
	}
	for _, tc := range []struct {
		name             string
		lines            []string
		wantDistinct     int
		wantContentFail  bool
		wantIdentityFail bool
	}{
		{name: "distinct sent below and above 50", lines: []string{valid(0, 40, 100), valid(1, 60, 101)}, wantDistinct: 2},
		{name: "duplicate cannot inflate", lines: []string{valid(0, 40, 100), valid(0, 40, 100)}, wantDistinct: 1},
		{name: "malformed selected json", lines: []string{`{"run_id":"run-current",`, valid(0, 40, 100)}, wantDistinct: 1, wantContentFail: true, wantIdentityFail: true},
		{name: "unsent sequence", lines: []string{valid(2, 20, 102)}, wantContentFail: true, wantIdentityFail: true},
		{name: "changed value", lines: []string{valid(0, 41, 100)}, wantContentFail: true},
		{name: "foreign run in selected line", lines: []string{`{"marker":"run-current","payload":{"data":{"run_id":"run-other","sequence":0,"value":40,"timestamp":100,"type":"test"}}}`}, wantContentFail: true, wantIdentityFail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := validateCorePassThrough(tc.lines, "run-current", sent)
			if got.Distinct != tc.wantDistinct || (len(got.ContentIssues) > 0) != tc.wantContentFail ||
				(len(got.IdentityIssues) > 0) != tc.wantIdentityFail {
				t.Fatalf("validation = %+v, want distinct=%d content fail=%t identity fail=%t", got, tc.wantDistinct, tc.wantContentFail, tc.wantIdentityFail)
			}
		})
	}
}

// spec: e2e-evidence / Required observations determine success
func TestCorePassThroughRecordsUnperformedSiblingComparisonsAsFailed(t *testing.T) {
	valid := `{"payload":{"data":{"run_id":"run-current","sequence":0,"value":40,"timestamp":100,"type":"test"}}}`
	for _, tc := range []struct {
		name string
		bad  string
	}{
		{name: "malformed", bad: `{"run_id":"run-current",`},
		{name: "unsent sequence", bad: `{"payload":{"data":{"run_id":"run-current","sequence":9,"value":90,"timestamp":109,"type":"test"}}}`},
		{name: "foreign run", bad: `{"marker":"run-current","payload":{"data":{"run_id":"run-other","sequence":0,"value":40,"timestamp":100,"type":"test"}}}`},
		{name: "missing sequence", bad: `{"payload":{"data":{"run_id":"run-current","value":40,"timestamp":100,"type":"test"}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeDir := t.TempDir()
			linesPath := filepath.Join(fakeDir, "lines.jsonl")
			if err := os.WriteFile(linesPath, []byte(valid+"\n"+tc.bad+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			dockerPath := filepath.Join(fakeDir, "docker")
			script := "#!/bin/sh\ncase \"$*\" in\n  *'wc -l'*) wc -l < \"$E2E_FAKE_DOCKER_LINES\" ;;\n  *) cat \"$E2E_FAKE_DOCKER_LINES\" ;;\nesac\n"
			if err := os.WriteFile(dockerPath, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("E2E_FAKE_DOCKER_LINES", linesPath)

			cfg := DefaultCoreDataflowConfig()
			cfg.EvidenceRunID, cfg.EvidenceMemberID = "run-current", "core-dataflow"
			cfg.MinProcessed, cfg.ValidationDelay = 1, 0
			scenario := NewCoreDataflowScenario(client.NewObservabilityClient("http://127.0.0.1"), nil, "", cfg)
			scenario.sentInputs = map[int]coreSentInput{0: {Value: 40, Timestamp: 100}}
			result := &Result{Metrics: map[string]any{}, Details: map[string]any{}}
			if err := result.DeclareChecks(cfg.EvidenceRunID, cfg.EvidenceMemberID, scenario.CheckRequirements()); err != nil {
				t.Fatal(err)
			}
			if err := scenario.executeValidateProcessing(context.Background(), result); err == nil {
				t.Fatal("bad selected record was accepted")
			}
			if err := result.FinalizeChecks(); err == nil || result.Success {
				t.Fatalf("bad selected record finalized successfully: %+v", result)
			}
			statuses := map[string]CheckObservation{}
			for _, observation := range result.CheckObservations {
				statuses[observation.ID] = observation
			}
			if got := statuses["core-dataflow.pass-through-count"].Status; got != "passed" {
				t.Fatalf("valid distinct output did not satisfy independent count: %q", got)
			}
			for _, id := range []string{"core-dataflow.pass-through-content", "core-dataflow.pass-through-identity"} {
				observation := statuses[id]
				if observation.Status != "failed" || observation.Reason == "" {
					t.Fatalf("%s reported unperformed comparison as %+v", id, observation)
				}
			}
		})
	}
}
