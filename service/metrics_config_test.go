package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMetricsRejectsRetiredInnerEnabled(t *testing.T) {
	_, err := NewMetrics(json.RawMessage(`{"enabled":true}`), &Dependencies{})
	if err == nil || !strings.Contains(err.Error(), `unknown field "enabled"`) {
		t.Fatalf("retired enabled error = %v", err)
	}
}
