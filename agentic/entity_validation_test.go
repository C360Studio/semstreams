package agentic_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
)

// requireUnpublishable asserts the writer's contract at the publication gate:
// the malformed payload fails Validate() naming the fault AND fails to marshal
// through BaseMessage, so an ordinary producer cannot put it on the wire.
func requireUnpublishable(t *testing.T, payload message.Payload, want string) {
	t.Helper()
	err := payload.Validate()
	require.Errorf(t, err, "Validate() accepted the malformed payload")
	require.Containsf(t, strings.ToLower(err.Error()), strings.ToLower(want), "Validate() error does not name the fault")
	_, marshalErr := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "test"))
	require.Errorf(t, marshalErr, "BaseMessage.MarshalJSON published a payload that fails Validate()")
}

// requirePublishable is the positive half: the fully populated fixture
// validates and marshals.
func requirePublishable(t *testing.T, payload message.Payload) {
	t.Helper()
	require.NoError(t, payload.Validate())
	_, err := json.Marshal(message.NewBaseMessage(payload.Schema(), payload, "test"))
	require.NoError(t, err)
}

// TestAgentLessonEntityRejectsMalformed carries every gate the emit_lesson
// argument parser used to own (ADR-080 decision 3) into the payload contract.
func TestAgentLessonEntityRejectsMalformed(t *testing.T) {
	requirePublishable(t, fullLesson())
	cases := []struct {
		name   string
		mutate func(e *agentic.AgentLessonEntity)
		want   string
	}{
		{"dotted org", func(e *agentic.AgentLessonEntity) { e.Org = "acme.corp" }, "org"},
		{"empty summary", func(e *agentic.AgentLessonEntity) { e.Summary = "" }, "summary"},
		{"control byte in summary", func(e *agentic.AgentLessonEntity) { e.Summary = "a\nb" }, "control"},
		{"empty detail", func(e *agentic.AgentLessonEntity) { e.Detail = "" }, "detail"},
		{"empty injection form", func(e *agentic.AgentLessonEntity) { e.InjectionForm = "" }, "injection_form"},
		{"control byte in injection form", func(e *agentic.AgentLessonEntity) { e.InjectionForm = "x\x1fy" }, "control"},
		{"injection form over the bound", func(e *agentic.AgentLessonEntity) { e.InjectionForm = strings.Repeat("a", 321) }, "320"},
		{"empty category", func(e *agentic.AgentLessonEntity) { e.Category = "" }, "category"},
		{"control byte in category", func(e *agentic.AgentLessonEntity) { e.Category = "a\tb" }, "control"},
		{"unknown polarity", func(e *agentic.AgentLessonEntity) { e.Polarity = "meh" }, "polarity"},
		{"unknown severity", func(e *agentic.AgentLessonEntity) { e.Severity = "warn" }, "severity"},
		{"unknown status", func(e *agentic.AgentLessonEntity) { e.Status = "draft" }, "status"},
		{"zero created_at", func(e *agentic.AgentLessonEntity) { e.CreatedAt = time.Time{} }, "created_at"},
		{"no evidence", func(e *agentic.AgentLessonEntity) { e.Evidence = nil }, "evidence"},
		{"malformed evidence id", func(e *agentic.AgentLessonEntity) { e.Evidence = []string{"not-an-entity"} }, "evidence"},
		{"no scope keys", func(e *agentic.AgentLessonEntity) { e.AppliesTo = nil }, "applies_to"},
		{"untyped scope key", func(e *agentic.AgentLessonEntity) { e.AppliesTo = []string{"go"} }, "untyped"},
		{"short id scope prefix", func(e *agentic.AgentLessonEntity) { e.AppliesTo = []string{"id:acme"} }, "segment"},
		{"malformed executed_by", func(e *agentic.AgentLessonEntity) { e.ExecutedBy = "loop-1" }, "executed_by"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := fullLesson()
			tc.mutate(e)
			requireUnpublishable(t, e, tc.want)
		})
	}
	t.Run("the three counted gates wrap their sentinels", func(t *testing.T) {
		e := fullLesson()
		e.Evidence = nil
		require.ErrorIs(t, e.Validate(), agentic.ErrLessonEvidence)
		e = fullLesson()
		e.InjectionForm = strings.Repeat("a", 321)
		require.ErrorIs(t, e.Validate(), agentic.ErrLessonBound)
		e = fullLesson()
		e.AppliesTo = []string{"go"}
		require.ErrorIs(t, e.Validate(), agentic.ErrLessonGrammar)
	})
}

// TestOpsDiagnosisEntityRejectsMalformed carries the emit_diagnosis argument
// gates into the payload contract. The first case is the Codex repro at
// b18fd518: no finding, recommendation, evidence, severity, or executor and a
// confidence of 2 validated and marshalled.
func TestOpsDiagnosisEntityRejectsMalformed(t *testing.T) {
	requirePublishable(t, fullDiagnosis())
	requireUnpublishable(t, &agentic.OpsDiagnosisEntity{Org: "acme", Platform: "ops", ID: "finding-1", Confidence: 2}, "finding")
	cases := []struct {
		name   string
		mutate func(e *agentic.OpsDiagnosisEntity)
		want   string
	}{
		{"dotted platform", func(e *agentic.OpsDiagnosisEntity) { e.Platform = "a.b" }, "platform"},
		{"empty finding", func(e *agentic.OpsDiagnosisEntity) { e.Finding = "" }, "finding"},
		{"empty recommendation", func(e *agentic.OpsDiagnosisEntity) { e.Recommendation = "" }, "recommendation"},
		{"confidence above one", func(e *agentic.OpsDiagnosisEntity) { e.Confidence = 2 }, "confidence"},
		{"confidence below zero", func(e *agentic.OpsDiagnosisEntity) { e.Confidence = -0.1 }, "confidence"},
		{"no evidence", func(e *agentic.OpsDiagnosisEntity) { e.Evidence = nil }, "evidence"},
		{"empty evidence entry", func(e *agentic.OpsDiagnosisEntity) { e.Evidence = []string{""} }, "evidence"},
		{"unknown severity", func(e *agentic.OpsDiagnosisEntity) { e.Severity = "medium" }, "severity"},
		{"malformed executed_by", func(e *agentic.OpsDiagnosisEntity) { e.ExecutedBy = "loop" }, "executed_by"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := fullDiagnosis()
			tc.mutate(e)
			requireUnpublishable(t, e, tc.want)
		})
	}
}

// TestModelEndpointEntityRejectsMalformed carries the endpoint writer's
// contract into the payload.
func TestModelEndpointEntityRejectsMalformed(t *testing.T) {
	requirePublishable(t, fullModelEndpoint())
	cases := []struct {
		name   string
		mutate func(e *agentic.ModelEndpointEntity)
		want   string
	}{
		{"dotted name", func(e *agentic.ModelEndpointEntity) { e.Name = "gpt.4o" }, "endpointname"},
		{"empty provider", func(e *agentic.ModelEndpointEntity) { e.Provider = "" }, "provider"},
		{"empty model", func(e *agentic.ModelEndpointEntity) { e.Model = "" }, "model"},
		{"negative max tokens", func(e *agentic.ModelEndpointEntity) { e.MaxTokens = -1 }, "max_tokens"},
		{"negative input price", func(e *agentic.ModelEndpointEntity) { e.InputPricePer1MTokens = -1 }, "input_price"},
		{"negative output price", func(e *agentic.ModelEndpointEntity) { e.OutputPricePer1MTokens = -1 }, "output_price"},
		{"negative rate limit", func(e *agentic.ModelEndpointEntity) { e.RequestsPerMinute = -1 }, "requests_per_minute"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := fullModelEndpoint()
			tc.mutate(e)
			requireUnpublishable(t, e, tc.want)
		})
	}
}

// TestLoopExecutionEntityRejectsMalformed: the spawn-identity payload carries
// its TaskMessage's own contract.
func TestLoopExecutionEntityRejectsMalformed(t *testing.T) {
	requirePublishable(t, fullLoopExecution())
	cases := []struct {
		name   string
		mutate func(e *agentic.LoopExecutionEntity)
		want   string
	}{
		{"dotted loop id", func(e *agentic.LoopExecutionEntity) { e.LoopID = "a.b" }, "loop"},
		{"nil task", func(e *agentic.LoopExecutionEntity) { e.Task = nil }, "task"},
		{"task without task_id", func(e *agentic.LoopExecutionEntity) { e.Task.TaskID = "" }, "task_id"},
		{"task without role", func(e *agentic.LoopExecutionEntity) { e.Task.Role = "" }, "role"},
		{"task without model", func(e *agentic.LoopExecutionEntity) { e.Task.Model = "" }, "model"},
		{"task without prompt", func(e *agentic.LoopExecutionEntity) { e.Task.Prompt = "" }, "prompt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := fullLoopExecution()
			tc.mutate(e)
			requireUnpublishable(t, e, tc.want)
		})
	}
}

// TestWebObservationEntityRejectsMalformed carries both observation writers'
// contracts into the payload, beyond the tool discriminator.
func TestWebObservationEntityRejectsMalformed(t *testing.T) {
	requirePublishable(t, fullWebObservation(agentic.WebObservationToolHTTPRequest))
	requirePublishable(t, fullWebObservation(agentic.WebObservationToolWebSearch))
	cases := []struct {
		name   string
		tool   agentic.WebObservationTool
		mutate func(e *agentic.WebObservationEntity)
		want   string
	}{
		{"unknown tool", agentic.WebObservationToolHTTPRequest, func(e *agentic.WebObservationEntity) { e.Tool = "ftp" }, "tool"},
		{"empty url", agentic.WebObservationToolHTTPRequest, func(e *agentic.WebObservationEntity) { e.CanonicalURL = "" }, "rawurl"},
		{"malformed loop entity id", agentic.WebObservationToolHTTPRequest, func(e *agentic.WebObservationEntity) { e.LoopEntityID = "loop-1" }, "loop_entity_id"},
		{"http_request without fetched_at", agentic.WebObservationToolHTTPRequest, func(e *agentic.WebObservationEntity) { e.FetchedAt = "" }, "fetched_at"},
		{"http_request with a 404", agentic.WebObservationToolHTTPRequest, func(e *agentic.WebObservationEntity) { e.StatusCode = 404 }, "status_code"},
		{"http_request with status 0", agentic.WebObservationToolHTTPRequest, func(e *agentic.WebObservationEntity) { e.StatusCode = 0 }, "status_code"},
		{"web_search without observed_at", agentic.WebObservationToolWebSearch, func(e *agentic.WebObservationEntity) { e.ObservedAt = "yesterday" }, "observed_at"},
		{"web_search without a query", agentic.WebObservationToolWebSearch, func(e *agentic.WebObservationEntity) { e.SourceQuery = "" }, "source_query"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := fullWebObservation(tc.tool)
			tc.mutate(e)
			requireUnpublishable(t, e, tc.want)
		})
	}
}
