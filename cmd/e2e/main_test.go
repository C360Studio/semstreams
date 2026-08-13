package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

type assertionReportingScenario struct {
	result *scenarios.Result
	err    error
}

func (s assertionReportingScenario) Name() string                { return "assertion-reporting" }
func (s assertionReportingScenario) Description() string         { return "test" }
func (s assertionReportingScenario) Setup(context.Context) error { return nil }
func (s assertionReportingScenario) Execute(context.Context) (*scenarios.Result, error) {
	return s.result, s.err
}
func (s assertionReportingScenario) Teardown(context.Context) error { return nil }

func TestRunScenarioReportsAssertionsOnSuccessAndPartialFailure(t *testing.T) {
	for _, tc := range []struct {
		name       string
		result     *scenarios.Result
		err        error
		wantExit   int
		wantOutput string
	}{
		{name: "success", result: &scenarios.Result{Success: true, AssertionsRun: 11}, wantOutput: "assertions_run=11"},
		{name: "partial failure", result: &scenarios.Result{AssertionsRun: 4}, err: errors.New("failed"),
			wantExit: 1, wantOutput: "assertions_run=4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&output, nil))
			exit := runScenario(t.Context(), logger,
				assertionReportingScenario{result: tc.result, err: tc.err}, &cliFlags{})
			assert.Equal(t, tc.wantExit, exit)
			assert.Contains(t, output.String(), tc.wantOutput)
		})
	}
}
