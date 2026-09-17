package agenticloop

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spec: agentic-loop / Loop-state authority has one port declaration
func TestLoopAuthorityRejectsRetiredJSONKey(t *testing.T) {
	for _, value := range []string{`null`, `""`, `"AGENT_LOOPS"`, `"CUSTOM_LOOPS"`, `123`} {
		t.Run(value, func(t *testing.T) {
			raw := []byte(fmt.Sprintf(`{"loops_bucket":%s}`, value))
			_, err := DeclarePorts(raw, "loop")
			require.ErrorContains(t, err, "loops_bucket")
			require.ErrorContains(t, err, "ports.outputs")
			_, err = NewComponent(raw, component.Dependencies{})
			require.ErrorContains(t, err, "loops_bucket")
			require.ErrorContains(t, err, "config.bucket")
		})
	}
	for _, key := range []string{"LOOPS_BUCKET", "Loops_Bucket"} {
		t.Run(key, func(t *testing.T) {
			raw := []byte(fmt.Sprintf(`{"%s":"AGENT_LOOPS"}`, key))
			_, declarationErr := DeclarePorts(raw, "loop")
			_, constructorErr := NewComponent(raw, component.Dependencies{})
			assert.ErrorContains(t, declarationErr, "loops_bucket", "DeclarePorts must reject the retired spelling")
			assert.ErrorContains(t, constructorErr, "config.bucket", "NewComponent must reject the retired spelling")
		})
	}
	t.Run("matching canonical port does not revive retired key", func(t *testing.T) {
		raw := []byte(`{"loops_bucket":"CUSTOM_LOOPS","ports":{"outputs":[{"name":"loops","config":{"kind":"kv-write","bucket":"CUSTOM_LOOPS"}}]}}`)
		_, err := DeclarePorts(raw, "loop")
		require.ErrorContains(t, err, "loops_bucket")
		_, err = NewComponent(raw, component.Dependencies{})
		require.ErrorContains(t, err, "config.bucket")
	})
}

// spec: agentic-loop / Approval lifetime is bounded by loop-state authority
func FuzzLoopAuthorityApprovalDuration(f *testing.F) {
	const capNanos = int64(12 * time.Hour)
	// The grammar emits signed integer nanosecond durations. Seeds reach both
	// sides of each inclusive boundary; acceptance is the spec's numeric range.
	for _, nanos := range []int64{-1, 0, 1, capNanos - 1, capNanos, capNanos + 1, 1<<63 - 1, -1 << 63} {
		f.Add(nanos)
	}
	f.Fuzz(func(t *testing.T, nanos int64) {
		raw := []byte(fmt.Sprintf(`{"approval_timeout":"%dns"}`, nanos))
		_, declarationErr := DeclarePorts(raw, "loop")
		d, err := NewComponent(raw, component.Dependencies{})
		if nanos <= 0 || nanos > capNanos {
			require.ErrorContains(t, declarationErr, "approval_timeout")
			require.ErrorContains(t, err, "approval_timeout")
			return
		}
		require.NoError(t, declarationErr)
		require.NoError(t, err)
		require.Equal(t, time.Duration(nanos), d.(*Component).config.ApprovalTimeout())
	})
}

// spec: agentic-loop / Approval lifetime is bounded by loop-state authority
func TestLoopAuthorityApprovalTimeoutJSON(t *testing.T) {
	for _, row := range []struct {
		name, raw string
		want      time.Duration
	}{
		{"omitted", `{}`, 12 * time.Hour},
		{"minimum", `{"approval_timeout":"1ns"}`, time.Nanosecond},
		{"short", `{"approval_timeout":"5m"}`, 5 * time.Minute},
		{"below cap", `{"approval_timeout":"11h59m59.999999999s"}`, 12*time.Hour - time.Nanosecond},
		{"at cap", `{"approval_timeout":"12h"}`, 12 * time.Hour},
		{"above cap", `{"approval_timeout":"12h0m0.000000001s"}`, 0},
		{"empty", `{"approval_timeout":""}`, 0},
		{"null", `{"approval_timeout":null}`, 0},
		{"uppercase null", `{"APPROVAL_TIMEOUT":null}`, 0},
		{"mixed case null", `{"Approval_Timeout":null}`, 0},
		{"folded number", `{"APPROVAL_TIMEOUT":1}`, 0},
		{"folded boolean", `{"Approval_Timeout":false}`, 0},
		{"folded string", `{"APPROVAL_TIMEOUT":"5m"}`, 5 * time.Minute},
		{"number", `{"approval_timeout":1}`, 0},
		{"boolean", `{"approval_timeout":false}`, 0},
		{"malformed", `{"approval_timeout":"forever"}`, 0},
		{"negative", `{"approval_timeout":"-1ns"}`, 0},
		{"zero", `{"approval_timeout":"0s"}`, 0},
	} {
		t.Run(row.name, func(t *testing.T) {
			_, declarationErr := DeclarePorts([]byte(row.raw), "loop")
			d, err := NewComponent([]byte(row.raw), component.Dependencies{})
			if row.want == 0 {
				assert.ErrorContains(t, declarationErr, "approval_timeout", "DeclarePorts must reject explicit invalid input")
				assert.ErrorContains(t, err, "approval_timeout", "NewComponent must reject explicit invalid input")
				return
			}
			require.NoError(t, declarationErr)
			require.NoError(t, err)
			require.Equal(t, row.want, d.(*Component).config.ApprovalTimeout())
		})
	}
}

// spec: agentic-loop / Loop-state authority is acquired and observed before loop work
func TestLoopAuthorityStartRefusalPrecedesDependents(t *testing.T) {
	d, err := NewComponent([]byte(`{}`), component.Dependencies{NATSClient: &natsclient.Client{}})
	require.NoError(t, err)
	c := d.(*Component)
	cause := errors.New("loop authority refused")
	var operation context.Context
	c.initializeKVBucketsInput = func(ctx context.Context) error { operation = ctx; return cause }
	c.waitForStreamInput = func(context.Context, string) error { t.Error("stream allocation preceded authority"); return nil }
	require.ErrorIs(t, c.Start(t.Context()), cause)
	require.NotNil(t, operation)
	require.ErrorIs(t, operation.Err(), context.Canceled)
	require.Nil(t, c.loopsBucket)
	require.Nil(t, c.trajectorySub)
	require.Nil(t, c.inflightSub)
	require.Empty(t, c.consumers)
	require.Nil(t, c.sweeperDone)
	require.False(t, c.Health().Healthy)
	require.NoError(t, c.Stop(t.Context()))
}
