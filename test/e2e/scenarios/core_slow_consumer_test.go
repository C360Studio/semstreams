package scenarios

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/internal/e2eslowconsumer"
)

func TestParseSlowConsumerRecordsIgnoresNonJSONAndUnrelatedRecords(t *testing.T) {
	logs := fmt.Sprintf("banner\n%s\n%s\n%s\n",
		`{"level":"INFO","msg":"ready"}`,
		`{"level":"ERROR","msg":"NATS error","component":"natsclient",`+
			`"subject":"unrelated.subject"}`,
		`{"level":"ERROR","msg":"NATS error","component":"natsclient",`+
			`"error":"nats: slow consumer, messages dropped","subject":"`+
			e2eslowconsumer.Subject+`","queue":"`+e2eslowconsumer.Queue+`","dropped":8}`)
	records, err := parseSlowConsumerRecords(logs)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, e2eslowconsumer.Subject, records[0]["subject"])
}

func TestParseSlowConsumerRecordsRejectsMalformedJSONRecord(t *testing.T) {
	_, err := parseSlowConsumerRecords("{not-json}\n")
	require.Error(t, err)
}

func TestAssertSlowConsumerObservationReportsActualPartialCount(t *testing.T) {
	result := &Result{}
	record := map[string]any{
		"level": "ERROR", "msg": "NATS error", "component": "wrong",
		"error": "nats: slow consumer, messages dropped", "subject": e2eslowconsumer.Subject,
		"queue": e2eslowconsumer.Queue, "dropped": float64(e2eslowconsumer.ExpectedDropped),
	}
	err := assertSlowConsumerObservation(result, []map[string]any{record}, 1)
	require.Error(t, err)
	assert.Equal(t, 4, result.AssertionsRun)
}

func TestAssertSlowConsumerObservationReportsStableSuccessCount(t *testing.T) {
	result := &Result{}
	record := map[string]any{
		"level": "ERROR", "msg": "NATS error", "component": "natsclient",
		"error": "nats: slow consumer, messages dropped", "subject": e2eslowconsumer.Subject,
		"queue": e2eslowconsumer.Queue, "dropped": float64(e2eslowconsumer.ExpectedDropped),
	}
	require.NoError(t, assertSlowConsumerObservation(result, []map[string]any{record}, 1))
	assert.Equal(t, slowConsumerExpectedAssertions, result.AssertionsRun)
}

func TestParseNATSClientErrorCounter(t *testing.T) {
	metrics := `# TYPE semstreams_log_entries_total counter
semstreams_log_entries_total{component="natsclient",level="error"} 1
semstreams_log_entries_total{component="other",level="error"} 7
`
	value, err := parseNATSClientErrorCounter(metrics)
	require.NoError(t, err)
	assert.Equal(t, float64(1), value)
}
